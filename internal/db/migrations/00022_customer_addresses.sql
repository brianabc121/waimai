-- +goose Up
-- A customer's saved delivery addresses. The pin (lat/lng) is what the
-- delivery area is decided from; the place name is only what people read.
CREATE TABLE customer_addresses (
    id            BIGSERIAL PRIMARY KEY,
    customer_id   BIGINT NOT NULL REFERENCES customers(id),
    contact_name  TEXT NOT NULL CHECK (length(contact_name) BETWEEN 1 AND 80),
    -- '', mr, ms
    title         TEXT NOT NULL DEFAULT '' CHECK (title IN ('', 'mr', 'ms')),
    phone         TEXT NOT NULL,
    place_name    TEXT NOT NULL CHECK (length(place_name) BETWEEN 1 AND 200),
    place_address TEXT NOT NULL DEFAULT '' CHECK (length(place_address) <= 300),
    -- NULL when the address was typed without a map
    lat           DOUBLE PRECISION,
    lng           DOUBLE PRECISION,
    unit          TEXT NOT NULL DEFAULT '' CHECK (length(unit) <= 100),
    note          TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 300),
    wechat        TEXT NOT NULL DEFAULT '' CHECK (length(wechat) <= 64),
    telegram      TEXT NOT NULL DEFAULT '' CHECK (length(telegram) <= 64),
    -- '', home, work, school
    label         TEXT NOT NULL DEFAULT '' CHECK (label IN ('', 'home', 'work', 'school')),
    is_default    BOOLEAN NOT NULL DEFAULT false,
    last_used_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ,
    CHECK ((lat IS NULL) = (lng IS NULL))
);
CREATE INDEX customer_addresses_customer_idx ON customer_addresses (customer_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX customer_addresses_default_idx ON customer_addresses (customer_id) WHERE is_default AND deleted_at IS NULL;

-- An order keeps a copy of the address it was sent to, so later edits to the
-- address book do not change it. contact_phone is who the rider calls; it may
-- differ from customer_phone, the account that ordered.
ALTER TABLE orders
    ADD COLUMN address_id       BIGINT REFERENCES customer_addresses(id),
    ADD COLUMN contact_phone    TEXT NOT NULL DEFAULT '',
    ADD COLUMN contact_title    TEXT NOT NULL DEFAULT '',
    ADD COLUMN contact_wechat   TEXT NOT NULL DEFAULT '',
    ADD COLUMN contact_telegram TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE orders DROP COLUMN address_id, DROP COLUMN contact_phone, DROP COLUMN contact_title,
    DROP COLUMN contact_wechat, DROP COLUMN contact_telegram;
DROP TABLE customer_addresses;
