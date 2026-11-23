package app

import (
	"reflect"
	"testing"
)

func TestParseSpecs(t *testing.T) {
	g, code := parseSpecs("辣度：不辣，小辣，大辣\n加料[多选]：加蛋+10，芝士 + 15.5\n饮料【可选】:可乐（+20）,雪碧+20；小菜[选1-3]：泡菜、海带\n杯型（大杯）：冰，去冰")
	if code != "" {
		t.Fatal(code)
	}
	want := []OptionGroupInput{
		{Name: "辣度", MinSelect: 1, MaxSelect: 1, Options: []OptionInput{{Name: "不辣"}, {Name: "小辣"}, {Name: "大辣"}}},
		{Name: "加料", MinSelect: 0, MaxSelect: 2, Options: []OptionInput{{Name: "加蛋", PriceDelta: 1000}, {Name: "芝士", PriceDelta: 1550}}},
		{Name: "饮料", MinSelect: 0, MaxSelect: 1, Options: []OptionInput{{Name: "可乐", PriceDelta: 2000}, {Name: "雪碧", PriceDelta: 2000}}},
		{Name: "小菜", MinSelect: 1, MaxSelect: 2, Options: []OptionInput{{Name: "泡菜"}, {Name: "海带"}}}, // max clamped to the options
		{Name: "杯型（大杯）", MinSelect: 1, MaxSelect: 1, Options: []OptionInput{{Name: "冰"}, {Name: "去冰"}}},
	}
	if !reflect.DeepEqual(g, want) {
		t.Fatalf("got %+v", g)
	}
	for _, bad := range []string{"辣度 不辣", "辣度：", "辣度[随便]：a", "辣度[选3-1]：a,b,c", "：a,b", "加料：蛋+abc"} {
		if _, code := parseSpecs(bad); code == "" {
			t.Errorf("%q should be refused", bad)
		}
	}

	// what the export writes reads back the same
	groups := []OptionGroup{
		{Name: "辣度", MinSelect: 1, MaxSelect: 1, Options: []Option{{Name: "不辣"}, {Name: "大辣", PriceDelta: 500}}},
		{Name: "加料", MinSelect: 0, MaxSelect: 2, Options: []Option{{Name: "蛋", PriceDelta: 1050}, {Name: "芝士"}}},
		{Name: "饮料", MinSelect: 0, MaxSelect: 1, Options: []Option{{Name: "可乐"}, {Name: "茶"}, {Name: "水"}}},
		{Name: "小菜", MinSelect: 2, MaxSelect: 3, Options: []Option{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}},
	}
	text := formatSpecs(groups)
	back, code := parseSpecs(text)
	if code != "" || len(back) != len(groups) {
		t.Fatalf("%q: %s %+v", text, code, back)
	}
	for i, g := range groups {
		b := back[i]
		if b.Name != g.Name || b.MinSelect != g.MinSelect || b.MaxSelect != g.MaxSelect || len(b.Options) != len(g.Options) {
			t.Fatalf("group %d: %+v from %q", i, b, text)
		}
		for j, o := range g.Options {
			if b.Options[j].Name != o.Name || b.Options[j].PriceDelta != o.PriceDelta {
				t.Fatalf("option %d/%d: %+v", i, j, b.Options[j])
			}
		}
	}
}

func TestImportHeaders(t *testing.T) {
	for in, want := range map[string]string{"价格（PHP） *": "价格", "主图链接": "主图链接", "Image URL": "imageurl", "规格[选填]": "规格"} {
		if got := normHeader(in); got != want {
			t.Errorf("normHeader(%q) = %q, want %q", in, got, want)
		}
	}
	for _, ok := range []string{"https://a.example/x.jpg", "http://a.example/x.png", "/api/v1/images/abc"} {
		if !validImageRef(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"javascript:alert(1)", "//evil.example/x", "ftp://x", "https://", "a.jpg", "https://a b"} {
		if validImageRef(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
}
