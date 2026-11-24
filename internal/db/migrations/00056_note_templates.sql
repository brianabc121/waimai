-- +goose Up
-- Flavour note templates (docs/NOTE_TEMPLATE_DESIGN.md): tags such as "less
-- sugar" or "no onion" a store writes once and many dishes share. They never
-- change the price and are never required.
CREATE TABLE note_templates (
    id          BIGSERIAL PRIMARY KEY,
    branch_id   BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,          -- only the store sees it
    icon        TEXT NOT NULL DEFAULT '',
    sort        INT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (branch_id, name)
);

-- sweetness (pick one), no-go (pick any)
CREATE TABLE note_groups (
    id           BIGSERIAL PRIMARY KEY,
    template_id  BIGINT NOT NULL REFERENCES note_templates(id) ON DELETE CASCADE,
    name         TEXT NOT NULL DEFAULT '',
    name_en      TEXT NOT NULL DEFAULT '',
    multi        BOOLEAN NOT NULL DEFAULT false,
    sort         INT NOT NULL DEFAULT 0
);
CREATE INDEX note_groups_template_idx ON note_groups (template_id);

CREATE TABLE note_tags (
    id        BIGSERIAL PRIMARY KEY,
    group_id  BIGINT NOT NULL REFERENCES note_groups(id) ON DELETE CASCADE,
    name      TEXT NOT NULL,
    name_en   TEXT NOT NULL DEFAULT '',
    active    BOOLEAN NOT NULL DEFAULT true,
    sort      INT NOT NULL DEFAULT 0
);
CREATE INDEX note_tags_group_idx ON note_tags (group_id);

-- which templates a dish shows
CREATE TABLE menu_item_note_templates (
    item_id      BIGINT NOT NULL REFERENCES menu_items(id) ON DELETE CASCADE,
    template_id  BIGINT NOT NULL REFERENCES note_templates(id) ON DELETE CASCADE,
    sort         INT NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, template_id)
);
CREATE INDEX menu_item_note_templates_tpl_idx ON menu_item_note_templates (template_id);

-- An order line keeps the tags as they were chosen: later changes to the
-- template never change past orders.
-- [{"id": 12, "group": "甜度", "name": "少糖", "group_tr": "Sweetness", "name_tr": "Less sugar"}]
ALTER TABLE order_items ADD COLUMN note_tags JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE order_items DROP COLUMN note_tags;
DROP TABLE menu_item_note_templates;
DROP TABLE note_tags;
DROP TABLE note_groups;
DROP TABLE note_templates;
