-- +goose Up
-- A customer's after-sales request on an order (wrong dish, something missing,
-- wants the money back). Orders are paid in cash, so nothing is refunded by
-- the system: the store reads the request, settles it with the customer and
-- records the outcome here. One request per order.
CREATE TABLE order_aftersales (
    id         BIGSERIAL PRIMARY KEY,
    order_id   BIGINT NOT NULL UNIQUE REFERENCES orders(id),
    branch_id  BIGINT NOT NULL REFERENCES branches(id),
    reason     TEXT NOT NULL,
    detail     TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 500),
    status     TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','declined')),
    reply      TEXT NOT NULL DEFAULT '' CHECK (length(reply) <= 500),
    handled_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    handled_at TIMESTAMPTZ
);
CREATE INDEX order_aftersales_open_idx ON order_aftersales (branch_id) WHERE status = 'open';

-- +goose Down
DROP TABLE order_aftersales;
