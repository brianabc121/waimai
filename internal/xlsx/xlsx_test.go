package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf,
		Sheet{Name: "汇总", Rows: [][]any{{"员工", "份数", "小时"}, {"小张 <A&B>", 86, 4.25}, {"后厨\x01大屏", int64(72), nil}}},
		Sheet{Name: "a/b:c", Rows: [][]any{{"x"}}},
		Sheet{Name: "A/B:C"})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r)
		r.Close()
		files[f.Name] = string(b)
		// every part is well-formed XML
		d := xml.NewDecoder(bytes.NewReader(b))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", f.Name, err)
			}
		}
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml",
		"xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml", "xl/worksheets/sheet3.xml"} {
		if files[name] == "" {
			t.Fatalf("%s is missing", name)
		}
	}
	wb := files["xl/workbook.xml"]
	if !strings.Contains(wb, `name="汇总"`) || !strings.Contains(wb, `name="a b c"`) || !strings.Contains(wb, `name="A B C 3"`) {
		t.Fatalf("sheet names: %s", wb)
	}
	s1 := files["xl/worksheets/sheet1.xml"]
	for _, want := range []string{
		`<c r="A1" s="1" t="inlineStr"><is><t xml:space="preserve">员工</t></is></c>`,
		`<c r="A2" t="inlineStr"><is><t xml:space="preserve">小张 &lt;A&amp;B&gt;</t></is></c>`,
		`<c r="B2"><v>86</v></c>`, `<c r="C2"><v>4.25</v></c>`, `<c r="B3"><v>72</v></c>`, `后厨大屏`,
	} {
		if !strings.Contains(s1, want) {
			t.Fatalf("sheet 1 lacks %s:\n%s", want, s1)
		}
	}
	if strings.Contains(s1, `r="C3"`) {
		t.Fatal("an empty cell was written")
	}
	if col(0) != "A" || col(25) != "Z" || col(26) != "AA" || col(27) != "AB" || col(701) != "ZZ" || col(702) != "AAA" {
		t.Fatal("column letters")
	}
}
