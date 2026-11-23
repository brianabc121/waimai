-- +goose Up
-- Where a store that applies to join is: picked on the map like a delivery
-- address, with the country and region read off the map (names in English),
-- so that the platform sees the country, province, area, street and the
-- point itself. Applications from before keep only the city typed by hand.
ALTER TABLE merchant_applications
    ADD COLUMN country_code  TEXT NOT NULL DEFAULT '' CHECK (length(country_code) <= 2),
    ADD COLUMN country       TEXT NOT NULL DEFAULT '' CHECK (length(country) <= 100),
    ADD COLUMN province      TEXT NOT NULL DEFAULT '' CHECK (length(province) <= 100),
    ADD COLUMN district      TEXT NOT NULL DEFAULT '' CHECK (length(district) <= 100),
    ADD COLUMN postal_code   TEXT NOT NULL DEFAULT '' CHECK (length(postal_code) <= 20),
    ADD COLUMN place_name    TEXT NOT NULL DEFAULT '' CHECK (length(place_name) <= 200),
    ADD COLUMN place_address TEXT NOT NULL DEFAULT '' CHECK (length(place_address) <= 300),
    ADD COLUMN unit          TEXT NOT NULL DEFAULT '' CHECK (length(unit) <= 100),
    ADD COLUMN lat           DOUBLE PRECISION,
    ADD COLUMN lng           DOUBLE PRECISION,
    ADD CONSTRAINT merchant_applications_point CHECK ((lat IS NULL) = (lng IS NULL));

-- +goose Down
ALTER TABLE merchant_applications
    DROP CONSTRAINT merchant_applications_point,
    DROP COLUMN country_code, DROP COLUMN country, DROP COLUMN province, DROP COLUMN district,
    DROP COLUMN postal_code, DROP COLUMN place_name, DROP COLUMN place_address, DROP COLUMN unit,
    DROP COLUMN lat, DROP COLUMN lng;
