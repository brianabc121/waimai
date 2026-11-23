package app

import "testing"

func TestLangFromHeader(t *testing.T) {
	for in, want := range map[string]string{
		"zh-TW,zh;q=0.9,en;q=0.8": "zh-Hant",
		"zh-CN,zh;q=0.9":          "zh",
		"zh-Hant-HK":              "zh-Hant",
		"th-TH,th;q=0.9":          "th",
		"de-DE,fr;q=0.8":          "fr",
		"my-MM":                   "my",
		"de-DE":                   "",
		"":                        "",
	} {
		if got := LangFromHeader(in); got != want {
			t.Errorf("LangFromHeader(%q) = %q, want %q", in, got, want)
		}
	}
	if !IsLang("km") || IsLang("zh-CN") {
		t.Error("IsLang")
	}
}
