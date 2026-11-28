-- +goose Up
-- Money that reaches an order after its first payment
-- (docs/CODE_REVIEW_FIX_PLAN.md M02, M04; docs/PRICING_CHECK.md P1–P3).
--   topup_cash:   the cash part of an order paid in advance another way: the
--                 difference for dishes added after it was paid, collected by
--                 the rider at the door or at the counter on pickup.
--   late_cash:    cash taken at the store afterwards for an order the rider
--                 collected too little for.
--   short_waived: what the store decided not to collect of such an order.
ALTER TABLE orders
    ADD COLUMN topup_cash   BIGINT NOT NULL DEFAULT 0 CHECK (topup_cash >= 0),
    ADD COLUMN late_cash    BIGINT NOT NULL DEFAULT 0 CHECK (late_cash >= 0),
    ADD COLUMN short_waived BIGINT NOT NULL DEFAULT 0 CHECK (short_waived >= 0);

-- +goose Down
ALTER TABLE orders DROP COLUMN topup_cash, DROP COLUMN late_cash, DROP COLUMN short_waived;
