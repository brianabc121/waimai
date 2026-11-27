-- +goose Up
-- Store credit: money a store refunds into a customer's balance, which can
-- only be spent at that store (docs/REFUND_BALANCE_DESIGN.md). The books are
-- double-entry: every change is a journal of two or more postings that add up
-- to zero, and neither journals nor postings can be changed or removed.

-- An account holds one currency. store_credit is a customer's balance at a
-- store and never goes below zero; store_refund (what the store put into
-- balances) and store_sales (what was paid from them) are the store's side.
CREATE TABLE wallet_accounts (
    id          BIGSERIAL PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('store_credit', 'store_refund', 'store_sales')),
    branch_id   BIGINT NOT NULL REFERENCES branches(id),
    customer_id BIGINT REFERENCES customers(id),
    currency    TEXT NOT NULL CHECK (length(currency) = 3),
    balance     BIGINT NOT NULL DEFAULT 0,
    -- a frozen balance cannot be spent (the platform's decision)
    frozen      BOOLEAN NOT NULL DEFAULT false,
    frozen_note TEXT NOT NULL DEFAULT '' CHECK (length(frozen_note) <= 200),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((kind = 'store_credit') = (customer_id IS NOT NULL)),
    CHECK (kind <> 'store_credit' OR balance >= 0)
);
CREATE UNIQUE INDEX wallet_accounts_key ON wallet_accounts (kind, branch_id, COALESCE(customer_id, 0), currency);
CREATE INDEX wallet_accounts_customer ON wallet_accounts (customer_id) WHERE customer_id IS NOT NULL;

-- One business event. idem_key makes a retried request post nothing twice.
CREATE TABLE wallet_journals (
    id          BIGSERIAL PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('refund', 'spend', 'return', 'revoke')),
    idem_key    TEXT NOT NULL UNIQUE CHECK (length(idem_key) BETWEEN 1 AND 200),
    branch_id   BIGINT NOT NULL REFERENCES branches(id),
    currency    TEXT NOT NULL CHECK (length(currency) = 3),
    order_id    BIGINT REFERENCES orders(id),
    refund_id   BIGINT REFERENCES order_refunds(id),
    actor_type  TEXT NOT NULL DEFAULT '',
    actor_id    BIGINT,
    approved_by BIGINT REFERENCES users(id),
    note        TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    reverses    BIGINT REFERENCES wallet_journals(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX wallet_journals_branch ON wallet_journals (branch_id, created_at);
CREATE INDEX wallet_journals_order ON wallet_journals (order_id) WHERE order_id IS NOT NULL;

CREATE TABLE wallet_postings (
    id            BIGSERIAL PRIMARY KEY,
    journal_id    BIGINT NOT NULL REFERENCES wallet_journals(id),
    account_id    BIGINT NOT NULL REFERENCES wallet_accounts(id),
    amount        BIGINT NOT NULL CHECK (amount <> 0),
    -- the account's balance right after this posting
    balance_after BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX wallet_postings_journal ON wallet_postings (journal_id);
CREATE INDEX wallet_postings_account ON wallet_postings (account_id, id);

-- Journals and postings are append-only.
-- +goose StatementBegin
CREATE FUNCTION wallet_frozen_rows() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END $$;
-- +goose StatementEnd
CREATE TRIGGER wallet_journals_frozen BEFORE UPDATE OR DELETE ON wallet_journals
    FOR EACH ROW EXECUTE FUNCTION wallet_frozen_rows();
CREATE TRIGGER wallet_postings_frozen BEFORE UPDATE OR DELETE ON wallet_postings
    FOR EACH ROW EXECUTE FUNCTION wallet_frozen_rows();

-- A posting moves its account's balance; nothing else can. The currency of
-- every posting is its journal's.
-- +goose StatementBegin
CREATE FUNCTION wallet_post() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    jcur TEXT;
    acur TEXT;
    after BIGINT;
BEGIN
    SELECT currency INTO jcur FROM wallet_journals WHERE id = NEW.journal_id;
    SELECT currency INTO acur FROM wallet_accounts WHERE id = NEW.account_id;
    IF jcur IS DISTINCT FROM acur THEN
        RAISE EXCEPTION 'wallet posting currency % does not match its journal %', acur, jcur;
    END IF;
    PERFORM set_config('waimai.wallet_posting', 'on', true);
    UPDATE wallet_accounts SET balance = balance + NEW.amount, updated_at = now()
        WHERE id = NEW.account_id RETURNING balance INTO after;
    PERFORM set_config('waimai.wallet_posting', '', true);
    NEW.balance_after := after;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER wallet_post BEFORE INSERT ON wallet_postings
    FOR EACH ROW EXECUTE FUNCTION wallet_post();

-- +goose StatementBegin
CREATE FUNCTION wallet_balance_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.balance <> OLD.balance AND COALESCE(current_setting('waimai.wallet_posting', true), '') <> 'on' THEN
        RAISE EXCEPTION 'a wallet balance only changes by a posting';
    END IF;
    IF NEW.kind <> OLD.kind OR NEW.branch_id <> OLD.branch_id OR NEW.currency <> OLD.currency
        OR NEW.customer_id IS DISTINCT FROM OLD.customer_id THEN
        RAISE EXCEPTION 'a wallet account keeps its owner and currency';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER wallet_balance_guard BEFORE UPDATE ON wallet_accounts
    FOR EACH ROW EXECUTE FUNCTION wallet_balance_guard();
CREATE TRIGGER wallet_accounts_kept BEFORE DELETE ON wallet_accounts
    FOR EACH ROW EXECUTE FUNCTION wallet_frozen_rows();

-- Every journal balances, checked when the transaction commits.
-- +goose StatementBegin
CREATE FUNCTION wallet_journal_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    n INT;
    s BIGINT;
BEGIN
    SELECT count(*), COALESCE(sum(amount), 0) INTO n, s FROM wallet_postings WHERE journal_id = NEW.journal_id;
    IF n < 2 OR s <> 0 THEN
        RAISE EXCEPTION 'wallet journal % does not balance (% postings, sum %)', NEW.journal_id, n, s;
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER wallet_journal_balanced AFTER INSERT ON wallet_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION wallet_journal_balanced();

-- What an order had paid from a store balance, and whose balance it was.
ALTER TABLE orders ADD COLUMN credit_paid BIGINT NOT NULL DEFAULT 0 CHECK (credit_paid >= 0),
    ADD COLUMN credit_customer_id BIGINT REFERENCES customers(id);
ALTER TABLE orders DROP CONSTRAINT orders_pay_type_check,
    ADD CONSTRAINT orders_pay_type_check CHECK (pay_type IN ('', 'cash', 'card', 'online', 'platform', 'credit'));

-- A balance never pays more than the order comes to (rule W4), checked at
-- commit: an order whose total drops gives the difference back in the same
-- transaction.
-- +goose StatementBegin
CREATE FUNCTION orders_credit_within_total() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM orders WHERE id = NEW.id AND credit_paid > total) THEN
        RAISE EXCEPTION 'order % paid more from a balance than it comes to', NEW.id;
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER orders_credit_within_total AFTER INSERT OR UPDATE OF credit_paid, total ON orders
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION orders_credit_within_total();

-- A refund splits into the part that goes back to the balance it was paid
-- from and the part that was paid in cash (cash_part, worked out in
-- proportion); to_credit is how much of it went into the store balance:
-- the balance part, plus the cash part when the customer agreed to that.
ALTER TABLE order_refunds ADD COLUMN cash_part BIGINT,
    ADD COLUMN to_credit BIGINT NOT NULL DEFAULT 0,
    -- how the customer agreed to a store balance instead of cash
    ADD COLUMN consent TEXT NOT NULL DEFAULT '' CHECK (consent IN ('', 'in_person', 'phone', 'chat', 'sms', 'self'));
UPDATE order_refunds SET cash_part = amount;
ALTER TABLE order_refunds ALTER COLUMN cash_part SET NOT NULL;
-- A refund written without the split (a platform's deduction) was all cash.
-- +goose StatementBegin
CREATE FUNCTION order_refunds_cash_part() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cash_part IS NULL THEN
        NEW.cash_part := NEW.amount;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER order_refunds_cash_part BEFORE INSERT ON order_refunds
    FOR EACH ROW EXECUTE FUNCTION order_refunds_cash_part();
ALTER TABLE order_refunds ADD CONSTRAINT order_refunds_parts CHECK (cash_part >= 0 AND cash_part <= amount
    AND to_credit >= amount - cash_part AND to_credit <= amount);
ALTER TABLE order_refunds DROP CONSTRAINT order_refunds_pay_type_check,
    ADD CONSTRAINT order_refunds_pay_type_check CHECK (pay_type IN ('cash', 'transfer', 'online', 'other', 'platform', 'store_credit'));

-- The owner's limits on refunds into a store balance (§6.4): one above
-- credit_approve_over needs a manager's code; a person's total for the day
-- above credit_daily_limit needs the owner.
ALTER TABLE branches ADD COLUMN credit_approve_over BIGINT NOT NULL DEFAULT 200000 CHECK (credit_approve_over >= 0),
    ADD COLUMN credit_daily_limit BIGINT NOT NULL DEFAULT 500000 CHECK (credit_daily_limit >= 0);

-- A code texted to a customer so that the store may use or fill their store
-- balance on an order the staff entered (§5.3). Kept apart from the login
-- codes, so that a code told to the staff never logs anyone in.
CREATE TABLE wallet_verifications (
    id          BIGSERIAL PRIMARY KEY,
    order_id    BIGINT NOT NULL REFERENCES orders(id),
    phone       TEXT NOT NULL,
    purpose     TEXT NOT NULL CHECK (purpose IN ('spend', 'refund')),
    code_hash   BYTEA NOT NULL,
    attempts    INT NOT NULL DEFAULT 0,
    requested_by BIGINT REFERENCES users(id),
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX wallet_verifications_order ON wallet_verifications (order_id, created_at DESC);

-- The nightly check of the books (§10.1): what it found.
CREATE TABLE wallet_check_runs (
    id       BIGSERIAL PRIMARY KEY,
    ran_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    problems JSONB NOT NULL DEFAULT '[]'
);

-- Managers may refund into a store balance and see the balances by default.
UPDATE branch_role_perms SET perms = perms || ARRAY['wallet.refund', 'wallet.view']
    WHERE role = 'manager' AND NOT 'wallet.refund' = ANY(perms);

-- +goose Down
UPDATE branch_role_perms SET perms = array_remove(array_remove(perms, 'wallet.refund'), 'wallet.view');
DROP TABLE wallet_check_runs;
DROP TABLE wallet_verifications;
ALTER TABLE branches DROP COLUMN credit_approve_over, DROP COLUMN credit_daily_limit;
UPDATE order_refunds SET pay_type = 'other' WHERE pay_type = 'store_credit';
ALTER TABLE order_refunds DROP CONSTRAINT order_refunds_pay_type_check,
    ADD CONSTRAINT order_refunds_pay_type_check CHECK (pay_type IN ('cash', 'transfer', 'online', 'other', 'platform'));
ALTER TABLE order_refunds DROP CONSTRAINT order_refunds_parts;
DROP TRIGGER order_refunds_cash_part ON order_refunds;
DROP FUNCTION order_refunds_cash_part();
ALTER TABLE order_refunds DROP COLUMN consent, DROP COLUMN to_credit, DROP COLUMN cash_part;
DROP TRIGGER orders_credit_within_total ON orders;
DROP FUNCTION orders_credit_within_total();
UPDATE orders SET pay_type = '' WHERE pay_type = 'credit';
ALTER TABLE orders DROP CONSTRAINT orders_pay_type_check,
    ADD CONSTRAINT orders_pay_type_check CHECK (pay_type IN ('', 'cash', 'card', 'online', 'platform'));
ALTER TABLE orders DROP COLUMN credit_customer_id, DROP COLUMN credit_paid;
DROP TABLE wallet_postings;
DROP TABLE wallet_journals;
DROP TABLE wallet_accounts;
DROP FUNCTION wallet_journal_balanced();
DROP FUNCTION wallet_balance_guard();
DROP FUNCTION wallet_post();
DROP FUNCTION wallet_frozen_rows();
