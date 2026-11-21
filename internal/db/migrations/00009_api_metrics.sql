-- +goose Up
-- Request counts and latency per minute, written by every api process for
-- itself. hist holds the counts of the fixed latency buckets (see
-- app.LatencyBuckets), so that percentiles can be merged across processes.
CREATE TABLE api_metrics (
    bucket    TIMESTAMPTZ NOT NULL,
    instance  TEXT NOT NULL,
    method    TEXT NOT NULL,
    route     TEXT NOT NULL,
    status    INT  NOT NULL,          -- first digit of the status code: 2, 3, 4, 5
    count     INT  NOT NULL,
    limited   INT  NOT NULL DEFAULT 0, -- how many of them were 429 RATE_LIMITED
    sum_ms    BIGINT NOT NULL,
    max_ms    INT  NOT NULL,
    hist      INT[] NOT NULL,
    PRIMARY KEY (bucket, instance, method, route, status)
);

-- Every 5xx and a sample of the 429s, one row each.
CREATE TABLE api_errors (
    id          BIGSERIAL PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    request_id  TEXT NOT NULL,
    method      TEXT NOT NULL,
    route       TEXT NOT NULL,
    status      INT  NOT NULL,
    code        TEXT NOT NULL,
    duration_ms INT  NOT NULL,
    user_id     BIGINT,
    message     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX api_errors_at_idx ON api_errors (at DESC);

-- One row per running api process: its heartbeat and how many event streams
-- it holds open right now.
CREATE TABLE api_instances (
    instance      TEXT PRIMARY KEY,
    hostname      TEXT NOT NULL,
    started_at    TIMESTAMPTZ NOT NULL,
    last_seen_at  TIMESTAMPTZ NOT NULL,
    sse_customer  INT NOT NULL DEFAULT 0,
    sse_table     INT NOT NULL DEFAULT 0,
    sse_store     INT NOT NULL DEFAULT 0,
    sse_rider     INT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE IF EXISTS api_instances, api_errors, api_metrics;
