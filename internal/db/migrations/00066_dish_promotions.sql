-- +goose Up
-- docs/PRICING_REVIEW.md §5.2, step 4: promotions on chosen dishes.
--   dish_percent  the chosen dishes at value (bp) off
--   dish_second   every second one of a chosen dish at value (bp) off
-- They take money off the dishes themselves, before any promotion on the
-- whole order, and several can apply to one order (each dish takes the one
-- that saves the most). A promotion on the whole order then counts its
-- threshold on what the dishes cost after them.
ALTER TABLE promotions DROP CONSTRAINT promotions_kind_check;
ALTER TABLE promotions ADD CONSTRAINT promotions_kind_check
    CHECK (kind IN ('amount_off','percent_off','free_delivery','dish_percent','dish_second'));
ALTER TABLE promotions ADD CONSTRAINT promotions_dish_value
    CHECK (kind NOT IN ('dish_percent','dish_second') OR (value > 0 AND value <= 10000));

-- The dishes a dish promotion is on.
CREATE TABLE promotion_items (
    promotion_id BIGINT NOT NULL REFERENCES promotions(id) ON DELETE CASCADE,
    item_id      BIGINT NOT NULL REFERENCES menu_items(id) ON DELETE CASCADE,
    PRIMARY KEY (promotion_id, item_id)
);
CREATE INDEX promotion_items_item ON promotion_items (item_id);

-- One discount row per dish promotion an order used, with the rule as it
-- was then ({"kind", "value", "item_ids"}), so that a later add-on, void or
-- refund prices those dishes as they were sold.
ALTER TABLE order_discounts DROP CONSTRAINT order_discounts_kind_check;
ALTER TABLE order_discounts ADD CONSTRAINT order_discounts_kind_check
    CHECK (kind IN ('promotion','dish_promotion','store_free','store_threshold','store_delivery','platform_free'));
ALTER TABLE order_discounts ADD COLUMN rule JSONB;

-- +goose Down
DELETE FROM order_discounts WHERE kind = 'dish_promotion';
ALTER TABLE order_discounts DROP COLUMN rule;
ALTER TABLE order_discounts DROP CONSTRAINT order_discounts_kind_check;
ALTER TABLE order_discounts ADD CONSTRAINT order_discounts_kind_check
    CHECK (kind IN ('promotion','store_free','store_threshold','store_delivery','platform_free'));
DROP TABLE promotion_items;
UPDATE orders SET promotion_id = NULL WHERE promotion_id IN (SELECT id FROM promotions WHERE kind IN ('dish_percent','dish_second'));
DELETE FROM promotion_redemptions WHERE promotion_id IN (SELECT id FROM promotions WHERE kind IN ('dish_percent','dish_second'));
DELETE FROM promotions WHERE kind IN ('dish_percent','dish_second');
ALTER TABLE promotions DROP CONSTRAINT promotions_dish_value;
ALTER TABLE promotions DROP CONSTRAINT promotions_kind_check;
ALTER TABLE promotions ADD CONSTRAINT promotions_kind_check CHECK (kind IN ('amount_off','percent_off','free_delivery'));
