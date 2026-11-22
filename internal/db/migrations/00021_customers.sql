-- +goose Up
-- Customer accounts: one per phone number, shared by every store. Logging in
-- with a texted code creates the account.
CREATE TABLE customers (
    id            BIGSERIAL PRIMARY KEY,
    phone         TEXT NOT NULL UNIQUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Phones that were verified or ordered before accounts existed get one.
INSERT INTO customers (phone)
SELECT phone FROM customer_sessions
UNION
SELECT customer_phone FROM orders WHERE customer_phone <> ''
ON CONFLICT DO NOTHING;

ALTER TABLE customer_sessions ADD COLUMN customer_id BIGINT REFERENCES customers(id);
UPDATE customer_sessions s SET customer_id = c.id FROM customers c WHERE c.phone = s.phone;

ALTER TABLE orders ADD COLUMN customer_id BIGINT REFERENCES customers(id);
UPDATE orders o SET customer_id = c.id FROM customers c WHERE c.phone = o.customer_phone;
CREATE INDEX orders_customer_idx ON orders (customer_id, id DESC) WHERE customer_id IS NOT NULL;

-- Where a code was requested from, for the daily cap per network address.
ALTER TABLE phone_verifications ADD COLUMN ip TEXT NOT NULL DEFAULT '';
CREATE INDEX phone_verifications_ip_idx ON phone_verifications (ip, created_at DESC) WHERE ip <> '';

-- +goose Down
DROP INDEX phone_verifications_ip_idx;
ALTER TABLE phone_verifications DROP COLUMN ip;
DROP INDEX orders_customer_idx;
ALTER TABLE orders DROP COLUMN customer_id;
ALTER TABLE customer_sessions DROP COLUMN customer_id;
DROP TABLE customers;
