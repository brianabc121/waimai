-- +goose Up
-- Customer addresses and delivery heat maps (docs/DELIVERY_ADDRESS_STATS_DESIGN.md)
-- read delivery orders by store and time, or by time across every store.
CREATE INDEX orders_branch_delivery_created ON orders (branch_id, created_at) WHERE service_type = 'delivery';
CREATE INDEX orders_delivery_created ON orders (created_at) WHERE service_type = 'delivery';

-- +goose Down
DROP INDEX orders_delivery_created;
DROP INDEX orders_branch_delivery_created;
