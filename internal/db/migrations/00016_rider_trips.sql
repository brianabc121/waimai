-- +goose Up
-- A trip is one outing of a rider: from collecting the first order at the
-- store until they are back at the store. Orders collected before the rider
-- leaves ride on the same trip.
CREATE TABLE rider_trips (
    id                  BIGSERIAL PRIMARY KEY,
    branch_id           BIGINT NOT NULL REFERENCES branches(id),
    rider_id            BIGINT NOT NULL REFERENCES users(id),
    shift_id            BIGINT REFERENCES staff_shifts(id),
    -- loading: collecting orders at the store; out: on the road; done
    status              TEXT NOT NULL DEFAULT 'loading' CHECK (status IN ('loading','out','done')),
    -- returned (back at the store), next_trip (collected the next order),
    -- off_duty (clocked off before coming back), timeout (never seen back),
    -- no_store (the store has no position, so coming back cannot be seen),
    -- empty (every order was taken off the trip)
    end_reason          TEXT NOT NULL DEFAULT ''
        CHECK (end_reason IN ('','returned','next_trip','off_duty','timeout','no_store','empty')),
    started_at          TIMESTAMPTZ NOT NULL,
    left_store_at       TIMESTAMPTZ,
    last_drop_at        TIMESTAMPTZ,
    returned_at         TIMESTAMPTZ,
    ended_at            TIMESTAMPTZ,
    order_count         INT NOT NULL DEFAULT 0,
    delivered_count     INT NOT NULL DEFAULT 0,
    failed_count        INT NOT NULL DEFAULT 0,
    -- what the rider actually rode, from the position fixes of the phone
    actual_distance_m   INT,
    -- [[lat, lng, seconds since started_at], ...], thinned out
    actual_path         JSONB,
    -- indexes into actual_path: the leg from that point to the next one was
    -- not recorded and is drawn and measured as a straight line
    track_gaps          JSONB,
    track_points        INT NOT NULL DEFAULT 0,
    track_gap_s         INT NOT NULL DEFAULT 0,
    -- when to work the track out once more, for fixes the phone sent late
    refine_at           TIMESTAMPTZ,
    -- the route the system would have taken; filled in by a later release
    planned_distance_m  INT,
    planned_duration_s  INT,
    planned_path        JSONB,
    planned_order       JSONB,
    planned_mode        TEXT NOT NULL DEFAULT '',
    planned_source      TEXT NOT NULL DEFAULT '' CHECK (planned_source IN ('','google','straight')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- A rider is on at most one trip at a time.
CREATE UNIQUE INDEX rider_trips_open ON rider_trips (rider_id) WHERE status <> 'done';
CREATE INDEX rider_trips_branch_idx ON rider_trips (branch_id, started_at);
CREATE INDEX rider_trips_rider_idx ON rider_trips (rider_id, started_at);
CREATE INDEX rider_trips_refine_idx ON rider_trips (refine_at) WHERE refine_at IS NOT NULL;

-- The orders a trip carried. An order that failed and went out again is on
-- two trips, so this is not a column of delivery_tasks.
CREATE TABLE rider_trip_orders (
    id            BIGSERIAL PRIMARY KEY,
    trip_id       BIGINT NOT NULL REFERENCES rider_trips(id) ON DELETE CASCADE,
    order_id      BIGINT NOT NULL,
    picked_up_at  TIMESTAMPTZ NOT NULL,
    -- '' while it is being carried; removed: cancelled on the way
    outcome       TEXT NOT NULL DEFAULT '' CHECK (outcome IN ('','delivered','failed','removed')),
    done_at       TIMESTAMPTZ,
    -- the order in which the rider actually dropped them, from 1
    drop_seq      INT
);
CREATE INDEX rider_trip_orders_trip_idx ON rider_trip_orders (trip_id);
CREATE INDEX rider_trip_orders_order_idx ON rider_trip_orders (order_id);

-- +goose Down
DROP TABLE rider_trip_orders;
DROP TABLE rider_trips;
