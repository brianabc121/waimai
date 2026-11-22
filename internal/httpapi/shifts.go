package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// ---- attendance ----

func (s *Server) shiftState(c *gin.Context) {
	st, err := s.App.ShiftState(c, currentUser(c).ID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, st)
}

// setShift clocks the signed-in person on or off. The position is optional:
// it only tells the manager whether the switch was used at the store.
func (s *Server) setShift(c *gin.Context) {
	var in struct {
		OnDuty   *bool    `json:"on_duty"`
		BranchID int64    `json:"branch_id"`
		Lat      *float64 `json:"lat"`
		Lng      *float64 `json:"lng"`
	}
	if !bind(c, &in) {
		return
	}
	if in.OnDuty == nil {
		fail(c, app.ErrValidation("on_duty is required"))
		return
	}
	var pos *app.LatLng
	if in.Lat != nil && in.Lng != nil {
		pos = &app.LatLng{*in.Lat, *in.Lng}
	}
	st, err := s.App.SetShift(c, currentUser(c), in.BranchID, *in.OnDuty, sessionToken(c), pos)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, st)
}

func (s *Server) attendance(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	out, err := s.App.Attendance(c, bid, c.Query("from"), c.Query("to"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

func (s *Server) addShift(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in app.ShiftInput
	if !bind(c, &in) {
		return
	}
	sh, err := s.App.AddShift(c, bid, in, currentUser(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, sh)
}

func (s *Server) editShift(c *gin.Context) {
	bid, id, ok := staffScope(c)
	if !ok {
		return
	}
	var in app.ShiftInput
	if !bind(c, &in) {
		return
	}
	sh, err := s.App.EditShift(c, bid, id, in, currentUser(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, sh)
}
