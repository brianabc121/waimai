-- +goose Up
-- Applications to join from the company website (docs/WEBSITE_DESIGN.md §8).
-- Visitors there have no customer account, so the customer is optional and
-- the row says where it came from. The website asks what kind of store it is,
-- how many stores, what they want to use and how to reach them; the platform
-- keeps track of following up apart from the decision (pending, approved,
-- rejected), which still means "may open a store".
ALTER TABLE merchant_applications
    ALTER COLUMN customer_id DROP NOT NULL,
    ADD COLUMN source      TEXT NOT NULL DEFAULT 'app' CHECK (source IN ('app','website')),
    ADD COLUMN store_type  TEXT NOT NULL DEFAULT '' CHECK (store_type IN ('','quick','full','tea','other')),
    ADD COLUMN store_count TEXT NOT NULL DEFAULT '' CHECK (store_count IN ('','1','2-5','6-20','20+')),
    ADD COLUMN wants       TEXT[] NOT NULL DEFAULT '{}' CHECK (wants <@ ARRAY['delivery','pickup','dine_in','phone']),
    ADD COLUMN im_kind     TEXT NOT NULL DEFAULT '' CHECK (im_kind IN ('','wechat','whatsapp','telegram','line','viber')),
    ADD COLUMN im_handle   TEXT NOT NULL DEFAULT '' CHECK (length(im_handle) <= 100),
    ADD COLUMN email       TEXT NOT NULL DEFAULT '' CHECK (length(email) <= 200),
    ADD COLUMN lang        TEXT NOT NULL DEFAULT '' CHECK (length(lang) <= 10),
    ADD COLUMN page        TEXT NOT NULL DEFAULT '' CHECK (length(page) <= 200),
    ADD COLUMN referrer    TEXT NOT NULL DEFAULT '' CHECK (length(referrer) <= 500),
    ADD COLUMN follow_up   TEXT NOT NULL DEFAULT 'new' CHECK (follow_up IN ('new','contacted','demoed','invalid')),
    ADD COLUMN follow_note TEXT NOT NULL DEFAULT '' CHECK (length(follow_note) <= 1000),
    ADD CONSTRAINT merchant_applications_who CHECK (source = 'website' OR customer_id IS NOT NULL);
CREATE INDEX merchant_applications_phone_idx ON merchant_applications (phone, created_at DESC) WHERE source = 'website';

-- +goose Down
DELETE FROM merchant_applications WHERE customer_id IS NULL;
DROP INDEX merchant_applications_phone_idx;
ALTER TABLE merchant_applications
    DROP CONSTRAINT merchant_applications_who,
    DROP COLUMN source, DROP COLUMN store_type, DROP COLUMN store_count, DROP COLUMN wants,
    DROP COLUMN im_kind, DROP COLUMN im_handle, DROP COLUMN email, DROP COLUMN lang, DROP COLUMN page,
    DROP COLUMN referrer, DROP COLUMN follow_up, DROP COLUMN follow_note,
    ALTER COLUMN customer_id SET NOT NULL;
