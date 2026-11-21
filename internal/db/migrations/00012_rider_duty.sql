-- +goose Up
-- Riders clock on and off, report where they are while on duty, and register
-- the phones and browsers that should be told about new deliveries.
CREATE TABLE rider_status (
    user_id     BIGINT PRIMARY KEY REFERENCES users(id),
    on_duty     BOOLEAN NOT NULL DEFAULT false,
    duty_since  TIMESTAMPTZ,
    -- the sign-in the shift was started from; signing out of it ends the shift
    duty_session BYTEA,
    lat         DOUBLE PRECISION,
    lng         DOUBLE PRECISION,
    accuracy    REAL,
    battery     SMALLINT,
    located_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Where a rider has been, kept for seven days to settle complaints.
CREATE TABLE rider_tracks (
    id        BIGSERIAL PRIMARY KEY,
    user_id   BIGINT NOT NULL REFERENCES users(id),
    lat       DOUBLE PRECISION NOT NULL,
    lng       DOUBLE PRECISION NOT NULL,
    accuracy  REAL,
    at        TIMESTAMPTZ NOT NULL
);
CREATE INDEX rider_tracks_user_idx ON rider_tracks (user_id, at);
CREATE INDEX rider_tracks_at_idx ON rider_tracks (at);

-- token is the FCM token of an app, or the JSON push subscription of a browser.
CREATE TABLE rider_devices (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id),
    platform      TEXT NOT NULL CHECK (platform IN ('android','ios','web')),
    token         TEXT NOT NULL UNIQUE,
    app_version   TEXT NOT NULL DEFAULT '',
    session_hash  BYTEA,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX rider_devices_user_idx ON rider_devices (user_id);

-- kind 'token' sessions belong to the app, which sends the token in a header.
ALTER TABLE sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'cookie';

-- +goose Down
ALTER TABLE sessions DROP COLUMN kind;
DROP TABLE rider_devices, rider_tracks, rider_status;
