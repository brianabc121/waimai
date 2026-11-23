-- +goose Up
-- Platform coupons belong to one country, and may be kept to one province of
-- it (docs/COUPON_DESIGN.md §7.1): only stores there take them, and only
-- customers there are given them. '' province is the whole country.
ALTER TABLE coupon_batches
    ADD COLUMN IF NOT EXISTS country_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS country      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS province     TEXT NOT NULL DEFAULT '';
ALTER TABLE customer_coupons
    ADD COLUMN IF NOT EXISTS country_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS country      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS province     TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE customer_coupons DROP COLUMN country_code, DROP COLUMN country, DROP COLUMN province;
ALTER TABLE coupon_batches DROP COLUMN country_code, DROP COLUMN country, DROP COLUMN province;
