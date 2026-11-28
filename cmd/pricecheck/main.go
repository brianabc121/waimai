// Command pricecheck checks the stored orders' money against
// docs/PRICING_CHECK.md §2: the sums on each order, and a replay that prices
// it again from what it holds. It only reads, so it can run on the live
// database at any time.
//
//	DATABASE_URL=postgres://... pricecheck            # orders of the last 30 days
//	DATABASE_URL=postgres://... pricecheck -days 0    # all orders
//	DATABASE_URL=postgres://... pricecheck -refund-audit -days 0
//
// It exits with 1 when an order breaks a rule, and lists them. -refund-audit
// instead lists the refunded orders worth a look (docs/CODE_REVIEW_FIX_PLAN.md
// H01, H07): refunded more than they took in, or refunded in several goes
// while an offer was on them. It changes nothing and always exits with 0.
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
	audit := flag.Bool("refund-audit", false, "list refunded orders to look at instead of checking (read-only)")
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
	if *audit {
		rows, err := app.New(pool, "").RefundAudit(ctx, since)
		if err != nil {
			log.Fatalf("refund audit: %v", err)
		}
		amt := func(v int64) string { return fmt.Sprintf("%d.%02d", v/100, v%100) }
		for _, r := range rows {
			why := "分次退款（带活动或券，没退完）"
			if r.Why == "unpaid" {
				why = "退款多于实收"
			}
			diff := r.Refunded - r.Should
			word := "多退"
			if diff < 0 {
				word, diff = "少退（或当时店员改小了）", -diff
			}
			fmt.Printf("订单 %d（门店 %d，单号 %s）%s：应付 %s %s，退了 %d 次共 %s，按新规则应为 %s，%s %s\n",
				r.OrderID, r.BranchID, r.PublicNo, why, r.Currency, amt(r.Total), r.Refunds, amt(r.Refunded), amt(r.Should), word, amt(diff))
		}
		fmt.Printf("共 %d 张单需要看一下；这个命令只读，没有改任何数据\n", len(rows))
		return
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
	// rule R: each store with several brands, the last 31 days read brand by brand
	stores, bad, err := app.New(pool, "").CheckAllBrandViews(ctx, 31)
	if err != nil {
		log.Fatalf("check brands: %v", err)
	}
	for id, lines := range bad {
		fmt.Printf("store %d\n", id)
		for _, l := range lines {
			fmt.Printf("  %s\n", l)
		}
	}
	fmt.Printf("%d stores with several brands checked, %d broken\n", stores, len(bad))
	// W1–W20: the balance books as a whole, every store and the platform (docs/BALANCE_CHECK.md)
	books, err := app.New(pool, "").CheckWallet(ctx)
	if err != nil {
		log.Fatalf("check store balances: %v", err)
	}
	for _, l := range books {
		fmt.Printf("  %s\n", l)
	}
	fmt.Printf("balance books: %d problems\n", len(books))
	if len(broken) > 0 || len(bad) > 0 || len(books) > 0 {
		os.Exit(1)
	}
}
