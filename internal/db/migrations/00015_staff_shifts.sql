-- +goose Up
-- Attendance: every time someone clocks on and off is kept. Until now only
-- "is this rider on duty right now" was stored (rider_status), which stays as
-- the live state of riders; a shift row is opened and closed alongside it.
CREATE TABLE staff_shifts (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT NOT NULL REFERENCES users(id),
    branch_id         BIGINT NOT NULL REFERENCES branches(id),
    started_at        TIMESTAMPTZ NOT NULL,
    ended_at          TIMESTAMPTZ,
    -- manual: the person tapped the switch; auto: they started working without
    -- clocking on; manager: a manager added the shift afterwards
    start_source      TEXT NOT NULL DEFAULT 'manual' CHECK (start_source IN ('manual','auto','manager')),
    -- manual, logout (signed out where the shift was started), auto (nobody
    -- ended it for 12 hours), manager (a manager ended it)
    end_reason        TEXT NOT NULL DEFAULT '' CHECK (end_reason IN ('','manual','logout','auto','manager')),
    -- the sign-in the shift was started from; signing out there ends it
    session           BYTEA,
    start_lat         DOUBLE PRECISION,
    start_lng         DOUBLE PRECISION,
    start_distance_m  INT,
    end_lat           DOUBLE PRECISION,
    end_lng           DOUBLE PRECISION,
    end_distance_m    INT,
    -- what the times were before a manager changed them
    orig_started_at   TIMESTAMPTZ,
    orig_ended_at     TIMESTAMPTZ,
    edited_by         BIGINT REFERENCES users(id),
    edited_at         TIMESTAMPTZ,
    edit_reason       TEXT NOT NULL DEFAULT '',
    voided            BOOLEAN NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);
-- A person is on at most one shift at a time, whatever the branch.
CREATE UNIQUE INDEX staff_shifts_open ON staff_shifts (user_id) WHERE ended_at IS NULL AND NOT voided;
CREATE INDEX staff_shifts_branch_idx ON staff_shifts (branch_id, started_at);
CREATE INDEX staff_shifts_user_idx ON staff_shifts (user_id, started_at);

-- Riders who are on duty right now get the shift they are in.
INSERT INTO staff_shifts (user_id, branch_id, started_at, session)
    SELECT rs.user_id, (SELECT min(p.branch_id) FROM staff_posts p WHERE p.user_id = rs.user_id AND p.kind = 'rider'),
        coalesce(rs.duty_since, now()), rs.duty_session
    FROM rider_status rs
    WHERE rs.on_duty AND EXISTS (SELECT 1 FROM staff_posts p WHERE p.user_id = rs.user_id AND p.kind = 'rider');

-- Who cooked a dish and when. Until now this was only in the audit log.
ALTER TABLE order_items
    ADD COLUMN prep_started_at  TIMESTAMPTZ,
    ADD COLUMN prep_started_by  BIGINT REFERENCES users(id),
    ADD COLUMN ready_at         TIMESTAMPTZ,
    ADD COLUMN ready_by         BIGINT REFERENCES users(id),
    -- where "ready" was tapped: work (the person's own workbench), board (the
    -- shared kitchen screen), '' (not recorded)
    ADD COLUMN ready_via        TEXT NOT NULL DEFAULT '',
    ADD COLUMN undo_count       INT NOT NULL DEFAULT 0;
CREATE INDEX order_items_ready_idx ON order_items (branch_id, ready_at) WHERE ready_by IS NOT NULL;

ALTER TABLE orders
    ADD COLUMN packed_by   BIGINT REFERENCES users(id),
    ADD COLUMN packed_via  TEXT NOT NULL DEFAULT '';
CREATE INDEX orders_packed_idx ON orders (branch_id, packed_at) WHERE packed_by IS NOT NULL;

-- History from the audit log, so that the work summary does not start empty.
UPDATE order_items oi SET prep_started_at = a.created_at, prep_started_by = a.actor_id
    FROM (SELECT DISTINCT ON ((detail->>'item_id')::bigint) (detail->>'item_id')::bigint AS item_id, created_at, actor_id
          FROM audit_logs WHERE action = 'item.start' AND detail ? 'item_id' ORDER BY (detail->>'item_id')::bigint, id) a
    WHERE oi.id = a.item_id AND EXISTS (SELECT 1 FROM users u WHERE u.id = a.actor_id);
-- A dish that was ready counts for whoever marked it last, also when the
-- order was cancelled afterwards; one that was taken back does not count.
UPDATE order_items oi SET ready_at = a.created_at, ready_by = a.actor_id
    FROM (SELECT DISTINCT ON ((detail->>'item_id')::bigint) (detail->>'item_id')::bigint AS item_id, created_at, actor_id, action
          FROM audit_logs WHERE action IN ('item.ready','item.undo') AND detail ? 'item_id'
          ORDER BY (detail->>'item_id')::bigint, id DESC) a
    WHERE oi.id = a.item_id AND a.action = 'item.ready' AND EXISTS (SELECT 1 FROM users u WHERE u.id = a.actor_id);
UPDATE order_items oi SET undo_count = a.n
    FROM (SELECT (detail->>'item_id')::bigint AS item_id, count(*) AS n FROM audit_logs
          WHERE action = 'item.undo' AND detail ? 'item_id' GROUP BY 1) a
    WHERE oi.id = a.item_id;
UPDATE orders o SET packed_by = a.actor_id
    FROM (SELECT DISTINCT ON (order_id) order_id, actor_id FROM audit_logs
          WHERE action = 'order.packed' AND order_id IS NOT NULL ORDER BY order_id, id DESC) a
    WHERE o.id = a.order_id AND o.packed_at IS NOT NULL AND EXISTS (SELECT 1 FROM users u WHERE u.id = a.actor_id);

-- +goose Down
DROP INDEX IF EXISTS orders_packed_idx;
ALTER TABLE orders DROP COLUMN packed_by, DROP COLUMN packed_via;
DROP INDEX IF EXISTS order_items_ready_idx;
ALTER TABLE order_items DROP COLUMN prep_started_at, DROP COLUMN prep_started_by, DROP COLUMN ready_at,
    DROP COLUMN ready_by, DROP COLUMN ready_via, DROP COLUMN undo_count;
DROP TABLE staff_shifts;
