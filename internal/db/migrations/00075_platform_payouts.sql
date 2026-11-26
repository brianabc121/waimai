-- +goose Up
-- What a delivery platform actually paid the store for a period, typed in by
-- staff from the platform's statement (docs/ORDER_TYPES_DESIGN.md §9.5). The
-- reconciliation page sets it against the expected payout of the orders.
CREATE TABLE platform_payouts (
    id          BIGSERIAL PRIMARY KEY,
    branch_id   BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    platform_id BIGINT NOT NULL REFERENCES delivery_platforms(id),
    period_from DATE NOT NULL,
    period_to   DATE NOT NULL CHECK (period_to >= period_from),
    amount      BIGINT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    created_by  BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX platform_payouts_period ON platform_payouts (branch_id, platform_id, period_from, period_to);

-- +goose Down
DROP TABLE platform_payouts;
