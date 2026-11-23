-- +goose Up
-- Coupons (docs/COUPON_DESIGN.md). A batch says how a kind of coupon is
-- given out; each coupon a customer holds is a row of customer_coupons with
-- its own copy of the amounts, so editing a batch never changes coupons
-- already given. Amounts are in minor units.
CREATE TABLE coupon_batches (
    id            BIGSERIAL PRIMARY KEY,
    issuer        TEXT NOT NULL CHECK (issuer IN ('store','platform')),
    branch_id     BIGINT REFERENCES branches(id),
    kind          TEXT NOT NULL CHECK (kind IN ('store_free','store_threshold','store_delivery','platform_free')),
    -- repurchase: given when a customer's first order at the store is completed
    source        TEXT NOT NULL CHECK (source IN ('repurchase','claim','new_customer','manual')),
    name          TEXT NOT NULL DEFAULT '',
    name_en       TEXT NOT NULL DEFAULT '',
    currency      TEXT NOT NULL,
    value         BIGINT NOT NULL CHECK (value >= 0),      -- 0 on store_delivery = the whole delivery fee
    threshold     BIGINT NOT NULL DEFAULT 0 CHECK (threshold >= 0), -- minimum item subtotal, original prices
    valid_days    INT NOT NULL CHECK (valid_days BETWEEN 1 AND 365),
    min_order     BIGINT NOT NULL DEFAULT 0 CHECK (min_order >= 0), -- repurchase: first order's items at least
    monthly_cap   INT CHECK (monthly_cap > 0),             -- repurchase: at most this many a month
    total_quota   INT CHECK (total_quota > 0),
    per_customer  INT CHECK (per_customer > 0),
    budget        BIGINT CHECK (budget > 0),
    scope         TEXT NOT NULL DEFAULT 'all' CHECK (scope IN ('all','only','except')),
    scope_branch_ids BIGINT[] NOT NULL DEFAULT '{}',
    issued_count  INT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','ended')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((issuer = 'store') = (branch_id IS NOT NULL)),
    CHECK ((issuer = 'platform') = (kind = 'platform_free'))
);
-- One repurchase rule per store; switching it off and on keeps the same row,
-- so nobody gets the gift twice.
CREATE UNIQUE INDEX coupon_batches_repurchase ON coupon_batches (branch_id) WHERE source = 'repurchase';

CREATE TABLE customer_coupons (
    id              BIGSERIAL PRIMARY KEY,
    batch_id        BIGINT NOT NULL REFERENCES coupon_batches(id),
    customer_id     BIGINT NOT NULL REFERENCES customers(id),
    issuer          TEXT NOT NULL,
    kind            TEXT NOT NULL,
    branch_id       BIGINT REFERENCES branches(id),         -- store coupons: the only store it works at
    currency        TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    name_en         TEXT NOT NULL DEFAULT '',
    value           BIGINT NOT NULL,
    threshold       BIGINT NOT NULL DEFAULT 0,
    scope           TEXT NOT NULL DEFAULT 'all',
    scope_branch_ids BIGINT[] NOT NULL DEFAULT '{}',
    source          TEXT NOT NULL,
    source_order_id BIGINT REFERENCES orders(id),          -- the order that earned a repurchase gift
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL DEFAULT 'unused' CHECK (status IN ('unused','used','void')),
    order_id        BIGINT REFERENCES orders(id),
    used_at         TIMESTAMPTZ,
    reminded_at     TIMESTAMPTZ
);
CREATE INDEX customer_coupons_mine ON customer_coupons (customer_id, status, expires_at);
CREATE INDEX customer_coupons_order ON customer_coupons (order_id) WHERE order_id IS NOT NULL;
CREATE INDEX customer_coupons_batch ON customer_coupons (batch_id, issued_at);
CREATE UNIQUE INDEX customer_coupons_repurchase ON customer_coupons (batch_id, customer_id) WHERE source = 'repurchase';

-- Every discount on an order, one row each: the store promotion and each
-- coupon. Bills read from here.
CREATE TABLE order_discounts (
    id           BIGSERIAL PRIMARY KEY,
    order_id     BIGINT NOT NULL REFERENCES orders(id),
    branch_id    BIGINT NOT NULL REFERENCES branches(id),
    kind         TEXT NOT NULL CHECK (kind IN ('promotion','store_free','store_threshold','store_delivery','platform_free')),
    funded_by    TEXT NOT NULL CHECK (funded_by IN ('store','platform')),
    name         TEXT NOT NULL DEFAULT '',
    name_en      TEXT NOT NULL DEFAULT '',
    amount       BIGINT NOT NULL CHECK (amount >= 0),
    promotion_id BIGINT REFERENCES promotions(id),
    coupon_id    BIGINT REFERENCES customer_coupons(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_discounts_order ON order_discounts (order_id);
CREATE INDEX order_discounts_branch ON order_discounts (branch_id, created_at);

-- discount stays the sum of everything taken off; total is what the customer
-- pays. The store's takings for an order are total + platform_subsidy.
ALTER TABLE orders ADD COLUMN merchant_discount BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN platform_subsidy BIGINT NOT NULL DEFAULT 0;

-- Orders from before coupons: their one promotion becomes a discount row.
UPDATE orders SET merchant_discount = discount WHERE discount > 0;
INSERT INTO order_discounts (order_id, branch_id, kind, funded_by, name, name_en, amount, promotion_id, created_at)
SELECT id, branch_id, 'promotion', 'store', promotion_name, promotion_name_en, discount, promotion_id, created_at
FROM orders WHERE discount > 0;

-- +goose Down
ALTER TABLE orders DROP COLUMN merchant_discount, DROP COLUMN platform_subsidy;
DROP TABLE order_discounts;
DROP TABLE customer_coupons;
DROP TABLE coupon_batches;
