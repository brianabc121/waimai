-- +goose Up
-- One store, several brands (docs/MULTI_BRAND_DESIGN.md §4). The store
-- (branches) is the kitchen, staff, cash drawer and delivery area; a brand is
-- what customers see: its name, its code for staff orders (YPX000112), its
-- members. Every store has one main brand that mirrors the store's own name,
-- slug, code and store number, so old links, codes and numbers stay as they were.
CREATE TABLE brands (
    id           BIGSERIAL PRIMARY KEY,
    branch_id    BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    name_en      TEXT NOT NULL DEFAULT '',
    -- the code staff order numbers start with; unique over the platform
    code         TEXT NOT NULL DEFAULT '',
    -- the brand's own page <store_no>.<site domain> and its mall address
    store_no     INT NOT NULL DEFAULT nextval('branch_store_no_seq'),
    slug         TEXT NOT NULL,
    dine_in      BOOLEAN NOT NULL DEFAULT false,
    delivery     BOOLEAN NOT NULL DEFAULT true,
    is_main      BOOLEAN NOT NULL DEFAULT false,
    -- the colour of the brand's tag on back-office pages
    color        TEXT NOT NULL DEFAULT '#a02c20',
    -- the brand's own hours inside the store's; equal (or empty) = the store's
    open_time    TEXT NOT NULL DEFAULT '',
    close_time   TEXT NOT NULL DEFAULT '',
    paused       BOOLEAN NOT NULL DEFAULT false,
    mall_visible BOOLEAN NOT NULL DEFAULT true,
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    sort         INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT brands_slug UNIQUE (slug),
    CONSTRAINT brands_store_no UNIQUE (store_no)
);
CREATE UNIQUE INDEX brands_code ON brands (code) WHERE code <> '';
CREATE UNIQUE INDEX brands_one_main ON brands (branch_id) WHERE is_main;
CREATE UNIQUE INDEX brands_one_dine ON brands (branch_id) WHERE dine_in AND status = 'active';
CREATE INDEX brands_branch ON brands (branch_id);

INSERT INTO brands (branch_id, name, name_en, code, store_no, slug, dine_in, delivery, is_main)
SELECT id, name, name_en, store_code, store_no, slug, true, true, true FROM branches ORDER BY id;

-- A new store gets its main brand; the main brand follows the store's name,
-- slug, code and number when they change.
-- +goose StatementBegin
CREATE FUNCTION branches_main_brand() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO brands (branch_id, name, name_en, code, store_no, slug, dine_in, delivery, is_main)
        VALUES (NEW.id, NEW.name, NEW.name_en, NEW.store_code, NEW.store_no, NEW.slug, true, true, true);
    ELSIF (NEW.name, NEW.name_en, NEW.store_code, NEW.store_no, NEW.slug)
        IS DISTINCT FROM (OLD.name, OLD.name_en, OLD.store_code, OLD.store_no, OLD.slug) THEN
        UPDATE brands SET name = NEW.name, name_en = NEW.name_en, code = NEW.store_code,
            store_no = NEW.store_no, slug = NEW.slug
        WHERE branch_id = NEW.id AND is_main;
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER branches_main_brand AFTER INSERT OR UPDATE ON branches
    FOR EACH ROW EXECUTE FUNCTION branches_main_brand();

-- Every order belongs to a brand, the store's main brand unless the order
-- says otherwise; brand_name keeps the name the order was placed under.
ALTER TABLE orders ADD COLUMN brand_id BIGINT REFERENCES brands(id),
    ADD COLUMN brand_name TEXT NOT NULL DEFAULT '';
UPDATE orders o SET brand_id = b.id, brand_name = b.name FROM brands b WHERE b.branch_id = o.branch_id AND b.is_main;

-- Members belong to a brand: numbers count up per brand (HCD000223).
ALTER TABLE store_members ADD COLUMN brand_id BIGINT REFERENCES brands(id);
UPDATE store_members m SET brand_id = b.id FROM brands b WHERE b.branch_id = m.branch_id AND b.is_main;

-- +goose StatementBegin
CREATE FUNCTION fill_main_brand() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.brand_id IS NULL THEN
        SELECT id INTO NEW.brand_id FROM brands WHERE branch_id = NEW.branch_id AND is_main;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION fill_order_brand() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.brand_id IS NULL THEN
        SELECT id INTO NEW.brand_id FROM brands WHERE branch_id = NEW.branch_id AND is_main;
    END IF;
    IF NEW.brand_name = '' THEN
        SELECT name INTO NEW.brand_name FROM brands WHERE id = NEW.brand_id;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER orders_brand BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION fill_order_brand();
CREATE TRIGGER store_members_brand BEFORE INSERT ON store_members FOR EACH ROW EXECUTE FUNCTION fill_main_brand();

ALTER TABLE orders ALTER COLUMN brand_id SET NOT NULL;
ALTER TABLE store_members ALTER COLUMN brand_id SET NOT NULL,
    DROP CONSTRAINT store_members_branch_id_member_no_key,
    ADD CONSTRAINT store_members_brand_no UNIQUE (brand_id, member_no);
CREATE INDEX store_members_brand_phone ON store_members (brand_id, phone);
CREATE INDEX orders_brand ON orders (brand_id, created_at);

-- The brands a person may work on in the store; NULL = all of them.
ALTER TABLE user_branch_roles ADD COLUMN brand_ids BIGINT[];

-- How many active delivery brands the store may have; the platform can raise it.
ALTER TABLE branches ADD COLUMN brand_limit INT NOT NULL DEFAULT 10 CHECK (brand_limit BETWEEN 1 AND 50);

-- +goose Down
ALTER TABLE branches DROP COLUMN brand_limit;
ALTER TABLE user_branch_roles DROP COLUMN brand_ids;
DROP INDEX orders_brand;
DROP INDEX store_members_brand_phone;
DROP TRIGGER store_members_brand ON store_members;
DROP TRIGGER orders_brand ON orders;
DROP FUNCTION fill_order_brand();
DROP FUNCTION fill_main_brand();
-- members of other brands cannot keep their numbers in one store-wide list
DELETE FROM store_member_addresses WHERE member_id IN (SELECT m.id FROM store_members m JOIN brands b ON b.id = m.brand_id WHERE NOT b.is_main);
UPDATE orders SET member_id = NULL WHERE member_id IN (SELECT m.id FROM store_members m JOIN brands b ON b.id = m.brand_id WHERE NOT b.is_main);
DELETE FROM store_members WHERE brand_id IN (SELECT id FROM brands WHERE NOT is_main);
ALTER TABLE store_members DROP CONSTRAINT store_members_brand_no,
    ADD CONSTRAINT store_members_branch_id_member_no_key UNIQUE (branch_id, member_no),
    DROP COLUMN brand_id;
ALTER TABLE orders DROP COLUMN brand_name, DROP COLUMN brand_id;
DROP TRIGGER branches_main_brand ON branches;
DROP FUNCTION branches_main_brand();
DROP TABLE brands;
