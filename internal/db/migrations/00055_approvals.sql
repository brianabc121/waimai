-- +goose Up
-- Manager approval (docs/STAFF_ACCESS_DESIGN.md §7): a 6-digit code of their
-- own, apart from the password, that lets an order taker do one thing their
-- role may not.
ALTER TABLE users ADD COLUMN approve_pin_hash TEXT,
    ADD COLUMN approve_fails INT NOT NULL DEFAULT 0,
    ADD COLUMN approve_locked_until TIMESTAMPTZ;

-- One approval is good for one action of one person, for a few minutes.
CREATE TABLE approvals (
    token_hash   BYTEA PRIMARY KEY,
    branch_id    BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    perm         TEXT NOT NULL,
    approver_id  BIGINT NOT NULL REFERENCES users(id),
    requester_id BIGINT NOT NULL REFERENCES users(id),
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE audit_logs ADD COLUMN approved_by BIGINT;

-- +goose Down
ALTER TABLE audit_logs DROP COLUMN approved_by;
DROP TABLE approvals;
ALTER TABLE users DROP COLUMN approve_pin_hash, DROP COLUMN approve_fails, DROP COLUMN approve_locked_until;
