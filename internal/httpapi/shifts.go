package httpapi

import (
	"strconv"

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

// myAttendance is the signed-in person's own shifts in one month.
func (s *Server) myAttendance(c *gin.Context) {
	bid, _ := strconv.ParseInt(c.Query("branch_id"), 10, 64)
	out, err := s.App.MyAttendance(c, currentUser(c), bid, c.Query("month"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
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
	if wantsXLSX(c) {
		attendanceXLSX(c, out)
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

// ---- in-store work summary ----

func (s *Server) kitchenReport(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	out, err := s.App.KitchenReport(c, bid, c.Query("from"), c.Query("to"))
	if err != nil {
		fail(c, err)
		return
	}
	if wantsXLSX(c) {
		dishes, err := s.App.KitchenDishes(c, bid, c.Query("from"), c.Query("to"))
		if err != nil {
			fail(c, err)
			return
		}
		kitchenXLSX(c, out, dishes)
		return
	}
	c.JSON(200, out)
}

// kitchenDetail is one person's work; id 0 is the shared kitchen screen.
func (s *Server) kitchenDetail(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 0 {
		fail(c, app.ErrNotFound)
		return
	}
	f := app.KitchenFilter{SlowOnly: c.Query("slow") == "1"}
	f.Page, _ = strconv.Atoi(c.Query("page"))
	if v := c.Query("station"); v != "" {
		st, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			fail(c, app.ErrValidation("station must be a number"))
			return
		}
		f.StationID = &st
	}
	out, err := s.App.KitchenDetail(c, bid, id, c.Query("from"), c.Query("to"), f)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

// ---- rider work summary ----

func (s *Server) riderReport(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	out, err := s.App.RiderReport(c, bid, c.Query("from"), c.Query("to"))
	if err != nil {
		fail(c, err)
		return
	}
	if wantsXLSX(c) {
		trips, err := s.App.TripsInPeriod(c, bid, c.Query("from"), c.Query("to"))
		if err != nil {
			fail(c, err)
			return
		}
		ridersXLSX(c, out, trips)
		return
	}
	c.JSON(200, out)
}

func (s *Server) riderDetail(c *gin.Context) {
	bid, id, ok := staffScope(c)
	if !ok {
		return
	}
	out, err := s.App.RiderDetail(c, bid, id, c.Query("from"), c.Query("to"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

func (s *Server) tripDetail(c *gin.Context) {
	bid, id, ok := staffScope(c)
	if !ok {
		return
	}
	out, err := s.App.TripDetail(c, bid, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}
