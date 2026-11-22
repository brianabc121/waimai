package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// The marketplace as the merchant and the platform set it up.

// MallListing is how one store shows in the marketplace.
type MallListing struct {
	Listed      bool    `json:"listed"`
	Hidden      bool    `json:"hidden"` // taken off by the platform
	Tagline     string  `json:"tagline"`
	CategoryIDs []int64 `json:"category_ids"`
	// Categories is the platform's list to pick from.
	Categories []MallCategory `json:"categories"`
	// What a listing needs to look right.
	HasLocation bool `json:"has_location"`
	HasZones    bool `json:"has_zones"`
	HasLogo     bool `json:"has_logo"`
	HasCover    bool `json:"has_cover"`
}

const maxStoreCategories = 2

func (a *App) MallListing(ctx context.Context, branchID int64) (*MallListing, error) {
	l := &MallListing{CategoryIDs: []int64{}}
	err := a.DB.QueryRow(ctx, `SELECT b.mall_listed, b.mall_hidden, b.tagline, b.lat IS NOT NULL,
		EXISTS (SELECT 1 FROM delivery_zones z WHERE z.branch_id = b.id AND z.active AND z.polygon IS NOT NULL),
		coalesce(t.published->>'logo_url', '') <> '', coalesce(t.published->>'cover_url', '') <> '',
		coalesce((SELECT array_agg(bc.category_id ORDER BY bc.category_id) FROM branch_categories bc WHERE bc.branch_id = b.id), '{}')
		FROM branches b LEFT JOIN channel_templates t ON t.branch_id = b.id AND t.channel = 'delivery'
		WHERE b.id = $1 AND b.deleted_at IS NULL`, branchID).
		Scan(&l.Listed, &l.Hidden, &l.Tagline, &l.HasLocation, &l.HasZones, &l.HasLogo, &l.HasCover, &l.CategoryIDs)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if l.CategoryIDs == nil {
		l.CategoryIDs = []int64{}
	}
	l.Categories, err = mallCategories(ctx, a.DB, true)
	return l, err
}

type MallListingInput struct {
	Listed      bool    `json:"listed"`
	Tagline     string  `json:"tagline"`
	CategoryIDs []int64 `json:"category_ids"`
}

func (a *App) SaveMallListing(ctx context.Context, branchID int64, in MallListingInput) (*MallListing, error) {
	in.Tagline = strings.TrimSpace(in.Tagline)
	if utf8.RuneCountInString(in.Tagline) > 40 {
		return nil, ErrValidation("the introduction must be at most 40 characters")
	}
	if len(in.CategoryIDs) > maxStoreCategories {
		return nil, ErrValidation("choose at most 2 categories")
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE branches SET mall_listed=$2, tagline=$3 WHERE id=$1 AND deleted_at IS NULL`, branchID, in.Listed, in.Tagline)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM branch_categories WHERE branch_id=$1`, branchID); err != nil {
			return err
		}
		for _, id := range in.CategoryIDs {
			tag, err := tx.Exec(ctx, `INSERT INTO branch_categories (branch_id, category_id)
				SELECT $1, id FROM store_categories WHERE id=$2 AND active ON CONFLICT DO NOTHING`, branchID, id)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrValidation("unknown category")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return a.MallListing(ctx, branchID)
}

// ---- platform ----

// MallAdminStore is a store in the platform's list of what the marketplace shows.
type MallAdminStore struct {
	ID         int64    `json:"id"`
	MerchantID int64    `json:"merchant_id"`
	Slug       string   `json:"slug"`
	Name       string   `json:"name"`
	NameEn     string   `json:"name_en"`
	LogoURL    string   `json:"logo_url"`
	City       string   `json:"city"`
	Categories []string `json:"categories"`
	Listed     bool     `json:"listed"`
	Hidden     bool     `json:"hidden"`
	// State is shown (customers can find it), hidden (by the platform), off
	// (the merchant opted out), suspended, no_location or no_zones (shown, but
	// only for pickup and when browsing by city).
	State string `json:"state"`
}

type MallAdmin struct {
	Banners    []MallBanner     `json:"banners"`
	Categories []MallCategory   `json:"categories"`
	Stores     []MallAdminStore `json:"stores"`
}

const maxMallBanners = 5

func (a *App) AdminMall(ctx context.Context) (*MallAdmin, error) {
	out := &MallAdmin{Stores: []MallAdminStore{}}
	var err error
	if out.Banners, err = mallBanners(ctx, a.DB, false); err != nil {
		return nil, err
	}
	if out.Categories, err = mallCategories(ctx, a.DB, false); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT b.id, b.merchant_id, b.slug, b.name, b.name_en, coalesce(t.published->>'logo_url', ''), b.city,
		coalesce((SELECT array_agg(c.code ORDER BY c.sort, c.id) FROM branch_categories bc JOIN store_categories c ON c.id = bc.category_id WHERE bc.branch_id = b.id), '{}'),
		b.mall_listed, b.mall_hidden, m.status = 'suspended', b.lat IS NOT NULL,
		EXISTS (SELECT 1 FROM delivery_zones z WHERE z.branch_id = b.id AND z.active AND z.polygon IS NOT NULL)
		FROM branches b JOIN merchants m ON m.id = b.merchant_id
		LEFT JOIN channel_templates t ON t.branch_id = b.id AND t.channel = 'delivery'
		WHERE b.deleted_at IS NULL AND m.deleted_at IS NULL ORDER BY b.id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s MallAdminStore
		var suspended, located, zoned bool
		if err := rows.Scan(&s.ID, &s.MerchantID, &s.Slug, &s.Name, &s.NameEn, &s.LogoURL, &s.City, &s.Categories,
			&s.Listed, &s.Hidden, &suspended, &located, &zoned); err != nil {
			return nil, err
		}
		if s.Categories == nil {
			s.Categories = []string{}
		}
		switch {
		case s.Hidden:
			s.State = "hidden"
		case suspended:
			s.State = "suspended"
		case !s.Listed:
			s.State = "off"
		case !located:
			s.State = "no_location"
		case !zoned:
			s.State = "no_zones"
		default:
			s.State = "shown"
		}
		out.Stores = append(out.Stores, s)
	}
	return out, rows.Err()
}

// AdminHideStore takes a store off the marketplace, or puts it back.
func (a *App) AdminHideStore(ctx context.Context, actor AdminActor, branchID int64, hidden bool) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE branches SET mall_hidden=$2 WHERE id=$1 AND deleted_at IS NULL`, branchID, hidden)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return adminAudit(ctx, tx, actor, "mall.store", "branch", branchID, map[string]any{"hidden": hidden})
	})
}

type MallCategoryInput struct {
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	Icon   string `json:"icon"`
	Sort   int    `json:"sort"`
	Active bool   `json:"active"`
}

// AdminSaveMallCategory renames, reorders or switches off one of the fixed categories.
func (a *App) AdminSaveMallCategory(ctx context.Context, actor AdminActor, id int64, in MallCategoryInput) error {
	in.Name, in.NameEn, in.Icon = strings.TrimSpace(in.Name), strings.TrimSpace(in.NameEn), strings.TrimSpace(in.Icon)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 12 || utf8.RuneCountInString(in.NameEn) > 24 || utf8.RuneCountInString(in.Icon) > 4 {
		return ErrValidation("a category needs a name of at most 12 characters")
	}
	if in.NameEn == "" {
		in.NameEn = in.Name
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE store_categories SET name=$2, name_en=$3, icon=$4, sort=$5, active=$6 WHERE id=$1`,
			id, in.Name, in.NameEn, in.Icon, in.Sort, in.Active)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return adminAudit(ctx, tx, actor, "mall.category", "store_category", id, map[string]any{"name": in.Name, "active": in.Active})
	})
}

type MallBannerInput struct {
	Title      string `json:"title"`
	TitleEn    string `json:"title_en"`
	Subtitle   string `json:"subtitle"`
	SubtitleEn string `json:"subtitle_en"`
	LinkSlug   string `json:"link_slug"`
	LinkURL    string `json:"link_url"`
	Sort       int    `json:"sort"`
	Active     bool   `json:"active"`
}

func (in *MallBannerInput) check(ctx context.Context, q Querier) error {
	for _, p := range []*string{&in.Title, &in.TitleEn, &in.Subtitle, &in.SubtitleEn, &in.LinkSlug, &in.LinkURL} {
		*p = strings.TrimSpace(*p)
	}
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 30 || utf8.RuneCountInString(in.TitleEn) > 60 {
		return ErrValidation("a banner needs a title of at most 30 characters")
	}
	if utf8.RuneCountInString(in.Subtitle) > 60 || utf8.RuneCountInString(in.SubtitleEn) > 120 {
		return ErrValidation("the second line must be at most 60 characters")
	}
	if in.LinkSlug != "" {
		in.LinkURL = ""
		var ok bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM branches WHERE slug=$1 AND deleted_at IS NULL)`, in.LinkSlug).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrValidation("there is no store with that link name")
		}
	}
	if in.LinkURL != "" {
		u, err := url.Parse(in.LinkURL)
		// a page on this site, or an https address
		if err != nil || len(in.LinkURL) > 300 || !(strings.HasPrefix(in.LinkURL, "/") && !strings.HasPrefix(in.LinkURL, "//") || u.Scheme == "https" && u.Host != "") {
			return ErrValidation("the link must start with / or https://")
		}
	}
	return nil
}

// AdminSaveBanner adds a banner (id 0) or changes one.
func (a *App) AdminSaveBanner(ctx context.Context, actor AdminActor, id int64, in MallBannerInput) (int64, error) {
	if err := in.check(ctx, a.DB); err != nil {
		return 0, err
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if id == 0 {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM mall_banners`).Scan(&n); err != nil {
				return err
			}
			if n >= maxMallBanners {
				return ErrValidation("at most 5 banners; delete one first")
			}
			if err := tx.QueryRow(ctx, `INSERT INTO mall_banners (title, title_en, subtitle, subtitle_en, link_slug, link_url, sort, active)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, in.Title, in.TitleEn, in.Subtitle, in.SubtitleEn, in.LinkSlug, in.LinkURL, in.Sort, in.Active).Scan(&id); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE mall_banners SET title=$2, title_en=$3, subtitle=$4, subtitle_en=$5, link_slug=$6, link_url=$7, sort=$8, active=$9
				WHERE id=$1`, id, in.Title, in.TitleEn, in.Subtitle, in.SubtitleEn, in.LinkSlug, in.LinkURL, in.Sort, in.Active)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		return adminAudit(ctx, tx, actor, "mall.banner", "mall_banner", id, map[string]any{"title": in.Title})
	})
	return id, err
}

// AdminSetBannerImage replaces a banner's picture; nil takes it away.
func (a *App) AdminSetBannerImage(ctx context.Context, actor AdminActor, id int64, image []byte) error {
	token := ""
	if image != nil {
		if !isJPEG(image, ImageMaxBytes) {
			return ErrValidation("the picture must be a JPEG of at most 1 MB")
		}
		tok := make([]byte, 12)
		if _, err := rand.Read(tok); err != nil {
			return err
		}
		token = hex.EncodeToString(tok)
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE mall_banners SET image=$2, image_token=$3 WHERE id=$1`, id, image, token)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return adminAudit(ctx, tx, actor, "mall.banner_image", "mall_banner", id, map[string]any{"set": image != nil})
	})
}

func (a *App) AdminDeleteBanner(ctx context.Context, actor AdminActor, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM mall_banners WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return adminAudit(ctx, tx, actor, "mall.banner_delete", "mall_banner", id, nil)
	})
}
