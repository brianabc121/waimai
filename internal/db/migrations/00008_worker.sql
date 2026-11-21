-- +goose Up
-- What the back office shows about the background worker: one row per running
-- worker process (heartbeat) and one row per periodic task (last run).
CREATE TABLE worker_instances (
    instance      TEXT PRIMARY KEY,
    hostname      TEXT NOT NULL,
    embedded      BOOLEAN NOT NULL,
    started_at    TIMESTAMPTZ NOT NULL,
    last_beat_at  TIMESTAMPTZ NOT NULL,
    last_loop_ms  INT NOT NULL DEFAULT 0
);

CREATE TABLE worker_tasks (
    task             TEXT PRIMARY KEY,
    last_started_at  TIMESTAMPTZ,
    last_ok_at       TIMESTAMPTZ,
    last_duration_ms INT NOT NULL DEFAULT 0,
    last_count       INT NOT NULL DEFAULT 0,
    last_error       TEXT NOT NULL DEFAULT '',
    last_error_at    TIMESTAMPTZ,
    runs             BIGINT NOT NULL DEFAULT 0,
    failures         BIGINT NOT NULL DEFAULT 0
);

-- A job an administrator gave up on. It is kept for the record and cleaned up
-- with the finished ones.
ALTER TABLE jobs DROP CONSTRAINT jobs_status_check,
    ADD CONSTRAINT jobs_status_check CHECK (status IN ('pending','done','failed','discarded'));
CREATE INDEX jobs_status_idx ON jobs (status, id DESC);

-- +goose Down
DROP INDEX IF EXISTS jobs_status_idx;
UPDATE jobs SET status='failed' WHERE status='discarded';
ALTER TABLE jobs DROP CONSTRAINT jobs_status_check,
    ADD CONSTRAINT jobs_status_check CHECK (status IN ('pending','done','failed'));
DROP TABLE IF EXISTS worker_tasks, worker_instances;
