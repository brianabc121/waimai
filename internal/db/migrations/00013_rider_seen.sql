-- +goose Up
-- When the rider first had the assignment on screen. The order desk flags a
-- delivery nobody has looked at two minutes after it was assigned.
ALTER TABLE delivery_tasks ADD COLUMN seen_at TIMESTAMPTZ;
-- Deliveries that were assigned before this column existed count as seen.
UPDATE delivery_tasks SET seen_at = assigned_at;

-- +goose Down
ALTER TABLE delivery_tasks DROP COLUMN seen_at;
