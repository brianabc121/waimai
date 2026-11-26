-- +goose Up
-- Each brand opens its own shops on the delivery platforms
-- (docs/MULTI_BRAND_DESIGN.md §10): 一品湘 Grab and 好吃党 Grab are two
-- shops with their own shop id, commission and markup. The store's platforms
-- so far are its main brand's.
ALTER TABLE branch_platforms ADD COLUMN brand_id BIGINT REFERENCES brands(id) ON DELETE CASCADE;
UPDATE branch_platforms bp SET brand_id = b.id FROM brands b WHERE b.branch_id = bp.branch_id AND b.is_main;
ALTER TABLE branch_platforms ALTER COLUMN brand_id SET NOT NULL;
ALTER TABLE branch_platforms DROP CONSTRAINT branch_platforms_pkey;
ALTER TABLE branch_platforms ADD PRIMARY KEY (brand_id, platform_id);
CREATE INDEX branch_platforms_branch ON branch_platforms (branch_id);

-- +goose Down
DELETE FROM branch_platforms bp USING brands b WHERE b.id = bp.brand_id AND NOT b.is_main;
DROP INDEX branch_platforms_branch;
ALTER TABLE branch_platforms DROP CONSTRAINT branch_platforms_pkey;
ALTER TABLE branch_platforms ADD PRIMARY KEY (branch_id, platform_id);
ALTER TABLE branch_platforms DROP COLUMN brand_id;
