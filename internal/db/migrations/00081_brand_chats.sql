-- +goose Up
-- A customer writes to a brand (docs/MULTI_BRAND_DESIGN.md §9): the store's
-- desk sees which brand's page each message came from. The conversation is
-- still one per customer and store (one OpenIM account per store); it keeps
-- the brand the customer last wrote from. Conversations so far are the main
-- brand's.
ALTER TABLE chat_threads ADD COLUMN brand_id BIGINT REFERENCES brands(id) ON DELETE SET NULL;
UPDATE chat_threads t SET brand_id = b.id FROM brands b WHERE b.branch_id = t.branch_id AND b.is_main;
ALTER TABLE chat_messages ADD COLUMN brand_id BIGINT REFERENCES brands(id) ON DELETE SET NULL;
UPDATE chat_messages m SET brand_id = t.brand_id FROM chat_threads t WHERE t.id = m.thread_id;

-- +goose Down
ALTER TABLE chat_messages DROP COLUMN brand_id;
ALTER TABLE chat_threads DROP COLUMN brand_id;
