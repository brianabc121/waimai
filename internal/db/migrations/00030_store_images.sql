-- +goose Up
-- Pictures a merchant uploads for dishes, the store logo and the cover. They
-- are cropped in the browser first; a dish or a template then points at
-- /api/v1/images/<token>.
CREATE TABLE store_images (
    id         BIGSERIAL PRIMARY KEY,
    branch_id  BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    token      TEXT NOT NULL UNIQUE,
    content    BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX store_images_branch ON store_images (branch_id);

-- +goose Down
DROP TABLE store_images;
