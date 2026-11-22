-- +goose Up
-- Promotional pictures, GIFs and short videos that play at the top of the
-- customer's store page. The file is kept as uploaded; the merchant's crop is
-- a rectangle (fractions of the picture) that the page applies when showing it,
-- so it can be changed later without uploading again.
CREATE TABLE store_media (
    id         BIGSERIAL PRIMARY KEY,
    branch_id  BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    -- part of the public address, so that addresses cannot be guessed
    token      TEXT NOT NULL UNIQUE,
    kind       TEXT NOT NULL CHECK (kind IN ('image', 'gif', 'video')),
    mime       TEXT NOT NULL,
    content    BYTEA NOT NULL,
    size       INT NOT NULL,
    -- a JPEG of the cropped frame, shown before a video plays
    poster     BYTEA,
    title      TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 40),
    crop_x     REAL NOT NULL DEFAULT 0,
    crop_y     REAL NOT NULL DEFAULT 0,
    crop_w     REAL NOT NULL DEFAULT 1,
    crop_h     REAL NOT NULL DEFAULT 1,
    -- seconds of a video to play; trim_end 0 plays to the end
    trim_start REAL NOT NULL DEFAULT 0,
    trim_end   REAL NOT NULL DEFAULT 0,
    active     BOOLEAN NOT NULL DEFAULT true,
    sort       INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX store_media_branch ON store_media (branch_id, sort, id);
-- Videos are already compressed; stored as they are, a part of one can be
-- read without loading the whole file.
ALTER TABLE store_media ALTER COLUMN content SET STORAGE EXTERNAL;

-- +goose Down
DROP TABLE store_media;
