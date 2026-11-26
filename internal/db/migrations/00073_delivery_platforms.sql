-- +goose Up
-- Delivery platforms (GrabFood, foodpanda...) and the prices a store charges on
-- them (docs/ORDER_TYPES_DESIGN.md §6). The platform administrator keeps the
-- list; a store turns on the ones it sells on.
CREATE TABLE delivery_platforms (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
    names           JSONB NOT NULL DEFAULT '{}',   -- language code -> name, empty = name everywhere
    prefix          TEXT NOT NULL CHECK (prefix ~ '^[A-Za-z0-9]{1,10}$'),
    countries       TEXT[] NOT NULL DEFAULT '{}',
    color           TEXT NOT NULL DEFAULT '#00b14f' CHECK (color ~ '^#[0-9a-fA-F]{6}$'),
    logo_url        TEXT NOT NULL DEFAULT '',
    no_hint         TEXT NOT NULL DEFAULT '',
    no_pattern      TEXT NOT NULL DEFAULT '',
    -- the commission the platform takes, and of what: the dishes after the
    -- store's own discount on the platform, or before it
    commission_bp   INT NOT NULL DEFAULT 0 CHECK (commission_bp BETWEEN 0 AND 10000),
    commission_base TEXT NOT NULL DEFAULT 'after_merchant' CHECK (commission_base IN ('after_merchant','original')),
    -- how much dearer the platform's prices are than the store's own
    markup_bp       INT NOT NULL DEFAULT 0 CHECK (markup_bp BETWEEN 0 AND 10000),
    active          BOOLEAN NOT NULL DEFAULT true,
    sort            INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX delivery_platforms_prefix ON delivery_platforms (lower(prefix));

INSERT INTO delivery_platforms (name, prefix, countries, color, no_hint, sort) VALUES
    ('Grab', 'Grab', '{PH,SG,MY,TH,VN,ID,MM,KH}', '#00b14f', 'GF-123', 1),
    ('foodpanda', 'FP', '{PH,SG,MY,TH,TW,KH,MM,LA}', '#d70f64', 'p8ck-a1b2', 2);

-- The platforms a store sells on, with its own contract: the commission and
-- the markup (empty takes the platform's), and how platform prices are
-- rounded (the step in cents, 0 = not rounded).
CREATE TABLE branch_platforms (
    branch_id       BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    platform_id     BIGINT NOT NULL REFERENCES delivery_platforms(id),
    shop_ref        TEXT NOT NULL DEFAULT '',
    commission_bp   INT CHECK (commission_bp BETWEEN 0 AND 10000),
    markup_bp       INT CHECK (markup_bp BETWEEN 0 AND 10000),
    round_step      INT NOT NULL DEFAULT 100 CHECK (round_step IN (0, 100, 500, 1000)),
    enabled         BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (branch_id, platform_id)
);

-- A dish's price on a platform, when the store set one; otherwise it is the
-- store price with the markup, rounded.
CREATE TABLE item_platform_prices (
    item_id     BIGINT NOT NULL REFERENCES menu_items(id) ON DELETE CASCADE,
    platform_id BIGINT NOT NULL REFERENCES delivery_platforms(id),
    price       BIGINT NOT NULL CHECK (price >= 0),
    PRIMARY KEY (item_id, platform_id)
);

-- +goose Down
DROP TABLE item_platform_prices;
DROP TABLE branch_platforms;
DROP TABLE delivery_platforms;
