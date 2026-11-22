-- +goose Up
-- Chat, third part: reminders outside the page, reports and blocks
-- (docs/CHAT_DESIGN.md).

-- A customer's browser that asked to be told about answers (web push).
CREATE TABLE customer_devices (
    id          BIGSERIAL PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    token       TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX customer_devices_customer_idx ON customer_devices (customer_id);

ALTER TABLE chat_threads
    -- the order a customer was last sent a text message about, for an answer
    -- that stayed unread: one text per order
    ADD COLUMN sms_order_id BIGINT,
    -- the customer does not want reminders about this store's messages
    ADD COLUMN customer_quiet BOOLEAN NOT NULL DEFAULT false,
    -- the customer removed the conversation from their list; it comes back
    -- with the next message
    ADD COLUMN customer_hidden_at TIMESTAMPTZ;

-- A store no longer takes messages from a customer.
CREATE TABLE chat_blocks (
    customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    branch_id   BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    staff_user_id BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (customer_id, branch_id)
);

-- A conversation one side reported to the platform.
CREATE TABLE chat_reports (
    id          BIGSERIAL PRIMARY KEY,
    thread_id   BIGINT NOT NULL REFERENCES chat_threads(id) ON DELETE CASCADE,
    reported_by TEXT NOT NULL CHECK (reported_by IN ('customer','store')),
    staff_user_id BIGINT REFERENCES users(id),
    reason      TEXT NOT NULL,
    detail      TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 1000),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
CREATE INDEX chat_reports_thread_idx ON chat_reports (thread_id);
CREATE INDEX chat_reports_open_idx ON chat_reports (created_at DESC) WHERE resolved_at IS NULL;

-- +goose Down
DROP TABLE chat_reports;
DROP TABLE chat_blocks;
ALTER TABLE chat_threads DROP COLUMN sms_order_id, DROP COLUMN customer_quiet, DROP COLUMN customer_hidden_at;
DROP TABLE customer_devices;
