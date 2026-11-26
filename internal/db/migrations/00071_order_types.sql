-- +goose Up
-- Order types and order numbers (docs/ORDER_TYPES_DESIGN.md §3–4). Every order
-- has one of five types; the number shown everywhere (public_no) is built per
-- type. Old orders keep their numbers and only get a type here.

-- The store code staff orders start with (YPX000123); upper case, unique over
-- the platform, filled in on first use when empty. The daily reset time is
-- when dine-in and online numbers start again from 1 (branch local time).
ALTER TABLE branches ADD COLUMN store_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN day_reset_time TEXT NOT NULL DEFAULT '04:00';
CREATE UNIQUE INDEX branches_store_code ON branches (store_code) WHERE store_code <> '';

-- Existing stores: the first letters of the slug, the branch id when taken.
WITH c AS (
    SELECT id, COALESCE(NULLIF(left(upper(regexp_replace(slug, '[^A-Za-z0-9]', '', 'g')), 3), ''), 'S') AS base
    FROM branches
), r AS (
    SELECT id, base, row_number() OVER (PARTITION BY base ORDER BY id) AS n FROM c
)
UPDATE branches b SET store_code = CASE WHEN r.n = 1 THEN r.base ELSE r.base || b.id END
FROM r WHERE r.id = b.id;

-- dine_in: tables (dine_source says who opened it); staff: orders staff place
-- for a member; platform: Grab, foodpanda...; mall: from the Hippo mall;
-- web: from the store's own link (numbered Online-).
ALTER TABLE orders ADD COLUMN order_type TEXT NOT NULL DEFAULT 'web'
        CHECK (order_type IN ('dine_in','staff','platform','mall','web')),
    ADD COLUMN dine_source TEXT NOT NULL DEFAULT '' CHECK (dine_source IN ('','staff','self')),
    ADD COLUMN biz_date DATE;

-- A table is self-ordered when its first round came from the customer's
-- phone or the table tablet, staff-ordered otherwise.
UPDATE orders o SET order_type = 'dine_in',
    dine_source = CASE WHEN (SELECT b.source FROM order_batches b WHERE b.order_id = o.id ORDER BY b.seq LIMIT 1)
        IN ('qr','ipad') THEN 'self' ELSE 'staff' END
WHERE o.service_type = 'dine_in' OR o.channel = 'dine_in';
UPDATE orders SET order_type = 'staff' WHERE channel = 'staff' AND order_type <> 'dine_in';
UPDATE orders o SET biz_date = (o.created_at AT TIME ZONE b.timezone)::date FROM branches b WHERE b.id = o.branch_id;

CREATE INDEX orders_branch_type_day ON orders (branch_id, biz_date, order_type);

-- One running number per store, kind and period (a business day or a date).
CREATE TABLE order_counters (
    branch_id  BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    period     DATE NOT NULL,
    seq        INT NOT NULL,
    PRIMARY KEY (branch_id, kind, period)
);

-- +goose Down
DROP TABLE order_counters;
DROP INDEX orders_branch_type_day;
ALTER TABLE orders DROP COLUMN biz_date, DROP COLUMN dine_source, DROP COLUMN order_type;
DROP INDEX branches_store_code;
ALTER TABLE branches DROP COLUMN day_reset_time, DROP COLUMN store_code;
