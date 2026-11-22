-- +goose Up
-- An optional photo of the door or the building, for the rider. One per
-- address; a new one replaces the old.
CREATE TABLE customer_address_photos (
    address_id BIGINT PRIMARY KEY REFERENCES customer_addresses(id) ON DELETE CASCADE,
    image      BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE customer_addresses ADD COLUMN photo_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE customer_addresses DROP COLUMN photo_at;
DROP TABLE customer_address_photos;
