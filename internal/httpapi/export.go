package httpapi

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/xlsx"
)

// The lists under "Staff" can be downloaded as Excel with ?format=xlsx. The
// headers are in Chinese for ?lang=zh / zh-Hant, English otherwise.

func wantsXLSX(c *gin.Context) bool { return c.Query("format") == "xlsx" }

// words picks the language of the headers.
func words(c *gin.Context) func(zh, en string) string {
	l := c.Query("lang")
	en := l != "" && l != "zh" && l != "zh-Hant"
	return func(zh, e string) string {
		if en {
			return e
		}
		return zh
	}
}

func sendXLSX(c *gin.Context, name string, sheets ...xlsx.Sheet) {
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, sheets...); err != nil {
		fail(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.xlsx"`, name))
	c.Data(200, xlsx.ContentType, buf.Bytes())
}

func zone(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

func stamp(t *time.Time, loc *time.Location) any {
	if t == nil {
		return nil
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

func hours(minutes int) float64 { return math.Round(float64(minutes)/60*100) / 100 }
func kms(m int) float64         { return math.Round(float64(m)/10) / 100 }

func intOrNil(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func kmOrNil(p *int) any {
	if p == nil {
		return nil
	}
	return kms(*p)
}

func attendanceXLSX(c *gin.Context, a *app.Attendance) {
	w, loc := words(c), zone(a.Timezone)
	names := map[int64]app.AttendanceStaff{}
	for _, p := range a.Staff {
		names[p.ID] = p
	}
	source := map[string]string{"manual": w("自己打卡", "clocked on"), "auto": w("自动上班", "automatic"), "manager": w("店长补的", "added by a manager")}
	ended := map[string]string{"": w("还在上班", "still at work"), "manual": w("自己下班", "clocked off"), "logout": w("退出登录", "signed out"),
		"auto": w("系统代下班", "ended by the system"), "manager": w("店长结束", "ended by a manager")}
	rows := [][]any{{w("日期", "Day"), w("员工", "Staff"), w("账号", "Username"), w("上班", "On"), w("下班", "Off"), w("工时（小时）", "Hours"),
		w("上班方式", "Started"), w("下班方式", "Ended"), w("上班时离店（米）", "Metres from the store (on)"), w("下班时离店（米）", "Metres from the store (off)"),
		w("已作废", "Voided"), w("店长改过", "Corrected"), w("修改人", "Corrected by"), w("修改原因", "Reason"),
		w("原上班时间", "On as first recorded"), w("原下班时间", "Off as first recorded")}}
	yes := func(b bool) any {
		if b {
			return w("是", "yes")
		}
		return nil
	}
	for _, s := range a.Shifts {
		p := names[s.UserID]
		var h any
		if !s.Voided {
			h = hours(s.Minutes)
		}
		rows = append(rows, []any{s.Day, p.DisplayName, p.Username, stamp(&s.StartedAt, loc), stamp(s.EndedAt, loc), h,
			source[s.StartSource], ended[s.EndReason], intOrNil(s.StartDistanceM), intOrNil(s.EndDistanceM),
			yes(s.Voided), yes(s.Edited), s.EditedBy, s.EditReason, stamp(s.OrigStartedAt, loc), stamp(s.OrigEndedAt, loc)})
	}
	// per person: days worked and hours
	type sum struct{ days, minutes int }
	total := map[int64]*sum{}
	day := map[string]bool{}
	for _, s := range a.Shifts {
		if s.Voided {
			continue
		}
		if total[s.UserID] == nil {
			total[s.UserID] = &sum{}
		}
		if k := fmt.Sprintf("%d %s", s.UserID, s.Day); !day[k] {
			day[k] = true
			total[s.UserID].days++
		}
		total[s.UserID].minutes += s.Minutes
	}
	sums := [][]any{{w("员工", "Staff"), w("账号", "Username"), w("出勤天数", "Days worked"), w("工时（小时）", "Hours")}}
	for _, p := range a.Staff {
		if t := total[p.ID]; t != nil {
			sums = append(sums, []any{p.DisplayName, p.Username, t.days, hours(t.minutes)})
		} else if p.Active && !p.Gone {
			sums = append(sums, []any{p.DisplayName, p.Username, 0, 0})
		}
	}
	sendXLSX(c, fmt.Sprintf("attendance_%s_%s", a.From, a.To),
		xlsx.Sheet{Name: w("合计", "Totals"), Rows: sums}, xlsx.Sheet{Name: w("每一班", "Shifts"), Rows: rows})
}

func kitchenXLSX(c *gin.Context, r *app.KitchenReport, dishes []app.KitchenDish) {
	w, loc := words(c), zone(r.Timezone)
	board := w("后厨大屏", "Kitchen screen")
	sums := [][]any{{w("员工", "Staff"), w("账号", "Username"), w("份数", "Portions"), w("道数", "Lines"), w("订单数", "Orders"),
		w("打包单数", "Orders packed"), w("在岗（小时）", "Hours at work")}}
	for _, p := range r.People {
		name := p.DisplayName
		if p.ID == 0 {
			name = board
		}
		sums = append(sums, []any{name, p.Username, p.Qty, p.Dishes, p.Orders, p.Packed, hours(p.ShiftMinutes)})
	}
	via := map[string]string{"work": w("手机工作台", "own workbench"), "board": board, "": w("后台或来源不明", "back office or unknown")}
	kind := map[string]string{"delivery": w("外卖", "delivery"), "pickup": w("自取", "pickup"), "dine_in": w("堂食", "dine-in")}
	rows := [][]any{{w("做好时间", "Ready at"), w("算谁的", "Counts for"), w("点的账号", "Marked by"), w("商品", "Item"), w("规格", "Options"), w("备注", "Note"),
		w("份数", "Qty"), w("工作台", "Station"), w("订单", "Order"), w("类型", "Type"), w("桌号", "Table"), w("开始做的人", "Started by"),
		w("用时（秒）", "Seconds"), w("超时", "Slow"), w("在哪点的", "Marked on"), w("撤回次数", "Taken back"), w("订单后来取消", "Order cancelled later")}}
	yes := func(b bool) any {
		if b {
			return w("是", "yes")
		}
		return nil
	}
	for _, d := range dishes {
		who := d.ReadyBy
		if d.Via == "board" {
			who = board
		}
		opts := make([]string, len(d.Options))
		for i, o := range d.Options {
			opts[i] = w(o.Name, o.NameEn)
			if opts[i] == "" {
				opts[i] = o.Name
			}
		}
		name := w(d.Name, d.NameEn)
		if name == "" {
			name = d.Name
		}
		rows = append(rows, []any{stamp(&d.ReadyAt, loc), who, d.ReadyBy, name, strings.Join(opts, ", "), d.Note, d.Qty, d.StationName,
			d.PublicNo, kind[d.ServiceType], d.TableName, d.StartedBy, intOrNil(d.Seconds), yes(d.Slow), via[d.Via], d.UndoCount, yes(d.Cancelled)})
	}
	sendXLSX(c, fmt.Sprintf("kitchen_%s_%s", r.From, r.To),
		xlsx.Sheet{Name: w("汇总", "Summary"), Rows: sums}, xlsx.Sheet{Name: w("明细", "Dishes"), Rows: rows})
}

func ridersXLSX(c *gin.Context, r *app.RiderReport, trips []app.Trip) {
	w, loc := words(c), zone(r.Timezone)
	names := map[int64]app.RiderLine{}
	sums := [][]any{{w("骑手", "Rider"), w("账号", "Username"), w("在岗（小时）", "Hours at work"), w("出车趟数", "Trips"), w("带的单数", "Orders"),
		w("送达", "Delivered"), w("异常", "Failed"), w("实际里程（公里）", "Ridden (km)"), w("没有位置记录的趟数", "Trips without positions"),
		w("轨迹中断的趟数", "Trips with a gap"), w("可对比的实际里程（公里）", "Ridden, comparable trips (km)"), w("推荐里程（公里）", "Planned (km)"),
		w("偏多的趟数", "Detours"), w("代收现金", "Cash collected") + " (" + r.Currency + ")"}}
	for _, l := range r.Riders {
		names[l.ID] = l
		sums = append(sums, []any{l.DisplayName, l.Username, hours(l.ShiftMinutes), l.Trips, l.Orders, l.Delivered, l.Failed, kms(l.ActualM),
			l.NoTrack, l.Gaps, kms(l.ComparedActualM), kms(l.CompareM), l.Detours, float64(l.Cash) / 100})
	}
	state := map[string]string{"returned": w("回到店里", "back at the store"), "next_trip": w("取了下一单", "collected the next order"),
		"off_duty": w("没回店就下班", "clocked off on the road"), "timeout": w("没回店", "did not come back"),
		"no_store": w("门店没有位置", "store has no position"), "": w("在路上", "on the road")}
	source := map[string]string{"straight": w("直线估算", "straight-line estimate"), "google": "Google"}
	rows := [][]any{{w("日期", "Day"), w("骑手", "Rider"), w("取餐", "Collected"), w("出发", "Left"), w("最后一单", "Last drop"), w("回店", "Back"),
		w("结束", "Ended"), w("怎么结束的", "How it ended"), w("带的单数", "Orders"), w("送达", "Delivered"), w("异常", "Failed"),
		w("订单（按送达顺序）", "Orders as dropped"), w("实际里程（公里）", "Ridden (km)"), w("推荐里程（公里）", "Planned (km)"),
		w("多走（公里）", "Extra (km)"), w("偏多", "Detour"), w("推荐来源", "Planned by"), w("轨迹中断（秒）", "Gap (s)"), w("位置点数", "Positions")}}
	for _, t := range trips {
		nos := make([]string, len(t.Items))
		for i, o := range t.Items {
			nos[i] = o.PublicNo
			switch o.Outcome {
			case "failed":
				nos[i] += w("（异常）", " (failed)")
			case "removed":
				nos[i] += w("（取消）", " (cancelled)")
			}
		}
		var detour any
		if t.Detour {
			detour = w("是", "yes")
		}
		rows = append(rows, []any{t.Day, names[t.RiderID].DisplayName, stamp(&t.StartedAt, loc), stamp(t.LeftStoreAt, loc), stamp(t.LastDropAt, loc),
			stamp(t.ReturnedAt, loc), stamp(t.EndedAt, loc), state[t.EndReason], t.Orders, t.Delivered, t.Failed, strings.Join(nos, ", "),
			kmOrNil(t.ActualM), kmOrNil(t.CompareM), kmOrNil(t.OverM), detour, source[t.PlannedSource], t.GapSeconds, t.TrackPoints})
	}
	sendXLSX(c, fmt.Sprintf("riders_%s_%s", r.From, r.To),
		xlsx.Sheet{Name: w("汇总", "Summary"), Rows: sums}, xlsx.Sheet{Name: w("每一趟", "Trips"), Rows: rows})
}
