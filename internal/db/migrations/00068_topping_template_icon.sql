-- +goose Up
-- A small picture (an emoji) beside a topping template in the menu page's list,
-- like the flavour note templates have.
ALTER TABLE topping_templates ADD COLUMN icon TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE topping_templates DROP COLUMN icon;
