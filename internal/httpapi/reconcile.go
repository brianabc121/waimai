package httpapi

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/xlsx"
)

// The store's reconciliation page (docs/CHECKOUT_DISCOUNT_DESIGN.md §8).

var pReconcile = need(app.PermReconcile)

func (s *Server) reconcileRoutes(auth *gin.RouterGroup) {
	auth.GET("/merchant/branches/:bid/reconcile", s.withBranch(pReconcile, s.reconcileSummary))
	auth.GET("/merchant/branches/:bid/reconcile/online", s.withBranch(pReconcile, s.reconcileOnline))
	auth.GET("/merchant/branches/:bid/reconcile/discounts", s.withBranch(pReconcile, s.reconcileDiscounts))
	auth.PUT("/merchant/branches/:bid/reconcile/checks", s.withBranch(pReconcile, s.reconcileChecks))
	auth.PUT("/merchant/branches/:bid/reconcile/cash-count", s.withBranch(pReconcile, s.cashCount))
	// delivery platforms (docs/ORDER_TYPES_DESIGN.md §9.5)
	auth.GET("/merchant/branches/:bid/reconcile/platform-orders", s.withBranch(pReconcile, s.reconcilePlatformOrders))
	auth.PUT("/merchant/branches/:bid/reconcile/platform-payouts", s.withBranch(pReconcile, s.savePlatformPayout))
	auth.DELETE("/merchant/branches/:bid/reconcile/platform-payouts/:pid", s.withBranch(pReconcile, s.deletePlatformPayout))
}

func (s *Server) reconcilePlatformOrders(c *gin.Context, bid int64) {
	pid, _ := strconv.ParseInt(c.Query("platform"), 10, 64)
	l, err := s.App.PlatformReconOrders(c, bid, c.Query("from"), c.Query("to"), pid)
	reply(c, gin.H{"orders": l}, err)
}

func (s *Server) savePlatformPayout(c *gin.Context, bid int64) {
	var in app.PlatformPayoutInput
	if !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.SavePlatformPayout(c, bid, in, staffActor(c)))
}

func (s *Server) deletePlatformPayout(c *gin.Context, bid int64) {
	if id, ok := idParam(c, "pid"); ok {
		reply(c, gin.H{"ok": true}, s.App.DeletePlatformPayout(c, bid, id, staffActor(c)))
	}
}

func (s *Server) reconcileSummary(c *gin.Context, bid int64) {
	from, to := c.Query("from"), c.Query("to")
	r, err := s.App.Reconciliation(c, bid, from, to)
	if err != nil || !wantsXLSX(c) {
		reply(c, r, err)
		return
	}
	online, err := s.App.OnlinePayments(c, bid, from, to, 0, "")
	if err != nil {
		fail(c, err)
		return
	}
	disc, err := s.App.Discounts(c, bid, from, to)
	if err != nil {
		fail(c, err)
		return
	}
	w := words(c)
	loc := zone(r.Timezone)
	m := func(v int64) float64 { return float64(v) / 100 }
	typ := map[string]string{"cash": w("现金", "Cash"), "card": w("刷卡", "Card"), "online": w("在线支付", "Online"),
		"platform": w("外卖平台", "Delivery platform")}
	svc := map[string]string{"dine_in": w("堂食", "Dine-in"), "delivery": w("外卖", "Delivery"), "pickup": w("自取", "Pickup")}
	check := map[string]string{"": w("未核对", "Not checked"), "ok": w("已到账", "Arrived"), "problem": w("有问题", "Problem")}
	sum := [][]any{{w("付款方式", "Method"), w("笔数", "Orders"), w("金额", "Amount"), w("堂食", "Dine-in"), w("外卖和自取", "Delivery and pickup"),
		w("已到账", "Arrived"), w("有问题", "Problem"), w("未核对", "Not checked")}}
	for _, row := range r.Rows {
		name := typ[row.PayType]
		if row.PayType == "online" || row.PayType == "platform" {
			name += " · " + row.Name
		}
		var ok, bad, un any
		if row.PayType == "online" {
			ok, bad, un = row.OK, row.Problem, row.Unchecked
		}
		sum = append(sum, []any{name, row.Count, m(row.Amount), m(row.Dine), m(row.Takeaway), ok, bad, un})
	}
	sum = append(sum, []any{w("合计", "Total"), r.Count, m(r.Total)}, []any{},
		[]any{w("期间", "Period"), r.From + " ~ " + r.To}, []any{w("货币", "Currency"), r.Currency},
		[]any{w("店里收的现金", "Cash taken at the store"), m(r.Cash.Counter)},
		[]any{w("骑手收的现金", "Cash riders collected"), m(r.Cash.RiderCollected)},
		[]any{w("骑手已交回", "Handed in by riders"), m(r.Cash.RiderReturned)},
		[]any{w("骑手未交回", "Not yet handed in"), m(r.Cash.RiderPending)},
		[]any{w("钱箱应有现金", "Cash the drawer should hold"), m(r.Cash.Expected)},
		[]any{w("活动让利", "Promotions"), m(r.Discounts.Promotion)}, []any{w("商家券", "Store coupons"), m(r.Discounts.Coupon)},
		[]any{w("结账打折", "Percentage off at checkout"), m(r.Discounts.Staff)},
		[]any{w("抹零和改实收", "Round-offs and typed amounts"), m(r.Discounts.RoundOff)},
		[]any{w("平台活动（商家出资）", "Platform offers (store-funded)"), m(r.Discounts.PlatformMerchant)},
		[]any{w("让利合计", "Discounts in all"), m(r.Discounts.Total)},
		[]any{w("平台补贴（平台出资，不算让利）", "Platform-funded discounts (not the store's)"), m(r.Discounts.PlatformSubsidy)})
	otype := map[string]string{"dine_in": w("堂食", "Dine-in"), "staff": w("代客", "By staff"), "platform": w("平台", "Platform"),
		"mall": w("商城", "Mall"), "web": w("网店", "Own link")}
	dsrc := map[string]string{"staff": w("店员", "staff"), "self": w("扫码", "scan")}
	sum = append(sum, []any{}, []any{w("营业额按类型", "Takings by order type"), w("笔数", "Orders"), w("金额", "Amount")})
	for _, x := range r.Types {
		name := otype[x.Type]
		if x.Source != "" {
			name += " · " + dsrc[x.Source]
		}
		if x.Name != "" {
			name += " · " + x.Name
		}
		sum = append(sum, []any{name, x.Count, m(x.Amount)})
	}
	if k := r.CashCount; k != nil {
		sum = append(sum, []any{w("现金盘点", "Cash count"), m(k.Counted), w("应有", "Expected"), m(k.Expected), w("差额", "Difference"), m(k.Counted - k.Expected), k.Note, k.CountedBy})
	}
	on := [][]any{{w("付款时间", "Paid at"), w("订单号", "Order"), w("类型", "Type"), w("桌号", "Table"), w("方式", "Method"), w("金额", "Amount"),
		w("骑手或下单店员", "Rider or staff"), w("核对", "Check"), w("备注", "Note"), w("核对人", "Checked by")}}
	for _, p := range online {
		on = append(on, []any{stamp(&p.PaidAt, loc), p.PublicNo, svc[p.ServiceType], p.TableName, p.Method, m(p.Amount), p.Staff, check[p.Status], p.Note, p.CheckedBy})
	}
	mode := map[string]string{"": "", "round": w("抹零", "Round-off"), "set_total": w("改实收", "Amount set")}
	dl := [][]any{{w("时间", "Time"), w("订单号", "Order"), w("类型", "Type"), w("打折", "Percentage"), w("打折金额", "Percentage off"),
		w("抹零/改实收", "Round-off / set"), w("金额", "Amount"), w("实付", "Paid"), w("原因", "Reason"), w("谁设的", "By"), w("谁授权", "Approved by")}}
	for _, e := range disc.Entries {
		var pct any
		if e.PercentBP > 0 {
			pct = strconv.FormatFloat(float64(10000-e.PercentBP)/100, 'f', -1, 64) + "%"
		}
		dl = append(dl, []any{stamp(&e.At, loc), e.PublicNo, svc[e.ServiceType], pct, m(e.Staff), mode[e.Mode], m(e.RoundOff), m(e.Total), e.Reason, e.By, e.ApprovedBy})
	}
	dl = append(dl, []any{}, []any{w("员工", "Staff"), w("笔数", "Orders"), w("让利", "Amount")})
	for _, x := range disc.ByStaff {
		dl = append(dl, []any{x.Name, x.Count, m(x.Amount)})
	}
	dl = append(dl, []any{}, []any{w("活动", "Promotion"), w("堂食/外卖", "Dine-in / delivery"), w("笔数", "Orders"), w("让利", "Amount")})
	for _, x := range disc.ByPromo {
		name := w(x.Name, x.NameEn)
		if name == "" {
			name = x.Name
		}
		sc := w("外卖", "Delivery")
		if x.Scope == "dine_in" {
			sc = w("堂食", "Dine-in")
		}
		dl = append(dl, []any{name, sc, x.Count, m(x.Amount)})
	}
	porders, err := s.App.PlatformReconOrders(c, bid, from, to, 0)
	if err != nil {
		fail(c, err)
		return
	}
	head := []any{w("平台", "Platform"), w("单数", "Orders"), w("平台价营业额", "Dishes at platform prices"), w("商家出资", "Store-funded"),
		w("平台出资", "Platform-funded"), w("顾客实付", "Customers paid"), w("商家应收", "Store receives"), w("佣金", "Commission"),
		w("其他扣费", "Other charges"), w("平台扣款", "Taken back"), w("预计到账", "Expected payout"), w("实际到账", "Actual payout"), w("差额", "Difference")}
	pl := [][]any{head}
	pname := map[int64]string{}
	for _, p := range r.Platforms {
		pname[p.PlatformID] = p.Name
		var actual, diff any
		if p.Actual != nil {
			actual, diff = m(*p.Actual), m(*p.Diff)
		}
		pl = append(pl, []any{p.Name, p.Count, m(p.Subtotal), m(p.Merchant), m(p.Subsidy), m(p.Paid), m(p.Receivable), m(p.Commission),
			m(p.OtherFees), m(p.Deducted), m(p.Payout), actual, diff})
	}
	pl = append(pl, []any{}, []any{w("付款时间", "Paid at"), w("订单号", "Order"), w("平台", "Platform"), w("平台价营业额", "Dishes at platform prices"),
		w("商家出资", "Store-funded"), w("平台出资", "Platform-funded"), w("顾客实付", "Customers paid"), w("商家应收", "Store receives"),
		w("佣金", "Commission"), w("其他扣费", "Other charges"), w("平台扣款", "Taken back"), w("预计到账", "Expected payout")})
	for _, o := range porders {
		pl = append(pl, []any{stamp(&o.PaidAt, loc), o.PublicNo, pname[o.PlatformID], m(o.Subtotal), m(o.Merchant), m(o.Subsidy), m(o.Paid),
			m(o.Receivable), m(o.Commission), m(o.OtherFees), m(o.Deducted), m(o.Payout)})
	}
	sendXLSX(c, fmt.Sprintf("reconcile_%s_%s", from, to), xlsx.Sheet{Name: w("收款汇总", "Takings"), Rows: sum},
		xlsx.Sheet{Name: w("在线支付核对", "Online payments"), Rows: on}, xlsx.Sheet{Name: w("让利记录", "Discounts"), Rows: dl},
		xlsx.Sheet{Name: w("平台订单", "Platform orders"), Rows: pl})
}

func (s *Server) reconcileOnline(c *gin.Context, bid int64) {
	ch, _ := strconv.ParseInt(c.Query("channel"), 10, 64)
	l, err := s.App.OnlinePayments(c, bid, c.Query("from"), c.Query("to"), ch, c.Query("status"))
	reply(c, gin.H{"payments": l}, err)
}

func (s *Server) reconcileDiscounts(c *gin.Context, bid int64) {
	l, err := s.App.Discounts(c, bid, c.Query("from"), c.Query("to"))
	reply(c, l, err)
}

func (s *Server) reconcileChecks(c *gin.Context, bid int64) {
	var in struct {
		OrderIDs []int64 `json:"order_ids"`
		Status   string  `json:"status"`
		Note     string  `json:"note"`
	}
	if !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.SetPaymentChecks(c, bid, in.OrderIDs, in.Status, in.Note, staffActor(c)))
}

func (s *Server) cashCount(c *gin.Context, bid int64) {
	var in struct {
		Day     string `json:"day"`
		Counted int64  `json:"counted"`
		Note    string `json:"note"`
	}
	if !bind(c, &in) {
		return
	}
	k, err := s.App.SaveCashCount(c, bid, in.Day, in.Counted, in.Note, staffActor(c))
	reply(c, gin.H{"cash_count": k}, err)
}
