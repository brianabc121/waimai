-- +goose Up
-- Chat, second part: pictures, quick replies and the answer a closed store
-- gives by itself (docs/CHAT_DESIGN.md).

-- A picture sent in a chat. It is uploaded here first; the message that
-- travels through OpenIM carries only the token.
CREATE TABLE chat_images (
    token       TEXT PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    branch_id   BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    sender      TEXT NOT NULL CHECK (sender IN ('customer','store')),
    content     BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX chat_images_age_idx ON chat_images (created_at);

-- Sentences the store's desk sends with one tap.
CREATE TABLE chat_quick_replies (
    id         BIGSERIAL PRIMARY KEY,
    branch_id  BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX chat_quick_replies_branch_idx ON chat_quick_replies (branch_id, id);

-- What a store answers by itself while it is closed. '' is the standard sentence.
ALTER TABLE branches ADD COLUMN chat_auto_reply_on BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE branches ADD COLUMN chat_auto_reply TEXT NOT NULL DEFAULT '';
-- when a conversation last got that answer: once in six hours is enough
ALTER TABLE chat_threads ADD COLUMN auto_replied_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE chat_threads DROP COLUMN auto_replied_at;
ALTER TABLE branches DROP COLUMN chat_auto_reply;
ALTER TABLE branches DROP COLUMN chat_auto_reply_on;
DROP TABLE chat_quick_replies;
DROP TABLE chat_images;
