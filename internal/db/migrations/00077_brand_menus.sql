-- +goose Up
-- Brand menus and prices, promotions and coupons per brand
-- (docs/MULTI_BRAND_DESIGN.md §5, §10). The store's dishes stay one list
-- (the kitchen's); a brand with its own menu picks dishes from it and may
-- give each its own name, picture and price. A brand without its own menu
-- sells the store's menu as it is, so nothing changes until a store sets one.
ALTER TABLE brands ADD COLUMN own_menu BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE brand_categories (
    id       BIGSERIAL PRIMARY KEY,
    brand_id BIGINT NOT NULL REFERENCES brands(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    name_en  TEXT NOT NULL DEFAULT '',
    sort     INT NOT NULL DEFAULT 0
);
CREATE INDEX brand_categories_brand ON brand_categories (brand_id, sort);

CREATE TABLE brand_items (
    brand_id    BIGINT NOT NULL REFERENCES brands(id) ON DELETE CASCADE,
    item_id     BIGINT NOT NULL REFERENCES menu_items(id) ON DELETE CASCADE,
    category_id BIGINT REFERENCES brand_categories(id) ON DELETE SET NULL,
    -- empty = the dish's own name, picture; NULL price = the dish's price
    name        TEXT NOT NULL DEFAULT '',
    name_en     TEXT NOT NULL DEFAULT '',
    image_url   TEXT NOT NULL DEFAULT '',
    price       BIGINT CHECK (price >= 0),
    published   BOOLEAN NOT NULL DEFAULT true,
    sort        INT NOT NULL DEFAULT 0,
    PRIMARY KEY (brand_id, item_id)
);
CREATE INDEX brand_items_item ON brand_items (item_id);

-- What the kitchen calls the dish when the brand sells it under another name.
ALTER TABLE order_items ADD COLUMN base_name TEXT NOT NULL DEFAULT '';

-- Promotions apply to the brands they list; the ones there are belong to
-- the main brand, which is every order they could apply to so far.
ALTER TABLE promotions ADD COLUMN brand_ids BIGINT[] NOT NULL DEFAULT '{}';
UPDATE promotions p SET brand_ids = ARRAY[b.id] FROM brands b WHERE b.branch_id = p.branch_id AND b.is_main;
-- the brands a promotion an order took applied to then (rule D13)
ALTER TABLE order_discounts ADD COLUMN brand_ids BIGINT[];

-- A store coupon belongs to the brand that gave it and only works there.
ALTER TABLE coupon_batches ADD COLUMN brand_id BIGINT REFERENCES brands(id);
UPDATE coupon_batches c SET brand_id = b.id FROM brands b WHERE b.branch_id = c.branch_id AND b.is_main;
ALTER TABLE customer_coupons ADD COLUMN brand_id BIGINT REFERENCES brands(id);
UPDATE customer_coupons c SET brand_id = b.id FROM brands b WHERE b.branch_id = c.branch_id AND b.is_main;
-- one repurchase gift rule per brand
DROP INDEX coupon_batches_repurchase;
CREATE UNIQUE INDEX coupon_batches_repurchase ON coupon_batches (brand_id) WHERE source = 'repurchase';

-- +goose StatementBegin
CREATE FUNCTION fill_promo_brands() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF cardinality(NEW.brand_ids) = 0 THEN
        NEW.brand_ids := ARRAY(SELECT id FROM brands WHERE branch_id = NEW.branch_id AND is_main);
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION fill_store_brand() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.brand_id IS NULL AND NEW.branch_id IS NOT NULL THEN
        SELECT id INTO NEW.brand_id FROM brands WHERE branch_id = NEW.branch_id AND is_main;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
-- A table's order belongs to the store's dine-in brand (the main brand when
-- none takes dine-in orders).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fill_order_brand() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.brand_id IS NULL AND NEW.service_type = 'dine_in' THEN
        SELECT id INTO NEW.brand_id FROM brands WHERE branch_id = NEW.branch_id AND dine_in AND status = 'active';
    END IF;
    IF NEW.brand_id IS NULL THEN
        SELECT id INTO NEW.brand_id FROM brands WHERE branch_id = NEW.branch_id AND is_main;
    END IF;
    IF NEW.brand_name = '' THEN
        SELECT name INTO NEW.brand_name FROM brands WHERE id = NEW.brand_id;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER promotions_brands BEFORE INSERT OR UPDATE ON promotions FOR EACH ROW EXECUTE FUNCTION fill_promo_brands();
CREATE TRIGGER coupon_batches_brand BEFORE INSERT ON coupon_batches FOR EACH ROW EXECUTE FUNCTION fill_store_brand();
-- +goose StatementBegin
CREATE FUNCTION fill_coupon_brand() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.brand_id IS NULL AND NEW.branch_id IS NOT NULL THEN
        SELECT brand_id INTO NEW.brand_id FROM coupon_batches WHERE id = NEW.batch_id;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER customer_coupons_brand BEFORE INSERT ON customer_coupons FOR EACH ROW EXECUTE FUNCTION fill_coupon_brand();

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fill_order_brand() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.brand_id IS NULL THEN
        SELECT id INTO NEW.brand_id FROM brands WHERE branch_id = NEW.branch_id AND is_main;
    END IF;
    IF NEW.brand_name = '' THEN
        SELECT name INTO NEW.brand_name FROM brands WHERE id = NEW.brand_id;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
DROP TRIGGER customer_coupons_brand ON customer_coupons;
DROP TRIGGER coupon_batches_brand ON coupon_batches;
DROP TRIGGER promotions_brands ON promotions;
DROP FUNCTION fill_coupon_brand();
DROP FUNCTION fill_store_brand();
DROP FUNCTION fill_promo_brands();
DROP INDEX coupon_batches_repurchase;
DELETE FROM customer_coupons c USING coupon_batches cb, brands b
    WHERE cb.id = c.batch_id AND b.id = cb.brand_id AND NOT b.is_main AND cb.source = 'repurchase' AND c.order_id IS NULL;
DELETE FROM coupon_batches c USING brands b WHERE b.id = c.brand_id AND NOT b.is_main AND c.source = 'repurchase'
    AND NOT EXISTS (SELECT 1 FROM customer_coupons cc WHERE cc.batch_id = c.id);
CREATE UNIQUE INDEX coupon_batches_repurchase ON coupon_batches (branch_id) WHERE source = 'repurchase';
ALTER TABLE customer_coupons DROP COLUMN brand_id;
ALTER TABLE coupon_batches DROP COLUMN brand_id;
ALTER TABLE order_discounts DROP COLUMN brand_ids;
ALTER TABLE promotions DROP COLUMN brand_ids;
ALTER TABLE order_items DROP COLUMN base_name;
DROP TABLE brand_items;
DROP TABLE brand_categories;
ALTER TABLE brands DROP COLUMN own_menu;
