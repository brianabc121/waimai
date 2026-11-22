-- +goose Up
-- The marketplace: one page that lists the stores delivering to where the
-- customer is. A store shows there unless it opted out (mall_listed) or the
-- platform took it off (mall_hidden).
ALTER TABLE branches
    ADD COLUMN mall_listed BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN mall_hidden BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN tagline     TEXT NOT NULL DEFAULT '' CHECK (length(tagline) <= 120);

-- The kinds of store customers browse by. The platform keeps the list; a store
-- picks up to two.
CREATE TABLE store_categories (
    id      BIGSERIAL PRIMARY KEY,
    code    TEXT NOT NULL UNIQUE,
    name    TEXT NOT NULL,
    name_en TEXT NOT NULL,
    icon    TEXT NOT NULL DEFAULT '',
    sort    INT NOT NULL DEFAULT 0,
    active  BOOLEAN NOT NULL DEFAULT true
);
INSERT INTO store_categories (code, name, name_en, icon, sort) VALUES
    ('chinese', '中餐', 'Chinese', '🍜', 1),
    ('fastfood', '快餐', 'Fast food', '🍗', 2),
    ('bento', '便当简餐', 'Rice meals', '🍱', 3),
    ('drinks', '饮品', 'Drinks', '🧋', 4),
    ('snacks', '小吃', 'Snacks', '🍢', 5),
    ('dessert', '甜点', 'Desserts', '🍰', 6),
    ('light', '轻食', 'Light meals', '🥗', 7);

CREATE TABLE branch_categories (
    branch_id   BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    category_id BIGINT NOT NULL REFERENCES store_categories(id) ON DELETE CASCADE,
    PRIMARY KEY (branch_id, category_id)
);

-- The banners at the top of the marketplace. link_slug leads to a store,
-- link_url anywhere else; a banner without a picture is drawn in the theme colour.
CREATE TABLE mall_banners (
    id          BIGSERIAL PRIMARY KEY,
    title       TEXT NOT NULL CHECK (length(title) <= 60),
    title_en    TEXT NOT NULL DEFAULT '' CHECK (length(title_en) <= 60),
    subtitle    TEXT NOT NULL DEFAULT '' CHECK (length(subtitle) <= 120),
    subtitle_en TEXT NOT NULL DEFAULT '' CHECK (length(subtitle_en) <= 120),
    link_slug   TEXT NOT NULL DEFAULT '',
    link_url    TEXT NOT NULL DEFAULT '',
    image       BYTEA,
    image_token TEXT NOT NULL DEFAULT '',
    sort        INT NOT NULL DEFAULT 0,
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX branches_mall_idx ON branches (lat, lng) WHERE deleted_at IS NULL AND mall_listed AND NOT mall_hidden;

-- +goose Down
DROP INDEX branches_mall_idx;
DROP TABLE mall_banners;
DROP TABLE branch_categories;
DROP TABLE store_categories;
ALTER TABLE branches DROP COLUMN mall_listed, DROP COLUMN mall_hidden, DROP COLUMN tagline;
