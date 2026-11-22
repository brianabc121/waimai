-- +goose Up
-- Where the store is, in words: filled in from the map when the merchant saves
-- the store's location. country_code is ISO 3166-1 alpha-2 ("PH"); address
-- searches of customers are kept to that country. Stores that already have a
-- location get these the next time their delivery map page is opened.
ALTER TABLE branches
    ADD COLUMN country_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN country      TEXT NOT NULL DEFAULT '',
    ADD COLUMN province     TEXT NOT NULL DEFAULT '',
    ADD COLUMN city         TEXT NOT NULL DEFAULT '',
    ADD COLUMN district     TEXT NOT NULL DEFAULT '',
    ADD COLUMN postal_code  TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE branches
    DROP COLUMN country_code, DROP COLUMN country, DROP COLUMN province,
    DROP COLUMN city, DROP COLUMN district, DROP COLUMN postal_code;
