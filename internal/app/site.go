package app

// The store's own page and domains (docs/ORDER_TYPES_DESIGN.md §5). The same
// site serves the mall (order.abcfood.com) and every store's own page
// (67891.abcfood.com, or a domain of the merchant's): the host of the
// request says which. On a store's own page the customer sees that store
// only, and what is ordered there is an Online- order.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	SiteMall = "mall"
	SiteWeb  = "web"
)

// Site is what a host serves.
type Site struct {
	Mode string `json:"mode"` // mall or web
	// Store is the store of a web site.
	Store *SiteStore `json:"store,omitempty"`
	// StoreDomain is the domain the stores' own pages are under, "" when
	// the platform has not set one; pages build store links with it.
	StoreDomain string `json:"store_domain"`
}

type SiteStore struct {
	ID int64 `json:"-"`
	// BrandID is the brand whose page this is (docs/MULTI_BRAND_DESIGN.md §9).
	BrandID int64  `json:"-"`
	Slug    string `json:"slug"`
	StoreNo int    `json:"store_no"`
	Name    string `json:"name"`
	NameEn  string `json:"name_en"`
}

// OwnSite says whether the site is the own page of the store slug.
func (s *Site) OwnSite() bool { return s != nil && s.Mode == SiteWeb }

// siteDomain is the domain of the stores' own pages: the setting in use, or
// SiteDomain when the App was not built from a configuration.
func (a *App) siteDomain() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.effective != nil && a.effective.StoreSiteDomain != "" {
		return strings.ToLower(a.effective.StoreSiteDomain)
	}
	return strings.ToLower(a.SiteDomain)
}

var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

func validDomain(d string) bool { return len(d) <= 253 && domainRe.MatchString(d) }

// normHost is the host of a request without port, in lower case.
func normHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimSuffix(host, ".")
}

// storeNoOf reads the store number from <store_no>.<domain>. A
// <store_no>.localhost host works on a development machine too.
func storeNoOf(host, domain string) (int, bool) {
	for _, d := range []string{domain, "localhost"} {
		if d == "" || !strings.HasSuffix(host, "."+d) {
			continue
		}
		label := strings.TrimSuffix(host, "."+d)
		if n, err := strconv.Atoi(label); err == nil && n > 0 && label[0] != '0' {
			return n, true
		}
	}
	return 0, false
}

// SiteFor says what the host serves. Anything that is not a store's own page
// is the mall.
func (a *App) SiteFor(ctx context.Context, host string) (*Site, error) {
	domain := a.siteDomain()
	site := &Site{Mode: SiteMall, StoreDomain: domain}
	host = normHost(host)
	if host == "" {
		return site, nil
	}
	var st SiteStore
	// each brand has its own number; a store's own domain is its main brand's
	const cols = `b.id, br.id, br.slug, br.store_no, br.name, br.name_en FROM brands br
		JOIN branches b ON b.id = br.branch_id
		JOIN merchants m ON m.id = b.merchant_id AND m.deleted_at IS NULL`
	var err error
	if no, ok := storeNoOf(host, domain); ok {
		err = a.DB.QueryRow(ctx, `SELECT `+cols+` WHERE br.store_no=$1 AND br.status='active' AND b.deleted_at IS NULL`, no).
			Scan(&st.ID, &st.BrandID, &st.Slug, &st.StoreNo, &st.Name, &st.NameEn)
	} else {
		err = a.DB.QueryRow(ctx, `SELECT `+cols+` JOIN branch_domains d ON d.branch_id = b.id
			WHERE d.domain=$1 AND d.status='active' AND b.deleted_at IS NULL AND br.is_main`, host).
			Scan(&st.ID, &st.BrandID, &st.Slug, &st.StoreNo, &st.Name, &st.NameEn)
	}
	if isNoRows(err) {
		return site, nil
	}
	if err != nil {
		return nil, err
	}
	site.Mode, site.Store = SiteWeb, &st
	return site, nil
}

// TLSAllowed answers the web server before it gets a certificate for a host
// (Caddy on-demand TLS): only stores' own pages are let through.
func (a *App) TLSAllowed(ctx context.Context, host string) (bool, error) {
	s, err := a.SiteFor(ctx, host)
	if err != nil {
		return false, err
	}
	return s.Mode == SiteWeb, nil
}

// StoreURL is the address of the store's own page, "" when the platform has
// not set the domain for them.
func (a *App) StoreURL(storeNo int) string {
	d := a.siteDomain()
	if d == "" || storeNo == 0 {
		return ""
	}
	return fmt.Sprintf("https://%d.%s", storeNo, d)
}

// ---- the merchant's domains ----

type BranchDomain struct {
	ID          int64      `json:"id"`
	BranchID    int64      `json:"branch_id"`
	Kind        string     `json:"kind"` // own, buy
	Domain      string     `json:"domain"`
	Candidates  string     `json:"candidates"`
	Status      string     `json:"status"`
	Note        string     `json:"note"`
	AdminNote   string     `json:"admin_note"`
	CheckError  string     `json:"check_error"`
	RequestedAt time.Time  `json:"requested_at"`
	CheckedAt   *time.Time `json:"checked_at"`
	VerifiedAt  *time.Time `json:"verified_at"`
	// for the platform's list
	StoreName string `json:"store_name,omitempty"`
	StoreSlug string `json:"store_slug,omitempty"`
	StoreNo   int    `json:"store_no,omitempty"`
	Target    string `json:"target,omitempty"` // where the CNAME points
}

const domainCols = `d.id, d.branch_id, d.kind, d.domain, d.candidates, d.status, d.note, d.admin_note, d.check_error,
	d.requested_at, d.checked_at, d.verified_at, b.name, b.slug, b.store_no
	FROM branch_domains d JOIN branches b ON b.id = d.branch_id`

func (a *App) queryDomains(ctx context.Context, q Querier, where string, args ...any) ([]BranchDomain, error) {
	rows, err := q.Query(ctx, `SELECT `+domainCols+` WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BranchDomain{}
	for rows.Next() {
		var d BranchDomain
		if err := rows.Scan(&d.ID, &d.BranchID, &d.Kind, &d.Domain, &d.Candidates, &d.Status, &d.Note, &d.AdminNote,
			&d.CheckError, &d.RequestedAt, &d.CheckedAt, &d.VerifiedAt, &d.StoreName, &d.StoreSlug, &d.StoreNo); err != nil {
			return nil, err
		}
		d.Target = a.cnameTarget(d.StoreNo)
		out = append(out, d)
	}
	return out, rows.Err()
}

// cnameTarget is the name a merchant's domain points to.
func (a *App) cnameTarget(storeNo int) string {
	if d := a.siteDomain(); d != "" {
		return fmt.Sprintf("%d.%s", storeNo, d)
	}
	return ""
}

// SiteLinks is the store's "store link" page.
type SiteLinks struct {
	StoreNo     int            `json:"store_no"`
	URL         string         `json:"url"`
	StoreDomain string         `json:"store_domain"`
	Target      string         `json:"target"`
	Domains     []BranchDomain `json:"domains"`
}

func (a *App) SiteLinks(ctx context.Context, branchID int64) (*SiteLinks, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	ds, err := a.queryDomains(ctx, a.DB, `d.branch_id=$1 ORDER BY d.requested_at DESC, d.id DESC`, branchID)
	if err != nil {
		return nil, err
	}
	return &SiteLinks{StoreNo: b.StoreNo, URL: a.StoreURL(b.StoreNo), StoreDomain: a.siteDomain(),
		Target: a.cnameTarget(b.StoreNo), Domains: ds}, nil
}

type DomainInput struct {
	Kind       string `json:"kind"`       // own, buy
	Domain     string `json:"domain"`     // own
	Candidates string `json:"candidates"` // buy: the names wanted, one per line or comma
	Note       string `json:"note"`
}

const maxDomains = 5

// AddDomain binds the merchant's own domain (checked by the worker) or asks
// the platform to buy one.
func (a *App) AddDomain(ctx context.Context, branchID int64, in DomainInput) (*BranchDomain, error) {
	in.Domain = normHost(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(in.Domain), "https://"), "http://"))
	in.Domain = strings.TrimSuffix(in.Domain, "/")
	in.Candidates, in.Note = strings.TrimSpace(in.Candidates), strings.TrimSpace(in.Note)
	if len(in.Note) > 300 || len(in.Candidates) > 300 {
		return nil, ErrValidation("text too long")
	}
	status := "pending"
	switch in.Kind {
	case "own":
		if !validDomain(in.Domain) {
			return nil, ErrValidation("enter a domain such as order.example.com")
		}
		site := a.siteDomain()
		if site != "" && (in.Domain == site || strings.HasSuffix(in.Domain, "."+site)) {
			return nil, ErrValidation("this is already a platform address")
		}
		in.Candidates = ""
	case "buy":
		if in.Candidates == "" {
			return nil, ErrValidation("enter the domain you want")
		}
		in.Domain, status = "", "requested"
	default:
		return nil, ErrValidation("kind must be own or buy")
	}
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM branch_domains WHERE branch_id=$1 AND status NOT IN ('rejected','failed')`,
			branchID).Scan(&n); err != nil {
			return err
		}
		if n >= maxDomains {
			return ErrValidation(fmt.Sprintf("at most %d domains", maxDomains))
		}
		err := tx.QueryRow(ctx, `INSERT INTO branch_domains (branch_id, kind, domain, candidates, status, note)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, branchID, in.Kind, in.Domain, in.Candidates, status, in.Note).Scan(&id)
		if isUnique(err, "branch_domains_domain") {
			return Err(http.StatusConflict, "DOMAIN_TAKEN", "this domain is already used by a store")
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	ds, err := a.queryDomains(ctx, a.DB, `d.id=$1`, id)
	if err != nil {
		return nil, err
	}
	return &ds[0], nil
}

// DeleteDomain stops using a domain, or withdraws a request.
func (a *App) DeleteDomain(ctx context.Context, branchID, id int64) error {
	tag, err := a.DB.Exec(ctx, `DELETE FROM branch_domains WHERE id=$1 AND branch_id=$2`, id, branchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CheckDomains looks up the merchants' own domains: one whose CNAME points to
// the store's page is put to use, and one that stopped pointing there is
// taken out again. Domains are looked at every few minutes while waiting
// and every hour once in use.
func (a *App) CheckDomains(ctx context.Context) (int, error) {
	if a.siteDomain() == "" {
		return 0, nil
	}
	ds, err := a.queryDomains(ctx, a.DB, `d.kind='own' AND d.status IN ('pending','active') AND (d.checked_at IS NULL
		OR d.checked_at < now() - CASE WHEN d.status='active' THEN interval '1 hour' ELSE interval '3 minutes' END)
		ORDER BY d.checked_at NULLS FIRST LIMIT 50`)
	if err != nil {
		return 0, err
	}
	lookup := a.LookupCNAME
	if lookup == nil {
		lookup = net.DefaultResolver.LookupCNAME
	}
	changed := 0
	for _, d := range ds {
		lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		got, err := lookup(lctx, d.Domain)
		cancel()
		ok := err == nil && strings.EqualFold(strings.TrimSuffix(got, "."), d.Target)
		msg := ""
		switch {
		case err != nil:
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				msg = "no DNS record yet"
			} else {
				msg = "DNS lookup failed"
			}
		case !ok:
			msg = "CNAME points to " + strings.TrimSuffix(got, ".")
		}
		status := "pending"
		if ok {
			status = "active"
		}
		if status != d.Status {
			changed++
		}
		if _, err := a.DB.Exec(ctx, `UPDATE branch_domains SET status=$2, check_error=$3, checked_at=now(),
			verified_at = CASE WHEN $2='active' THEN COALESCE(verified_at, now()) ELSE NULL END WHERE id=$1`,
			d.ID, status, msg); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// ---- the platform's list of domain requests ----

func (a *App) AdminDomains(ctx context.Context, status string) ([]BranchDomain, error) {
	switch status {
	case "":
		return a.queryDomains(ctx, a.DB, `true ORDER BY d.requested_at DESC, d.id DESC LIMIT 500`)
	case "open":
		return a.queryDomains(ctx, a.DB, `d.status IN ('requested','quoted','bought','pending')
			ORDER BY d.requested_at, d.id LIMIT 500`)
	}
	return a.queryDomains(ctx, a.DB, `d.status=$1 ORDER BY d.requested_at DESC, d.id DESC LIMIT 500`, status)
}

type AdminDomainInput struct {
	Status    string `json:"status"`
	Domain    string `json:"domain"`
	AdminNote string `json:"admin_note"`
}

// AdminUpdateDomain moves a purchase request on (quoted, bought, active,
// rejected), with the domain bought; an own domain may be put to use or
// refused by hand.
func (a *App) AdminUpdateDomain(ctx context.Context, actor AdminActor, id int64, in AdminDomainInput) (*BranchDomain, error) {
	in.Domain = normHost(in.Domain)
	in.AdminNote = strings.TrimSpace(in.AdminNote)
	if len(in.AdminNote) > 500 {
		return nil, ErrValidation("note too long")
	}
	var kind, domain string
	if err := a.DB.QueryRow(ctx, `SELECT kind, domain FROM branch_domains WHERE id=$1`, id).Scan(&kind, &domain); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	allowed := map[string][]string{"buy": {"requested", "quoted", "bought", "active", "rejected"}, "own": {"pending", "active", "failed"}}
	ok := false
	for _, s := range allowed[kind] {
		ok = ok || s == in.Status
	}
	if !ok {
		return nil, ErrValidation("status not allowed")
	}
	if kind == "own" {
		in.Domain = domain
	}
	if in.Domain != "" && !validDomain(in.Domain) {
		return nil, ErrValidation("enter a domain such as order.example.com")
	}
	if (in.Status == "bought" || in.Status == "active") && in.Domain == "" {
		return nil, ErrValidation("enter the domain bought")
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE branch_domains SET status=$2, domain=$3, admin_note=$4,
			verified_at = CASE WHEN $2='active' THEN COALESCE(verified_at, now()) ELSE NULL END WHERE id=$1`,
			id, in.Status, in.Domain, in.AdminNote)
		if isUnique(err, "branch_domains_domain") {
			return Err(http.StatusConflict, "DOMAIN_TAKEN", "this domain is already used by a store")
		}
		if err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "domain.update", "branch_domain", id, map[string]any{"status": in.Status, "domain": in.Domain})
	})
	if err != nil {
		return nil, err
	}
	ds, err := a.queryDomains(ctx, a.DB, `d.id=$1`, id)
	if err != nil {
		return nil, err
	}
	return &ds[0], nil
}
