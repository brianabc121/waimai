-- +goose Up
-- A platform pays each brand's shop on its own statement
-- (docs/MULTI_BRAND_DESIGN.md §12): what 一品湘 Grab and 好吃党 Grab paid
-- are typed in apart. Statements so far are the main brand's.
ALTER TABLE platform_payouts ADD COLUMN brand_id BIGINT REFERENCES brands(id) ON DELETE CASCADE;
UPDATE platform_payouts x SET brand_id = b.id FROM brands b WHERE b.branch_id = x.branch_id AND b.is_main;
ALTER TABLE platform_payouts ALTER COLUMN brand_id SET NOT NULL;
DROP INDEX platform_payouts_period;
CREATE UNIQUE INDEX platform_payouts_period ON platform_payouts (branch_id, brand_id, platform_id, period_from, period_to);

-- +goose Down
DELETE FROM platform_payouts x USING brands b WHERE b.id = x.brand_id AND NOT b.is_main;
DROP INDEX platform_payouts_period;
CREATE UNIQUE INDEX platform_payouts_period ON platform_payouts (branch_id, platform_id, period_from, period_to);
ALTER TABLE platform_payouts DROP COLUMN brand_id;
