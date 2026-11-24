package httpapi

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/xlsx"

	"github.com/brianabc121/waimai/internal/app"
)

// Bills: the store's orders with their discounts, sums per kind of discount,
// and the platform's monthly coupon statements (docs/COUPON_DESIGN.md §8, §9).

func (s *Server) billRoutes(auth, admin *gin.RouterGroup) {
	auth.GET("/merchant/branches/:bid/bills", s.bills)
	auth.GET("/merchant/branches/:bid/bills/summary", s.billSummary)
	auth.GET("/merchant/branches/:bid/settlements/:id/orders", s.settlementOrders)
	admin.GET("/bills", s.adminBills)
	admin.GET("/settlements", s.adminSettlements)
	admin.GET("/settlements/:id/orders", s.adminSettlementOrders)
	admin.POST("/settlements/:id/paid", s.adminSettlementPaid)
}

func (s *Server) bills(c *gin.Context) {
	bid, ok := branchScope(c, pBills)
	if !ok {
		return
	}
	from, to := c.Query("from"), c.Query("to")
	l, err := s.App.Bills(c, bid, from, to, c.Query("filter"))
	if err != nil || !wantsXLSX(c) {
		reply(c, l, err)
		return
	}
	sum, err := s.App.BillSummary(c, bid, from, to)
	if err != nil {
		fail(c, err)
		return
	}
	b, err := s.App.BranchByID(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	w := words(c)
	loc := zone(b.Timezone)
	m := func(v int64) float64 { return float64(v) / 100 }
	kind := map[string]string{"delivery": w("外卖", "delivery"), "pickup": w("自取", "pickup")}
	state := map[string]string{"completed": w("已完成", "completed"), "cancelled": w("已取消", "cancelled"),
		"failed": w("配送异常", "not delivered"), "open": w("进行中", "in progress")}
	kindName := map[string]string{"promotion": w("门店活动", "Store promotion"), app.CouponStoreFree: w("商家无门槛券", "Store coupon"),
		app.CouponStoreThreshold: w("商家满减券", "Spend-and-save coupon"), app.CouponStoreDelivery: w("满减配送费券", "Delivery fee coupon"),
		app.CouponPlatformFree: w("平台无门槛券", "Platform coupon")}
	payer := map[string]string{"store": w("商家", "store"), "platform": w("平台", "platform")}
	head := []any{w("订单号", "Order"), w("下单时间", "Placed"), w("方式", "Type"), w("状态", "Status"), w("菜品原价", "Food"),
		w("配送费", "Delivery fee"), w("门店活动", "Promotion"), w("商家无门槛券", "Store coupon"), w("商家满减券", "Spend-and-save"),
		w("满减配送费券", "Delivery coupon"), w("平台券", "Platform coupon"), w("顾客实付", "Customer paid"),
		w("商家应收", "Store takings"), w("用了哪些券", "Coupons used")}
	rows := [][]any{head}
	for _, o := range l.Orders {
		used := ""
		for _, d := range o.Discounts {
			n := w(d.Name, d.NameEn)
			if n == "" {
				n = d.Name
			}
			if n == "" {
				n = kindName[d.Kind]
			}
			if used != "" {
				used += "; "
			}
			used += fmt.Sprintf("%s %s %.2f", kindName[d.Kind], n, m(d.Amount))
		}
		rows = append(rows, []any{o.PublicNo, stamp(&o.CreatedAt, loc), kind[o.ServiceType], state[o.State], m(o.Food), m(o.DeliveryFee),
			m(o.Promotion), m(o.StoreFree), m(o.StoreThreshold), m(o.StoreDelivery), m(o.PlatformFree), m(o.Paid), m(o.Takings), used})
	}
	t := l.Sum
	rows = append(rows, []any{w("合计（已完成）", "Total (completed)"), nil, nil, nil, m(t.Food), m(t.DeliveryFee), m(t.Promotion),
		m(t.StoreFree), m(t.StoreThreshold), m(t.StoreDelivery), m(t.PlatformFree), m(t.Paid), m(t.Takings), nil})
	sums := [][]any{
		{w("期间", "Period"), sum.From + " ~ " + sum.To},
		{w("货币", "Currency"), sum.Currency},
		{w("完成订单", "Completed orders"), sum.Orders},
		{w("取消订单", "Cancelled orders"), sum.Cancelled},
		{w("菜品原价合计", "Food at full price"), m(sum.Food)},
		{w("商家承担的优惠", "Discounts paid by the store"), m(sum.MerchantDiscount)},
		{w("平台承担的优惠", "Discounts paid by the platform"), m(sum.PlatformDiscount)},
		{w("顾客实付合计", "Customers paid"), m(sum.Paid)},
		{w("商家实际收入", "Store takings"), m(sum.Takings)},
		{},
		{w("优惠种类", "Discount"), w("谁承担", "Paid by"), w("用了多少单", "Orders"), w("合计减了", "Amount"), w("平均每单", "Per order")},
	}
	for _, k := range sum.Kinds {
		var avg any
		if k.Orders > 0 {
			avg = m(k.Amount) / float64(k.Orders)
		}
		sums = append(sums, []any{kindName[k.Kind], payer[k.FundedBy], k.Orders, m(k.Amount), avg})
	}
	sendXLSX(c, fmt.Sprintf("bills_%s_%s", from, to),
		xlsx.Sheet{Name: w("订单明细", "Orders"), Rows: rows}, xlsx.Sheet{Name: w("汇总", "Summary"), Rows: sums})
}

func (s *Server) billSummary(c *gin.Context) {
	bid, ok := branchScope(c, pBills)
	if !ok {
		return
	}
	r, err := s.App.BillSummary(c, bid, c.Query("from"), c.Query("to"))
	reply(c, r, err)
}

func (s *Server) settlementOrders(c *gin.Context) {
	bid, ok := branchScope(c, pBills)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	l, err := s.App.SettlementOrders(c, bid, id)
	reply(c, gin.H{"orders": l}, err)
}

func (s *Server) adminBills(c *gin.Context) {
	l, err := s.App.StoreBills(c, c.Query("from"), c.Query("to"))
	reply(c, gin.H{"stores": l}, err)
}

func (s *Server) adminSettlements(c *gin.Context) {
	l, err := s.App.ListSettlements(c, c.Query("status"))
	reply(c, gin.H{"settlements": l}, err)
}

func (s *Server) adminSettlementOrders(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	l, err := s.App.SettlementOrders(c, 0, id)
	reply(c, gin.H{"orders": l}, err)
}

func (s *Server) adminSettlementPaid(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in struct {
		PaidOn      string `json:"paid_on"`
		TransferRef string `json:"transfer_ref"`
	}
	if !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.MarkSettlementPaid(c, adminActor(c), id, in.PaidOn, in.TransferRef))
}
