-- +goose Up
-- The platform's own texts (mall categories and banners) are translated too,
-- and belong to no store (docs/I18N_DESIGN.md §4, class A).
ALTER TABLE content_translations ALTER COLUMN branch_id DROP NOT NULL;
CREATE INDEX content_translations_platform_idx ON content_translations (lang) WHERE branch_id IS NULL;

-- +goose Down
DROP INDEX content_translations_platform_idx;
DELETE FROM content_translations WHERE branch_id IS NULL;
ALTER TABLE content_translations ALTER COLUMN branch_id SET NOT NULL;
