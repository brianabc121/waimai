package app

import "testing"

// Customers log in with a number of any country; a number written without a
// calling code is still read as a Philippine one, as before the code could be chosen.
func TestNormalizeE164(t *testing.T) {
	good := map[string]string{
		// the home country, written every way it was accepted before
		"+63 917 555 0123": "+639175550123", "0917 555 0123": "+639175550123", "917-555-0123": "+639175550123",
		"639175550123": "+639175550123", "0063 917 555 0123": "+639175550123",
		// other countries, with their code
		"+86 138 0013 8000": "+8613800138000", "0086 138 0013 8000": "+8613800138000",
		"+65 8123 4567": "+6581234567", "+852 9123 4567": "+85291234567", "+886 0912 345 678": "+886912345678",
		"+60 12-345 6789": "+60123456789", "+1 (415) 555-0123": "+14155550123", "+61 0412 345 678": "+61412345678",
		"+44 07911 123456": "+447911123456",
		// Italy keeps the 0: it is part of the number there
		"+39 06 1234 5678": "+390612345678",
	}
	for in, want := range good {
		got, err := normalizeCustomerPhone(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"", "12345", "+63 2 8123 4567", "0917 555 012", "+63917555012345", // the home country's mobile rule
		"+86 2 1234 5678", "+86 138 0013 800", // mainland China: 1XX XXXX XXXX
		"+999 1234 5678",        // no such code
		"+65 1234",              // too short
		"+1 415 555 0123 45678", // longer than 15 digits
		"+44 7911 12345a",
	} {
		if got, err := normalizeCustomerPhone(bad); err == nil {
			t.Errorf("%q: got %q, want PHONE_INVALID", bad, got)
		}
	}
	// A number to call need not be a mobile, except at home as before.
	if got, err := normalizeContactPhone("+86 21 1234 5678"); err != nil || got != "+862112345678" {
		t.Errorf("a landline abroad: %q, %v", got, err)
	}
	if _, err := normalizeContactPhone("+63 2 8123 4567"); err == nil {
		t.Errorf("a Philippine landline was accepted")
	}
}
