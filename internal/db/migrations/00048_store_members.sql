-- +goose Up
-- Orders the store places for a customer who asked over WeChat, LINE,
-- WhatsApp or the phone (docs/MANUAL_ORDER_DESIGN.md).

-- A short code staff type to find a dish, such as B1.
ALTER TABLE menu_items ADD COLUMN code TEXT NOT NULL DEFAULT '';

-- The store's own members. Numbers count up per store and are shown with at
-- least six digits. "New member" takes the next number at once as a temporary
-- member; one that is neither filled in nor ordered for within an hour is
-- removed again.
CREATE TABLE store_members (
    id           BIGSERIAL PRIMARY KEY,
    branch_id    BIGINT NOT NULL REFERENCES branches(id),
    member_no    INT NOT NULL CHECK (member_no > 0),
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('temp','active')),
    phone        TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    source       TEXT NOT NULL DEFAULT '',
    contact      TEXT NOT NULL DEFAULT '',
    address_line TEXT NOT NULL DEFAULT '',
    address_unit TEXT NOT NULL DEFAULT '',
    address_note TEXT NOT NULL DEFAULT '',
    lat          DOUBLE PRECISION,
    lng          DOUBLE PRECISION,
    note         TEXT NOT NULL DEFAULT '',
    expires_at   TIMESTAMPTZ,
    created_by   BIGINT REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (branch_id, member_no)
);
CREATE INDEX store_members_phone_idx ON store_members (branch_id, phone);
CREATE INDEX store_members_temp_idx ON store_members (expires_at) WHERE status = 'temp';

ALTER TABLE orders ADD COLUMN member_id BIGINT REFERENCES store_members(id) ON DELETE SET NULL,
    ADD COLUMN order_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN placed_by BIGINT REFERENCES users(id),
    ADD COLUMN staff_note TEXT NOT NULL DEFAULT '';
CREATE INDEX orders_member_idx ON orders (member_id) WHERE member_id IS NOT NULL;

-- +goose Down
DROP INDEX orders_member_idx;
ALTER TABLE orders DROP COLUMN staff_note, DROP COLUMN placed_by, DROP COLUMN order_source, DROP COLUMN member_id;
DROP TABLE store_members;
ALTER TABLE menu_items DROP COLUMN code;
