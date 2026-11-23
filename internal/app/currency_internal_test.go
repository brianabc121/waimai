package app

import (
	"os"
	"regexp"
	"testing"
)

// The migration that moved existing stores to their country's money holds
// the same table as currencyByCountry.
func TestCurrencyTableMatchesMigration(t *testing.T) {
	sql, err := os.ReadFile("../db/migrations/00040_currency_by_country.sql")
	if err != nil {
		t.Fatal(err)
	}
	pairs := regexp.MustCompile(`\('([A-Z]{2})','([A-Z]{3})'\)`).FindAllStringSubmatch(string(sql), -1)
	if len(pairs) != len(currencyByCountry) {
		t.Fatalf("migration has %d countries, the map %d", len(pairs), len(currencyByCountry))
	}
	for _, p := range pairs {
		if currencyByCountry[p[1]] != p[2] {
			t.Errorf("%s: migration %s, map %s", p[1], p[2], currencyByCountry[p[1]])
		}
	}
	for cc, want := range map[string]string{"PH": "PHP", "SG": "SGD", "MY": "MYR", "TH": "THB", "CN": "CNY", "US": "USD", "XX": ""} {
		if got := CurrencyOf(cc); got != want {
			t.Errorf("CurrencyOf(%s) = %q, want %q", cc, got, want)
		}
	}
}
