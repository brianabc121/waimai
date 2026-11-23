-- +goose Up
-- Messages in "My messages" are kept as a text key and its values
-- (docs/I18N_DESIGN.md §10), so they read in the customer's language of the
-- moment. title / body stay as the Chinese and English fallback.
ALTER TABLE customer_messages ADD COLUMN msg_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN msg_args JSONB NOT NULL DEFAULT '{}';
-- Each device's language, for its push reminders.
ALTER TABLE rider_devices ADD COLUMN lang TEXT NOT NULL DEFAULT '';
ALTER TABLE customer_devices ADD COLUMN lang TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE customer_devices DROP COLUMN lang;
ALTER TABLE rider_devices DROP COLUMN lang;
ALTER TABLE customer_messages DROP COLUMN msg_key, DROP COLUMN msg_args;
