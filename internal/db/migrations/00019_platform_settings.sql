-- +goose Up
-- Keys of outside services (maps, push, SMS) that the platform administrator
-- sets in the back office. A row here wins over the same name in the
-- server's environment. Values are encrypted with a key derived from
-- APP_SECRET, so a copy of the database alone does not reveal them.
CREATE TABLE platform_settings (
    key        TEXT PRIMARY KEY,
    value      BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL
);

-- +goose Down
DROP TABLE platform_settings;
