-- +goose Up
-- Toppings are translated once, on their template (docs/I18N_SIMPLE_DESIGN.md
-- §4.8), not once for every item the template is used on. What the items'
-- copies had moves to the template: the merchant's own translation first,
-- else the newest.
INSERT INTO content_translations (entity, entity_id, field, lang, branch_id, text, status, source_hash, provider, updated_by, updated_at, prev_text)
SELECT DISTINCT ON (o.topping_id, t.lang) 'topping_option', o.topping_id, 'name', t.lang, t.branch_id, t.text, t.status,
       t.source_hash, t.provider, t.updated_by, t.updated_at, t.prev_text
FROM content_translations t JOIN options o ON o.id = t.entity_id
WHERE t.entity = 'option' AND t.field = 'name' AND o.topping_id IS NOT NULL
ORDER BY o.topping_id, t.lang, (t.status = 'human') DESC, t.updated_at DESC
ON CONFLICT DO NOTHING;

INSERT INTO content_translations (entity, entity_id, field, lang, branch_id, text, status, source_hash, provider, updated_by, updated_at, prev_text)
SELECT DISTINCT ON (g.topping_template_id, t.lang) 'topping_template', g.topping_template_id, 'title', t.lang, t.branch_id, t.text, t.status,
       t.source_hash, t.provider, t.updated_by, t.updated_at, t.prev_text
FROM content_translations t JOIN option_groups g ON g.id = t.entity_id
WHERE t.entity = 'group' AND t.field = 'name' AND g.topping_template_id IS NOT NULL
ORDER BY g.topping_template_id, t.lang, (t.status = 'human') DESC, t.updated_at DESC
ON CONFLICT DO NOTHING;

DELETE FROM content_translations t USING options o
WHERE t.entity = 'option' AND o.id = t.entity_id AND o.topping_id IS NOT NULL;
DELETE FROM content_translations t USING option_groups g
WHERE t.entity = 'group' AND g.id = t.entity_id AND g.topping_template_id IS NOT NULL;

-- +goose Down
DELETE FROM content_translations WHERE entity IN ('topping_option', 'topping_template');
