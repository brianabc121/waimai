-- +goose Up
-- Platform support by brand (docs/MULTI_BRAND_DESIGN.md §16.7): a store with
-- several brands says which brand a message to the platform is about, and an
-- order card is about its order's brand. The conversation is still one per
-- customer or store; it keeps the brand of its latest message that had one.
ALTER TABLE support_threads ADD COLUMN brand_id BIGINT REFERENCES brands(id) ON DELETE SET NULL;
ALTER TABLE support_messages ADD COLUMN brand_id BIGINT REFERENCES brands(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE support_messages DROP COLUMN brand_id;
ALTER TABLE support_threads DROP COLUMN brand_id;
