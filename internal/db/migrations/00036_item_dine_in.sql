-- +goose Up
-- A dish may be offered for dine-in, delivery and pickup separately; until
-- now every published dish could be ordered at the table.
ALTER TABLE menu_items ADD COLUMN dine_in_available BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE menu_items DROP COLUMN dine_in_available;
