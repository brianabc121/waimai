-- +goose Up
-- Merchants no longer check translations one by one (docs/I18N_SIMPLE_DESIGN.md
-- §4.4): when the original changes, the merchant's own translation is translated
-- again and kept here, so the language window can offer it back.
ALTER TABLE content_translations ADD COLUMN prev_text TEXT NOT NULL DEFAULT '';
-- "stale" is gone: those are the merchant's translations of an older original,
-- which the next run translates again.
UPDATE content_translations SET status = 'human' WHERE status = 'stale';

-- +goose Down
ALTER TABLE content_translations DROP COLUMN prev_text;
