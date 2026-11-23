-- +goose Up
-- Platform support: customers and stores write to the platform, and the
-- platform's administrators answer from one shared inbox. The messages travel
-- through OpenIM like the chats between customers and stores
-- (docs/CHAT_DESIGN.md section 17); these tables are this system's index and
-- copy of them.

-- One conversation per customer or store with the platform.
CREATE TABLE support_threads (
    id               BIGSERIAL PRIMARY KEY,
    -- the other side's OpenIM account: c_<customer id> or b_<branch id>
    party            TEXT NOT NULL UNIQUE,
    customer_id      BIGINT REFERENCES customers(id) ON DELETE CASCADE,
    branch_id        BIGINT REFERENCES branches(id) ON DELETE CASCADE,
    last_kind        TEXT NOT NULL DEFAULT 'text',
    last_text        TEXT NOT NULL DEFAULT '',
    last_sender      TEXT NOT NULL DEFAULT 'customer',
    last_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    msg_count        INT NOT NULL DEFAULT 0,
    party_unread     INT NOT NULL DEFAULT 0,
    platform_unread  INT NOT NULL DEFAULT 0,
    party_read_at    TIMESTAMPTZ,
    platform_read_at TIMESTAMPTZ,
    -- the platform marked the question dealt with; the next message from the
    -- customer or the store opens it again
    resolved_at      TIMESTAMPTZ,
    resolved_by      BIGINT REFERENCES users(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((customer_id IS NULL) <> (branch_id IS NULL))
);
CREATE INDEX support_threads_last_idx ON support_threads (last_at DESC, id DESC);

CREATE TABLE support_messages (
    id            BIGSERIAL PRIMARY KEY,
    thread_id     BIGINT NOT NULL REFERENCES support_threads(id) ON DELETE CASCADE,
    sender        TEXT NOT NULL CHECK (sender IN ('customer','store','platform')),
    -- the member of the store's staff, or the administrator, who wrote
    staff_user_id BIGINT REFERENCES users(id),
    kind          TEXT NOT NULL CHECK (kind IN ('text','image','order')),
    body          TEXT NOT NULL DEFAULT '',
    order_id      BIGINT REFERENCES orders(id) ON DELETE SET NULL,
    im_msg_id     TEXT NOT NULL UNIQUE,
    sent_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX support_messages_thread_idx ON support_messages (thread_id, id);

-- A picture sent in a support conversation, uploaded here first.
CREATE TABLE support_images (
    token      TEXT PRIMARY KEY,
    party      TEXT NOT NULL,
    sender     TEXT NOT NULL CHECK (sender IN ('party','platform')),
    content    BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX support_images_party_idx ON support_images (party, created_at);

-- +goose Down
DROP TABLE support_images;
DROP TABLE support_messages;
DROP TABLE support_threads;
