package app_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// Applications from the company website (docs/WEBSITE_DESIGN.md §8): no
// customer account, checked fields, the same phone within a day updates the
// earlier one, a cap per hour, and the platform follows up and decides.
func TestWebsiteLeads(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	actor := adminActor(t)
	_, err := a.DB.Exec(ctx, `DELETE FROM merchant_applications WHERE source='website'`)
	noErr(t, err)

	ok := app.LeadInput{StoreName: " 河马小馆 ", ContactName: "陈先生", CountryCode: "my", Country: "Malaysia",
		Phone: "+60 12-345 6789", City: "Kuala Lumpur", StoreType: "full", StoreCount: "2-5",
		Wants: []string{"dine_in", "delivery", "dine_in"}, IMKind: "wechat", IMHandle: "abc123",
		Email: "chen@example.com", Note: "想下个月开始用", Lang: "zh", Page: "/zh/demo/", Agree: true}

	bad := func(change func(*app.LeadInput)) {
		t.Helper()
		in := ok
		in.Wants = append([]string(nil), ok.Wants...)
		change(&in)
		_, err := a.SubmitLead(ctx, in)
		wantCode(t, err, "VALIDATION")
	}
	bad(func(in *app.LeadInput) { in.Agree = false })
	bad(func(in *app.LeadInput) { in.StoreName = "  " })
	bad(func(in *app.LeadInput) { in.ContactName = "" })
	bad(func(in *app.LeadInput) { in.CountryCode = "" })
	bad(func(in *app.LeadInput) { in.CountryCode = "MYS" })
	bad(func(in *app.LeadInput) { in.Phone = "12345" })
	bad(func(in *app.LeadInput) { in.Phone = "call me 12345678" })
	bad(func(in *app.LeadInput) { in.StoreType = "bar" })
	bad(func(in *app.LeadInput) { in.StoreCount = "3" })
	bad(func(in *app.LeadInput) { in.Wants = []string{"catering"} })
	bad(func(in *app.LeadInput) { in.IMKind = "qq" })
	bad(func(in *app.LeadInput) { in.IMKind = "" })
	bad(func(in *app.LeadInput) { in.Email = "not an email" })
	bad(func(in *app.LeadInput) { in.Email = "a@b" })

	id, err := a.SubmitLead(ctx, ok)
	noErr(t, err)

	// the same phone again within a day: the earlier one is updated
	again := ok
	again.StoreName, again.Wants, again.Note = "河马小馆（总店）", []string{"phone"}, ""
	id2, err := a.SubmitLead(ctx, again)
	noErr(t, err)
	if id2 != id {
		t.Fatalf("same phone made a new application: %d then %d", id, id2)
	}

	l, err := a.AdminApplications(ctx, app.ApplicationFilter{Source: "website"}, app.Page{})
	noErr(t, err)
	if l.Total != 1 || l.New != 1 || len(l.Items) != 1 {
		t.Fatalf("website applications: %+v", l)
	}
	got := l.Items[0]
	if got.StoreName != "河马小馆（总店）" || got.Phone != "+60 12-345 6789" || got.CountryCode != "MY" || got.City != "Kuala Lumpur" ||
		got.CustomerID != 0 || got.AccountPhone != "" || got.Status != "pending" || got.Lead == nil {
		t.Fatalf("application: %+v", got)
	}
	if ld := got.Lead; ld.Source != "website" || ld.StoreType != "full" || ld.StoreCount != "2-5" || fmt.Sprint(ld.Wants) != "[phone]" ||
		ld.IMKind != "wechat" || ld.IMHandle != "abc123" || ld.Email != "chen@example.com" || ld.Lang != "zh" ||
		ld.Page != "/zh/demo/" || ld.FollowUp != "new" {
		t.Fatalf("lead: %+v", ld)
	}
	// the filters
	l, err = a.AdminApplications(ctx, app.ApplicationFilter{Source: "app", FollowUp: "new"}, app.Page{})
	noErr(t, err)
	for _, x := range l.Items {
		if x.Lead.Source != "app" {
			t.Fatalf("source filter: %+v", x)
		}
	}
	_, err = a.AdminApplications(ctx, app.ApplicationFilter{Source: "mail"}, app.Page{})
	wantCode(t, err, "VALIDATION")
	_, err = a.AdminApplications(ctx, app.ApplicationFilter{FollowUp: "done"}, app.Page{})
	wantCode(t, err, "VALIDATION")

	// following up
	wantCode(t, a.AdminFollowUp(ctx, actor, id, "called", ""), "VALIDATION")
	wantCode(t, a.AdminFollowUp(ctx, actor, id+1_000_000, "contacted", ""), "NOT_FOUND")
	noErr(t, a.AdminFollowUp(ctx, actor, id, "contacted", " 微信加上了，周五演示 "))
	l, err = a.AdminApplications(ctx, app.ApplicationFilter{Source: "website", FollowUp: "contacted"}, app.Page{})
	noErr(t, err)
	if len(l.Items) != 1 || l.New != 0 || l.Items[0].Lead.FollowNote != "微信加上了，周五演示" {
		t.Fatalf("followed up: %+v", l)
	}
	if auditCount(t, "application.follow_up") == 0 {
		t.Fatal("no audit for following up")
	}

	// approving one from the website: no customer to tell, still works
	noErr(t, a.AdminDecideApplication(ctx, actor, id, "approved", ""))
	// once decided, the same phone makes a new application
	id3, err := a.SubmitLead(ctx, ok)
	noErr(t, err)
	if id3 == id {
		t.Fatal("a decided application was updated")
	}

	// at most LeadsPerHour new ones an hour from everyone together
	_, err = a.DB.Exec(ctx, `INSERT INTO merchant_applications (source, store_name, contact_name, phone)
		SELECT 'website', 'x', 'y', '+63 900 000 ' || g FROM generate_series(1, $1) g`, app.LeadsPerHour)
	noErr(t, err)
	other := ok
	other.Phone = "+63 917 000 0001"
	_, err = a.SubmitLead(ctx, other)
	wantCode(t, err, "BUSY")
	_, err = a.DB.Exec(ctx, `DELETE FROM merchant_applications WHERE source='website'`)
	noErr(t, err)
}
