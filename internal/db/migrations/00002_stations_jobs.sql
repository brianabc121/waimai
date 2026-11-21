-- +goose Up
CREATE TABLE kitchen_stations (
    id          BIGSERIAL PRIMARY KEY,
    branch_id   BIGINT NOT NULL REFERENCES branches(id),
    name        TEXT NOT NULL,
    name_en     TEXT NOT NULL DEFAULT '',
    active      BOOLEAN NOT NULL DEFAULT true,
    sort        INT NOT NULL DEFAULT 0,
    UNIQUE (id, branch_id)
);
-- Names are unique among the active stations of a branch.
CREATE UNIQUE INDEX kitchen_stations_active_name ON kitchen_stations (branch_id, name) WHERE active;

ALTER TABLE menu_items ADD COLUMN station_id BIGINT,
    ADD FOREIGN KEY (station_id, branch_id) REFERENCES kitchen_stations (id, branch_id);

-- The station an order item was dispatched to, fixed when the order is accepted.
-- Later menu or station changes only affect new orders; moving an item that is
-- already in the kitchen is an explicit, audited action.
ALTER TABLE order_items ADD COLUMN station_id BIGINT,
    ADD COLUMN station_name TEXT NOT NULL DEFAULT '',
    ADD FOREIGN KEY (station_id, branch_id) REFERENCES kitchen_stations (id, branch_id);

ALTER TABLE branches ADD COLUMN notify_sms BOOLEAN NOT NULL DEFAULT false;

-- Durable background jobs (outbox). Enqueued in the same transaction as the
-- business change; the worker leases them with SKIP LOCKED and retries.
CREATE TABLE jobs (
    id            BIGSERIAL PRIMARY KEY,
    kind          TEXT NOT NULL,
    payload       JSONB NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','done','failed')),
    attempts      INT NOT NULL DEFAULT 0,
    run_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until  TIMESTAMPTZ,
    lease_token   UUID,
    last_error    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ
);
CREATE INDEX jobs_pending_idx ON jobs (run_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS jobs;
ALTER TABLE branches DROP COLUMN IF EXISTS notify_sms;
ALTER TABLE order_items DROP COLUMN IF EXISTS station_id, DROP COLUMN IF EXISTS station_name;
ALTER TABLE menu_items DROP COLUMN IF EXISTS station_id;
DROP TABLE IF EXISTS kitchen_stations;
