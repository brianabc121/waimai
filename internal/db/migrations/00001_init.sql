-- +goose Up
CREATE TABLE merchants (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE branches (
    id                      BIGSERIAL PRIMARY KEY,
    merchant_id             BIGINT NOT NULL REFERENCES merchants(id),
    slug                    TEXT NOT NULL UNIQUE,
    name                    TEXT NOT NULL,
    name_en                 TEXT NOT NULL DEFAULT '',
    address                 TEXT NOT NULL DEFAULT '',
    phone                   TEXT NOT NULL DEFAULT '',
    timezone                TEXT NOT NULL DEFAULT 'Asia/Manila',
    currency                TEXT NOT NULL DEFAULT 'PHP',
    tax_rate_bp             INT  NOT NULL DEFAULT 1200 CHECK (tax_rate_bp >= 0),
    tax_inclusive           BOOLEAN NOT NULL DEFAULT true,
    service_charge_bp       INT  NOT NULL DEFAULT 0 CHECK (service_charge_bp >= 0),
    packaging_fee           BIGINT NOT NULL DEFAULT 0 CHECK (packaging_fee >= 0),
    min_order_amount        BIGINT NOT NULL DEFAULT 0 CHECK (min_order_amount >= 0),
    accept_timeout_minutes  INT  NOT NULL DEFAULT 10 CHECK (accept_timeout_minutes > 0),
    delivery_enabled        BOOLEAN NOT NULL DEFAULT true,
    pickup_enabled          BOOLEAN NOT NULL DEFAULT true,
    paused                  BOOLEAN NOT NULL DEFAULT false,
    -- opening hours in branch local time, "HH:MM"; equal values mean open all day
    open_time               TEXT NOT NULL DEFAULT '00:00',
    close_time              TEXT NOT NULL DEFAULT '00:00',
    order_seq_date          DATE,
    order_seq               INT NOT NULL DEFAULT 0,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, merchant_id)
);

CREATE TABLE users (
    id                BIGSERIAL PRIMARY KEY,
    username          TEXT NOT NULL UNIQUE,
    password_hash     TEXT NOT NULL,
    display_name      TEXT NOT NULL,
    phone             TEXT NOT NULL DEFAULT '',
    is_platform_admin BOOLEAN NOT NULL DEFAULT false,
    active            BOOLEAN NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_branch_roles (
    user_id    BIGINT NOT NULL REFERENCES users(id),
    branch_id  BIGINT NOT NULL REFERENCES branches(id),
    role       TEXT NOT NULL CHECK (role IN ('owner','manager','cashier','kitchen','rider')),
    PRIMARY KEY (user_id, branch_id)
);

CREATE TABLE sessions (
    token_hash  BYTEA PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id),
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE phone_verifications (
    id           BIGSERIAL PRIMARY KEY,
    phone        TEXT NOT NULL,
    code_hash    BYTEA NOT NULL,
    attempts     INT NOT NULL DEFAULT 0,
    expires_at   TIMESTAMPTZ NOT NULL,
    consumed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX phone_verifications_phone_idx ON phone_verifications (phone, created_at DESC);

CREATE TABLE customer_sessions (
    token_hash  BYTEA PRIMARY KEY,
    phone       TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE delivery_zones (
    id          BIGSERIAL PRIMARY KEY,
    branch_id   BIGINT NOT NULL REFERENCES branches(id),
    name        TEXT NOT NULL,
    name_en     TEXT NOT NULL DEFAULT '',
    fee         BIGINT NOT NULL CHECK (fee >= 0),
    min_order   BIGINT NOT NULL DEFAULT 0 CHECK (min_order >= 0),
    active      BOOLEAN NOT NULL DEFAULT true,
    sort        INT NOT NULL DEFAULT 0,
    UNIQUE (id, branch_id)
);

CREATE TABLE menu_categories (
    id          BIGSERIAL PRIMARY KEY,
    branch_id   BIGINT NOT NULL REFERENCES branches(id),
    name        TEXT NOT NULL,
    name_en     TEXT NOT NULL DEFAULT '',
    sort        INT NOT NULL DEFAULT 0,
    UNIQUE (id, branch_id)
);

CREATE TABLE menu_items (
    id                  BIGSERIAL PRIMARY KEY,
    branch_id           BIGINT NOT NULL REFERENCES branches(id),
    category_id         BIGINT NOT NULL,
    name                TEXT NOT NULL,
    name_en             TEXT NOT NULL DEFAULT '',
    description         TEXT NOT NULL DEFAULT '',
    image_url           TEXT NOT NULL DEFAULT '',
    price               BIGINT NOT NULL CHECK (price >= 0),
    published           BOOLEAN NOT NULL DEFAULT false,
    sold_out            BOOLEAN NOT NULL DEFAULT false,
    stock_qty           INT CHECK (stock_qty IS NULL OR stock_qty >= 0),
    needs_prep          BOOLEAN NOT NULL DEFAULT true,
    delivery_available  BOOLEAN NOT NULL DEFAULT true,
    pickup_available    BOOLEAN NOT NULL DEFAULT true,
    sort                INT NOT NULL DEFAULT 0,
    archived            BOOLEAN NOT NULL DEFAULT false,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, branch_id),
    FOREIGN KEY (category_id, branch_id) REFERENCES menu_categories (id, branch_id)
);
CREATE INDEX menu_items_branch_idx ON menu_items (branch_id) WHERE NOT archived;

CREATE TABLE option_groups (
    id          BIGSERIAL PRIMARY KEY,
    item_id     BIGINT NOT NULL REFERENCES menu_items(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    name_en     TEXT NOT NULL DEFAULT '',
    min_select  INT NOT NULL DEFAULT 0 CHECK (min_select >= 0),
    max_select  INT NOT NULL DEFAULT 1 CHECK (max_select >= 1),
    sort        INT NOT NULL DEFAULT 0,
    CHECK (min_select <= max_select)
);
CREATE INDEX option_groups_item_idx ON option_groups (item_id);

CREATE TABLE options (
    id           BIGSERIAL PRIMARY KEY,
    group_id     BIGINT NOT NULL REFERENCES option_groups(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    name_en      TEXT NOT NULL DEFAULT '',
    price_delta  BIGINT NOT NULL DEFAULT 0 CHECK (price_delta >= 0),
    sold_out     BOOLEAN NOT NULL DEFAULT false,
    sort         INT NOT NULL DEFAULT 0
);
CREATE INDEX options_group_idx ON options (group_id);

CREATE TABLE orders (
    id                  BIGSERIAL PRIMARY KEY,
    merchant_id         BIGINT NOT NULL,
    branch_id           BIGINT NOT NULL,
    public_no           TEXT NOT NULL,
    access_token_hash   BYTEA NOT NULL UNIQUE,
    token_nonce         BYTEA NOT NULL,
    pickup_code         TEXT NOT NULL,
    channel             TEXT NOT NULL DEFAULT 'web' CHECK (channel IN ('web','staff','dine_in')),
    service_type        TEXT NOT NULL CHECK (service_type IN ('delivery','pickup')),
    customer_name       TEXT NOT NULL,
    customer_phone      TEXT NOT NULL,
    zone_id             BIGINT,
    zone_name           TEXT NOT NULL DEFAULT '',
    address_line        TEXT NOT NULL DEFAULT '',
    address_unit        TEXT NOT NULL DEFAULT '',
    address_note        TEXT NOT NULL DEFAULT '',
    note                TEXT NOT NULL DEFAULT '',
    currency            TEXT NOT NULL,
    subtotal            BIGINT NOT NULL,
    packaging_fee       BIGINT NOT NULL,
    delivery_fee        BIGINT NOT NULL,
    service_charge      BIGINT NOT NULL,
    tax_amount          BIGINT NOT NULL,
    tax_inclusive       BOOLEAN NOT NULL,
    tax_rate_bp         INT NOT NULL,
    discount            BIGINT NOT NULL DEFAULT 0,
    total               BIGINT NOT NULL,
    payment_method      TEXT NOT NULL CHECK (payment_method IN ('cod','pay_at_pickup')),
    acceptance_status   TEXT NOT NULL DEFAULT 'pending'
        CHECK (acceptance_status IN ('pending','accepted','rejected','cancelled')),
    packing_status      TEXT NOT NULL DEFAULT 'none'
        CHECK (packing_status IN ('none','waiting_items','ready_to_pack','packed')),
    fulfillment_status  TEXT NOT NULL DEFAULT 'none'
        CHECK (fulfillment_status IN ('none','awaiting_handoff','out_for_delivery','arrived',
                                      'delivered','delivery_failed','awaiting_pickup','picked_up')),
    payment_status      TEXT NOT NULL DEFAULT 'unpaid' CHECK (payment_status IN ('unpaid','paid')),
    close_reason        TEXT NOT NULL DEFAULT '',
    bags                INT,
    version             INT NOT NULL DEFAULT 1,
    manifest_version    INT NOT NULL DEFAULT 1,
    accept_deadline     TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at         TIMESTAMPTZ,
    packed_at           TIMESTAMPTZ,
    handed_off_at       TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (branch_id, merchant_id) REFERENCES branches (id, merchant_id),
    UNIQUE (id, branch_id)
);
CREATE INDEX orders_branch_created_idx ON orders (branch_id, created_at DESC);
CREATE INDEX orders_pending_deadline_idx ON orders (accept_deadline) WHERE acceptance_status = 'pending';

CREATE TABLE order_batches (
    id          BIGSERIAL PRIMARY KEY,
    order_id    BIGINT NOT NULL REFERENCES orders(id),
    seq         INT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (order_id, seq)
);

CREATE TABLE order_items (
    id           BIGSERIAL PRIMARY KEY,
    order_id     BIGINT NOT NULL,
    branch_id    BIGINT NOT NULL,
    batch_id     BIGINT NOT NULL REFERENCES order_batches(id),
    item_id      BIGINT NOT NULL,
    name         TEXT NOT NULL,
    name_en      TEXT NOT NULL DEFAULT '',
    unit_price   BIGINT NOT NULL,
    qty          INT NOT NULL CHECK (qty > 0),
    line_total   BIGINT NOT NULL,
    options      JSONB NOT NULL DEFAULT '[]',
    note         TEXT NOT NULL DEFAULT '',
    needs_prep   BOOLEAN NOT NULL DEFAULT true,
    prep_status  TEXT NOT NULL DEFAULT 'pending'
        CHECK (prep_status IN ('pending','preparing','ready','cancelled')),
    FOREIGN KEY (order_id, branch_id) REFERENCES orders (id, branch_id)
);
CREATE INDEX order_items_order_idx ON order_items (order_id);

CREATE TABLE quotes (
    id             UUID PRIMARY KEY,
    branch_id      BIGINT NOT NULL REFERENCES branches(id),
    payload        JSONB NOT NULL,
    total          BIGINT NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    used_order_id  BIGINT REFERENCES orders(id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE delivery_tasks (
    id                  BIGSERIAL PRIMARY KEY,
    order_id            BIGINT NOT NULL UNIQUE,
    branch_id           BIGINT NOT NULL,
    rider_id            BIGINT NOT NULL REFERENCES users(id),
    assignment_version  INT NOT NULL DEFAULT 1,
    status              TEXT NOT NULL DEFAULT 'assigned'
        CHECK (status IN ('assigned','picked_up','arrived','delivered','failed','cancelled')),
    fail_reason         TEXT NOT NULL DEFAULT '',
    assigned_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    picked_up_at        TIMESTAMPTZ,
    arrived_at          TIMESTAMPTZ,
    delivered_at        TIMESTAMPTZ,
    FOREIGN KEY (order_id, branch_id) REFERENCES orders (id, branch_id)
);
CREATE INDEX delivery_tasks_rider_idx ON delivery_tasks (rider_id, status);

CREATE TABLE cash_collections (
    id                 BIGSERIAL PRIMARY KEY,
    order_id           BIGINT NOT NULL UNIQUE,
    branch_id          BIGINT NOT NULL,
    rider_id           BIGINT NOT NULL REFERENCES users(id),
    amount_due         BIGINT NOT NULL,
    amount_collected   BIGINT NOT NULL CHECK (amount_collected >= 0),
    reconcile_status   TEXT NOT NULL DEFAULT 'pending'
        CHECK (reconcile_status IN ('pending','reconciled','discrepancy')),
    amount_returned    BIGINT,
    reconciled_by      BIGINT REFERENCES users(id),
    reconciled_at      TIMESTAMPTZ,
    note               TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (order_id, branch_id) REFERENCES orders (id, branch_id)
);
CREATE INDEX cash_collections_branch_idx ON cash_collections (branch_id, created_at DESC);

CREATE TABLE idempotency_records (
    scope          TEXT NOT NULL,
    key            TEXT NOT NULL,
    request_hash   BYTEA NOT NULL,
    response       JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, key)
);

CREATE TABLE audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    branch_id   BIGINT NOT NULL,
    order_id    BIGINT,
    actor_type  TEXT NOT NULL,
    actor_id    BIGINT,
    action      TEXT NOT NULL,
    detail      JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_order_idx ON audit_logs (order_id, id);

-- +goose Down
DROP TABLE IF EXISTS audit_logs, idempotency_records, cash_collections, delivery_tasks, quotes,
    order_items, order_batches, orders, options, option_groups, menu_items, menu_categories,
    delivery_zones, customer_sessions, phone_verifications, sessions, user_branch_roles, users,
    branches, merchants CASCADE;
