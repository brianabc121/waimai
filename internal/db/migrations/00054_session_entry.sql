-- +goose Up
-- Signing in says where to (docs/STAFF_ACCESS_DESIGN.md §5): the store's back
-- office, the staff workbench, or the platform's own back office. Sessions
-- from before count as back office, so nobody is signed out.
ALTER TABLE sessions ADD COLUMN entry TEXT NOT NULL DEFAULT 'office'
    CHECK (entry IN ('office', 'work', 'admin'));
-- The platform signs in on its own page now: its old sessions end.
DELETE FROM sessions s USING users u WHERE u.id = s.user_id AND u.is_platform_admin;

-- The role the person had in the store when they did it.
ALTER TABLE audit_logs ADD COLUMN actor_role TEXT;

-- +goose Down
ALTER TABLE audit_logs DROP COLUMN actor_role;
ALTER TABLE sessions DROP COLUMN entry;
