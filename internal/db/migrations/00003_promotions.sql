-- +goose Up
-- Store promotions. One promotion at most applies to an order: the one that
-- saves the customer the most. Amounts are in minor units, percentages in
-- basis points.
CREATE TABLE promotions (
    id              BIGSERIAL PRIMARY KEY,
    branch_id       BIGINT NOT NULL REFERENCES branches(id),
    name            TEXT NOT NULL,
    name_en         TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL CHECK (kind IN ('amount_off','percent_off','free_delivery')),
    threshold       BIGINT NOT NULL DEFAULT 0 CHECK (threshold >= 0),   -- minimum item subtotal
    value           BIGINT NOT NULL DEFAULT 0 CHECK (value >= 0),       -- amount off, or percent in bp
    max_discount    BIGINT CHECK (max_discount > 0),                    -- cap for percent_off
    applies_delivery BOOLEAN NOT NULL DEFAULT true,
    applies_pickup  BOOLEAN NOT NULL DEFAULT true,
    starts_at       TIMESTAMPTZ,
    ends_at         TIMESTAMPTZ,
    daily_start     TEXT NOT NULL DEFAULT '00:00',  -- branch local time; equal = all day,
    daily_end       TEXT NOT NULL DEFAULT '00:00',  -- end < start crosses midnight
    total_quota     INT CHECK (total_quota > 0),
    per_phone_limit INT CHECK (per_phone_limit > 0),
    used_count      INT NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    status          TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','paused','ended')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (total_quota IS NULL OR used_count <= total_quota)
);
CREATE INDEX promotions_branch ON promotions (branch_id, status);

-- One row per order that used a promotion. Released when the order is
-- rejected, cancelled or times out before the food leaves.
CREATE TABLE promotion_redemptions (
    id           BIGSERIAL PRIMARY KEY,
    promotion_id BIGINT NOT NULL REFERENCES promotions(id),
    branch_id    BIGINT NOT NULL REFERENCES branches(id),
    order_id     BIGINT NOT NULL UNIQUE REFERENCES orders(id),
    phone        TEXT NOT NULL,
    amount       BIGINT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'applied' CHECK (status IN ('applied','released')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    released_at  TIMESTAMPTZ
);
CREATE INDEX promotion_redemptions_phone ON promotion_redemptions (promotion_id, phone) WHERE status = 'applied';

-- Snapshot on the order, so history does not change when a promotion is edited.
ALTER TABLE orders ADD COLUMN promotion_id BIGINT REFERENCES promotions(id),
    ADD COLUMN promotion_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN promotion_name_en TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE orders DROP COLUMN promotion_id, DROP COLUMN promotion_name, DROP COLUMN promotion_name_en;
DROP TABLE promotion_redemptions;
DROP TABLE promotions;
