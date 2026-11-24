-- +goose Up
-- Voiding dishes on any order, and refunds by dish once paid
-- (docs/VOID_ITEM_DESIGN.md §10.1). 00061 and 00062 belong to the checkout
-- discount work in another branch.

-- One row each time dishes are taken off an order.
CREATE TABLE order_voids (
    id             BIGSERIAL PRIMARY KEY,
    order_id       BIGINT NOT NULL REFERENCES orders(id),
    branch_id      BIGINT NOT NULL REFERENCES branches(id),
    batch_id       BIGINT REFERENCES order_batches(id),
    -- the cancelled line (a split-off one when only part of a line was voided)
    item_id        BIGINT NOT NULL REFERENCES order_items(id),
    name           TEXT NOT NULL,
    qty            INT NOT NULL CHECK (qty > 0),
    amount         BIGINT NOT NULL,
    reason         TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 200),
    reason_kind    TEXT NOT NULL CHECK (reason_kind IN ('store', 'customer')),
    -- how far the dish had got: pending, preparing, ready, served or packed
    was_status     TEXT NOT NULL,
    -- the dish was started or made, so it is lost and its stock not returned
    wasted         BOOLEAN NOT NULL,
    station_id     BIGINT,
    total_before   BIGINT NOT NULL,
    total_after    BIGINT NOT NULL,
    voided_by      BIGINT REFERENCES users(id),
    approved_by    BIGINT REFERENCES users(id),
    -- the kitchen saw it (only voids of dishes already started need this)
    needs_ack      BOOLEAN NOT NULL DEFAULT false,
    kitchen_ack_at TIMESTAMPTZ,
    kitchen_ack_by BIGINT REFERENCES users(id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_voids_order ON order_voids (order_id);
CREATE INDEX order_voids_branch_day ON order_voids (branch_id, created_at);
CREATE INDEX order_voids_unacked ON order_voids (branch_id) WHERE needs_ack AND kitchen_ack_at IS NULL;

ALTER TABLE order_items ADD COLUMN void_id BIGINT REFERENCES order_voids(id);

-- Money given back by dish after the customer paid (delivered, collected or
-- the table closed). The money moves outside the system; this records it.
CREATE TABLE order_refunds (
    id           BIGSERIAL PRIMARY KEY,
    order_id     BIGINT NOT NULL REFERENCES orders(id),
    branch_id    BIGINT NOT NULL REFERENCES branches(id),
    -- [{item_id, name, qty, amount}]
    lines        JSONB NOT NULL,
    amount       BIGINT NOT NULL CHECK (amount > 0),
    method       TEXT NOT NULL CHECK (length(method) BETWEEN 1 AND 20),
    reason       TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 200),
    reason_kind  TEXT NOT NULL CHECK (reason_kind IN ('store', 'customer')),
    refunded_by  BIGINT REFERENCES users(id),
    approved_by  BIGINT REFERENCES users(id),
    aftersale_id BIGINT REFERENCES order_aftersales(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_refunds_order ON order_refunds (order_id);
CREATE INDEX order_refunds_branch_day ON order_refunds (branch_id, created_at);

-- The new "void dishes already made" permission is a manager's by default;
-- stores whose owner already set the table give it to their managers too.
UPDATE branch_role_perms SET perms = array_append(perms, 'order.void')
    WHERE role = 'manager' AND NOT 'order.void' = ANY(perms);

-- +goose Down
UPDATE branch_role_perms SET perms = array_remove(perms, 'order.void');
DROP TABLE order_refunds;
ALTER TABLE order_items DROP COLUMN void_id;
DROP TABLE order_voids;
