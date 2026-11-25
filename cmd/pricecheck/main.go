// Command pricecheck checks the stored orders' money against
// docs/PRICING_CHECK.md §2: the sums on each order, and a replay that prices
// it again from what it holds. It only reads, so it can run on the live
// database at any time.
//
//	DATABASE_URL=postgres://... pricecheck            # orders of the last 30 days
//	DATABASE_URL=postgres://... pricecheck -days 0    # all orders
//
// It exits with 1 when an order breaks a rule, and lists them.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/db"
)

func main() {
	days := flag.Int("days", 30, "check the orders of the last this many days (0 = all)")
	flag.Parse()
	cfg := config.Load()
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()
	since := time.Time{}
	if *days > 0 {
		since = time.Now().AddDate(0, 0, -*days)
	}
	n, broken, err := app.New(pool, "").CheckAllOrderMoney(ctx, since)
	if err != nil {
		log.Fatalf("check: %v", err)
	}
	ids := make([]int64, 0, len(broken))
	for id := range broken {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		fmt.Printf("order %d\n", id)
		for _, b := range broken[id] {
			fmt.Printf("  %s\n", b)
		}
	}
	fmt.Printf("%d orders checked, %d broken\n", n, len(broken))
	if len(broken) > 0 {
		os.Exit(1)
	}
}
