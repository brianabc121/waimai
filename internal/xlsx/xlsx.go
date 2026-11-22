// Package xlsx writes plain Excel workbooks: sheets of text and numbers with
// a bold first row. It covers what the exports need and nothing more.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Sheet is one tab. A cell is a string, an int, int64 or float64, or nil for
// an empty cell. The first row is the header.
type Sheet struct {
	Name string
	Rows [][]any
}

const (
	head       = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	nsMain     = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"`
	nsRel      = `xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	relOffice  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	relPackage = "http://schemas.openxmlformats.org/package/2006/relationships"
	typeMain   = "application/vnd.openxmlformats-officedocument.spreadsheetml"
)

// ContentType is the media type of a workbook.
const ContentType = typeMain + ".sheet"

func esc(s string) string {
	var b bytes.Buffer
	// Control characters other than tab and line breaks are not allowed in XML 1.0.
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s)
	_ = xml.EscapeText(&b, []byte(clean))
	return b.String()
}

// col is the letter of a column, from 0: A, B, ..., Z, AA.
func col(i int) string {
	s := ""
	for i >= 0 {
		s = string(rune('A'+i%26)) + s
		i = i/26 - 1
	}
	return s
}

// sheetName fits a name into what Excel accepts.
func sheetName(s string, i int) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" {
		s = fmt.Sprintf("Sheet%d", i+1)
	}
	if r := []rune(s); len(r) > 31 {
		s = string(r[:31])
	}
	return s
}

func sheetXML(sh Sheet) string {
	var b strings.Builder
	b.WriteString(head + `<worksheet ` + nsMain + `>`)
	// Column widths from the longest cell; a CJK character is about two wide.
	var widths []int
	for _, row := range sh.Rows {
		for i, c := range row {
			for len(widths) <= i {
				widths = append(widths, 6)
			}
			text := fmt.Sprint(c)
			if c == nil {
				text = ""
			}
			w := utf8.RuneCountInString(text) + (len(text)-utf8.RuneCountInString(text))/2 + 2
			if w > 60 {
				w = 60
			}
			if w > widths[i] {
				widths[i] = w
			}
		}
	}
	b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	if len(widths) > 0 {
		b.WriteString(`<cols>`)
		for i, w := range widths {
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, i+1, i+1, w)
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	for r, row := range sh.Rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		style := ""
		if r == 0 {
			style = ` s="1"`
		}
		for i, c := range row {
			ref := col(i) + strconv.Itoa(r+1)
			switch v := c.(type) {
			case nil:
			case int:
				fmt.Fprintf(&b, `<c r="%s"%s><v>%d</v></c>`, ref, style, v)
			case int64:
				fmt.Fprintf(&b, `<c r="%s"%s><v>%d</v></c>`, ref, style, v)
			case float64:
				fmt.Fprintf(&b, `<c r="%s"%s><v>%s</v></c>`, ref, style, strconv.FormatFloat(v, 'f', -1, 64))
			default:
				if s := fmt.Sprint(v); s != "" {
					fmt.Fprintf(&b, `<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, esc(s))
				}
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

// Write writes a workbook with the sheets in order.
func Write(w io.Writer, sheets ...Sheet) error {
	if len(sheets) == 0 {
		sheets = []Sheet{{}}
	}
	z := zip.NewWriter(w)
	put := func(name, body string) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	var types, tabs, rels strings.Builder
	seen := map[string]bool{}
	for i, sh := range sheets {
		name := sheetName(sh.Name, i)
		for seen[strings.ToLower(name)] {
			name = sheetName(fmt.Sprintf("%s %d", name, i+1), i)
		}
		seen[strings.ToLower(name)] = true
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="%s.worksheet+xml"/>`, i+1, typeMain)
		fmt.Fprintf(&tabs, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(name), i+1, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="%s/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, relOffice, i+1)
	}
	files := [][2]string{
		{"[Content_Types].xml", head + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="` + typeMain + `.sheet.main+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="` + typeMain + `.styles+xml"/>` + types.String() + `</Types>`},
		{"_rels/.rels", head + `<Relationships xmlns="` + relPackage + `">` +
			`<Relationship Id="rId1" Type="` + relOffice + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", head + `<workbook ` + nsMain + ` ` + nsRel + `><sheets>` + tabs.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", head + `<Relationships xmlns="` + relPackage + `">` + rels.String() +
			fmt.Sprintf(`<Relationship Id="rId%d" Type="%s/styles" Target="styles.xml"/>`, len(sheets)+1, relOffice) + `</Relationships>`},
		{"xl/styles.xml", head + `<styleSheet ` + nsMain + `>` +
			`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
			`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
			`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
			`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
			`<cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
			`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs></styleSheet>`},
	}
	for _, f := range files {
		if err := put(f[0], f[1]); err != nil {
			return err
		}
	}
	for i, sh := range sheets {
		if err := put(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheetXML(sh)); err != nil {
			return err
		}
	}
	return z.Close()
}
