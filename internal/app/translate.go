package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/longbridgeapp/opencc"

	"github.com/brianabc121/waimai/internal/config"
)

// Translator turns merchant text (menu items, categories, options) into other
// languages (docs/I18N_DESIGN.md §6.5). Codes are those of Langs.
type Translator interface {
	// Name is the provider shown in the back office and kept on each translation.
	Name() string
	// Translate returns one translation per text, in order.
	Translate(ctx context.Context, from, to string, texts []string) ([]string, error)
}

// translateBatch is how many texts go to the service in one call.
const translateBatch = 50

func validTranslateProvider(v string) bool {
	switch v {
	case "claude-haiku-5-5", "claude-sonnet-5-5", "google", "off", "stub":
		return true
	}
	return false
}

// newTranslator builds the service the keys set up, nil for none. Without a
// provider named, an Anthropic key means Claude Haiku and a Google key alone
// means Google.
func newTranslator(cfg config.Config) Translator {
	p := cfg.TranslateProvider
	if p == "" {
		switch {
		case cfg.AnthropicAPIKey != "":
			p = "claude-haiku-5-5"
		case cfg.GoogleTranslateKey != "":
			p = "google"
		}
	}
	switch p {
	case "claude-haiku-5-5", "claude-sonnet-5-5":
		if cfg.AnthropicAPIKey == "" {
			return nil
		}
		return &claudeTranslator{model: p, client: anthropic.NewClient(option.WithAPIKey(cfg.AnthropicAPIKey),
			option.WithRequestTimeout(90*time.Second))}
	case "google":
		if cfg.GoogleTranslateKey == "" {
			return nil
		}
		return &googleTranslator{key: cfg.GoogleTranslateKey}
	case "stub":
		return stubTranslator{}
	}
	return nil
}

// langNames are the names the translation model is given.
var langNames = map[string]string{
	"zh": "Simplified Chinese", "en": "English", "zh-Hant": "Traditional Chinese (Taiwan)", "th": "Thai",
	"vi": "Vietnamese", "fr": "French", "ar": "Arabic", "es": "Spanish", "ru": "Russian", "ja": "Japanese",
	"my": "Burmese (Unicode)", "km": "Khmer",
}

// ---- Claude ----

type claudeTranslator struct {
	model  string
	client anthropic.Client
}

func (t *claudeTranslator) Name() string { return t.model }

const menuPrompt = `You translate restaurant menus for a food ordering app. You get a JSON list of texts from one restaurant (dish names, descriptions, category names, option groups and options) and translate each into the target language.

- Use the name people who speak the target language already know a dish by, if there is one (宫保鸡丁 → Kung Pao Chicken, Pad Thai → ผัดไทย). Otherwise transliterate the name and add a short plain description in brackets when the literal meaning would mislead (夫妻肺片 → "Fuqi Feipian (spicy sliced beef)").
- Keep brand names, the restaurant's name, numbers, units, prices, sizes, {placeholders} and emoji as they are.
- Do not add anything the original does not say. Keep it as short as the original; menus are read on phones.
- A text that is already in the target language, or has nothing to translate, comes back unchanged.
- Return one translation for every id.`

var translationSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"translations": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"id": map[string]any{"type": "integer"}, "text": map[string]any{"type": "string"}},
				"required":             []string{"id", "text"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"translations"},
	"additionalProperties": false,
}

func (t *claudeTranslator) Translate(ctx context.Context, from, to string, texts []string) ([]string, error) {
	type in struct {
		ID   int    `json:"id"`
		Text string `json:"text"`
	}
	list := make([]in, len(texts))
	for i, s := range texts {
		list[i] = in{i + 1, s}
	}
	body, _ := json.Marshal(map[string]any{"from": langNames[from], "to": langNames[to], "texts": list})
	msg, err := t.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(t.model),
		MaxTokens: 16000,
		System:    []anthropic.TextBlockParam{{Text: menuPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Translate into " + langNames[to] + ":\n" + string(body))),
		},
		OutputConfig: anthropic.OutputConfigParam{Format: anthropic.JSONOutputFormatParam{Schema: translationSchema}},
	})
	if err != nil {
		return nil, err
	}
	if msg.StopReason == anthropic.StopReasonMaxTokens {
		return nil, fmt.Errorf("translation was cut off")
	}
	var raw strings.Builder
	for _, b := range msg.Content {
		if b.Type == "text" {
			raw.WriteString(b.Text)
		}
	}
	var out struct {
		Translations []in `json:"translations"`
	}
	if err := json.Unmarshal([]byte(raw.String()), &out); err != nil {
		return nil, fmt.Errorf("translation reply: %w", err)
	}
	return matchTranslations(len(texts), func(yield func(int, string)) {
		for _, tr := range out.Translations {
			yield(tr.ID-1, tr.Text)
		}
	})
}

// matchTranslations puts numbered replies back in order and fails when one
// is missing.
func matchTranslations(n int, each func(func(int, string))) ([]string, error) {
	res := make([]string, n)
	got := make([]bool, n)
	each(func(i int, s string) {
		if i >= 0 && i < n && strings.TrimSpace(s) != "" {
			res[i], got[i] = strings.TrimSpace(s), true
		}
	})
	for i := range got {
		if !got[i] {
			return nil, fmt.Errorf("translation %d is missing", i+1)
		}
	}
	return res, nil
}

// ---- Google Cloud Translation ----

type googleTranslator struct{ key string }

func (googleTranslator) Name() string { return "google" }

func googleLang(l string) string {
	switch l {
	case "zh":
		return "zh-CN"
	case "zh-Hant":
		return "zh-TW"
	}
	return l
}

var googleTranslateURL = "https://translation.googleapis.com/language/translate/v2"

func (t *googleTranslator) Translate(ctx context.Context, from, to string, texts []string) ([]string, error) {
	body, _ := json.Marshal(map[string]any{"q": texts, "source": googleLang(from), "target": googleLang(to), "format": "text"})
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTranslateURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", t.key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google translate: %s: %s", res.Status, truncate(string(b), 200))
	}
	var out struct {
		Data struct {
			Translations []struct {
				TranslatedText string `json:"translatedText"`
			} `json:"translations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return matchTranslations(len(texts), func(yield func(int, string)) {
		for i, tr := range out.Data.Translations {
			yield(i, tr.TranslatedText)
		}
	})
}

// ---- stub, for tests ----

type stubTranslator struct{}

func (stubTranslator) Name() string { return "stub" }

func (stubTranslator) Translate(_ context.Context, _, to string, texts []string) ([]string, error) {
	out := make([]string, len(texts))
	for i, s := range texts {
		out[i] = "[" + to + "] " + s
	}
	return out, nil
}

// ---- Simplified and Traditional Chinese ----

var (
	ccOnce       sync.Once
	ccS2T, ccT2S *opencc.OpenCC
)

// convertChinese turns Simplified Chinese into Traditional (Taiwan wording)
// or back. It returns "" when it cannot.
func convertChinese(s, to string) string {
	ccOnce.Do(func() {
		ccS2T, _ = opencc.New("s2twp")
		ccT2S, _ = opencc.New("tw2sp")
	})
	cc := ccS2T
	if to == "zh" {
		cc = ccT2S
	}
	if cc == nil {
		return ""
	}
	out, err := cc.Convert(s)
	if err != nil {
		return ""
	}
	return out
}
