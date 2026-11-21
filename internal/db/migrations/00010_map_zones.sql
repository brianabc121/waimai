-- +goose Up
-- Delivery zones drawn on a map (docs/MAP_ZONES_DESIGN.md). Only new columns:
-- a branch that never opens the map page keeps working exactly as before.
ALTER TABLE branches
    ADD COLUMN lat              DOUBLE PRECISION,
    ADD COLUMN lng              DOUBLE PRECISION,
    -- 'list': the customer picks a zone by name. 'map': the customer drops a
    -- pin and the server decides the zone.
    ADD COLUMN zone_mode        TEXT NOT NULL DEFAULT 'list' CHECK (zone_mode IN ('list', 'map')),
    -- A pin outside every zone but this close to one still belongs to it.
    ADD COLUMN zone_tolerance_m INT NOT NULL DEFAULT 200 CHECK (zone_tolerance_m BETWEEN 0 AND 500),
    ADD CONSTRAINT branches_location_pair CHECK ((lat IS NULL) = (lng IS NULL));

ALTER TABLE delivery_zones
    -- [[lat, lng], ...] without repeating the first point; NULL = not drawn yet.
    ADD COLUMN polygon     JSONB,
    ADD COLUMN color       TEXT NOT NULL DEFAULT '',
    ADD COLUMN eta_minutes INT CHECK (eta_minutes BETWEEN 1 AND 600);

ALTER TABLE orders
    ADD COLUMN dest_lat        DOUBLE PRECISION,
    ADD COLUMN dest_lng        DOUBLE PRECISION,
    -- Metres outside the zone's outline when the order was matched by
    -- tolerance; NULL when the pin was inside.
    ADD COLUMN zone_distance_m INT;

-- +goose Down
ALTER TABLE orders DROP COLUMN dest_lat, DROP COLUMN dest_lng, DROP COLUMN zone_distance_m;
ALTER TABLE delivery_zones DROP COLUMN polygon, DROP COLUMN color, DROP COLUMN eta_minutes;
ALTER TABLE branches DROP CONSTRAINT branches_location_pair,
    DROP COLUMN lat, DROP COLUMN lng, DROP COLUMN zone_mode, DROP COLUMN zone_tolerance_m;
