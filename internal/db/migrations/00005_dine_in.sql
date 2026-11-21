-- +goose Up
-- Dine-in. A table session is one sitting at a table; it owns one order (the
-- bill), which grows by batches. Closing the session ends every credential
-- that was issued for it.
ALTER TABLE orders DROP CONSTRAINT orders_service_type_check,
    ADD CONSTRAINT orders_service_type_check CHECK (service_type IN ('delivery','pickup','dine_in'));
ALTER TABLE orders DROP CONSTRAINT orders_payment_method_check,
    ADD CONSTRAINT orders_payment_method_check CHECK (payment_method IN ('cod','pay_at_pickup','pay_at_counter'));
ALTER TABLE orders DROP CONSTRAINT orders_fulfillment_status_check,
    ADD CONSTRAINT orders_fulfillment_status_check CHECK (fulfillment_status IN ('none','awaiting_handoff',
        'out_for_delivery','arrived','delivered','delivery_failed','awaiting_pickup','picked_up','dined'));

CREATE TABLE dining_tables (
    id         BIGSERIAL PRIMARY KEY,
    branch_id  BIGINT NOT NULL REFERENCES branches(id),
    name       TEXT NOT NULL,
    qr_token   TEXT NOT NULL UNIQUE,   -- printed on the table; only locates the table
    active     BOOLEAN NOT NULL DEFAULT true,
    sort       INT NOT NULL DEFAULT 0,
    UNIQUE (id, branch_id)
);
CREATE UNIQUE INDEX dining_tables_active_name ON dining_tables (branch_id, name) WHERE active;

CREATE TABLE dining_sessions (
    id                BIGSERIAL PRIMARY KEY,
    branch_id         BIGINT NOT NULL REFERENCES branches(id),
    table_id          BIGINT NOT NULL,
    order_id          BIGINT NOT NULL REFERENCES orders(id),
    status            TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
    guests            INT NOT NULL DEFAULT 0,
    access_token_hash BYTEA NOT NULL UNIQUE,
    token_nonce       BYTEA NOT NULL,
    opened_by         BIGINT,
    opened_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_by         BIGINT,
    closed_at         TIMESTAMPTZ,
    FOREIGN KEY (table_id, branch_id) REFERENCES dining_tables (id, branch_id)
);
-- At most one open session per table.
CREATE UNIQUE INDEX dining_sessions_one_open ON dining_sessions (table_id) WHERE status = 'open';
CREATE INDEX dining_sessions_branch ON dining_sessions (branch_id, id DESC);

-- Tablets paired to a table by staff. A device acts for whatever session is
-- open on its table; revoking it or re-pairing ends its access.
CREATE TABLE table_devices (
    id              BIGSERIAL PRIMARY KEY,
    branch_id       BIGINT NOT NULL REFERENCES branches(id),
    table_id        BIGINT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    pair_code_hash  BYTEA UNIQUE,          -- one-time code shown to staff; cleared when used
    pair_expires_at TIMESTAMPTZ,
    token_hash      BYTEA UNIQUE,          -- set when the tablet pairs
    paired_at       TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_by      BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (table_id, branch_id) REFERENCES dining_tables (id, branch_id)
);
CREATE INDEX table_devices_table ON table_devices (table_id) WHERE revoked_at IS NULL;

ALTER TABLE orders ADD COLUMN session_id BIGINT REFERENCES dining_sessions(id),
    ADD COLUMN table_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN amount_received BIGINT,
    ADD COLUMN payment_note TEXT NOT NULL DEFAULT '';
ALTER TABLE order_batches ADD COLUMN source TEXT NOT NULL DEFAULT 'web';   -- web, qr, ipad, staff
ALTER TABLE order_items ADD COLUMN served_at TIMESTAMPTZ,
    ADD COLUMN void_reason TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE order_items DROP COLUMN served_at, DROP COLUMN void_reason;
ALTER TABLE order_batches DROP COLUMN source;
ALTER TABLE orders DROP COLUMN session_id, DROP COLUMN table_name, DROP COLUMN amount_received, DROP COLUMN payment_note;
DROP TABLE table_devices;
DROP TABLE dining_sessions;
DROP TABLE dining_tables;
