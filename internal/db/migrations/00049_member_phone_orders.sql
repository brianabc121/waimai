-- +goose Up
-- The member pages count a member's orders by member and by phone number.
CREATE INDEX orders_branch_phone_idx ON orders (branch_id, customer_phone);

-- +goose Down
DROP INDEX orders_branch_phone_idx;
