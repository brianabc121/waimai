package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/gin-gonic/gin"
)

// Promotional media of the store page. The files are public and never change
// under the same address, so browsers and proxies may keep them.

func (s *Server) mediaFile(c *gin.Context) {
	f, err := s.App.OpenMedia(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	h := c.Writer.Header()
	h.Set("Content-Type", f.Mime)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	// ServeContent answers range requests, which video players need.
	http.ServeContent(c.Writer, c.Request, "", f.Created, f)
}

func (s *Server) mediaPoster(c *gin.Context) {
	image, err := s.App.MediaPoster(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	// The cover is replaced when the merchant crops again.
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "image/jpeg", image)
}

func (s *Server) listMedia(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	list, err := s.App.ListMedia(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"media": list, "max": app.MediaMaxPerStore})
}

// uploadMedia takes the file as the request body and its title, crop and
// trim as JSON in the "meta" query parameter.
func (s *Server) uploadMedia(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in app.MediaInput
	if meta := c.Query("meta"); meta != "" {
		if err := json.Unmarshal([]byte(meta), &in); err != nil {
			fail(c, app.ErrValidation("invalid meta"))
			return
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, app.MediaVideoMax+1))
	if err != nil || len(body) > app.MediaVideoMax {
		fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the file is too large"))
		return
	}
	m, err := s.App.AddMedia(c, bid, body, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

func (s *Server) mediaScope(c *gin.Context) (bid, id int64, ok bool) {
	if bid, ok = branchScope(c, managerRoles); !ok {
		return
	}
	id, ok = idParam(c, "id")
	return
}

func (s *Server) updateMedia(c *gin.Context) {
	bid, id, ok := s.mediaScope(c)
	if !ok {
		return
	}
	var in app.MediaInput
	if !bind(c, &in) {
		return
	}
	m, err := s.App.UpdateMedia(c, bid, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

func (s *Server) uploadMediaPoster(c *gin.Context) {
	bid, id, ok := s.mediaScope(c)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, app.ProofMaxBytes+1))
	if err != nil || len(body) > app.ProofMaxBytes {
		fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the cover is larger than 600 KB"))
		return
	}
	m, err := s.App.SetMediaPoster(c, bid, id, body)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

func (s *Server) deleteMedia(c *gin.Context) {
	bid, id, ok := s.mediaScope(c)
	if !ok {
		return
	}
	if err := s.App.DeleteMedia(c, bid, id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) reorderMedia(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !bind(c, &in) {
		return
	}
	list, err := s.App.ReorderMedia(c, bid, in.IDs)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"media": list})
}

// ---- pictures of dishes, the logo and the cover ----

func (s *Server) image(c *gin.Context) {
	image, err := s.App.Image(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "image/jpeg", image)
}

func (s *Server) saveImage(c *gin.Context, bid int64) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, app.ImageMaxBytes+1))
	if err != nil || len(body) > app.ImageMaxBytes {
		fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the picture is larger than 1 MB"))
		return
	}
	url, err := s.App.AddImage(c, bid, body)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"url": url})
}

func (s *Server) uploadImage(c *gin.Context) {
	if bid, ok := branchScope(c, managerRoles); ok {
		s.saveImage(c, bid)
	}
}

// adminUploadImage lets the platform back office add a picture to a dish of any store.
func (s *Server) adminUploadImage(c *gin.Context) {
	if bid, ok := idParam(c, "bid"); ok {
		s.saveImage(c, bid)
	}
}
