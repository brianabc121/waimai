package app_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

var (
	tinyJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{7}, 64)...)
	tinyGIF  = append([]byte("GIF89a"), bytes.Repeat([]byte{1}, 32)...)
)

// fakeMP4 is not a playable video, only something that starts like one and is
// large enough to be read in several pieces.
func fakeMP4(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	copy(b, []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'})
	return b
}

func TestPromoMedia(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp

	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Media) != 0 {
		t.Fatalf("a new store shows %d media", len(sf.Media))
	}

	_, err = a.AddMedia(ctx, s.branchID, []byte("<html>not a picture</html>"), app.MediaInput{})
	wantCode(t, err, "VALIDATION")
	_, err = a.AddMedia(ctx, s.branchID, tinyJPEG, app.MediaInput{Crop: &app.Crop{X: 0.5, Y: 0, W: 0.8, H: 1}})
	wantCode(t, err, "VALIDATION")

	title := "招牌盐酥鸡"
	img, err := a.AddMedia(ctx, s.branchID, tinyJPEG, app.MediaInput{Title: &title, Crop: &app.Crop{X: 0.1, Y: 0.2, W: 0.8, H: 0.6}})
	noErr(t, err)
	if img.Kind != "image" || img.Title != title || img.Crop.W < 0.79 || img.PosterURL != "" || !img.Active {
		t.Fatalf("image = %+v", img)
	}
	gif, err := a.AddMedia(ctx, s.branchID, tinyGIF, app.MediaInput{})
	noErr(t, err)
	start, end := 1.5, 6.0
	video := fakeMP4(3<<20 + 123)
	vid, err := a.AddMedia(ctx, s.branchID, video, app.MediaInput{TrimStart: &start, TrimEnd: &end})
	noErr(t, err)
	if gif.Kind != "gif" || vid.Kind != "video" || vid.TrimEnd != 6 || vid.Crop.W != 1 {
		t.Fatalf("gif = %+v, video = %+v", gif, vid)
	}

	// The file comes back whole, and any part of it on its own.
	f, err := a.OpenMedia(ctx, vid.URL[len("/api/v1/media/"):])
	noErr(t, err)
	got, err := io.ReadAll(f)
	noErr(t, err)
	if f.Mime != "video/mp4" || !bytes.Equal(got, video) {
		t.Fatalf("read %d bytes of %d as %s", len(got), len(video), f.Mime)
	}
	_, err = f.Seek(-1000, io.SeekEnd)
	noErr(t, err)
	tail, err := io.ReadAll(f)
	noErr(t, err)
	if !bytes.Equal(tail, video[len(video)-1000:]) {
		t.Fatal("the last 1000 bytes differ")
	}
	_, err = a.OpenMedia(ctx, "nope")
	wantCode(t, err, "NOT_FOUND")

	// A cover for the video.
	_, err = a.SetMediaPoster(ctx, s.branchID, vid.ID, []byte("png?"))
	wantCode(t, err, "VALIDATION")
	vid, err = a.SetMediaPoster(ctx, s.branchID, vid.ID, tinyJPEG)
	noErr(t, err)
	if vid.PosterURL == "" {
		t.Fatal("no poster address")
	}

	// Order, hide, edit.
	list, err := a.ReorderMedia(ctx, s.branchID, []int64{vid.ID, img.ID})
	noErr(t, err)
	if len(list) != 3 || list[0].ID != vid.ID || list[1].ID != img.ID || list[2].ID != gif.ID {
		t.Fatalf("order = %+v", list)
	}
	off := false
	_, err = a.UpdateMedia(ctx, s.branchID, gif.ID, app.MediaInput{Active: &off})
	noErr(t, err)
	img, err = a.UpdateMedia(ctx, s.branchID, img.ID, app.MediaInput{Crop: &app.Crop{X: 0, Y: 0, W: 1, H: 0.5}})
	noErr(t, err)
	if img.Title != title || img.Crop.H != 0.5 {
		t.Fatalf("after the edit: %+v", img)
	}
	sf, err = a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Media) != 2 || sf.Media[0].ID != vid.ID || sf.Media[1].ID != img.ID {
		t.Fatalf("customers see %+v", sf.Media)
	}

	// Another store cannot touch them.
	other := newStore(t, false)
	_, err = a.UpdateMedia(ctx, other.branchID, img.ID, app.MediaInput{Active: &off})
	wantCode(t, err, "NOT_FOUND")
	wantCode(t, a.DeleteMedia(ctx, other.branchID, img.ID), "NOT_FOUND")

	// At most eight.
	for i := 3; i < app.MediaMaxPerStore; i++ {
		_, err = a.AddMedia(ctx, s.branchID, tinyJPEG, app.MediaInput{})
		noErr(t, err)
	}
	_, err = a.AddMedia(ctx, s.branchID, tinyJPEG, app.MediaInput{})
	wantCode(t, err, "MEDIA_FULL")
	noErr(t, a.DeleteMedia(ctx, s.branchID, gif.ID))
	_, err = a.AddMedia(ctx, s.branchID, tinyJPEG, app.MediaInput{})
	noErr(t, err)
}
