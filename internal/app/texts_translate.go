package app

import (
	"context"
	"slices"
)

// The texts a customer reads outside the catalog (docs/I18N_SIMPLE_DESIGN.md
// §4.8): stored value campaigns, coupons to claim, the titles of promotional
// pictures, the names of queue groups. They are translated by the same job as
// the catalog and read here, by record.

// pickTexts turns rows of (key, lang, text, the store's language, the
// languages it turned off) into the text a reader of lang gets for each key:
// their language, English when the store turned theirs off or has no
// translation yet, and nothing when they read the store's own language.
func pickTexts(ctx context.Context, q Querier, lang, sql string, args ...any) (map[int64]string, error) {
	out := map[int64]string{}
	if !IsLang(lang) {
		return out, nil
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	en := map[int64]string{}
	for rows.Next() {
		var id int64
		var l, text, menuLang string
		var off []string
		if err := rows.Scan(&id, &l, &text, &menuLang, &off); err != nil {
			return nil, err
		}
		if lang == menuLang {
			continue
		}
		want := lang
		if slices.Contains(off, lang) {
			want = "en"
		}
		if l == want {
			out[id] = text
		} else if l == "en" && menuLang != "en" {
			en[id] = text
		}
	}
	for id, text := range en {
		if _, ok := out[id]; !ok {
			out[id] = text
		}
	}
	return out, rows.Err()
}

// Texts gives one field of some records in a customer's language, by the
// record's id; a record with no entry is shown as written.
func (a *App) Texts(ctx context.Context, lang, entity, field string, ids []int64) (map[int64]string, error) {
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	return pickTexts(ctx, a.DB, lang, `SELECT t.entity_id, t.lang, t.text, b.menu_lang, b.menu_langs_off
		FROM content_translations t JOIN branches b ON b.id = t.branch_id
		WHERE t.entity=$1 AND t.field=$2 AND t.entity_id = ANY($3) AND t.lang IN ($4, 'en')`, entity, field, ids, lang)
}

// CouponTexts names the coupons customers hold in their language, by the
// coupon's id: a coupon is named after the campaign it came from.
func (a *App) CouponTexts(ctx context.Context, lang string, couponIDs []int64) (map[int64]string, error) {
	if len(couponIDs) == 0 {
		return map[int64]string{}, nil
	}
	return pickTexts(ctx, a.DB, lang, `SELECT c.id, t.lang, t.text, b.menu_lang, b.menu_langs_off
		FROM customer_coupons c JOIN coupon_batches cb ON cb.id = c.batch_id AND cb.name = c.name
		JOIN content_translations t ON t.entity='coupon_batch' AND t.field='name' AND t.entity_id = cb.id
		JOIN branches b ON b.id = t.branch_id
		WHERE c.id = ANY($1) AND t.lang IN ($2, 'en')`, couponIDs, lang)
}
