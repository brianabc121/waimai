-- +goose Up
-- The store's own page and domains (docs/ORDER_TYPES_DESIGN.md §5). Each store
-- has a store number from 10001 on: its page is <store_no>.<site domain>.
CREATE SEQUENCE branch_store_no_seq START 10001;
ALTER TABLE branches ADD COLUMN store_no INT;
UPDATE branches b SET store_no = n.no
FROM (SELECT id, nextval('branch_store_no_seq') AS no FROM (SELECT id FROM branches ORDER BY id) s) n
WHERE n.id = b.id;
ALTER TABLE branches ALTER COLUMN store_no SET DEFAULT nextval('branch_store_no_seq'),
    ALTER COLUMN store_no SET NOT NULL,
    ADD CONSTRAINT branches_store_no UNIQUE (store_no);
ALTER SEQUENCE branch_store_no_seq OWNED BY branches.store_no;

-- A store's own domains. kind own: the merchant's domain pointed here with a
-- CNAME, checked by the worker; kind buy: a domain the merchant asks the
-- platform to buy (candidates are the names wanted, the admin fills domain).
CREATE TABLE branch_domains (
    id           BIGSERIAL PRIMARY KEY,
    branch_id    BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('own','buy')),
    domain       TEXT NOT NULL DEFAULT '',
    candidates   TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL CHECK (status IN ('pending','active','failed','requested','quoted','bought','rejected')),
    note         TEXT NOT NULL DEFAULT '',
    admin_note   TEXT NOT NULL DEFAULT '',
    check_error  TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    checked_at   TIMESTAMPTZ,
    verified_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX branch_domains_domain ON branch_domains (domain) WHERE domain <> '';
CREATE INDEX branch_domains_branch ON branch_domains (branch_id);

-- +goose Down
DROP TABLE branch_domains;
ALTER TABLE branches DROP COLUMN store_no;
