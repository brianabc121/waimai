-- +goose Up
-- docs/PRICING_REVIEW.md §7 step 0: an order keeps the service charge rate it
-- was priced with, and the kind of its promotion, so that a later add-on,
-- void, discount or refund never reads the store's or the promotion's
-- current settings. Older orders take today's values as the best guess.
ALTER TABLE orders ADD COLUMN service_charge_bp INT;
ALTER TABLE orders ADD COLUMN promotion_kind TEXT NOT NULL DEFAULT '';
UPDATE orders o SET service_charge_bp = b.service_charge_bp FROM branches b WHERE b.id = o.branch_id;
UPDATE orders o SET promotion_kind = p.kind FROM promotions p WHERE p.id = o.promotion_id;

-- +goose StatementBegin
CREATE FUNCTION orders_pricing_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.service_charge_bp IS NULL THEN
        SELECT service_charge_bp INTO NEW.service_charge_bp FROM branches WHERE id = NEW.branch_id;
    END IF;
    IF TG_OP = 'INSERT' OR NEW.promotion_id IS DISTINCT FROM OLD.promotion_id THEN
        IF NEW.promotion_id IS NULL THEN
            NEW.promotion_kind := '';
        ELSE
            SELECT kind INTO NEW.promotion_kind FROM promotions WHERE id = NEW.promotion_id;
        END IF;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER orders_pricing_snapshot BEFORE INSERT OR UPDATE OF promotion_id ON orders
    FOR EACH ROW EXECUTE FUNCTION orders_pricing_snapshot();
ALTER TABLE orders ALTER COLUMN service_charge_bp SET NOT NULL;

-- How a refund was paid back: cash leaves the drawer, the rest does not.
ALTER TABLE order_refunds ADD COLUMN pay_type TEXT NOT NULL DEFAULT 'other'
    CHECK (pay_type IN ('cash', 'transfer', 'online', 'other'));
UPDATE order_refunds SET pay_type = 'cash'
    WHERE lower(method) IN ('现金', '現金', 'cash', 'efectivo', 'espèces', 'наличные', 'tiền mặt', 'เงินสด', 'نقدًا', 'ငွေသား', 'សាច់ប្រាក់');

-- +goose Down
ALTER TABLE order_refunds DROP COLUMN pay_type;
DROP TRIGGER orders_pricing_snapshot ON orders;
DROP FUNCTION orders_pricing_snapshot();
ALTER TABLE orders DROP COLUMN promotion_kind;
ALTER TABLE orders DROP COLUMN service_charge_bp;
