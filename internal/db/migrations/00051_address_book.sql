-- +goose Up
-- The platform's address book (docs/ADDRESS_BOOK_DESIGN.md): addresses with a
-- position, kept per country / province / city. Staff orders add to it, the
-- order desk offers it first while staff type an address, and the platform
-- manages it on /admin/addresses. Matching is done by PostgreSQL itself
-- (pg_trgm), not a search server: see the design doc for why.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- addr_key is what two addresses are compared by: full-width letters and
-- digits made plain (NFKC), lower case, no spaces and no punctuation. "Ayala
-- Tower One, 6750 Ayala Ave." and "ayala tower one 6750 ayala ave" are one
-- address. Search terms go through the same function.
-- +goose StatementBegin
CREATE FUNCTION addr_key(s TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS $$
    SELECT lower(regexp_replace(normalize(s, NFKC), '[[:space:][:punct:]、。，．·・「」『』（）【】《》〈〉！？：；]+', '', 'g'))
$$;
-- +goose StatementEnd

-- addr_line is how an address is shown and stored: NFKC, one space between
-- words, no trailing commas or full stops.
-- +goose StatementBegin
CREATE FUNCTION addr_line(s TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS $$
    SELECT regexp_replace(regexp_replace(btrim(regexp_replace(normalize(s, NFKC), '\s+', ' ', 'g')),
        '\s+([,.;:!?])', '\1', 'g'), '[\s,.;:、。，]+$', '')
$$;
-- +goose StatementEnd

CREATE TABLE address_book (
    id           BIGSERIAL PRIMARY KEY,
    -- The region, as the store's own (branches.country_code, province, city;
    -- English names read off the map). '' where it is not known.
    country_code TEXT NOT NULL DEFAULT '',
    country      TEXT NOT NULL DEFAULT '',
    province     TEXT NOT NULL DEFAULT '',
    city         TEXT NOT NULL DEFAULT '',
    line         TEXT NOT NULL CHECK (length(line) BETWEEN 1 AND 300),
    line_key     TEXT GENERATED ALWAYS AS (addr_key(line)) STORED,
    lat          DOUBLE PRECISION NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng          DOUBLE PRECISION NOT NULL CHECK (lng BETWEEN -180 AND 180),
    -- staff: a staff order; admin: put in on the platform; import: orders before the book
    source       TEXT NOT NULL DEFAULT 'staff' CHECK (source IN ('staff', 'admin', 'import')),
    branch_id    BIGINT REFERENCES branches(id) ON DELETE SET NULL,
    uses         INT NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX address_book_uniq ON address_book (country_code, province, city, line_key);
CREATE INDEX address_book_key_trgm ON address_book USING gin (line_key gin_trgm_ops);

-- What staff already delivered to with a pin: staff orders, then the
-- addresses kept against members.
INSERT INTO address_book (country_code, country, province, city, line, lat, lng, source, branch_id, uses, created_at, last_used_at)
SELECT DISTINCT ON (b.country_code, b.province, b.city, addr_key(o.address_line))
    b.country_code, b.country, b.province, b.city, left(addr_line(o.address_line), 300), o.dest_lat, o.dest_lng, 'import', b.id,
    count(*) OVER w, min(o.created_at) OVER w, max(o.created_at) OVER w
FROM orders o JOIN branches b ON b.id = o.branch_id
WHERE o.channel = 'staff' AND o.service_type = 'delivery' AND o.dest_lat IS NOT NULL AND addr_key(o.address_line) <> ''
WINDOW w AS (PARTITION BY b.country_code, b.province, b.city, addr_key(o.address_line))
ORDER BY b.country_code, b.province, b.city, addr_key(o.address_line), o.created_at DESC;

INSERT INTO address_book (country_code, country, province, city, line, lat, lng, source, branch_id, uses, created_at, last_used_at)
SELECT DISTINCT ON (b.country_code, b.province, b.city, addr_key(a.address_line))
    b.country_code, b.country, b.province, b.city, left(addr_line(a.address_line), 300), a.lat, a.lng, 'import', b.id,
    greatest(a.uses, 1), a.created_at, a.last_used_at
FROM store_member_addresses a JOIN store_members m ON m.id = a.member_id JOIN branches b ON b.id = m.branch_id
WHERE a.lat IS NOT NULL AND addr_key(a.address_line) <> ''
ORDER BY b.country_code, b.province, b.city, addr_key(a.address_line), a.last_used_at DESC
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE address_book;
DROP FUNCTION addr_line(TEXT);
DROP FUNCTION addr_key(TEXT);
