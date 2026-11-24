-- +goose Up
-- Promotions split into two sets that never mix (docs/CHECKOUT_DISCOUNT_DESIGN.md §4):
-- delivery (delivery and pickup orders, as before) and dine_in (table bills).
-- Every existing promotion is a delivery one.
ALTER TABLE promotions ADD COLUMN scope TEXT NOT NULL DEFAULT 'delivery'
    CHECK (scope IN ('delivery', 'dine_in'));
ALTER TABLE promotions ADD CONSTRAINT promotions_dine_kind
    CHECK (scope = 'delivery' OR (kind <> 'free_delivery' AND per_phone_limit IS NULL));
DROP INDEX promotions_branch;
CREATE INDEX promotions_branch ON promotions (branch_id, scope, status);

-- +goose Down
DROP INDEX promotions_branch;
CREATE INDEX promotions_branch ON promotions (branch_id, status);
DELETE FROM promotion_redemptions WHERE promotion_id IN (SELECT id FROM promotions WHERE scope = 'dine_in');
UPDATE orders SET promotion_id = NULL WHERE promotion_id IN (SELECT id FROM promotions WHERE scope = 'dine_in');
DELETE FROM promotions WHERE scope = 'dine_in';
ALTER TABLE promotions DROP CONSTRAINT promotions_dine_kind;
ALTER TABLE promotions DROP COLUMN scope;
