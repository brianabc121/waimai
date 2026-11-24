-- +goose Up
-- Topping templates: extras with a price (上海青 +20, 烤羊肉 +100) a store
-- writes once and many dishes share. A dish that uses a template gets a
-- normal option group made from it, kept in step with the template, so
-- ordering, prices, the kitchen and receipts work as for any option.
CREATE TABLE topping_templates (
    id          BIGSERIAL PRIMARY KEY,
    branch_id   BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,                     -- only the store sees it
    title       TEXT NOT NULL DEFAULT '加料',       -- the group name customers see
    max_select  INT NOT NULL DEFAULT 0 CHECK (max_select >= 0),  -- 0: any number
    sort        INT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (branch_id, name)
);

CREATE TABLE topping_options (
    id           BIGSERIAL PRIMARY KEY,
    template_id  BIGINT NOT NULL REFERENCES topping_templates(id) ON DELETE CASCADE,
    code         TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL,
    name_en      TEXT NOT NULL DEFAULT '',
    price        BIGINT NOT NULL DEFAULT 0 CHECK (price >= 0),
    sold_out     BOOLEAN NOT NULL DEFAULT false,
    sort         INT NOT NULL DEFAULT 0
);
CREATE INDEX topping_options_template_idx ON topping_options (template_id);

CREATE TABLE menu_item_topping_templates (
    item_id      BIGINT NOT NULL REFERENCES menu_items(id) ON DELETE CASCADE,
    template_id  BIGINT NOT NULL REFERENCES topping_templates(id) ON DELETE CASCADE,
    sort         INT NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, template_id)
);
CREATE INDEX menu_item_topping_templates_tpl_idx ON menu_item_topping_templates (template_id);

-- the option group a dish gets from a template, and its options
ALTER TABLE option_groups ADD COLUMN topping_template_id BIGINT REFERENCES topping_templates(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX option_groups_topping_idx ON option_groups (item_id, topping_template_id) WHERE topping_template_id IS NOT NULL;
ALTER TABLE options ADD COLUMN code TEXT NOT NULL DEFAULT '';
ALTER TABLE options ADD COLUMN topping_id BIGINT REFERENCES topping_options(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE options DROP COLUMN topping_id;
ALTER TABLE options DROP COLUMN code;
DROP INDEX option_groups_topping_idx;
ALTER TABLE option_groups DROP COLUMN topping_template_id;
DROP TABLE menu_item_topping_templates;
DROP TABLE topping_options;
DROP TABLE topping_templates;
