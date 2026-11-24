package app

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// ---- the 规格 cell ----
//
// One option group per line (or separated by ";"), the group name, a colon,
// then the options separated by commas. An extra price follows an option as
// "+5". A tag after the group name says how many may be picked:
//
//	辣度：不辣，小辣，大辣             pick exactly one (the default)
//	加料[多选]：加蛋+10，芝士+15       any number, or none
//	饮料[可选]：可乐+20，雪碧+20       at most one, or none
//	小菜[选1-3]：泡菜，海带，豆芽       one to three

const (
	maxSpecGroups  = 20
	maxSpecOptions = 50
)

var (
	specTagRe    = regexp.MustCompile(`^(.+?)\s*[\[【(（]\s*([^\]】)）]*?)\s*[\]】)）]$`)
	specRangeRe  = regexp.MustCompile(`^(?:选|pick)?\s*(\d+)\s*(?:-|~|～|—|到|至)\s*(\d+)\s*(?:项)?$`)
	specOptionRe = regexp.MustCompile(`^(.+?)\s*[(（]?\s*[+＋]\s*([0-9][0-9,]*(?:\.[0-9]+)?)\s*[)）]?$`)
)

func splitAny(s string, seps string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return strings.ContainsRune(seps, r) })
}

// parseSpecs reads a 规格 cell. The code is "" or a row error code.
func parseSpecs(s string) ([]OptionGroupInput, string) {
	out := []OptionGroupInput{}
	for _, part := range splitAny(s, "\n\r;；") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		i := strings.IndexAny(part, ":：")
		if i < 0 {
			return nil, "spec_invalid:" + clip(part, 20)
		}
		_, w := utf8.DecodeRuneInString(part[i:])
		head, body := strings.TrimSpace(part[:i]), part[i+w:]
		g := OptionGroupInput{Name: head, MinSelect: 1, MaxSelect: 1}
		tag := ""
		if m := specTagRe.FindStringSubmatch(head); m != nil {
			g.Name, tag = strings.TrimSpace(m[1]), strings.ToLower(strings.TrimSpace(m[2]))
		}
		for _, o := range splitAny(body, ",，、|") {
			o = strings.TrimSpace(o)
			if o == "" {
				continue
			}
			opt := OptionInput{Name: o}
			if m := specOptionRe.FindStringSubmatch(o); m != nil {
				p, ok := parsePrice(m[2])
				if !ok {
					return nil, "spec_invalid:" + clip(g.Name, 20)
				}
				opt.Name, opt.PriceDelta = strings.TrimSpace(m[1]), p
			} else if strings.ContainsAny(o, "+＋") {
				return nil, "spec_invalid:" + clip(g.Name, 20) // a price that is not a number
			}
			if opt.Name == "" || utf8.RuneCountInString(opt.Name) > 40 {
				return nil, "spec_invalid:" + clip(g.Name, 20)
			}
			g.Options = append(g.Options, opt)
		}
		if g.Name == "" || utf8.RuneCountInString(g.Name) > 40 || len(g.Options) == 0 || len(g.Options) > maxSpecOptions {
			return nil, "spec_invalid:" + clip(head, 20)
		}
		switch {
		case tag == "" || tag == "必选" || tag == "单选" || tag == "required":
		case tag == "可选" || tag == "optional":
			g.MinSelect = 0
		case tag == "多选" || tag == "multi" || tag == "any":
			g.MinSelect, g.MaxSelect = 0, len(g.Options)
		default:
			m := specRangeRe.FindStringSubmatch(tag)
			if m == nil {
				// "饮料（大杯）" is a name with brackets, not a tag
				if strings.HasSuffix(head, ")") || strings.HasSuffix(head, "）") {
					g.Name = head
					break
				}
				return nil, "spec_invalid:" + clip(g.Name, 20)
			}
			g.MinSelect, _ = strconv.Atoi(m[1])
			g.MaxSelect, _ = strconv.Atoi(m[2])
			if g.MaxSelect > len(g.Options) {
				g.MaxSelect = len(g.Options)
			}
			if g.MaxSelect < 1 || g.MinSelect > g.MaxSelect {
				return nil, "spec_invalid:" + clip(g.Name, 20)
			}
		}
		out = append(out, g)
	}
	if len(out) > maxSpecGroups {
		return nil, "spec_invalid:" + strconv.Itoa(maxSpecGroups)
	}
	return out, ""
}

func clip(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, ":", " "))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

// majorText shows hundredths as "169", "219.5" or "0.05".
func majorText(minor int64) string {
	return strconv.FormatFloat(float64(minor)/100, 'f', -1, 64)
}

// formatSpecs writes option groups back in the 规格 cell syntax.
func formatSpecs(groups []OptionGroup) string {
	lines := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.ToppingTemplateID != nil {
			continue // from a topping template: the template keeps it
		}
		tag := ""
		n := len(g.Options)
		switch {
		case g.MinSelect == 1 && g.MaxSelect == 1:
		case g.MinSelect == 0 && g.MaxSelect == 1:
			tag = "[可选]"
		case g.MinSelect == 0 && g.MaxSelect >= n:
			tag = "[多选]"
		default:
			tag = fmt.Sprintf("[选%d-%d]", g.MinSelect, g.MaxSelect)
		}
		opts := make([]string, 0, n)
		for _, o := range g.Options {
			s := o.Name
			if o.PriceDelta > 0 {
				s += "+" + majorText(o.PriceDelta)
			}
			opts = append(opts, s)
		}
		lines = append(lines, g.Name+tag+"："+strings.Join(opts, "，"))
	}
	return strings.Join(lines, "\n")
}

// ---- the template ----

type templateCol struct {
	head     string
	width    float64
	required bool
	yesNo    bool
	note     string
}

func templateCols(currency string) []templateCol {
	price := "价格"
	if currency != "" {
		price = "价格（" + currency + "）"
	}
	return []templateCol{
		{"分类", 12, true, false, "必填。没有的分类会自动新建。"},
		{"菜名", 18, true, false, "必填。同一分类里菜名相同的菜品会被更新，不会重复新建。"},
		{price, 11, true, false, "必填。填数字，例如 169 或 219.5，单位是店铺的货币。"},
		{"英文名", 18, false, false, "选填。"},
		{"描述", 24, false, false, "选填，最多 300 字。显示在菜名下面。"},
		{"规格", 30, false, false, "选填。一行一组：组名：选项1，选项2+加价。组名后可加 [可选] [多选] [选1-3]，不加就是必选一项。更新时填“无”清空规格。"},
		{"主图链接", 30, false, false, "选填。图片网址（https:// 开头）。更新时填“无”清空。"},
		{"更多图片链接", 30, false, false, "选填，最多 8 张。每行一个网址，或用逗号隔开。"},
		{"商品详情", 30, false, false, "选填，最多 2000 字。菜品详情页里的长介绍。"},
		{"上架", 8, false, true, "是 / 否。新菜品不填就是草稿（不上架），老菜品不填保持不变。"},
		{"堂食", 8, false, true, "是 / 否。不填：新菜品为“是”，老菜品不变。"},
		{"外卖", 8, false, true, "是 / 否。不填：新菜品为“是”，老菜品不变。"},
		{"自取", 8, false, true, "是 / 否。不填：新菜品为“是”，老菜品不变。"},
		{"需要制作", 10, false, true, "是 / 否。瓶装饮料这类不用后厨做的填“否”。"},
		{"工作台", 12, false, false, "选填。填后厨工作台的名称，要和系统里的一致。"},
		{"限量库存", 10, false, false, "选填。填数字限量；填“不限”取消限量。"},
		{"口味模板", 16, false, false, "选填。填店里口味模板的名字，多个用逗号隔开，最多 3 个，例如：奶茶类，通用。模板要先在菜单页“口味模板”里建好。更新时填“无”去掉。"},
	}
}

// The examples are new dishes, so a template uploaded as it is only adds
// three drafts and never changes a dish the store already has.
var templateExamples = [][]any{
	{"小吃", "椒盐杏鲍菇", 129, "Salt & Pepper Mushroom", "现炸，外酥里嫩", "辣度：不辣，小辣，大辣\n加料[多选]：九层塔+10，蒜片+10", "", "", "选用新鲜杏鲍菇，现点现炸。", "", "是", "是", "是", "是", "", "", ""},
	{"饮品", "柠檬气泡水", 45, "Lemon Soda", "", "", "", "", "", "", "是", "是", "是", "否", "", "", ""},
	{"主食", "招牌鸡腿饭", 219.5, "Chicken Leg Rice", "每日限量", "饮料[可选]：冰红茶+20，柠檬气泡水+30", "", "", "", "", "否", "是", "否", "是", "", 5, ""},
}

// MenuImportTemplate builds the blank template with three example rows.
func MenuImportTemplate() ([]byte, error) { return buildTemplate("", nil, nil, nil, nil) }

// MenuTemplate builds the template for one store: its currency in the price
// header, its categories and stations on the help sheet, and with withMenu
// every current dish filled in, so the file can be edited and imported back.
func (a *App) MenuTemplate(ctx context.Context, branchID int64, withMenu bool) ([]byte, error) {
	m, err := a.AdminMenu(ctx, branchID)
	if err != nil {
		return nil, err
	}
	stationName := map[int64]string{}
	stations := []string{}
	for _, s := range m.Stations {
		stationName[s.ID] = s.Name
		if s.Active {
			stations = append(stations, s.Name)
		}
	}
	tpls, err := loadNoteTemplates(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	tplName := map[int64]string{}
	notes := []string{}
	for _, t := range tpls {
		tplName[t.ID] = t.Name
		notes = append(notes, t.Name)
	}
	cats := []string{}
	var rows [][]any
	yn := func(b bool) string {
		if b {
			return "是"
		}
		return "否"
	}
	for _, c := range m.Categories {
		cats = append(cats, c.Name)
		if !withMenu {
			continue
		}
		for _, it := range c.Items {
			station, stock := "", any("")
			if it.StationID != nil {
				station = stationName[*it.StationID]
			}
			if it.StockQty != nil {
				stock = *it.StockQty
			}
			price, _ := strconv.ParseFloat(majorText(it.Price), 64)
			names := []string{}
			for _, id := range it.NoteTemplateIDs {
				names = append(names, tplName[id])
			}
			rows = append(rows, []any{c.Name, it.Name, price, it.NameEn, it.Description, formatSpecs(it.Groups),
				it.ImageURL, strings.Join(it.Photos, "\n"), it.Details, yn(it.Published), yn(it.DineInAvailable),
				yn(it.DeliveryAvailable), yn(it.PickupAvailable), yn(it.NeedsPrep), station, stock, strings.Join(names, "，")})
		}
	}
	if withMenu && rows == nil {
		rows = [][]any{}
	}
	return buildTemplate(m.Branch.Currency, rows, cats, stations, notes)
}

// buildTemplate writes the 菜品 sheet (rows nil: the examples) and the
// 填写说明 sheet.
func buildTemplate(currency string, rows [][]any, cats, stations, notes []string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	const sheet, help = "菜品", "填写说明"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	cols := templateCols(currency)
	if rows == nil {
		rows = templateExamples
	}
	head, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F3EAD8"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	need, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "9B1C1C"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F7DCD4"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	wrap, _ := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}})
	last := len(rows) + 1
	if last < 200 {
		last = 200
	}
	for i, c := range cols {
		name, _ := excelize.ColumnNumberToName(i + 1)
		h := c.head
		style := head
		if c.required {
			h += " *"
			style = need
		}
		cell := name + "1"
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return nil, err
		}
		_ = f.SetCellStyle(sheet, cell, cell, style)
		_ = f.SetColWidth(sheet, name, name, c.width)
		_ = f.SetCellStyle(sheet, name+"2", fmt.Sprintf("%s%d", name, last), wrap)
		_ = f.AddComment(sheet, excelize.Comment{Cell: cell, Author: "说明", Paragraph: []excelize.RichTextRun{{Text: c.note}}})
		if c.yesNo {
			dv := excelize.NewDataValidation(true)
			dv.Sqref = fmt.Sprintf("%s2:%s%d", name, name, last)
			_ = dv.SetDropList([]string{"是", "否"})
			_ = f.AddDataValidation(sheet, dv)
		}
	}
	_ = f.SetRowHeight(sheet, 1, 30)
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		r := r
		if err := f.SetSheetRow(sheet, cell, &r); err != nil {
			return nil, err
		}
	}
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, XSplit: 2, TopLeftCell: "C2", ActivePane: "bottomRight"})

	// the help sheet
	if _, err := f.NewSheet(help); err != nil {
		return nil, err
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}})
	lines := [][]any{
		{"怎么用"},
		{"1. 在“菜品”表里一行填一个菜品，第一行表头不要改。带 * 的列必填，其余可以留空。"},
		{"2. 保存后回到菜单页点“导入 Excel”上传。系统先给出预览，有错的行会标出原因，确认后才会写入。"},
		{"3. 同一分类里菜名相同就更新这个菜品，留空的格子保持原样；否则新建。新菜品默认是草稿，上架列填“是”会直接上架。"},
		{"4. 想批量改价或改图：先点“导出现有菜品”，改完再导入。"},
		{""},
		{"规格怎么写"},
		{"一行一组：组名：选项，选项+加价。例如："},
		{"辣度：不辣，小辣，大辣", "必选一项（默认）"},
		{"饮料[可选]：可乐+20，雪碧+20", "最多选一项，可以不选"},
		{"加料[多选]：加蛋+10，芝士+15", "随便选几项，可以不选"},
		{"小菜[选1-3]：泡菜，海带，豆芽", "至少选 1 项，最多 3 项"},
		{"多组用换行（Alt+Enter）或分号隔开。更新时填“无”会清空规格，留空保持原样。"},
		{""},
		{"图片"},
		{"填图片网址（https:// 开头），例如 https://example.com/dish.jpg。更多图片最多 8 张，每行一个或用逗号隔开。"},
		{"图片也可以导入后在菜品编辑里上传和裁剪。"},
		{""},
		{"各列说明"},
	}
	for _, c := range cols {
		h := c.head
		if c.required {
			h += " *"
		}
		lines = append(lines, []any{h, c.note})
	}
	if len(cats) > 0 {
		lines = append(lines, []any{""}, []any{"店里现有的分类"}, []any{strings.Join(cats, "、")})
	}
	if len(stations) > 0 {
		lines = append(lines, []any{""}, []any{"店里的工作台"}, []any{strings.Join(stations, "、")})
	}
	if len(notes) > 0 {
		lines = append(lines, []any{""}, []any{"店里现有的口味模板"}, []any{strings.Join(notes, "、")})
	}
	titles := map[string]bool{"怎么用": true, "规格怎么写": true, "图片": true, "各列说明": true, "店里现有的分类": true, "店里的工作台": true, "店里现有的口味模板": true}
	for i, l := range lines {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		l := l
		if err := f.SetSheetRow(help, cell, &l); err != nil {
			return nil, err
		}
		if s, ok := l[0].(string); ok && titles[s] {
			_ = f.SetCellStyle(help, cell, cell, bold)
		}
	}
	_ = f.SetColWidth(help, "A", "A", 46)
	_ = f.SetColWidth(help, "B", "B", 80)
	f.SetActiveSheet(0)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
