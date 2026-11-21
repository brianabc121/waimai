-- +goose Up
-- Platform back office: merchant lifecycle, soft deletes and the admin audit trail.
ALTER TABLE merchants
    ADD COLUMN status         TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
    ADD COLUMN contact_name   TEXT NOT NULL DEFAULT '',
    ADD COLUMN contact_phone  TEXT NOT NULL DEFAULT '',
    ADD COLUMN note           TEXT NOT NULL DEFAULT '',
    ADD COLUMN suspend_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN suspended_at   TIMESTAMPTZ,
    ADD COLUMN updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN deleted_at     TIMESTAMPTZ;

-- A deleted branch keeps its slug, so an old link or printed code can never
-- point at a different store.
ALTER TABLE branches ADD COLUMN deleted_at TIMESTAMPTZ;

ALTER TABLE users ADD COLUMN last_login_at TIMESTAMPTZ;

-- Archived items still reference their category, so categories are archived too.
ALTER TABLE menu_categories ADD COLUMN archived BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE admin_audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    admin_id     BIGINT NOT NULL REFERENCES users(id),
    action       TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    target_id    BIGINT,
    detail       JSONB NOT NULL DEFAULT '{}',
    ip           TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX admin_audit_logs_created_idx ON admin_audit_logs (created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS admin_audit_logs;
ALTER TABLE menu_categories DROP COLUMN IF EXISTS archived;
ALTER TABLE users DROP COLUMN IF EXISTS last_login_at;
ALTER TABLE branches DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE merchants DROP COLUMN IF EXISTS status, DROP COLUMN IF EXISTS contact_name,
    DROP COLUMN IF EXISTS contact_phone, DROP COLUMN IF EXISTS note, DROP COLUMN IF EXISTS suspend_reason,
    DROP COLUMN IF EXISTS suspended_at, DROP COLUMN IF EXISTS updated_at, DROP COLUMN IF EXISTS deleted_at;
