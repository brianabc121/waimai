-- +goose Up
-- A delivery address that is a place people know by name (a tower, a mall, a
-- compound): docs/ADDRESS_PLACE_DESIGN.md. Riders look for the name, not for
-- the street, so the name is kept on its own beside the address.
--
-- address_line (address_book.line) stays the whole address as one line,
-- "name, street address", exactly as before: everything that compares or
-- counts addresses by that line (the address book's duplicates, the delivery
-- statistics, a member's saved addresses) keeps working, and old rows need no
-- change. place_name says which leading part of the line is the name;
-- place_id is Google's id of the place ('' when it was typed or pinned by
-- hand), which opens the place's own page in Google Maps.
ALTER TABLE orders
    ADD COLUMN place_name TEXT NOT NULL DEFAULT '' CHECK (length(place_name) <= 200),
    ADD COLUMN place_id   TEXT NOT NULL DEFAULT '' CHECK (length(place_id) <= 300);
ALTER TABLE store_members
    ADD COLUMN place_name TEXT NOT NULL DEFAULT '' CHECK (length(place_name) <= 200),
    ADD COLUMN place_id   TEXT NOT NULL DEFAULT '' CHECK (length(place_id) <= 300);
ALTER TABLE store_member_addresses
    ADD COLUMN place_name TEXT NOT NULL DEFAULT '' CHECK (length(place_name) <= 200),
    ADD COLUMN place_id   TEXT NOT NULL DEFAULT '' CHECK (length(place_id) <= 300);
ALTER TABLE address_book
    ADD COLUMN place_name TEXT NOT NULL DEFAULT '' CHECK (length(place_name) <= 200),
    ADD COLUMN place_id   TEXT NOT NULL DEFAULT '' CHECK (length(place_id) <= 300);
-- One place is one entry of the book in its region, however its line is written.
CREATE INDEX address_book_place_idx ON address_book (country_code, province, city, place_id) WHERE place_id <> '';

-- Orders customers placed themselves already carry "name, address" in
-- address_line, made from the saved address: where the line still is exactly
-- that, the name is known without asking anyone.
UPDATE orders o SET place_name = a.place_name
FROM customer_addresses a
WHERE o.address_id = a.id AND a.place_address <> '' AND length(a.place_name) <= 200
  AND o.address_line = a.place_name || ', ' || a.place_address;

-- +goose Down
DROP INDEX address_book_place_idx;
ALTER TABLE address_book DROP COLUMN place_name, DROP COLUMN place_id;
ALTER TABLE store_member_addresses DROP COLUMN place_name, DROP COLUMN place_id;
ALTER TABLE store_members DROP COLUMN place_name, DROP COLUMN place_id;
ALTER TABLE orders DROP COLUMN place_name, DROP COLUMN place_id;
