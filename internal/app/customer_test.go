package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

func freshPhone() string { return fmt.Sprintf("+6394%08d", time.Now().UnixNano()%1e8) }

func TestCustomerPhoneFormats(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	for _, bad := range []string{"12345", "+8613800138000", "+63 2 8123 4567", "0917 555 012", "+63917555012345"} {
		_, err := a.RequestOTP(ctx, bad, "")
		wantCode(t, err, "PHONE_INVALID")
	}
	// the same number written three ways is one account
	local := fmt.Sprintf("95%08d", time.Now().UnixNano()%1e8)
	code, err := a.RequestOTP(ctx, "0"+local[:3]+" "+local[3:6]+" "+local[6:], "")
	noErr(t, err)
	token, phone, err := a.VerifyOTP(ctx, local, code)
	noErr(t, err)
	if phone != "+63"+local {
		t.Fatalf("normalised to %s", phone)
	}
	c, err := a.CustomerBySession(ctx, token)
	noErr(t, err)
	if c.ID == 0 || c.Phone != "+63"+local {
		t.Fatalf("customer %+v", c)
	}
	noErr(t, a.CustomerLogout(ctx, token))
	_, err = a.CustomerBySession(ctx, token)
	wantCode(t, err, "PHONE_NOT_VERIFIED")
}

func TestCustomerAccountIsKeptAcrossLogins(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	phone := freshPhone()
	login := func() *app.Customer {
		code, err := a.RequestOTP(ctx, phone, "")
		noErr(t, err)
		token, _, err := a.VerifyOTP(ctx, phone, code)
		noErr(t, err)
		c, err := a.CustomerBySession(ctx, token)
		noErr(t, err)
		return c
	}
	first := login()
	// allow another code without waiting a minute
	_, err := a.DB.Exec(ctx, `UPDATE phone_verifications SET created_at = now() - interval '2 minutes' WHERE phone=$1`, phone)
	noErr(t, err)
	if second := login(); second.ID != first.ID {
		t.Fatalf("a second login made another account: %d then %d", first.ID, second.ID)
	}
}

func TestOTPDailyLimits(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	age := func(where string, arg any) {
		_, err := a.DB.Exec(ctx, `UPDATE phone_verifications SET created_at = now() - interval '2 minutes' WHERE `+where, arg)
		noErr(t, err)
	}
	phone := freshPhone()
	for i := 0; i < app.OTPDailyPerPhone; i++ {
		_, err := a.RequestOTP(ctx, phone, "")
		noErr(t, err)
		age("phone=$1", phone)
	}
	_, err := a.RequestOTP(ctx, phone, "")
	wantCode(t, err, "OTP_DAILY_LIMIT")

	// One network address asking for many phones. The cap is off in demo mode.
	echo := a.OTPDevEcho
	defer func() { a.OTPDevEcho = echo }()
	ip := fmt.Sprintf("198.51.%d.%d", time.Now().UnixNano()%250, time.Now().UnixNano()/250%250)
	a.OTPDevEcho = false
	for i := 0; i < app.OTPDailyPerIP; i++ {
		_, err := a.RequestOTP(ctx, fmt.Sprintf("+6396%08d", (time.Now().UnixNano()+int64(i))%1e8), ip)
		noErr(t, err)
	}
	_, err = a.RequestOTP(ctx, freshPhone(), ip)
	wantCode(t, err, "OTP_DAILY_LIMIT")
	_, err = a.RequestOTP(ctx, freshPhone(), "198.51.251.1")
	noErr(t, err)
	a.OTPDevEcho = true
	_, err = a.RequestOTP(ctx, freshPhone(), ip)
	noErr(t, err)

	n, err := a.OTPSentToday(ctx)
	noErr(t, err)
	if n < app.OTPDailyPerPhone+app.OTPDailyPerIP {
		t.Fatalf("sent today: %d", n)
	}
}
