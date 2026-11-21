-- +goose Up
-- Storefront templates. Each ordering channel of a branch has its own layout
-- and theme, with a draft that is edited and previewed before it is published.
-- A template only changes how the menu looks; menu, prices, carts, orders,
-- table codes and links are untouched.
CREATE TABLE channel_templates (
    branch_id    BIGINT NOT NULL REFERENCES branches(id),
    channel      TEXT NOT NULL CHECK (channel IN ('delivery','qr','ipad')),
    published    JSONB NOT NULL,
    draft        JSONB,                 -- NULL when nothing is waiting to be published
    published_at TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (branch_id, channel)
);

-- +goose Down
DROP TABLE channel_templates;
