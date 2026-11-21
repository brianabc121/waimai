-- +goose Up
-- Scheduled orders: the customer picks a time; the kitchen sees the order
-- prep_minutes before it.
ALTER TABLE branches ADD COLUMN scheduling_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN prep_minutes INT NOT NULL DEFAULT 30 CHECK (prep_minutes BETWEEN 5 AND 240);

ALTER TABLE orders ADD COLUMN scheduled_for TIMESTAMPTZ,
    ADD COLUMN release_at TIMESTAMPTZ,                          -- when the kitchen starts; NULL = as soon as accepted
    ADD COLUMN release_notified BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX orders_release_idx ON orders (release_at) WHERE release_at IS NOT NULL AND NOT release_notified;

-- Menu spreadsheet imports. The file is parsed in the background; the
-- merchant reviews row-level results and then confirms.
CREATE TABLE menu_imports (
    id           BIGSERIAL PRIMARY KEY,
    branch_id    BIGINT NOT NULL REFERENCES branches(id),
    uploaded_by  BIGINT,
    filename     TEXT NOT NULL,
    content      BYTEA,                 -- cleared once parsed
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','parsed','failed','applied')),
    error        TEXT NOT NULL DEFAULT '',
    rows         JSONB NOT NULL DEFAULT '[]',
    total_rows   INT NOT NULL DEFAULT 0,
    valid_rows   INT NOT NULL DEFAULT 0,
    error_rows   INT NOT NULL DEFAULT 0,
    created_count INT NOT NULL DEFAULT 0,
    updated_count INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_at   TIMESTAMPTZ
);
CREATE INDEX menu_imports_branch ON menu_imports (branch_id, id DESC);

-- +goose Down
DROP TABLE menu_imports;
DROP INDEX orders_release_idx;
ALTER TABLE orders DROP COLUMN scheduled_for, DROP COLUMN release_at, DROP COLUMN release_notified;
ALTER TABLE branches DROP COLUMN scheduling_enabled, DROP COLUMN prep_minutes;
