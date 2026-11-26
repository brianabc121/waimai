-- +goose Up
-- Orders taken on a delivery platform and entered by staff
-- (docs/ORDER_TYPES_DESIGN.md §7, §9). The order is numbered with the
-- platform's prefix and its own number (Grab GF-123); its dishes are at the
-- platform's prices, which are worked out once, when it is placed, with the
-- markup and rounding kept on the order for its add-ons.
ALTER TABLE orders
    ADD COLUMN platform_id BIGINT REFERENCES delivery_platforms(id),
    ADD COLUMN platform_order_no TEXT NOT NULL DEFAULT '',
    -- rider: the platform's rider collects it; customer: the customer collects it
    ADD COLUMN platform_pickup TEXT NOT NULL DEFAULT '' CHECK (platform_pickup IN ('', 'rider', 'customer')),
    ADD COLUMN platform_markup_bp INT NOT NULL DEFAULT 0,
    ADD COLUMN platform_round_step INT NOT NULL DEFAULT 0;
CREATE INDEX orders_platform_no ON orders (branch_id, platform_id, platform_order_no) WHERE platform_id IS NOT NULL;

-- Paid on the platform: the money is with the platform until it pays the store.
ALTER TABLE orders DROP CONSTRAINT orders_pay_type_check;
ALTER TABLE orders ADD CONSTRAINT orders_pay_type_check CHECK (pay_type IN ('', 'cash', 'card', 'online', 'platform'));
-- What the platform took back from the store for a complaint is recorded as a refund paid by the platform.
ALTER TABLE order_refunds DROP CONSTRAINT order_refunds_pay_type_check;
ALTER TABLE order_refunds ADD CONSTRAINT order_refunds_pay_type_check
    CHECK (pay_type IN ('cash', 'transfer', 'online', 'other', 'platform'));

-- The two discounts a platform order carries: the store's own offer on the
-- platform (a fixed amount the store pays, on the whole order) and the
-- platform's (after tax; it does not lower what the store is owed).
ALTER TABLE order_discounts DROP CONSTRAINT order_discounts_kind_check;
ALTER TABLE order_discounts ADD CONSTRAINT order_discounts_kind_check
    CHECK (kind IN ('promotion','dish_promotion','store_free','store_threshold','store_delivery','platform_free',
        'platform_merchant_promo','platform_promo'));

-- What the store pays the platform for an order: its commission and other
-- charges. Not part of the order's amounts (§9.1). The commission follows the
-- order (rate × base, the base as the platform counts it) unless staff typed
-- an amount, with a reason.
CREATE TABLE order_platform_fees (
    id          BIGSERIAL PRIMARY KEY,
    order_id    BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    branch_id   BIGINT NOT NULL REFERENCES branches(id),
    platform_id BIGINT NOT NULL REFERENCES delivery_platforms(id),
    kind        TEXT NOT NULL CHECK (kind IN ('commission', 'other')),
    rate_bp     INT NOT NULL DEFAULT 0 CHECK (rate_bp BETWEEN 0 AND 10000),
    -- after_merchant: the dishes after the store's discount; original: before it
    base_kind   TEXT NOT NULL DEFAULT 'after_merchant' CHECK (base_kind IN ('after_merchant', 'original', '')),
    base        BIGINT NOT NULL DEFAULT 0 CHECK (base >= 0),
    amount      BIGINT NOT NULL CHECK (amount >= 0),
    manual      BOOLEAN NOT NULL DEFAULT false,
    reason      TEXT NOT NULL DEFAULT '',
    created_by  BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX order_platform_fees_commission ON order_platform_fees (order_id) WHERE kind = 'commission';
CREATE INDEX order_platform_fees_branch ON order_platform_fees (branch_id, created_at);

-- Entering platform orders is a permission of its own, on by default for
-- managers and order takers; stores that set their roles' permissions get it
-- where staff could already take orders for customers.
UPDATE branch_role_perms SET perms = array_append(perms, 'order.platform')
    WHERE 'order.manual' = ANY (perms) AND NOT 'order.platform' = ANY (perms);

-- +goose Down
UPDATE branch_role_perms SET perms = array_remove(perms, 'order.platform');
DROP TABLE order_platform_fees;
DELETE FROM order_discounts WHERE kind IN ('platform_merchant_promo', 'platform_promo');
ALTER TABLE order_discounts DROP CONSTRAINT order_discounts_kind_check;
ALTER TABLE order_discounts ADD CONSTRAINT order_discounts_kind_check
    CHECK (kind IN ('promotion','dish_promotion','store_free','store_threshold','store_delivery','platform_free'));
UPDATE order_refunds SET pay_type = 'other' WHERE pay_type = 'platform';
ALTER TABLE order_refunds DROP CONSTRAINT order_refunds_pay_type_check;
ALTER TABLE order_refunds ADD CONSTRAINT order_refunds_pay_type_check CHECK (pay_type IN ('cash', 'transfer', 'online', 'other'));
UPDATE orders SET pay_type = 'online' WHERE pay_type = 'platform';
ALTER TABLE orders DROP CONSTRAINT orders_pay_type_check;
ALTER TABLE orders ADD CONSTRAINT orders_pay_type_check CHECK (pay_type IN ('', 'cash', 'card', 'online'));
DROP INDEX orders_platform_no;
ALTER TABLE orders DROP COLUMN platform_round_step, DROP COLUMN platform_markup_bp, DROP COLUMN platform_pickup,
    DROP COLUMN platform_order_no, DROP COLUMN platform_id;
