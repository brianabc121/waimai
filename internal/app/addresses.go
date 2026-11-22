package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// MaxAddresses is how many addresses one customer may keep.
const MaxAddresses = 20

// Address is one entry of a customer's address book.
type Address struct {
	ID          int64  `json:"id"`
	ContactName string `json:"contact_name"`
	// Title is "", "mr" or "ms".
	Title string `json:"title"`
	Phone string `json:"phone"`
	// PlaceName is the building or landmark picked on the map (or the street
	// typed by hand where there is no map); PlaceAddress its street address.
	PlaceName    string `json:"place_name"`
	PlaceAddress string `json:"place_address"`
	// Lat/Lng is the pin. The delivery area is decided from it, not from the name.
	Lat      *float64 `json:"lat"`
	Lng      *float64 `json:"lng"`
	Unit     string   `json:"unit"`
	Note     string   `json:"note"`
	Wechat   string   `json:"wechat"`
	Telegram string   `json:"telegram"`
	// Label is "", "home", "work" or "school".
	Label      string     `json:"label"`
	IsDefault  bool       `json:"is_default"`
	LastUsedAt *time.Time `json:"last_used_at"`
	// PhotoAt is when the photo of the door was added, nil when there is none.
	PhotoAt *time.Time `json:"photo_at"`
	// Delivery says whether the store asked about delivers here. It is absent
	// when no store was named or the store does not decide by the pin.
	Delivery *DeliveryTerms `json:"delivery,omitempty"`
}

// DeliveryTerms is what a customer is told about delivery to one spot: whether
// it is possible and what it costs. The store's areas, their names and how far
// a spot is from them stay with the store.
type DeliveryTerms struct {
	Deliverable bool  `json:"deliverable"`
	Fee         int64 `json:"fee"`
	MinOrder    int64 `json:"min_order"`
	EtaMinutes  *int  `json:"eta_minutes"`
}

func termsOf(b *Branch, loc *Location) *DeliveryTerms {
	if loc.Zone == nil {
		return &DeliveryTerms{}
	}
	return &DeliveryTerms{Deliverable: true, Fee: loc.Zone.Fee, MinOrder: max(b.MinOrderAmount, loc.Zone.MinOrder),
		EtaMinutes: loc.Zone.EtaMinutes}
}

// DeliveryTermsAt answers a customer's "do you deliver here?".
func (a *App) DeliveryTermsAt(ctx context.Context, slug string, lat, lng float64) (*DeliveryTerms, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	loc, err := locate(ctx, a.DB, b, LatLng{lat, lng})
	if err != nil {
		return nil, err
	}
	return termsOf(b, loc), nil
}

const addressCols = `id, contact_name, title, phone, place_name, place_address, lat, lng, unit, note, wechat, telegram,
	label, is_default, last_used_at, photo_at`

func scanAddress(row pgx.Row) (*Address, error) {
	var a Address
	err := row.Scan(&a.ID, &a.ContactName, &a.Title, &a.Phone, &a.PlaceName, &a.PlaceAddress, &a.Lat, &a.Lng, &a.Unit,
		&a.Note, &a.Wechat, &a.Telegram, &a.Label, &a.IsDefault, &a.LastUsedAt, &a.PhotoAt)
	return &a, err
}

var errAddressNotFound = Err(http.StatusNotFound, "ADDRESS_NOT_FOUND", "this address no longer exists")

// ListAddresses returns the customer's addresses: the default first, then the
// most recently used. With a store, each address that has a pin also says
// whether that store delivers to it.
func (a *App) ListAddresses(ctx context.Context, customerID int64, slug string) ([]Address, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+addressCols+` FROM customer_addresses
		WHERE customer_id=$1 AND deleted_at IS NULL
		ORDER BY is_default DESC, last_used_at DESC NULLS LAST, id DESC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Address{}
	for rows.Next() {
		ad, err := scanAddress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ad)
	}
	if err := rows.Err(); err != nil || slug == "" {
		return out, err
	}
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	if b.ZoneMode != "map" {
		return out, nil
	}
	zones, err := loadZones(ctx, a.DB, b.ID, true)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Lat == nil || out[i].Lng == nil {
			// Without a pin this store cannot tell; the customer has to place one.
			out[i].Delivery = &DeliveryTerms{}
			continue
		}
		loc := &Location{}
		if hit, _ := matchZone(zones, LatLng{*out[i].Lat, *out[i].Lng}, b.ZoneToleranceM); hit != nil {
			loc.Zone = hit.Zone
		}
		out[i].Delivery = termsOf(b, loc)
	}
	return out, nil
}

// AddressInput is what the address form sends.
type AddressInput struct {
	ContactName  string   `json:"contact_name"`
	Title        string   `json:"title"`
	Phone        string   `json:"phone"`
	PlaceName    string   `json:"place_name"`
	PlaceAddress string   `json:"place_address"`
	Lat          *float64 `json:"lat"`
	Lng          *float64 `json:"lng"`
	Unit         string   `json:"unit"`
	Note         string   `json:"note"`
	Wechat       string   `json:"wechat"`
	Telegram     string   `json:"telegram"`
	Label        string   `json:"label"`
	IsDefault    bool     `json:"is_default"`
}

func (in *AddressInput) check() error {
	for _, f := range []*string{&in.ContactName, &in.PlaceName, &in.PlaceAddress, &in.Unit, &in.Note, &in.Wechat, &in.Telegram} {
		*f = strings.TrimSpace(*f)
	}
	phone, err := normalizeCustomerPhone(in.Phone)
	if err != nil {
		return err
	}
	in.Phone = phone
	n := func(s string) int { return len([]rune(s)) }
	switch {
	case in.ContactName == "" || n(in.ContactName) > 80:
		return ErrValidation("contact_name is required (at most 80 characters)")
	case in.Title != "" && in.Title != "mr" && in.Title != "ms":
		return ErrValidation("title must be mr or ms")
	case in.PlaceName == "" || n(in.PlaceName) > 200:
		return ErrValidation("place_name is required (at most 200 characters)")
	case n(in.PlaceAddress) > 300:
		return ErrValidation("place_address is too long")
	case in.Unit == "" || n(in.Unit) > 100:
		return ErrValidation("unit is required (at most 100 characters)")
	case n(in.Note) > 300:
		return ErrValidation("note is too long")
	case n(in.Wechat) > 64 || n(in.Telegram) > 64:
		return ErrValidation("wechat and telegram are at most 64 characters")
	case in.Label != "" && in.Label != "home" && in.Label != "work" && in.Label != "school":
		return ErrValidation("label must be home, work or school")
	case (in.Lat == nil) != (in.Lng == nil):
		return ErrValidation("lat and lng go together")
	case in.Lat != nil && !validLatLng(LatLng{*in.Lat, *in.Lng}):
		return ErrValidation("invalid coordinates")
	}
	return nil
}

// SaveAddress adds an address (id 0) or changes one of the customer's own.
// The first address, and one saved as the default, becomes the default.
func (a *App) SaveAddress(ctx context.Context, customerID, id int64, in AddressInput) (*Address, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	var out *Address
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		// One customer's changes go one at a time, so the count and the single default hold.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM customers WHERE id=$1 FOR UPDATE`, customerID); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM customer_addresses WHERE customer_id=$1 AND deleted_at IS NULL`,
			customerID).Scan(&count); err != nil {
			return err
		}
		if id == 0 && count >= MaxAddresses {
			return ErrD(http.StatusConflict, "ADDRESS_LIMIT", "the address book is full", map[string]any{"limit": MaxAddresses})
		}
		def := in.IsDefault || (id == 0 && count == 0)
		if def {
			if _, err := tx.Exec(ctx, `UPDATE customer_addresses SET is_default=false
				WHERE customer_id=$1 AND is_default AND deleted_at IS NULL AND id<>$2`, customerID, id); err != nil {
				return err
			}
		}
		var err error
		if id == 0 {
			out, err = scanAddress(tx.QueryRow(ctx, `INSERT INTO customer_addresses (customer_id, contact_name, title, phone,
				place_name, place_address, lat, lng, unit, note, wechat, telegram, label, is_default)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING `+addressCols,
				customerID, in.ContactName, in.Title, in.Phone, in.PlaceName, in.PlaceAddress, in.Lat, in.Lng, in.Unit,
				in.Note, in.Wechat, in.Telegram, in.Label, def))
			return err
		}
		out, err = scanAddress(tx.QueryRow(ctx, `UPDATE customer_addresses SET contact_name=$3, title=$4, phone=$5,
			place_name=$6, place_address=$7, lat=$8, lng=$9, unit=$10, note=$11, wechat=$12, telegram=$13, label=$14,
			is_default=$15, updated_at=now()
			WHERE id=$2 AND customer_id=$1 AND deleted_at IS NULL RETURNING `+addressCols,
			customerID, id, in.ContactName, in.Title, in.Phone, in.PlaceName, in.PlaceAddress, in.Lat, in.Lng, in.Unit,
			in.Note, in.Wechat, in.Telegram, in.Label, def))
		if isNoRows(err) {
			return errAddressNotFound
		}
		return err
	})
	return out, err
}

// DeleteAddress removes an address from the book. Orders sent to it keep their copy.
func (a *App) DeleteAddress(ctx context.Context, customerID, id int64) error {
	tag, err := a.DB.Exec(ctx, `UPDATE customer_addresses SET deleted_at=now(), is_default=false
		WHERE id=$1 AND customer_id=$2 AND deleted_at IS NULL`, id, customerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errAddressNotFound
	}
	return nil
}

// ---- photo of the door ----

func isJPEG(image []byte, max int) bool {
	return len(image) >= 4 && len(image) <= max && image[0] == 0xFF && image[1] == 0xD8 && image[2] == 0xFF
}

// SaveAddressPhoto keeps a photo of the door for the rider. A second photo
// replaces the first.
func (a *App) SaveAddressPhoto(ctx context.Context, customerID, id int64, image []byte) (*Address, error) {
	if !isJPEG(image, ProofMaxBytes) {
		return nil, ErrValidation("the photo must be a JPEG of at most 600 KB")
	}
	var out *Address
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanAddress(tx.QueryRow(ctx, `UPDATE customer_addresses SET photo_at=now(), updated_at=now()
			WHERE id=$1 AND customer_id=$2 AND deleted_at IS NULL RETURNING `+addressCols, id, customerID))
		if isNoRows(err) {
			return errAddressNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO customer_address_photos (address_id, image) VALUES ($1,$2)
			ON CONFLICT (address_id) DO UPDATE SET image=EXCLUDED.image, created_at=now()`, id, image)
		return err
	})
	return out, err
}

// DeleteAddressPhoto takes the photo away again.
func (a *App) DeleteAddressPhoto(ctx context.Context, customerID, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE customer_addresses SET photo_at=NULL, updated_at=now()
			WHERE id=$1 AND customer_id=$2 AND deleted_at IS NULL`, id, customerID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errAddressNotFound
		}
		_, err = tx.Exec(ctx, `DELETE FROM customer_address_photos WHERE address_id=$1`, id)
		return err
	})
}

var errNoPhoto = Err(http.StatusNotFound, "NOT_FOUND", "there is no photo")

func photoBy(ctx context.Context, q Querier, query string, args ...any) ([]byte, error) {
	var image []byte
	err := q.QueryRow(ctx, query, args...).Scan(&image)
	if isNoRows(err) {
		return nil, errNoPhoto
	}
	return image, err
}

// AddressPhoto returns the photo to the customer who owns the address.
func (a *App) AddressPhoto(ctx context.Context, customerID, id int64) ([]byte, error) {
	return photoBy(ctx, a.DB, `SELECT p.image FROM customer_address_photos p JOIN customer_addresses ad ON ad.id = p.address_id
		WHERE ad.id=$1 AND ad.customer_id=$2 AND ad.deleted_at IS NULL`, id, customerID)
}

// OrderAddressPhoto returns the door photo of the address an order goes to, for the store.
func (a *App) OrderAddressPhoto(ctx context.Context, orderID int64) ([]byte, error) {
	return photoBy(ctx, a.DB, `SELECT p.image FROM customer_address_photos p JOIN orders o ON o.address_id = p.address_id
		WHERE o.id=$1`, orderID)
}

// RiderAddressPhoto returns it to the rider carrying the order, and only until
// the delivery is over.
func (a *App) RiderAddressPhoto(ctx context.Context, taskID, riderID int64) ([]byte, error) {
	return photoBy(ctx, a.DB, `SELECT p.image FROM customer_address_photos p JOIN orders o ON o.address_id = p.address_id
		JOIN delivery_tasks t ON t.order_id = o.id
		WHERE t.id=$1 AND t.rider_id=$2 AND t.status NOT IN ('delivered', 'failed')`, taskID, riderID)
}

// addressOf loads an address for the customer who owns phone.
func addressOf(ctx context.Context, q Querier, phone string, id int64) (*Address, error) {
	if phone == "" {
		return nil, Err(http.StatusForbidden, "PHONE_NOT_VERIFIED", "log in to use a saved address")
	}
	ad, err := scanAddress(q.QueryRow(ctx, `SELECT `+addressCols+` FROM customer_addresses
		WHERE id=$1 AND deleted_at IS NULL AND customer_id=(SELECT id FROM customers WHERE phone=$2)`, id, phone))
	if isNoRows(err) {
		return nil, errAddressNotFound
	}
	return ad, err
}

// line is the address as one line of text, as staff and riders read it.
func (ad *Address) line() string {
	if ad.PlaceAddress == "" || strings.Contains(ad.PlaceAddress, ad.PlaceName) {
		if ad.PlaceAddress != "" {
			return ad.PlaceAddress
		}
		return ad.PlaceName
	}
	return ad.PlaceName + ", " + ad.PlaceAddress
}
