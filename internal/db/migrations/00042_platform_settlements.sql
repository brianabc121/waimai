-- +goose Up
-- What the platform owes each store for its coupons, one statement a month
-- (docs/COUPON_DESIGN.md §7.3). month is the first day of the store's month.
CREATE TABLE platform_settlements (
    id            BIGSERIAL PRIMARY KEY,
    branch_id     BIGINT NOT NULL REFERENCES branches(id),
    month         DATE NOT NULL,
    currency      TEXT NOT NULL,
    orders        INT NOT NULL,
    amount        BIGINT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','paid')),
    paid_on       DATE,
    transfer_ref  TEXT NOT NULL DEFAULT '',
    paid_by       BIGINT REFERENCES users(id),
    paid_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (branch_id, month, currency)
);
CREATE INDEX platform_settlements_status ON platform_settlements (status, month DESC);
CREATE INDEX orders_platform_subsidy ON orders (completed_at) WHERE platform_subsidy > 0;

-- +goose Down
DROP INDEX orders_platform_subsidy;
DROP TABLE platform_settlements;
