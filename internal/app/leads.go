package app

import (
	"context"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Applications to join that come from the company website (docs/WEBSITE_DESIGN.md §8).
// They go into the same table and the same review tab as the ones customers
// send from "Me → Open a store", but without a customer account: anyone may
// send one, so the checks are on the content, the address (rate limit, in the
// HTTP layer) and how many arrive.

// ApplicationLead is what the website asks beyond an application from the
// app, and how the platform is following it up. Only the platform sees it.
type ApplicationLead struct {
	Source     string   `json:"source"`
	StoreType  string   `json:"store_type"`
	StoreCount string   `json:"store_count"`
	Wants      []string `json:"wants"`
	IMKind     string   `json:"im_kind"`
	IMHandle   string   `json:"im_handle"`
	Email      string   `json:"email"`
	Lang       string   `json:"lang"`
	Page       string   `json:"page"`
	Referrer   string   `json:"referrer"`
	FollowUp   string   `json:"follow_up"`
	FollowNote string   `json:"follow_note"`
}

const leadCols = `p.source, p.store_type, p.store_count, p.wants, p.im_kind, p.im_handle, p.email, p.lang,
	p.page, p.referrer, p.follow_up, p.follow_note`

func (l *ApplicationLead) dest() []any {
	return []any{&l.Source, &l.StoreType, &l.StoreCount, &l.Wants, &l.IMKind, &l.IMHandle, &l.Email, &l.Lang,
		&l.Page, &l.Referrer, &l.FollowUp, &l.FollowNote}
}

var (
	storeTypes  = map[string]bool{"": true, "quick": true, "full": true, "tea": true, "other": true}
	storeCounts = map[string]bool{"": true, "1": true, "2-5": true, "6-20": true, "20+": true}
	leadWants   = map[string]bool{"delivery": true, "pickup": true, "dine_in": true, "phone": true}
	imKinds     = map[string]bool{"": true, "wechat": true, "whatsapp": true, "telegram": true, "line": true, "viber": true}
	followUps   = map[string]bool{"new": true, "contacted": true, "demoed": true, "invalid": true}
	countryRe   = regexp.MustCompile(`^[A-Z]{2}$`)
	// "+63 917 555 0123", "(02) 8123-4567": digits with the usual separators, 6 to 20 digits.
	leadPhoneRe = regexp.MustCompile(`^\+?[0-9 ()\-.]+$`)
)

// LeadInput is the form on the website.
type LeadInput struct {
	StoreName   string   `json:"store_name"`
	ContactName string   `json:"contact_name"`
	CountryCode string   `json:"country_code"`
	Country     string   `json:"country"`
	Phone       string   `json:"phone"`
	City        string   `json:"city"`
	StoreType   string   `json:"store_type"`
	StoreCount  string   `json:"store_count"`
	Wants       []string `json:"wants"`
	IMKind      string   `json:"im_kind"`
	IMHandle    string   `json:"im_handle"`
	Email       string   `json:"email"`
	Note        string   `json:"note"`
	Lang        string   `json:"lang"`
	Page        string   `json:"page"`
	Referrer    string   `json:"referrer"`
	Agree       bool     `json:"agree"`
}

// LeadsPerHour caps how many applications the website may add in an hour,
// from everyone together: past it the form says to try later, so a flood from
// many addresses cannot bury the real ones.
const LeadsPerHour = 60

// SubmitLead saves an application from the website and returns its number.
// The same phone number again within a day updates the earlier one instead
// of adding another, as long as nobody has decided on it yet.
func (a *App) SubmitLead(ctx context.Context, in LeadInput) (int64, error) {
	for _, s := range []*string{&in.StoreName, &in.ContactName, &in.CountryCode, &in.Country, &in.Phone, &in.City,
		&in.StoreType, &in.StoreCount, &in.IMKind, &in.IMHandle, &in.Email, &in.Note, &in.Lang, &in.Page, &in.Referrer} {
		*s = strings.TrimSpace(*s)
	}
	in.CountryCode = strings.ToUpper(in.CountryCode)
	in.Page, in.Referrer = firstRunes(in.Page, 200), firstRunes(in.Referrer, 500)
	wants := []string{}
	for _, w := range in.Wants {
		if !leadWants[w] {
			return 0, ErrValidation("unknown choice of what to use: " + w)
		}
		if !slices.Contains(wants, w) {
			wants = append(wants, w)
		}
	}
	n := utf8.RuneCountInString
	digits := 0
	for _, r := range in.Phone {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	switch {
	case !in.Agree:
		return 0, ErrValidation("please agree to the privacy policy")
	case in.StoreName == "" || n(in.StoreName) > 100:
		return 0, ErrValidation("enter the name of the store")
	case in.ContactName == "" || n(in.ContactName) > 80:
		return 0, ErrValidation("enter who to contact")
	case !countryRe.MatchString(in.CountryCode) || n(in.Country) > 100:
		return 0, ErrValidation("choose the country")
	case n(in.Phone) > 40 || !leadPhoneRe.MatchString(in.Phone) || digits < 6 || digits > 20:
		return 0, ErrValidation("enter a phone number")
	case !storeTypes[in.StoreType]:
		return 0, ErrValidation("unknown type of store")
	case !storeCounts[in.StoreCount]:
		return 0, ErrValidation("unknown number of stores")
	case !imKinds[in.IMKind] || n(in.IMHandle) > 100 || in.IMKind == "" && in.IMHandle != "":
		return 0, ErrValidation("choose the chat app for that account")
	case in.Email != "" && (n(in.Email) > 200 || !validEmail(in.Email)):
		return 0, ErrValidation("enter a valid email address")
	case n(in.City) > 100 || n(in.Note) > 1000 || n(in.Lang) > 10:
		return 0, ErrValidation("one of the fields is too long")
	}
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		// One at a time, so that two sends of the same form cannot both add a row.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('website_leads'))`); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `UPDATE merchant_applications SET store_name=$2, contact_name=$3, country_code=$4, country=$5,
			city=$6, store_type=$7, store_count=$8, wants=$9, im_kind=$10, im_handle=$11, email=$12, note=$13, lang=$14,
			page=$15, referrer=$16
			WHERE id = (SELECT id FROM merchant_applications WHERE source='website' AND phone=$1 AND status='pending'
				AND created_at > now() - interval '1 day' ORDER BY id DESC LIMIT 1)
			RETURNING id`, in.Phone, in.StoreName, in.ContactName, in.CountryCode, in.Country, in.City, in.StoreType,
			in.StoreCount, wants, in.IMKind, in.IMHandle, in.Email, in.Note, in.Lang, in.Page, in.Referrer).Scan(&id)
		if !isNoRows(err) {
			return err
		}
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM merchant_applications WHERE source='website'
			AND created_at > now() - interval '1 hour'`).Scan(&recent); err != nil {
			return err
		}
		if recent >= LeadsPerHour {
			return Err(http.StatusTooManyRequests, "BUSY", "we are receiving many requests; please try again later or message us")
		}
		return tx.QueryRow(ctx, `INSERT INTO merchant_applications (source, store_name, contact_name, phone, country_code, country,
			city, store_type, store_count, wants, im_kind, im_handle, email, note, lang, page, referrer)
			VALUES ('website',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`,
			in.StoreName, in.ContactName, in.Phone, in.CountryCode, in.Country, in.City, in.StoreType, in.StoreCount, wants,
			in.IMKind, in.IMHandle, in.Email, in.Note, in.Lang, in.Page, in.Referrer).Scan(&id)
	})
	return id, err
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && strings.Contains(s[strings.LastIndex(s, "@"):], ".")
}

// firstRunes keeps the first n characters: where the form was sent from is only a hint.
func firstRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// AdminFollowUp records how far the platform got with reaching an applicant.
func (a *App) AdminFollowUp(ctx context.Context, actor AdminActor, id int64, followUp, note string) error {
	note = strings.TrimSpace(note)
	if !followUps[followUp] {
		return ErrValidation("follow_up must be new, contacted, demoed or invalid")
	}
	if utf8.RuneCountInString(note) > 1000 {
		return ErrValidation("the note can have up to 1000 characters")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE merchant_applications SET follow_up=$2, follow_note=$3 WHERE id=$1`, id, followUp, note)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return adminAudit(ctx, tx, actor, "application.follow_up", "application", id, map[string]any{"follow_up": followUp})
	})
}
