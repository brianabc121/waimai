package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Promotional media: the pictures, GIFs and short videos that play at the top
// of the customer's store page. A store without any shows nothing there.

const (
	MediaMaxPerStore = 8
	MediaImageMax    = 2 << 20
	MediaGIFMax      = 8 << 20
	MediaVideoMax    = 20 << 20
)

// Crop is the part of the picture to show, in fractions of its width and height.
type Crop struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func (c Crop) valid() bool {
	const eps = 1e-3
	return c.X >= 0 && c.Y >= 0 && c.W >= 0.02 && c.H >= 0.02 && c.X+c.W <= 1+eps && c.Y+c.H <= 1+eps
}

// Media is one promotional picture, GIF or video.
type Media struct {
	ID        int64   `json:"id"`
	Kind      string  `json:"kind"` // image, gif or video
	URL       string  `json:"url"`
	PosterURL string  `json:"poster_url"` // empty when there is none
	Title     string  `json:"title"`
	Crop      Crop    `json:"crop"`
	TrimStart float64 `json:"trim_start"`
	TrimEnd   float64 `json:"trim_end"`
	// The rest is for the merchant only.
	Active    bool      `json:"active"`
	Size      int       `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// MediaInput is what the merchant sets on a medium besides the file itself.
type MediaInput struct {
	Title     *string  `json:"title"`
	Crop      *Crop    `json:"crop"`
	TrimStart *float64 `json:"trim_start"`
	TrimEnd   *float64 `json:"trim_end"`
	Active    *bool    `json:"active"`
}

func (in *MediaInput) check() error {
	if in.Title != nil {
		*in.Title = strings.TrimSpace(*in.Title)
		if utf8.RuneCountInString(*in.Title) > 40 {
			return ErrValidation("the title is at most 40 characters")
		}
	}
	if in.Crop != nil && !in.Crop.valid() {
		return ErrValidation("the crop does not fit in the picture")
	}
	s, e := 0.0, 0.0
	if in.TrimStart != nil {
		s = *in.TrimStart
	}
	if in.TrimEnd != nil {
		e = *in.TrimEnd
	}
	if s < 0 || e < 0 || s > 3600 || e > 3600 || (in.TrimStart != nil && in.TrimEnd != nil && e != 0 && e-s < 0.5) {
		return ErrValidation("the part of the video to play is not valid")
	}
	return nil
}

// sniffMedia tells what a file is from its first bytes; what the browser
// claims it to be is not trusted.
func sniffMedia(b []byte) (kind, mime string) {
	switch {
	case len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image", "image/jpeg"
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image", "image/png"
	case len(b) > 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image", "image/webp"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "gif", "image/gif"
	case bytes.HasPrefix(b, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "video", "video/webm"
	case len(b) > 12 && string(b[4:8]) == "ftyp":
		if strings.HasPrefix(string(b[8:12]), "qt") {
			return "video", "video/quicktime"
		}
		return "video", "video/mp4"
	}
	return "", ""
}

var mediaLimit = map[string]int{"image": MediaImageMax, "gif": MediaGIFMax, "video": MediaVideoMax}

const mediaCols = `id, kind, token, poster IS NOT NULL, title, crop_x, crop_y, crop_w, crop_h, trim_start, trim_end, active, size, created_at`

func scanMedia(row pgx.Row) (*Media, error) {
	var m Media
	var token string
	var poster bool
	err := row.Scan(&m.ID, &m.Kind, &token, &poster, &m.Title, &m.Crop.X, &m.Crop.Y, &m.Crop.W, &m.Crop.H,
		&m.TrimStart, &m.TrimEnd, &m.Active, &m.Size, &m.CreatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.URL = "/api/v1/media/" + token
	if poster {
		m.PosterURL = m.URL + "/poster"
	}
	return &m, nil
}

func listMedia(ctx context.Context, q Querier, branchID int64, activeOnly bool) ([]Media, error) {
	rows, err := q.Query(ctx, `SELECT `+mediaCols+` FROM store_media WHERE branch_id=$1 AND (active OR NOT $2)
		ORDER BY sort, id`, branchID, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *m)
	}
	return list, rows.Err()
}

// ListMedia returns everything the merchant uploaded, hidden ones included.
func (a *App) ListMedia(ctx context.Context, branchID int64) ([]Media, error) {
	return listMedia(ctx, a.DB, branchID, false)
}

// AddMedia stores an uploaded picture, GIF or video at the end of the list.
func (a *App) AddMedia(ctx context.Context, branchID int64, content []byte, in MediaInput) (*Media, error) {
	kind, mime := sniffMedia(content)
	if kind == "" {
		return nil, ErrValidation("upload a JPEG, PNG, WebP or GIF picture, or an MP4, MOV or WebM video")
	}
	if len(content) > mediaLimit[kind] {
		return nil, Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the file is too large")
	}
	if err := in.check(); err != nil {
		return nil, err
	}
	crop := Crop{0, 0, 1, 1}
	if in.Crop != nil {
		crop = *in.Crop
	}
	title, start, end := "", 0.0, 0.0
	if in.Title != nil {
		title = *in.Title
	}
	if kind == "video" {
		if in.TrimStart != nil {
			start = *in.TrimStart
		}
		if in.TrimEnd != nil {
			end = *in.TrimEnd
		}
	}
	tok := make([]byte, 12)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}
	var out *Media
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		// One store's uploads are counted one at a time.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM branches WHERE id=$1 FOR UPDATE`, branchID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM store_media WHERE branch_id=$1`, branchID).Scan(&n); err != nil {
			return err
		}
		if n >= MediaMaxPerStore {
			return Err(http.StatusConflict, "MEDIA_FULL", "a store can have at most 8 promotional pictures and videos")
		}
		var err error
		out, err = scanMedia(tx.QueryRow(ctx, `INSERT INTO store_media
			(branch_id, token, kind, mime, content, size, title, crop_x, crop_y, crop_w, crop_h, trim_start, trim_end, sort)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,
				(SELECT coalesce(max(sort), -1) + 1 FROM store_media WHERE branch_id=$1))
			RETURNING `+mediaCols, branchID, hex.EncodeToString(tok), kind, mime, content, len(content), title,
			crop.X, crop.Y, crop.W, crop.H, start, end))
		return err
	})
	return out, err
}

// UpdateMedia changes the title, the crop, the part of a video that plays, or
// whether customers see it. Fields left out stay as they are.
func (a *App) UpdateMedia(ctx context.Context, branchID, id int64, in MediaInput) (*Media, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	var x, y, w, h *float64
	if in.Crop != nil {
		x, y, w, h = &in.Crop.X, &in.Crop.Y, &in.Crop.W, &in.Crop.H
	}
	return scanMedia(a.DB.QueryRow(ctx, `UPDATE store_media SET title=coalesce($3, title),
		crop_x=coalesce($4, crop_x), crop_y=coalesce($5, crop_y), crop_w=coalesce($6, crop_w), crop_h=coalesce($7, crop_h),
		trim_start=coalesce($8, trim_start), trim_end=coalesce($9, trim_end), active=coalesce($10, active)
		WHERE id=$1 AND branch_id=$2 RETURNING `+mediaCols, id, branchID, in.Title, x, y, w, h, in.TrimStart, in.TrimEnd, in.Active))
}

// SetMediaPoster keeps the still picture shown before a video plays.
func (a *App) SetMediaPoster(ctx context.Context, branchID, id int64, image []byte) (*Media, error) {
	if !isJPEG(image, ProofMaxBytes) {
		return nil, ErrValidation("the cover must be a JPEG of at most 600 KB")
	}
	return scanMedia(a.DB.QueryRow(ctx, `UPDATE store_media SET poster=$3 WHERE id=$1 AND branch_id=$2
		RETURNING `+mediaCols, id, branchID, image))
}

func (a *App) DeleteMedia(ctx context.Context, branchID, id int64) error {
	tag, err := a.DB.Exec(ctx, `DELETE FROM store_media WHERE id=$1 AND branch_id=$2`, id, branchID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ReorderMedia puts the store's media in the order of ids; any left out
// follow in their old order.
func (a *App) ReorderMedia(ctx context.Context, branchID int64, ids []int64) ([]Media, error) {
	_, err := a.DB.Exec(ctx, `UPDATE store_media m SET sort = o.pos FROM (
			SELECT id, row_number() OVER (ORDER BY coalesce(array_position($2::bigint[], id), 2147483647), sort, id) AS pos
			FROM store_media WHERE branch_id=$1) o
		WHERE m.id = o.id`, branchID, ids)
	if err != nil {
		return nil, err
	}
	return a.ListMedia(ctx, branchID)
}

// MediaFile is a stored file that is read from the database a piece at a
// time, so that a browser can ask for any part of a video.
type MediaFile struct {
	Mime    string
	Size    int64
	Created time.Time
	ctx     context.Context
	q       Querier
	token   string
	pos     int64
}

// OpenMedia finds a file by the token in its address.
func (a *App) OpenMedia(ctx context.Context, token string) (*MediaFile, error) {
	f := &MediaFile{ctx: ctx, q: a.DB, token: token}
	err := a.DB.QueryRow(ctx, `SELECT mime, size, created_at FROM store_media WHERE token=$1`, token).Scan(&f.Mime, &f.Size, &f.Created)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return f, err
}

func (f *MediaFile) Read(p []byte) (int, error) {
	if f.pos >= f.Size {
		return 0, io.EOF
	}
	n := min(int64(len(p)), f.Size-f.pos, 1<<20)
	var chunk []byte
	if err := f.q.QueryRow(f.ctx, `SELECT substring(content FROM $2::int FOR $3::int) FROM store_media WHERE token=$1`,
		f.token, f.pos+1, n).Scan(&chunk); err != nil {
		return 0, err
	}
	if len(chunk) == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	copy(p, chunk)
	f.pos += int64(len(chunk))
	return len(chunk), nil
}

func (f *MediaFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += f.pos
	case io.SeekEnd:
		offset += f.Size
	}
	if offset < 0 {
		return 0, ErrValidation("seek before the start")
	}
	f.pos = offset
	return offset, nil
}

// MediaPoster returns the still picture of a video.
func (a *App) MediaPoster(ctx context.Context, token string) ([]byte, error) {
	var image []byte
	err := a.DB.QueryRow(ctx, `SELECT poster FROM store_media WHERE token=$1 AND poster IS NOT NULL`, token).Scan(&image)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return image, err
}

// ---- pictures of dishes, the logo and the cover ----

// ImageMaxBytes caps an uploaded picture; the browser crops and shrinks it first.
const ImageMaxBytes = 1 << 20

const imagePath = "/api/v1/images/"

// AddImage stores a cropped JPEG and returns the address a dish or a template
// can use for it.
func (a *App) AddImage(ctx context.Context, branchID int64, image []byte) (string, error) {
	if !isJPEG(image, ImageMaxBytes) {
		return "", ErrValidation("the picture must be a JPEG of at most 1 MB")
	}
	tok := make([]byte, 12)
	if _, err := rand.Read(tok); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tok)
	tag, err := a.DB.Exec(ctx, `INSERT INTO store_images (branch_id, token, content)
		SELECT id, $2, $3 FROM branches WHERE id=$1 AND deleted_at IS NULL`, branchID, token, image)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return imagePath + token, nil
}

// Image returns an uploaded picture by the token in its address.
func (a *App) Image(ctx context.Context, token string) ([]byte, error) {
	var image []byte
	err := a.DB.QueryRow(ctx, `SELECT content FROM store_images WHERE token=$1`, token).Scan(&image)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return image, err
}

// purgeUnusedImages is part of the daily clean-up: pictures that no dish and
// no template (published or draft) points at any more, a week after upload.
const purgeUnusedImages = `DELETE FROM store_images i WHERE i.created_at < now() - interval '7 days'
	AND NOT EXISTS (SELECT 1 FROM menu_items m WHERE m.image_url = '` + imagePath + `' || i.token
		OR m.photos @> jsonb_build_array('` + imagePath + `' || i.token))
	AND NOT EXISTS (SELECT 1 FROM channel_templates t WHERE ('` + imagePath + `' || i.token) IN (
		t.published->>'logo_url', t.published->>'cover_url', t.draft->>'logo_url', t.draft->>'cover_url'))`
