package app

import "strings"

// A delivery address that is a place known by name (docs/ADDRESS_PLACE_DESIGN.md)
// keeps that name beside the address line. The line itself stays the whole
// address, "name, street address": the name only says which leading part of
// it riders should look for.

// cleanPlace keeps a place's name and Google id only while the name still
// leads the address line: once staff rewrote the line, the name (and the id
// of the place the old line was) no longer describe it.
func cleanPlace(line, name, id string) (string, string, error) {
	name, id = strings.TrimSpace(name), strings.TrimSpace(id)
	if len([]rune(name)) > 200 || len(id) > 300 {
		return "", "", ErrValidation("text too long")
	}
	if name == "" || !leadsWith(line, name) {
		return "", "", nil
	}
	return name, id, nil
}

func leadsWith(line, name string) bool {
	l, n := []rune(strings.TrimSpace(line)), []rune(name)
	return len(l) >= len(n) && strings.EqualFold(string(l[:len(n)]), name)
}

// placeOfAddress is the place name of a customer's saved address as it is
// written into an order: known when the order's line is "name, address".
func (ad *Address) placeOfLine() string {
	if ad.PlaceAddress != "" && len([]rune(ad.PlaceName)) <= 200 && ad.line() == ad.PlaceName+", "+ad.PlaceAddress {
		return ad.PlaceName
	}
	return ""
}
