package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// The menu page drags categories and dishes into a new order, and a dish
// dragged under another category moves there.
func TestReorderMenu(t *testing.T) {
	need(t)
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp

	menu, err := a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	c1 := menu.Categories[0].ID
	c2, err := a.SaveCategory(ctx, s.branchID, 0, app.CategoryInput{Name: "Drinks", Sort: 1})
	noErr(t, err)

	// something missing or listed twice is refused
	for _, bad := range [][]app.MenuOrderCategory{
		{{ID: c2}, {ID: c1, ItemIDs: []int64{s.chicken, s.tea, s.water}}},
		{{ID: c2, ItemIDs: []int64{s.tea}}, {ID: c1, ItemIDs: []int64{s.chicken, s.tea, s.water, s.limited}}},
		{{ID: c1, ItemIDs: []int64{s.chicken, s.tea, s.water, s.limited}}},
	} {
		if err := a.ReorderMenu(ctx, s.branchID, bad); err == nil {
			t.Fatalf("order %+v was taken", bad)
		}
	}
	// another store's category is refused
	other := newStore(t, false)
	om, err := a.AdminMenu(ctx, other.branchID)
	noErr(t, err)
	if err := a.ReorderMenu(ctx, s.branchID, []app.MenuOrderCategory{{ID: om.Categories[0].ID, ItemIDs: []int64{s.chicken, s.tea, s.water, s.limited}}, {ID: c2}}); err == nil {
		t.Fatal("another store's category was taken")
	}

	noErr(t, a.ReorderMenu(ctx, s.branchID, []app.MenuOrderCategory{
		{ID: c2, ItemIDs: []int64{s.water, s.tea}},
		{ID: c1, ItemIDs: []int64{s.limited, s.chicken}},
	}))
	menu, err = a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	got := [][]int64{}
	for _, c := range menu.Categories {
		ids := []int64{c.ID}
		for _, it := range c.Items {
			ids = append(ids, it.ID)
		}
		got = append(got, ids)
	}
	want := [][]int64{{c2, s.water, s.tea}, {c1, s.limited, s.chicken}}
	if len(got) != 2 || len(got[0]) != 3 || len(got[1]) != 3 {
		t.Fatalf("menu order %v, want %v", got, want)
	}
	for i := range want {
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("menu order %v, want %v", got, want)
			}
		}
	}
}
