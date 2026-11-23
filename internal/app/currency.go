package app

// A store's prices are in the money of the country it stands in. The country
// comes from the store's pin on the merchant's map (SaveLocation); a store
// that has not set its location yet keeps the currency the platform admin
// gave it.

// currencyByCountry maps ISO 3166-1 alpha-2 country codes to the ISO 4217
// code of the money people pay with there.
var currencyByCountry = map[string]string{
	// Asia
	"PH": "PHP", "CN": "CNY", "HK": "HKD", "MO": "MOP", "TW": "TWD", "JP": "JPY", "KR": "KRW", "KP": "KPW", "MN": "MNT",
	"SG": "SGD", "MY": "MYR", "TH": "THB", "VN": "VND", "ID": "IDR", "BN": "BND", "KH": "KHR", "LA": "LAK", "MM": "MMK",
	"TL": "USD", "IN": "INR", "PK": "PKR", "BD": "BDT", "LK": "LKR", "NP": "NPR", "BT": "BTN", "MV": "MVR", "AF": "AFN",
	"KZ": "KZT", "UZ": "UZS", "KG": "KGS", "TJ": "TJS", "TM": "TMT",
	"AE": "AED", "SA": "SAR", "QA": "QAR", "KW": "KWD", "BH": "BHD", "OM": "OMR", "YE": "YER", "JO": "JOD", "IL": "ILS",
	"PS": "ILS", "LB": "LBP", "SY": "SYP", "IQ": "IQD", "IR": "IRR", "TR": "TRY", "GE": "GEL", "AM": "AMD", "AZ": "AZN", "CY": "EUR",
	// Oceania
	"AU": "AUD", "NZ": "NZD", "PG": "PGK", "FJ": "FJD", "SB": "SBD", "VU": "VUV", "WS": "WST", "TO": "TOP", "KI": "AUD",
	"NR": "AUD", "TV": "AUD", "FM": "USD", "MH": "USD", "PW": "USD", "GU": "USD", "MP": "USD", "AS": "USD", "NC": "XPF", "PF": "XPF",
	"CK": "NZD", "NU": "NZD",
	// Europe
	"GB": "GBP", "IE": "EUR", "FR": "EUR", "DE": "EUR", "NL": "EUR", "BE": "EUR", "LU": "EUR", "AT": "EUR", "IT": "EUR",
	"ES": "EUR", "PT": "EUR", "FI": "EUR", "GR": "EUR", "MT": "EUR", "SK": "EUR", "SI": "EUR", "EE": "EUR", "LV": "EUR",
	"LT": "EUR", "HR": "EUR", "MC": "EUR", "SM": "EUR", "VA": "EUR", "AD": "EUR", "ME": "EUR", "XK": "EUR",
	"CH": "CHF", "LI": "CHF", "NO": "NOK", "SE": "SEK", "DK": "DKK", "IS": "ISK", "PL": "PLN", "CZ": "CZK", "HU": "HUF",
	"RO": "RON", "BG": "BGN", "RS": "RSD", "BA": "BAM", "MK": "MKD", "AL": "ALL", "MD": "MDL", "UA": "UAH", "BY": "BYN",
	"RU": "RUB", "GI": "GIP", "FO": "DKK", "GL": "DKK",
	// Americas
	"US": "USD", "CA": "CAD", "MX": "MXN", "BR": "BRL", "AR": "ARS", "CL": "CLP", "CO": "COP", "PE": "PEN", "VE": "VES",
	"EC": "USD", "BO": "BOB", "PY": "PYG", "UY": "UYU", "GY": "GYD", "SR": "SRD", "GT": "GTQ", "HN": "HNL", "SV": "USD",
	"NI": "NIO", "CR": "CRC", "PA": "PAB", "CU": "CUP", "DO": "DOP", "HT": "HTG", "JM": "JMD", "TT": "TTD", "BS": "BSD",
	"BB": "BBD", "BZ": "BZD", "PR": "USD", "AG": "XCD", "DM": "XCD", "GD": "XCD", "KN": "XCD", "LC": "XCD", "VC": "XCD",
	"AW": "AWG", "CW": "ANG", "KY": "KYD", "BM": "BMD", "VI": "USD", "VG": "USD",
	// Africa
	"ZA": "ZAR", "NG": "NGN", "EG": "EGP", "KE": "KES", "MA": "MAD", "DZ": "DZD", "TN": "TND", "LY": "LYD", "GH": "GHS",
	"ET": "ETB", "TZ": "TZS", "UG": "UGX", "RW": "RWF", "SD": "SDG", "SS": "SSP", "AO": "AOA", "MZ": "MZN", "ZM": "ZMW",
	"ZW": "USD", "BW": "BWP", "NA": "NAD", "LS": "LSL", "SZ": "SZL", "MW": "MWK", "MG": "MGA", "MU": "MUR", "SC": "SCR",
	"CD": "CDF", "SO": "SOS", "DJ": "DJF", "ER": "ERN", "GM": "GMD", "GN": "GNF", "LR": "LRD", "SL": "SLE", "CV": "CVE",
	"ST": "STN", "MR": "MRU", "BI": "BIF", "KM": "KMF",
	"SN": "XOF", "CI": "XOF", "ML": "XOF", "BF": "XOF", "NE": "XOF", "TG": "XOF", "BJ": "XOF", "GW": "XOF",
	"CM": "XAF", "GA": "XAF", "CG": "XAF", "CF": "XAF", "TD": "XAF", "GQ": "XAF",
}

// CurrencyOf is the currency of a country, or "" when it is not known.
func CurrencyOf(countryCode string) string { return currencyByCountry[countryCode] }
