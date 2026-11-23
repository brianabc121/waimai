-- +goose Up
-- The language a customer chose on the site (docs/I18N_DESIGN.md §5.1): used
-- when they sign in on a new device, and for their texts and notifications.
-- Empty means they never chose; the device's language is used then.
ALTER TABLE customers ADD COLUMN lang TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE customers DROP COLUMN lang;
