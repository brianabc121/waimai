-- +goose Up
-- Adding dishes to an order before the rider takes it (docs/ADD_ON_ORDER_DESIGN.md §9.1).
-- An add-on is one more batch of the same order. The customer's own add-ons
-- wait for the store: their dishes stay in pending_lines until accepted, so
-- the kitchen, packing and the bill never see them before that.
ALTER TABLE order_batches
    ADD COLUMN status TEXT NOT NULL DEFAULT 'accepted'
        CHECK (status IN ('pending','accepted','rejected','expired','cancelled')),
    ADD COLUMN pending_lines JSONB,
    ADD COLUMN total_before BIGINT,
    ADD COLUMN total_after BIGINT,
    ADD COLUMN decided_by BIGINT REFERENCES users(id),
    ADD COLUMN decided_at TIMESTAMPTZ,
    ADD COLUMN reject_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN expires_at TIMESTAMPTZ,
    -- for online payment later: whether this batch was paid on its own
    ADD COLUMN paid BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX order_batches_pending_idx ON order_batches (expires_at) WHERE status = 'pending';

-- How long the store has to confirm a customer's add-on.
ALTER TABLE branches ADD COLUMN addon_confirm_minutes INT NOT NULL DEFAULT 5
    CHECK (addon_confirm_minutes BETWEEN 1 AND 60);

-- +goose Down
ALTER TABLE branches DROP COLUMN addon_confirm_minutes;
DROP INDEX order_batches_pending_idx;
ALTER TABLE order_batches DROP COLUMN status, DROP COLUMN pending_lines, DROP COLUMN total_before,
    DROP COLUMN total_after, DROP COLUMN decided_by, DROP COLUMN decided_at, DROP COLUMN reject_reason,
    DROP COLUMN expires_at, DROP COLUMN paid;
