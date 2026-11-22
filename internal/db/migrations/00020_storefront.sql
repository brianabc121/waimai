-- +goose Up
-- The customer's store page: a notice and an introduction, baskets that keep
-- one order's dishes apart, categories a customer has to pick from, reviews
-- and reports.
ALTER TABLE branches
    ADD COLUMN notice          TEXT NOT NULL DEFAULT '' CHECK (length(notice) <= 500),
    ADD COLUMN intro           TEXT NOT NULL DEFAULT '' CHECK (length(intro) <= 1000),
    -- customers may split a delivery or pickup order into up to four baskets
    ADD COLUMN baskets_enabled BOOLEAN NOT NULL DEFAULT true;

-- An order must contain something from a required category (a soup base, for example).
ALTER TABLE menu_categories ADD COLUMN required BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE order_items ADD COLUMN basket_no INT NOT NULL DEFAULT 1 CHECK (basket_no BETWEEN 1 AND 4);

-- One review per finished order, written by the customer who holds its link.
CREATE TABLE reviews (
    id           BIGSERIAL PRIMARY KEY,
    branch_id    BIGINT NOT NULL REFERENCES branches(id),
    order_id     BIGINT NOT NULL UNIQUE REFERENCES orders(id),
    rating       INT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    taste        INT NOT NULL CHECK (taste BETWEEN 1 AND 5),
    packaging    INT NOT NULL CHECK (packaging BETWEEN 1 AND 5),
    -- NULL for pickup orders
    delivery     INT CHECK (delivery BETWEEN 1 AND 5),
    comment      TEXT NOT NULL DEFAULT '' CHECK (length(comment) <= 1000),
    -- shown instead of the customer's name or phone
    display_name TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX reviews_branch_idx ON reviews (branch_id, created_at DESC);

-- A customer's complaint about a store, read by the platform.
CREATE TABLE store_reports (
    id         BIGSERIAL PRIMARY KEY,
    branch_id  BIGINT NOT NULL REFERENCES branches(id),
    reason     TEXT NOT NULL,
    detail     TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 1000),
    contact    TEXT NOT NULL DEFAULT '' CHECK (length(contact) <= 100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX store_reports_branch_idx ON store_reports (branch_id, created_at DESC);

-- +goose Down
DROP TABLE store_reports;
DROP TABLE reviews;
ALTER TABLE order_items DROP COLUMN basket_no;
ALTER TABLE menu_categories DROP COLUMN required;
ALTER TABLE branches DROP COLUMN notice, DROP COLUMN intro, DROP COLUMN baskets_enabled;
