-- +goose Up
-- Queue display (docs/QUEUE_DISPLAY_DESIGN.md §12.1): the store's queue
-- settings, the screens paired to show the queue, the restaurant waiting list
-- and the pickup numbers called again at the counter.

-- Settings of the queue pages (QueueSettings in internal/app/queue_screens.go);
-- an empty object means the defaults.
ALTER TABLE branches ADD COLUMN queue_settings JSONB NOT NULL DEFAULT '{}';

-- A TV, monitor or old tablet that shows the queue. Paired like a table
-- tablet: a one-time code turns into a long-lived token that can only read.
CREATE TABLE queue_screens (
    id              BIGSERIAL PRIMARY KEY,
    branch_id       BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name            TEXT NOT NULL DEFAULT '',
    mode            TEXT NOT NULL DEFAULT 'tea' CHECK (mode IN ('tea', 'restaurant')),
    orientation     TEXT NOT NULL DEFAULT 'landscape' CHECK (orientation IN ('landscape', 'portrait')),
    lang            TEXT NOT NULL DEFAULT '',
    lang2           TEXT NOT NULL DEFAULT '',
    pair_code_hash  BYTEA,
    pair_expires_at TIMESTAMPTZ,
    token_hash      BYTEA UNIQUE,
    paired_at       TIMESTAMPTZ,
    last_seen_at    TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_by      BIGINT REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX queue_screens_branch ON queue_screens (branch_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX queue_screens_pair_code ON queue_screens (pair_code_hash) WHERE pair_code_hash IS NOT NULL;

-- Restaurant mode (§6): the groups of tables by party size, the numbers
-- taken for them, and every step of a number for the queue log.
CREATE TABLE queue_groups (
    id         BIGSERIAL PRIMARY KEY,
    branch_id  BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    letter     TEXT NOT NULL CHECK (letter ~ '^[A-Z]$'),
    -- preset names the default label (small, medium, large, room) shown in
    -- the reader's language while label is empty.
    preset     TEXT NOT NULL DEFAULT '',
    label      TEXT NOT NULL DEFAULT '',
    label_en   TEXT NOT NULL DEFAULT '',
    min_people INT NOT NULL CHECK (min_people BETWEEN 1 AND 99),
    max_people INT NOT NULL CHECK (max_people BETWEEN 1 AND 99),
    -- Empty: the tables whose seats are within the range.
    table_ids  BIGINT[] NOT NULL DEFAULT '{}',
    sort       INT NOT NULL DEFAULT 0,
    active     BOOLEAN NOT NULL DEFAULT true,
    CHECK (min_people <= max_people)
);
CREATE UNIQUE INDEX queue_groups_letter ON queue_groups (branch_id, letter) WHERE active;

CREATE TABLE queue_tickets (
    id               BIGSERIAL PRIMARY KEY,
    branch_id        BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    biz_date         DATE NOT NULL,
    group_id         BIGINT NOT NULL REFERENCES queue_groups(id),
    letter           TEXT NOT NULL,
    seq              INT NOT NULL,
    people           INT NOT NULL CHECK (people BETWEEN 1 AND 99),
    source           TEXT NOT NULL CHECK (source IN ('qr', 'paper', 'staff')),
    phone            TEXT NOT NULL DEFAULT '',
    tags             TEXT[] NOT NULL DEFAULT '{}',
    status           TEXT NOT NULL DEFAULT 'waiting'
        CHECK (status IN ('waiting', 'called', 'seated', 'skipped', 'cancelled', 'void')),
    -- The order of the line within a group; jumping the queue changes it.
    rank             DOUBLE PRECISION NOT NULL,
    calls            INT NOT NULL DEFAULT 0,
    called_at        TIMESTAMPTZ,
    called_table_id  BIGINT REFERENCES dining_tables(id),
    seated_at        TIMESTAMPTZ,
    seated_table_id  BIGINT REFERENCES dining_tables(id),
    session_id       BIGINT REFERENCES dining_sessions(id),
    jump_reason      TEXT NOT NULL DEFAULT '',
    lang             TEXT NOT NULL DEFAULT '',
    -- The guest's page (/q/t/<token>) and the browser that took it (one
    -- number at a time per browser and per phone).
    token_hash       BYTEA NOT NULL UNIQUE,
    token_nonce      BYTEA NOT NULL,
    client_hash      BYTEA,
    created_by       BIGINT REFERENCES users(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at        TIMESTAMPTZ,
    UNIQUE (branch_id, biz_date, letter, seq)
);
CREATE INDEX queue_tickets_open ON queue_tickets (branch_id, biz_date) WHERE status IN ('waiting', 'called', 'skipped');
CREATE INDEX queue_tickets_day ON queue_tickets (branch_id, biz_date, id);

CREATE TABLE queue_events (
    id         BIGSERIAL PRIMARY KEY,
    branch_id  BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    ticket_id  BIGINT NOT NULL REFERENCES queue_tickets(id) ON DELETE CASCADE,
    action     TEXT NOT NULL,
    actor_id   BIGINT REFERENCES users(id),
    detail     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX queue_events_branch ON queue_events (branch_id, id DESC);
CREATE INDEX queue_events_ticket ON queue_events (ticket_id, id);

-- A pickup number called again ("重叫") at the counter: the screen shows it
-- big once more and reads it out.
CREATE TABLE pickup_calls (
    order_id  BIGINT PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
    branch_id BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    calls     INT NOT NULL DEFAULT 0,
    called_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    called_by BIGINT REFERENCES users(id)
);

-- The new permissions (§10): using the queue is on wherever the role can
-- run tables or see orders; jumping the queue and its settings follow
-- "set up tables" for managers. Stores with the defaults get them from the
-- catalog.
UPDATE branch_role_perms SET perms = array_append(perms, 'queue.use')
    WHERE ('table.use' = ANY(perms) OR 'order.view' = ANY(perms)) AND NOT 'queue.use' = ANY(perms);
UPDATE branch_role_perms SET perms = array_append(perms, 'queue.jump')
    WHERE role = 'manager' AND 'queue.use' = ANY(perms) AND NOT 'queue.jump' = ANY(perms);
UPDATE branch_role_perms SET perms = array_append(perms, 'queue.manage')
    WHERE role = 'manager' AND 'queue.use' = ANY(perms) AND NOT 'queue.manage' = ANY(perms);

-- +goose Down
UPDATE branch_role_perms SET perms = array_remove(array_remove(array_remove(perms, 'queue.use'), 'queue.jump'), 'queue.manage');
DROP TABLE pickup_calls;
DROP TABLE queue_events;
DROP TABLE queue_tickets;
DROP TABLE queue_groups;
DROP TABLE queue_screens;
ALTER TABLE branches DROP COLUMN queue_settings;
