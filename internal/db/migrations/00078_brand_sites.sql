-- +goose Up
-- Each brand is a shop of its own to customers (docs/MULTI_BRAND_DESIGN.md
-- §9): its own page, mall card, logo, reviews and favourites. The kitchen,
-- delivery areas and fees stay the store's.
ALTER TABLE brands ADD COLUMN logo_url  TEXT NOT NULL DEFAULT '';
ALTER TABLE brands ADD COLUMN cover_url TEXT NOT NULL DEFAULT '';
ALTER TABLE brands ADD COLUMN intro     TEXT NOT NULL DEFAULT '';

-- A review is of the brand the order was placed with.
ALTER TABLE reviews ADD COLUMN brand_id BIGINT REFERENCES brands(id);
UPDATE reviews r SET brand_id = o.brand_id FROM orders o WHERE o.id = r.order_id;
UPDATE reviews r SET brand_id = b.id FROM brands b WHERE r.brand_id IS NULL AND b.branch_id = r.branch_id AND b.is_main;
CREATE INDEX reviews_brand_idx ON reviews (brand_id, created_at DESC);
-- +goose StatementBegin
CREATE FUNCTION fill_review_brand() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.brand_id IS NULL THEN
        SELECT brand_id INTO NEW.brand_id FROM orders WHERE id = NEW.order_id;
    END IF;
    IF NEW.brand_id IS NULL THEN
        SELECT id INTO NEW.brand_id FROM brands WHERE branch_id = NEW.branch_id AND is_main;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER reviews_brand BEFORE INSERT ON reviews FOR EACH ROW EXECUTE FUNCTION fill_review_brand();

-- A customer keeps a brand, not the store behind it.
ALTER TABLE customer_favorites ADD COLUMN brand_id BIGINT REFERENCES brands(id) ON DELETE CASCADE;
UPDATE customer_favorites f SET brand_id = b.id FROM brands b WHERE b.branch_id = f.branch_id AND b.is_main;
ALTER TABLE customer_favorites ALTER COLUMN brand_id SET NOT NULL;
ALTER TABLE customer_favorites DROP CONSTRAINT customer_favorites_pkey;
ALTER TABLE customer_favorites ADD PRIMARY KEY (customer_id, brand_id);

-- +goose Down
DELETE FROM customer_favorites f USING brands b WHERE b.id = f.brand_id AND NOT b.is_main;
ALTER TABLE customer_favorites DROP CONSTRAINT customer_favorites_pkey;
ALTER TABLE customer_favorites ADD PRIMARY KEY (customer_id, branch_id);
ALTER TABLE customer_favorites DROP COLUMN brand_id;
DROP TRIGGER reviews_brand ON reviews;
DROP FUNCTION fill_review_brand();
DROP INDEX reviews_brand_idx;
ALTER TABLE reviews DROP COLUMN brand_id;
ALTER TABLE brands DROP COLUMN intro;
ALTER TABLE brands DROP COLUMN cover_url;
ALTER TABLE brands DROP COLUMN logo_url;
