-- +goose Up
-- docs/PRICING_REVIEW.md §5.3, steps 2 and 3.

-- Each dish line's share of the order's money, as the pricing engine shared
-- it out: the store-paid discounts on it, and its part of the service charge
-- and of the tax. What the line cost the customer is
-- line_total - discount_share + service_share (+ tax_share when tax is added).
ALTER TABLE order_items ADD COLUMN discount_share BIGINT NOT NULL DEFAULT 0;
ALTER TABLE order_items ADD COLUMN service_share BIGINT NOT NULL DEFAULT 0;
ALTER TABLE order_items ADD COLUMN tax_share BIGINT NOT NULL DEFAULT 0;
-- How each discount row is shared over the lines: {"<order_items.id>": amount}.
ALTER TABLE order_discounts ADD COLUMN per_line JSONB NOT NULL DEFAULT '{}';

-- The money ledger: one row each time an order's amounts change, written by
-- the database itself so that no change can be missed. Rows are never
-- updated or deleted. The amounts are the order's after the change; the
-- deltas are the change. kind says why (placed, addon, void, adjust, table,
-- refund, ...), set by the application for its transaction with
-- set_config('waimai.money_kind', ...); a change without one is 'change'.
CREATE TABLE order_money_events (
    id                BIGSERIAL PRIMARY KEY,
    order_id          BIGINT NOT NULL REFERENCES orders(id),
    branch_id         BIGINT NOT NULL,
    kind              TEXT NOT NULL,
    ref_id            BIGINT,
    actor_type        TEXT NOT NULL DEFAULT '',
    actor_id          BIGINT,
    subtotal          BIGINT NOT NULL,
    discount          BIGINT NOT NULL,
    merchant_discount BIGINT NOT NULL,
    platform_subsidy  BIGINT NOT NULL,
    staff_discount    BIGINT NOT NULL,
    round_off         BIGINT NOT NULL,
    service_charge    BIGINT NOT NULL,
    tax_amount        BIGINT NOT NULL,
    total             BIGINT NOT NULL,
    -- money given back by dish (refund rows only)
    refunded          BIGINT NOT NULL DEFAULT 0,
    delta_total       BIGINT NOT NULL,
    delta_merchant    BIGINT NOT NULL,
    delta_platform    BIGINT NOT NULL,
    -- the transaction: changes made together (an order and its discounts) share it
    tx                BIGINT NOT NULL DEFAULT txid_current(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_money_events_order ON order_money_events (order_id, id);
CREATE INDEX order_money_events_branch_day ON order_money_events (branch_id, created_at);

-- +goose StatementBegin
CREATE FUNCTION order_money_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    prev order_money_events%ROWTYPE;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.subtotal = OLD.subtotal AND NEW.discount = OLD.discount
        AND NEW.merchant_discount = OLD.merchant_discount AND NEW.platform_subsidy = OLD.platform_subsidy
        AND NEW.staff_discount = OLD.staff_discount AND NEW.round_off = OLD.round_off
        AND NEW.service_charge = OLD.service_charge AND NEW.tax_amount = OLD.tax_amount AND NEW.total = OLD.total THEN
        RETURN NULL;
    END IF;
    SELECT * INTO prev FROM order_money_events WHERE order_id = NEW.id ORDER BY id DESC LIMIT 1;
    INSERT INTO order_money_events (order_id, branch_id, kind, ref_id, actor_type, actor_id, subtotal, discount,
        merchant_discount, platform_subsidy, staff_discount, round_off, service_charge, tax_amount, total,
        delta_total, delta_merchant, delta_platform)
    VALUES (NEW.id, NEW.branch_id,
        COALESCE(NULLIF(current_setting('waimai.money_kind', true), ''), CASE WHEN TG_OP = 'INSERT' THEN 'placed' ELSE 'change' END),
        NULLIF(current_setting('waimai.money_ref', true), '')::bigint,
        COALESCE(current_setting('waimai.money_actor_type', true), ''),
        NULLIF(current_setting('waimai.money_actor_id', true), '')::bigint,
        NEW.subtotal, NEW.discount, NEW.merchant_discount, NEW.platform_subsidy, NEW.staff_discount, NEW.round_off,
        NEW.service_charge, NEW.tax_amount, NEW.total,
        NEW.total - COALESCE(prev.total, 0), NEW.merchant_discount - COALESCE(prev.merchant_discount, 0),
        NEW.platform_subsidy - COALESCE(prev.platform_subsidy, 0));
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER order_money_event AFTER INSERT OR UPDATE ON orders
    FOR EACH ROW EXECUTE FUNCTION order_money_event();

-- The rows cannot be changed or removed.
-- +goose StatementBegin
CREATE FUNCTION order_money_events_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'order_money_events is append-only';
END $$;
-- +goose StatementEnd
CREATE TRIGGER order_money_events_frozen BEFORE UPDATE OR DELETE ON order_money_events
    FOR EACH ROW EXECUTE FUNCTION order_money_events_frozen();

-- Older orders start the ledger with what they are now.
INSERT INTO order_money_events (order_id, branch_id, kind, subtotal, discount, merchant_discount, platform_subsidy,
    staff_discount, round_off, service_charge, tax_amount, total, delta_total, delta_merchant, delta_platform, created_at)
SELECT id, branch_id, 'opening', subtotal, discount, merchant_discount, platform_subsidy, staff_discount, round_off,
    service_charge, tax_amount, total, total, merchant_discount, platform_subsidy, created_at FROM orders ORDER BY id;
INSERT INTO order_money_events (order_id, branch_id, kind, ref_id, actor_id, subtotal, discount, merchant_discount,
    platform_subsidy, staff_discount, round_off, service_charge, tax_amount, total, refunded, delta_total,
    delta_merchant, delta_platform, created_at)
SELECT r.order_id, r.branch_id, 'refund', r.id, r.refunded_by, o.subtotal, o.discount, o.merchant_discount,
    o.platform_subsidy, o.staff_discount, o.round_off, o.service_charge, o.tax_amount, o.total, r.amount, 0, 0, 0, r.created_at
FROM order_refunds r JOIN orders o ON o.id = r.order_id ORDER BY r.id;

-- A refund gives money back without changing the order's amounts, so it
-- gets its own row: the order as it is, and what was given back.
-- +goose StatementBegin
CREATE FUNCTION order_refund_money_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO order_money_events (order_id, branch_id, kind, ref_id, actor_type, actor_id, subtotal, discount,
        merchant_discount, platform_subsidy, staff_discount, round_off, service_charge, tax_amount, total,
        refunded, delta_total, delta_merchant, delta_platform)
    SELECT o.id, o.branch_id, 'refund', NEW.id, CASE WHEN NEW.refunded_by IS NULL THEN '' ELSE 'staff' END,
        NEW.refunded_by, o.subtotal, o.discount, o.merchant_discount, o.platform_subsidy, o.staff_discount,
        o.round_off, o.service_charge, o.tax_amount, o.total, NEW.amount, 0, 0, 0
    FROM orders o WHERE o.id = NEW.order_id;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER order_refund_money_event AFTER INSERT ON order_refunds
    FOR EACH ROW EXECUTE FUNCTION order_refund_money_event();

-- +goose Down
DROP TRIGGER order_refund_money_event ON order_refunds;
DROP FUNCTION order_refund_money_event();
DROP TRIGGER order_money_event ON orders;
DROP FUNCTION order_money_event();
DROP TABLE order_money_events;
DROP FUNCTION order_money_events_frozen();
ALTER TABLE order_discounts DROP COLUMN per_line;
ALTER TABLE order_items DROP COLUMN tax_share;
ALTER TABLE order_items DROP COLUMN service_share;
ALTER TABLE order_items DROP COLUMN discount_share;
