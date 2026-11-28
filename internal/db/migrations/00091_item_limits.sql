-- +goose Up
-- Item marketing: purchase limits on chosen items, set up like the item
-- promotions (same dates, daily hours, scope, brands and promotion_items).
--   dish_limit_order  at most value of the item on one order
--   dish_limit_day    at most value of the item per customer per day
-- A limit takes no money off; it is checked when lines are added to an order
-- (internal/app/item_limits.go) and never recorded on the order.
ALTER TABLE promotions DROP CONSTRAINT promotions_kind_check;
ALTER TABLE promotions ADD CONSTRAINT promotions_kind_check
    CHECK (kind IN ('amount_off','percent_off','free_delivery','dish_percent','dish_second','dish_limit_order','dish_limit_day'));
ALTER TABLE promotions DROP CONSTRAINT promotions_dish_value;
ALTER TABLE promotions ADD CONSTRAINT promotions_dish_value
    CHECK ((kind NOT IN ('dish_percent','dish_second') OR (value > 0 AND value <= 10000))
       AND (kind NOT IN ('dish_limit_order','dish_limit_day') OR (value > 0 AND value <= 99)));

-- +goose Down
DELETE FROM promotions WHERE kind IN ('dish_limit_order','dish_limit_day');
ALTER TABLE promotions DROP CONSTRAINT promotions_dish_value;
ALTER TABLE promotions ADD CONSTRAINT promotions_dish_value
    CHECK (kind NOT IN ('dish_percent','dish_second') OR (value > 0 AND value <= 10000));
ALTER TABLE promotions DROP CONSTRAINT promotions_kind_check;
ALTER TABLE promotions ADD CONSTRAINT promotions_kind_check
    CHECK (kind IN ('amount_off','percent_off','free_delivery','dish_percent','dish_second'));
