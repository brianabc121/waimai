-- +goose Up
-- The store's reconciliation page (docs/CHECKOUT_DISCOUNT_DESIGN.md §8):
-- when each order was paid, the owner's check of each online payment, and
-- the cash counted in the drawer at the end of a day.

ALTER TABLE orders ADD COLUMN paid_at TIMESTAMPTZ;

-- +goose StatementBegin
CREATE FUNCTION orders_paid_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.payment_status = 'paid' AND NEW.paid_at IS NULL THEN
        NEW.paid_at := now();
    ELSIF NEW.payment_status <> 'paid' THEN
        NEW.paid_at := NULL;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER orders_paid_at BEFORE INSERT OR UPDATE OF payment_status ON orders
    FOR EACH ROW EXECUTE FUNCTION orders_paid_at();

-- older paid orders: the last time they changed is the best guess
UPDATE orders SET paid_at = updated_at WHERE payment_status = 'paid';
CREATE INDEX orders_paid ON orders (branch_id, paid_at) WHERE paid_at IS NOT NULL;

-- No row means "not checked yet".
CREATE TABLE payment_checks (
    order_id   BIGINT PRIMARY KEY,
    branch_id  BIGINT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN ('ok','problem')),
    note       TEXT NOT NULL DEFAULT '',
    checked_by BIGINT REFERENCES users(id),
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (order_id, branch_id) REFERENCES orders (id, branch_id) ON DELETE CASCADE
);
CREATE INDEX payment_checks_branch ON payment_checks (branch_id, status);

-- One count per store per day; counting again replaces it (the audit log keeps both).
CREATE TABLE cash_counts (
    branch_id  BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    day        DATE NOT NULL,
    expected   BIGINT NOT NULL,
    counted    BIGINT NOT NULL CHECK (counted >= 0),
    note       TEXT NOT NULL DEFAULT '',
    counted_by BIGINT REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (branch_id, day)
);

-- +goose Down
DROP TABLE cash_counts;
DROP TABLE payment_checks;
DROP TRIGGER orders_paid_at ON orders;
DROP FUNCTION orders_paid_at();
ALTER TABLE orders DROP COLUMN paid_at;
