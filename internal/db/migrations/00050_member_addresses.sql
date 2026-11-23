-- +goose Up
-- A store member can have several addresses: every delivery address used for
-- them is kept, once (docs/MANUAL_ORDER_DESIGN.md).
CREATE TABLE store_member_addresses (
    id           BIGSERIAL PRIMARY KEY,
    member_id    BIGINT NOT NULL REFERENCES store_members(id) ON DELETE CASCADE,
    address_line TEXT NOT NULL,
    address_unit TEXT NOT NULL DEFAULT '',
    address_note TEXT NOT NULL DEFAULT '',
    lat          DOUBLE PRECISION,
    lng          DOUBLE PRECISION,
    uses         INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX store_member_addresses_uniq
    ON store_member_addresses (member_id, lower(btrim(address_line)), lower(btrim(address_unit)));

-- The addresses already known: past deliveries kept against a member, then
-- each member's current address.
INSERT INTO store_member_addresses (member_id, address_line, address_unit, address_note, lat, lng, uses, created_at, last_used_at)
SELECT DISTINCT ON (o.member_id, lower(btrim(o.address_line)), lower(btrim(o.address_unit)))
    o.member_id, btrim(o.address_line), btrim(o.address_unit), o.address_note, o.dest_lat, o.dest_lng,
    count(*) OVER (PARTITION BY o.member_id, lower(btrim(o.address_line)), lower(btrim(o.address_unit))),
    min(o.created_at) OVER (PARTITION BY o.member_id, lower(btrim(o.address_line)), lower(btrim(o.address_unit))),
    o.created_at
FROM orders o
WHERE o.member_id IS NOT NULL AND o.service_type = 'delivery' AND btrim(o.address_line) <> ''
ORDER BY o.member_id, lower(btrim(o.address_line)), lower(btrim(o.address_unit)), o.created_at DESC;

INSERT INTO store_member_addresses (member_id, address_line, address_unit, address_note, lat, lng, created_at, last_used_at)
SELECT id, btrim(address_line), btrim(address_unit), address_note, lat, lng, created_at, updated_at
FROM store_members WHERE btrim(address_line) <> ''
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE store_member_addresses;
