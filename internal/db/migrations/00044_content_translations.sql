-- +goose Up
-- Translations of what merchants write (docs/I18N_DESIGN.md §6.3). The tables
-- keep the merchant's own text; this table holds it in other languages.
CREATE TABLE content_translations (
    entity      TEXT NOT NULL,   -- category, item, group, option, promotion
    entity_id   BIGINT NOT NULL,
    field       TEXT NOT NULL,   -- name, description, details
    lang        TEXT NOT NULL,
    branch_id   BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    text        TEXT NOT NULL,
    -- machine: translated by the translation service; converted: Simplified to
    -- Traditional Chinese; human: written or confirmed by the merchant; stale:
    -- the merchant's text whose original changed since.
    status      TEXT NOT NULL CHECK (status IN ('machine','converted','human','stale')),
    source_hash TEXT NOT NULL DEFAULT '',
    provider    TEXT NOT NULL DEFAULT '',
    updated_by  BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (entity, entity_id, field, lang)
);
CREATE INDEX content_translations_branch_idx ON content_translations (branch_id, lang);
CREATE INDEX content_translations_search_idx ON content_translations (entity, field) INCLUDE (text);

-- The language the merchant writes the menu in, and the languages the merchant
-- turned off (customers reading those see English).
ALTER TABLE branches ADD COLUMN menu_lang TEXT NOT NULL DEFAULT 'zh',
    ADD COLUMN menu_langs_off TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN translate_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN translate_error_at TIMESTAMPTZ;

-- Orders keep the language the customer ordered in and each line's name in it.
ALTER TABLE orders ADD COLUMN customer_lang TEXT NOT NULL DEFAULT '';
ALTER TABLE order_items ADD COLUMN name_customer TEXT NOT NULL DEFAULT '';

-- English names the merchants already wrote are their own translations.
INSERT INTO content_translations (entity, entity_id, field, lang, branch_id, text, status, source_hash)
SELECT 'category', id, 'name', 'en', branch_id, name_en, 'human', md5(name) FROM menu_categories WHERE name_en <> ''
ON CONFLICT DO NOTHING;
INSERT INTO content_translations (entity, entity_id, field, lang, branch_id, text, status, source_hash)
SELECT 'item', id, 'name', 'en', branch_id, name_en, 'human', md5(name) FROM menu_items WHERE name_en <> ''
ON CONFLICT DO NOTHING;
INSERT INTO content_translations (entity, entity_id, field, lang, branch_id, text, status, source_hash)
SELECT 'group', g.id, 'name', 'en', i.branch_id, g.name_en, 'human', md5(g.name)
FROM option_groups g JOIN menu_items i ON i.id = g.item_id WHERE g.name_en <> ''
ON CONFLICT DO NOTHING;
INSERT INTO content_translations (entity, entity_id, field, lang, branch_id, text, status, source_hash)
SELECT 'option', o.id, 'name', 'en', i.branch_id, o.name_en, 'human', md5(o.name)
FROM options o JOIN option_groups g ON g.id = o.group_id JOIN menu_items i ON i.id = g.item_id WHERE o.name_en <> ''
ON CONFLICT DO NOTHING;
INSERT INTO content_translations (entity, entity_id, field, lang, branch_id, text, status, source_hash)
SELECT 'promotion', id, 'name', 'en', branch_id, name_en, 'human', md5(name) FROM promotions WHERE name_en <> ''
ON CONFLICT DO NOTHING;

-- +goose Down
ALTER TABLE order_items DROP COLUMN name_customer;
ALTER TABLE orders DROP COLUMN customer_lang;
ALTER TABLE branches DROP COLUMN menu_lang, DROP COLUMN menu_langs_off, DROP COLUMN translate_error,
    DROP COLUMN translate_error_at;
DROP TABLE content_translations;
