-- +goose Up
-- Chat between a customer and a store. The messages travel through OpenIM
-- (docs/CHAT_DESIGN.md); these tables are this system's own index and copy,
-- written from OpenIM's callbacks. The lists, the unread badges and the
-- platform's "all conversations" page read them.

-- Accounts already created in OpenIM: c_<customer id> and b_<branch id>.
CREATE TABLE chat_accounts (
    im_user_id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One conversation per customer and store.
CREATE TABLE chat_threads (
    id               BIGSERIAL PRIMARY KEY,
    customer_id      BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    branch_id        BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    last_kind        TEXT NOT NULL DEFAULT 'text',
    last_text        TEXT NOT NULL DEFAULT '',
    last_sender      TEXT NOT NULL DEFAULT 'customer',
    last_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    msg_count        INT NOT NULL DEFAULT 0,
    customer_unread  INT NOT NULL DEFAULT 0,
    store_unread     INT NOT NULL DEFAULT 0,
    -- the other side shows "read" on messages sent up to these times
    customer_read_at TIMESTAMPTZ,
    store_read_at    TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, branch_id)
);
CREATE INDEX chat_threads_branch_idx ON chat_threads (branch_id, last_at DESC);
CREATE INDEX chat_threads_customer_idx ON chat_threads (customer_id, last_at DESC);
CREATE INDEX chat_threads_last_idx ON chat_threads (last_at DESC, id DESC);

CREATE TABLE chat_messages (
    id            BIGSERIAL PRIMARY KEY,
    thread_id     BIGINT NOT NULL REFERENCES chat_threads(id) ON DELETE CASCADE,
    sender        TEXT NOT NULL CHECK (sender IN ('customer','store','system')),
    -- the member of staff who answered for the store
    staff_user_id BIGINT REFERENCES users(id),
    kind          TEXT NOT NULL CHECK (kind IN ('text','image','order','system')),
    body          TEXT NOT NULL DEFAULT '',
    order_id      BIGINT REFERENCES orders(id),
    -- OpenIM's id of the message; a callback that is repeated changes nothing
    im_msg_id     TEXT NOT NULL UNIQUE,
    sent_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX chat_messages_thread_idx ON chat_messages (thread_id, id);

-- The platform can close a store's chat, or stop a customer from writing.
ALTER TABLE branches ADD COLUMN chat_enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE customers ADD COLUMN chat_muted_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE customers DROP COLUMN chat_muted_at;
ALTER TABLE branches DROP COLUMN chat_enabled;
DROP TABLE chat_messages;
DROP TABLE chat_threads;
DROP TABLE chat_accounts;
