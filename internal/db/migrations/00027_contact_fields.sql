-- +goose Up
-- Which other ways to reach a customer (besides the phone) a store asks for
-- on the address form: any of wechat, telegram, line, whatsapp. None until the
-- store turns them on in its settings.
ALTER TABLE branches
    ADD COLUMN contact_fields TEXT[] NOT NULL DEFAULT '{}'
        CHECK (contact_fields <@ ARRAY['wechat', 'telegram', 'line', 'whatsapp']);

ALTER TABLE customer_addresses
    ADD COLUMN line     TEXT NOT NULL DEFAULT '' CHECK (length(line) <= 64),
    ADD COLUMN whatsapp TEXT NOT NULL DEFAULT '' CHECK (length(whatsapp) <= 64);

ALTER TABLE orders
    ADD COLUMN contact_line     TEXT NOT NULL DEFAULT '',
    ADD COLUMN contact_whatsapp TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE orders DROP COLUMN contact_line, DROP COLUMN contact_whatsapp;
ALTER TABLE customer_addresses DROP COLUMN line, DROP COLUMN whatsapp;
ALTER TABLE branches DROP COLUMN contact_fields;
