-- +goose Up
-- Dine-in desk for staff (docs/DINE_IN_ORDER_DESIGN.md §10.1): areas and
-- seats on tables, tags on a sitting and on a round, rushing a dish, moving a
-- sitting to another table, and how the bill was paid.
ALTER TABLE dining_tables ADD COLUMN area TEXT NOT NULL DEFAULT '',
    ADD COLUMN seats INT NOT NULL DEFAULT 4 CHECK (seats BETWEEN 1 AND 99);
ALTER TABLE dining_sessions ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN moved_from TEXT NOT NULL DEFAULT '';
ALTER TABLE order_batches ADD COLUMN round_tags TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN placed_by BIGINT REFERENCES users(id);
ALTER TABLE order_items ADD COLUMN rush_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN pay_method TEXT NOT NULL DEFAULT '';
-- The desk's preset tags and choices; NULL means the built-in defaults.
ALTER TABLE branches ADD COLUMN dine_presets JSONB;

-- +goose Down
ALTER TABLE branches DROP COLUMN dine_presets;
ALTER TABLE orders DROP COLUMN pay_method;
ALTER TABLE order_items DROP COLUMN rush_at;
ALTER TABLE order_batches DROP COLUMN round_tags, DROP COLUMN placed_by;
ALTER TABLE dining_sessions DROP COLUMN tags, DROP COLUMN moved_from;
ALTER TABLE dining_tables DROP COLUMN area, DROP COLUMN seats;
