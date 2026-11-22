-- +goose Up
-- The customer's own pages ("me"): a profile, favourite stores, messages,
-- feedback, applications to open a store, and a wallet ledger.
ALTER TABLE customers
    ADD COLUMN nickname  TEXT NOT NULL DEFAULT '' CHECK (length(nickname) <= 40),
    ADD COLUMN avatar_at TIMESTAMPTZ;

CREATE TABLE customer_avatars (
    customer_id BIGINT PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
    image       BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE customer_favorites (
    customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    branch_id   BIGINT NOT NULL REFERENCES branches(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (customer_id, branch_id)
);

-- What the platform tells a customer: order updates, answers to feedback,
-- the outcome of an application, changes to the wallet.
CREATE TABLE customer_messages (
    id          BIGSERIAL PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('order','service','wallet')),
    title       TEXT NOT NULL,
    title_en    TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    body_en     TEXT NOT NULL DEFAULT '',
    -- a path inside the site, opened when the message is tapped
    link        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at     TIMESTAMPTZ
);
CREATE INDEX customer_messages_idx ON customer_messages (customer_id, id DESC);

CREATE TABLE customer_feedback (
    id          BIGSERIAL PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('suggestion','problem','complaint','account','other')),
    content     TEXT NOT NULL CHECK (length(content) BETWEEN 1 AND 1000),
    contact     TEXT NOT NULL DEFAULT '' CHECK (length(contact) <= 100),
    reply       TEXT NOT NULL DEFAULT '' CHECK (length(reply) <= 1000),
    replied_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX customer_feedback_idx ON customer_feedback (created_at DESC);

-- Someone asking to open a store on the platform. Approving one does not
-- create the merchant: the platform does that by hand afterwards.
CREATE TABLE merchant_applications (
    id           BIGSERIAL PRIMARY KEY,
    customer_id  BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    store_name   TEXT NOT NULL CHECK (length(store_name) BETWEEN 1 AND 100),
    contact_name TEXT NOT NULL CHECK (length(contact_name) BETWEEN 1 AND 80),
    phone        TEXT NOT NULL CHECK (length(phone) BETWEEN 1 AND 40),
    city         TEXT NOT NULL DEFAULT '' CHECK (length(city) <= 100),
    cuisine      TEXT NOT NULL DEFAULT '' CHECK (length(cuisine) <= 100),
    note         TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
    reply        TEXT NOT NULL DEFAULT '' CHECK (length(reply) <= 1000),
    decided_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX merchant_applications_idx ON merchant_applications (created_at DESC);

-- The wallet is a ledger: the balance of an account is the sum of its
-- entries. Entries are only written by the platform (there is no online
-- top-up, and orders cannot be paid from it yet).
CREATE TABLE customer_wallet_entries (
    id          BIGSERIAL PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    account     TEXT NOT NULL CHECK (account IN ('balance','bonus')),
    currency    TEXT NOT NULL CHECK (length(currency) = 3),
    amount      BIGINT NOT NULL CHECK (amount <> 0),
    note        TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    admin_id    BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX customer_wallet_entries_idx ON customer_wallet_entries (customer_id, id DESC);

-- What the platform says about itself on the customer's pages.
CREATE TABLE platform_info (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL CHECK (length(value) <= 4000)
);

-- +goose Down
DROP TABLE platform_info;
DROP TABLE customer_wallet_entries;
DROP TABLE merchant_applications;
DROP TABLE customer_feedback;
DROP TABLE customer_messages;
DROP TABLE customer_favorites;
DROP TABLE customer_avatars;
ALTER TABLE customers DROP COLUMN nickname, DROP COLUMN avatar_at;
