-- +goose Up
-- The dish page: more pictures besides the main one, and a longer text
-- (ingredients, portion, how it is made) under the short description.
ALTER TABLE menu_items
    ADD COLUMN photos  JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN details TEXT  NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE menu_items DROP COLUMN photos, DROP COLUMN details;
