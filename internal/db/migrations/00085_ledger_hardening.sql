-- +goose Up
-- Hardening the balance books (docs/BALANCE_REVIEW.md batches 1–3, rules in
-- docs/BALANCE_CHECK.md). Nothing here changes what any balance holds: the
-- reclassifications below move amounts between the store's own accounts only.

-- ---- account kinds ----
-- H1: where stored value that left a customer went. store_cashout is what
-- was paid back in money, store_clawback the used bonus the store kept back,
-- store_bonus_void the bonus made void by a payback (a top-up undone the
-- same day still goes back to store_topup and store_bonus).
-- H3: the platform's own balance and rewards (they were single-entry rows in
-- customer_wallet_entries) belong to no store; platform_issued is the
-- platform's side.
ALTER TABLE wallet_accounts ALTER COLUMN branch_id DROP NOT NULL;
ALTER TABLE wallet_accounts DROP CONSTRAINT wallet_accounts_kind_check,
    ADD CONSTRAINT wallet_accounts_kind_check CHECK (kind IN ('store_credit', 'store_refund', 'store_sales',
        'stored_principal', 'stored_bonus', 'store_topup', 'store_bonus', 'store_stored_sales',
        'store_cashout', 'store_clawback', 'store_bonus_void',
        'platform_balance', 'platform_bonus', 'platform_issued'));
ALTER TABLE wallet_accounts DROP CONSTRAINT wallet_accounts_customer,
    ADD CONSTRAINT wallet_accounts_customer CHECK ((kind IN ('store_credit', 'stored_principal', 'stored_bonus',
        'platform_balance', 'platform_bonus')) = (customer_id IS NOT NULL));
ALTER TABLE wallet_accounts DROP CONSTRAINT wallet_accounts_not_negative,
    ADD CONSTRAINT wallet_accounts_not_negative CHECK (kind NOT IN ('store_credit', 'stored_principal', 'stored_bonus',
        'platform_balance', 'platform_bonus') OR balance >= 0);
ALTER TABLE wallet_accounts ADD CONSTRAINT wallet_accounts_scope CHECK ((left(kind, 9) = 'platform_') = (branch_id IS NULL));
DROP INDEX wallet_accounts_key;
CREATE UNIQUE INDEX wallet_accounts_key ON wallet_accounts (kind, COALESCE(branch_id, 0), COALESCE(customer_id, 0), currency);

-- Each account numbers its postings (posting_seq) and keeps the hash of the
-- last one (last_hash): the postings of an account are a hash chain (§H2).
ALTER TABLE wallet_accounts ADD COLUMN posting_seq BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN last_hash BYTEA;
ALTER TABLE wallet_postings ADD COLUMN seq BIGINT, ADD COLUMN hash BYTEA;

ALTER TABLE wallet_journals ALTER COLUMN branch_id DROP NOT NULL;
ALTER TABLE wallet_journals DROP CONSTRAINT wallet_journals_kind_check,
    ADD CONSTRAINT wallet_journals_kind_check CHECK (kind IN ('refund', 'spend', 'return', 'revoke',
        'topup', 'topup_revoke', 'stored_spend', 'stored_return', 'stored_refund', 'stored_cashout',
        'reclass', 'platform_adjust'));
ALTER TABLE wallet_journals ADD CONSTRAINT wallet_journals_scope CHECK ((kind = 'platform_adjust') = (branch_id IS NULL));

-- ---- M1: which accounts each kind of journal may post to ----
-- A legacy rule is one that journals written before 00085 used: the nightly
-- check accepts it on them, nothing new may use it.
CREATE TABLE wallet_journal_rules (
    journal_kind TEXT NOT NULL,
    account_kind TEXT NOT NULL,
    legacy       BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (journal_kind, account_kind)
);
INSERT INTO wallet_journal_rules (journal_kind, account_kind, legacy) VALUES
    ('refund', 'store_credit', false), ('refund', 'store_refund', false), ('refund', 'store_sales', false),
    ('revoke', 'store_credit', false), ('revoke', 'store_refund', false),
    ('spend', 'store_credit', false), ('spend', 'store_sales', false),
    ('return', 'store_credit', false), ('return', 'store_sales', false),
    ('topup', 'stored_principal', false), ('topup', 'stored_bonus', false), ('topup', 'store_topup', false), ('topup', 'store_bonus', false),
    ('topup_revoke', 'stored_principal', false), ('topup_revoke', 'stored_bonus', false),
    ('topup_revoke', 'store_topup', false), ('topup_revoke', 'store_bonus', false),
    ('stored_spend', 'stored_principal', false), ('stored_spend', 'stored_bonus', false), ('stored_spend', 'store_stored_sales', false),
    ('stored_return', 'stored_principal', false), ('stored_return', 'stored_bonus', false), ('stored_return', 'store_stored_sales', false),
    ('stored_refund', 'stored_principal', false), ('stored_refund', 'stored_bonus', false), ('stored_refund', 'store_stored_sales', false),
    ('stored_cashout', 'stored_principal', false), ('stored_cashout', 'stored_bonus', false),
    ('stored_cashout', 'store_cashout', false), ('stored_cashout', 'store_clawback', false), ('stored_cashout', 'store_bonus_void', false),
    ('stored_cashout', 'store_topup', true), ('stored_cashout', 'store_bonus', true),
    ('reclass', 'store_refund', false), ('reclass', 'store_sales', false), ('reclass', 'store_topup', false),
    ('reclass', 'store_bonus', false), ('reclass', 'store_stored_sales', false), ('reclass', 'store_cashout', false),
    ('reclass', 'store_clawback', false), ('reclass', 'store_bonus_void', false),
    ('platform_adjust', 'platform_balance', false), ('platform_adjust', 'platform_bonus', false), ('platform_adjust', 'platform_issued', false);

-- ---- H2: one way in ----
-- The hash of a posting covers the previous posting's hash on the account,
-- the posting and its journal; changing any of them later breaks the chain.
-- +goose StatementBegin
CREATE FUNCTION wallet_posting_hash(prev BYTEA, seq BIGINT, journal_id BIGINT, account_id BIGINT, amount BIGINT,
    after BIGINT, j wallet_journals) RETURNS BYTEA LANGUAGE sql STABLE AS $$
    SELECT sha256(convert_to(concat_ws('|', COALESCE(encode(prev, 'hex'), ''), seq, journal_id, account_id, amount, after,
        j.kind, j.idem_key, COALESCE(j.branch_id, 0), j.currency, COALESCE(j.order_id, 0), COALESCE(j.refund_id, 0),
        COALESCE(j.topup_id, 0), COALESCE(j.cashout_id, 0), COALESCE(j.reverses, 0), j.actor_type,
        COALESCE(j.actor_id, 0), COALESCE(j.approved_by, 0), j.note), 'UTF8'))
$$;
-- +goose StatementEnd

-- A posting moves its account's balance, numbers itself on the account and
-- extends the account's hash chain. Its account must be of the journal's
-- store and currency, and of a kind the journal's kind may post to.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION wallet_post() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    j wallet_journals;
    a wallet_accounts;
BEGIN
    SELECT * INTO j FROM wallet_journals WHERE id = NEW.journal_id;
    SELECT * INTO a FROM wallet_accounts WHERE id = NEW.account_id FOR UPDATE;
    IF j.currency IS DISTINCT FROM a.currency THEN
        RAISE EXCEPTION 'wallet posting currency % does not match its journal %', a.currency, j.currency;
    END IF;
    IF j.branch_id IS DISTINCT FROM a.branch_id THEN
        RAISE EXCEPTION 'wallet posting on account % of another store than journal %', a.id, j.id;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM wallet_journal_rules r WHERE r.journal_kind = j.kind AND r.account_kind = a.kind AND NOT r.legacy) THEN
        RAISE EXCEPTION 'a % journal may not post to a % account', j.kind, a.kind;
    END IF;
    NEW.balance_after := a.balance + NEW.amount;
    NEW.seq := a.posting_seq + 1;
    NEW.hash := wallet_posting_hash(a.last_hash, NEW.seq, NEW.journal_id, NEW.account_id, NEW.amount, NEW.balance_after, j);
    UPDATE wallet_accounts SET balance = NEW.balance_after, posting_seq = NEW.seq, last_hash = NEW.hash, updated_at = now()
        WHERE id = NEW.account_id;
    RETURN NEW;
END $$;
-- +goose StatementEnd

-- Only the posting trigger moves a balance: it runs one trigger level down.
-- A session setting can no longer open the door (it could before: H2).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION wallet_balance_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.balance <> OLD.balance OR NEW.posting_seq <> OLD.posting_seq OR NEW.last_hash IS DISTINCT FROM OLD.last_hash)
        AND pg_trigger_depth() < 2 THEN
        RAISE EXCEPTION 'a wallet balance only changes by a posting';
    END IF;
    IF NEW.kind <> OLD.kind OR NEW.branch_id IS DISTINCT FROM OLD.branch_id OR NEW.currency <> OLD.currency
        OR NEW.customer_id IS DISTINCT FROM OLD.customer_id THEN
        RAISE EXCEPTION 'a wallet account keeps its owner and currency';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd

-- A new account starts empty.
-- +goose StatementBegin
CREATE FUNCTION wallet_account_new() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.balance := 0;
    NEW.posting_seq := 0;
    NEW.last_hash := NULL;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER wallet_account_new BEFORE INSERT ON wallet_accounts
    FOR EACH ROW EXECUTE FUNCTION wallet_account_new();

-- The existing postings get their numbers and hashes, in the order they were
-- written (the triggers that keep them unchanged step aside for this once).
ALTER TABLE wallet_postings DISABLE TRIGGER wallet_postings_frozen;
ALTER TABLE wallet_accounts DISABLE TRIGGER wallet_balance_guard;
-- +goose StatementBegin
DO $$
DECLARE
    p RECORD;
    j wallet_journals;
    cur BIGINT := 0;
    n BIGINT := 0;
    prev BYTEA;
BEGIN
    FOR p IN SELECT * FROM wallet_postings ORDER BY account_id, id LOOP
        IF p.account_id <> cur THEN
            IF cur <> 0 THEN
                UPDATE wallet_accounts SET posting_seq = n, last_hash = prev WHERE id = cur;
            END IF;
            cur := p.account_id;
            n := 0;
            prev := NULL;
        END IF;
        n := n + 1;
        SELECT * INTO j FROM wallet_journals WHERE id = p.journal_id;
        prev := wallet_posting_hash(prev, n, p.journal_id, p.account_id, p.amount, p.balance_after, j);
        UPDATE wallet_postings SET seq = n, hash = prev WHERE id = p.id;
    END LOOP;
    IF cur <> 0 THEN
        UPDATE wallet_accounts SET posting_seq = n, last_hash = prev WHERE id = cur;
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE wallet_accounts ENABLE TRIGGER wallet_balance_guard;
ALTER TABLE wallet_postings ENABLE TRIGGER wallet_postings_frozen;
ALTER TABLE wallet_postings ALTER COLUMN seq SET NOT NULL, ALTER COLUMN hash SET NOT NULL;
CREATE UNIQUE INDEX wallet_postings_seq ON wallet_postings (account_id, seq);

-- The account of a kind at a store (or the platform) for a customer, opened
-- when missing.
-- +goose StatementBegin
CREATE FUNCTION wallet_account_id(p_kind TEXT, p_branch BIGINT, p_customer BIGINT, p_currency TEXT) RETURNS BIGINT
LANGUAGE plpgsql AS $$
DECLARE
    acct BIGINT;
BEGIN
    INSERT INTO wallet_accounts (kind, branch_id, customer_id, currency) VALUES (p_kind, p_branch, p_customer, p_currency)
        ON CONFLICT DO NOTHING;
    SELECT id INTO acct FROM wallet_accounts WHERE kind = p_kind AND COALESCE(branch_id, 0) = COALESCE(p_branch, 0)
        AND COALESCE(customer_id, 0) = COALESCE(p_customer, 0) AND currency = p_currency;
    RETURN acct;
END $$;
-- +goose StatementEnd

-- The only way to write to the books: one journal and its postings, which
-- must balance. An idem key already written writes nothing (o_fresh false).
-- It runs as the owner of the books, so that the application's own database
-- role (waimai_app, below) needs no right to write them.
-- +goose StatementBegin
CREATE FUNCTION wallet_post_journal(p_kind TEXT, p_idem TEXT, p_branch BIGINT, p_currency TEXT, p_order BIGINT,
    p_refund BIGINT, p_topup BIGINT, p_cashout BIGINT, p_actor_type TEXT, p_actor_id BIGINT, p_approved BIGINT,
    p_note TEXT, p_reverses BIGINT, p_accounts BIGINT[], p_amounts BIGINT[], p_at TIMESTAMPTZ DEFAULT NULL,
    OUT o_id BIGINT, OUT o_fresh BOOLEAN)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE
    s BIGINT := 0;
    n INT := 0;
    i INT;
BEGIN
    IF COALESCE(array_length(p_accounts, 1), 0) <> COALESCE(array_length(p_amounts, 1), 0) THEN
        RAISE EXCEPTION 'wallet journal %: accounts and amounts differ in number', p_idem;
    END IF;
    FOR i IN 1 .. COALESCE(array_length(p_amounts, 1), 0) LOOP
        IF p_amounts[i] <> 0 THEN
            s := s + p_amounts[i];
            n := n + 1;
        END IF;
    END LOOP;
    IF n < 2 OR s <> 0 THEN
        RAISE EXCEPTION 'wallet journal % does not balance', p_idem;
    END IF;
    INSERT INTO wallet_journals (kind, idem_key, branch_id, currency, order_id, refund_id, topup_id, cashout_id,
        actor_type, actor_id, approved_by, note, reverses, created_at)
        VALUES (p_kind, p_idem, p_branch, p_currency, p_order, p_refund, p_topup, p_cashout, COALESCE(p_actor_type, ''),
            p_actor_id, p_approved, left(COALESCE(p_note, ''), 200), p_reverses, COALESCE(p_at, now()))
        ON CONFLICT (idem_key) DO NOTHING RETURNING id INTO o_id;
    IF o_id IS NULL THEN
        SELECT id INTO o_id FROM wallet_journals WHERE idem_key = p_idem;
        o_fresh := false;
        RETURN;
    END IF;
    o_fresh := true;
    INSERT INTO wallet_postings (journal_id, account_id, amount)
        SELECT o_id, t.a, t.m FROM unnest(p_accounts, p_amounts) AS t(a, m) WHERE t.m <> 0 ORDER BY t.a;
END $$;
-- +goose StatementEnd

-- Where the chain or the numbering of an account's postings is broken, from
-- posting p_since on (0: all). Each posting is checked against the one
-- before it as stored, so that one break is reported once.
-- +goose StatementBegin
CREATE FUNCTION wallet_chain_breaks(p_since BIGINT) RETURNS TABLE (b_account BIGINT, b_seq BIGINT, b_problem TEXT)
LANGUAGE plpgsql STABLE AS $$
DECLARE
    p RECORD;
    q RECORD;
    a RECORD;
    j wallet_journals;
    prev BYTEA;
    prevbal BIGINT;
BEGIN
    FOR p IN SELECT * FROM wallet_postings WHERE id > p_since ORDER BY account_id, seq LOOP
        IF p.seq = 1 THEN
            prev := NULL;
            prevbal := 0;
        ELSE
            SELECT hash, balance_after INTO q FROM wallet_postings WHERE account_id = p.account_id AND seq = p.seq - 1;
            IF NOT FOUND THEN
                b_account := p.account_id; b_seq := p.seq; b_problem := 'posting before it is missing';
                RETURN NEXT;
                CONTINUE;
            END IF;
            prev := q.hash;
            prevbal := q.balance_after;
        END IF;
        IF p.balance_after <> prevbal + p.amount THEN
            b_account := p.account_id; b_seq := p.seq; b_problem := 'balance after does not follow';
            RETURN NEXT;
        END IF;
        SELECT * INTO j FROM wallet_journals WHERE id = p.journal_id;
        IF p.hash IS DISTINCT FROM wallet_posting_hash(prev, p.seq, p.journal_id, p.account_id, p.amount, p.balance_after, j) THEN
            b_account := p.account_id; b_seq := p.seq; b_problem := 'hash does not match';
            RETURN NEXT;
        END IF;
    END LOOP;
    FOR a IN SELECT w.id, w.balance, w.posting_seq, w.last_hash,
            (SELECT count(*) FROM wallet_postings x WHERE x.account_id = w.id) AS n,
            (SELECT x.hash FROM wallet_postings x WHERE x.account_id = w.id ORDER BY x.seq DESC LIMIT 1) AS h,
            (SELECT x.balance_after FROM wallet_postings x WHERE x.account_id = w.id ORDER BY x.seq DESC LIMIT 1) AS bal
        FROM wallet_accounts w
        WHERE p_since = 0 OR EXISTS (SELECT 1 FROM wallet_postings x WHERE x.account_id = w.id AND x.id > p_since) LOOP
        IF a.posting_seq <> a.n OR a.last_hash IS DISTINCT FROM a.h OR a.balance <> COALESCE(a.bal, 0) THEN
            b_account := a.id; b_seq := a.posting_seq; b_problem := 'account head does not match its last posting';
            RETURN NEXT;
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd

-- ---- H3: the platform's balance and rewards move into the books ----
-- Every row of customer_wallet_entries becomes a journal, at the time it was
-- written; the old table is kept as it is and closed.
-- +goose StatementBegin
DO $$
DECLARE
    e RECORD;
BEGIN
    FOR e IN SELECT * FROM customer_wallet_entries ORDER BY id LOOP
        PERFORM wallet_post_journal('platform_adjust', 'legacy_wallet:' || e.id, NULL, e.currency, NULL, NULL, NULL, NULL,
            CASE WHEN e.admin_id IS NULL THEN 'system' ELSE 'admin' END, e.admin_id, NULL, e.note, NULL,
            ARRAY[wallet_account_id('platform_' || e.account, NULL, e.customer_id, e.currency),
                  wallet_account_id('platform_issued', NULL, NULL, e.currency)],
            ARRAY[e.amount, -e.amount], e.created_at);
    END LOOP;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION customer_wallet_entries_closed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'customer_wallet_entries is closed: the platform wallet is in the wallet books (00085)';
END $$;
-- +goose StatementEnd
CREATE TRIGGER customer_wallet_entries_closed BEFORE INSERT OR UPDATE OR DELETE ON customer_wallet_entries
    FOR EACH ROW EXECUTE FUNCTION customer_wallet_entries_closed();
ALTER TABLE customer_wallet_entries DROP CONSTRAINT customer_wallet_entries_customer_id_fkey,
    ADD CONSTRAINT customer_wallet_entries_customer_id_fkey FOREIGN KEY (customer_id) REFERENCES customers(id);

-- ---- H1, M2: the store's side, as it should have been ----
-- A payback written before 00085 took everything to store_topup and
-- store_bonus; one reclassification per payback moves it to what was paid
-- out, what was kept back and what was made void. Customers' balances do not
-- move.
-- +goose StatementBegin
DO $$
DECLARE
    c RECORD;
BEGIN
    FOR c IN SELECT * FROM stored_cashouts ORDER BY id LOOP
        PERFORM wallet_post_journal('reclass', 'reclass_cashout:' || c.id, c.branch_id, c.currency, NULL, NULL, NULL, c.id,
            'system', NULL, NULL, 'payback split into paid out, kept back and void (00085)', NULL,
            ARRAY[wallet_account_id('store_topup', c.branch_id, NULL, c.currency),
                  wallet_account_id('store_cashout', c.branch_id, NULL, c.currency),
                  wallet_account_id('store_clawback', c.branch_id, NULL, c.currency),
                  wallet_account_id('store_bonus', c.branch_id, NULL, c.currency),
                  wallet_account_id('store_bonus_void', c.branch_id, NULL, c.currency)],
            ARRAY[-c.principal, c.paid_out, c.clawback, -c.bonus, c.bonus]);
    END LOOP;
END $$;
-- +goose StatementEnd

-- A refund's share that the order had paid from the refund balance went
-- back against store_refund; it is a reversal of store_sales, as stored
-- value's share always was.
-- +goose StatementBegin
DO $$
DECLARE
    rr RECORD;
BEGIN
    FOR rr IN SELECT x.id, x.order_id, j.branch_id, j.currency,
            x.amount - x.cash_part - x.principal_part - x.bonus_part AS share
        FROM order_refunds x JOIN wallet_journals j ON j.idem_key = 'refund:' || x.id
        WHERE x.amount - x.cash_part - x.principal_part - x.bonus_part > 0 ORDER BY x.id LOOP
        PERFORM wallet_post_journal('reclass', 'reclass_refund:' || rr.id, rr.branch_id, rr.currency, rr.order_id, rr.id,
            NULL, NULL, 'system', NULL, NULL, 'balance share of a refund reverses balance sales (00085)', NULL,
            ARRAY[wallet_account_id('store_refund', rr.branch_id, NULL, rr.currency),
                  wallet_account_id('store_sales', rr.branch_id, NULL, rr.currency)],
            ARRAY[rr.share, -rr.share]);
    END LOOP;
END $$;
-- +goose StatementEnd

-- ---- M6: coupons that came with a top-up name it ----
ALTER TABLE customer_coupons ADD COLUMN topup_id BIGINT REFERENCES topups(id);
UPDATE customer_coupons c SET topup_id = t.id FROM topups t
    WHERE c.source = 'topup' AND c.topup_id IS NULL AND t.status IN ('paid', 'revoked') AND c.customer_id = t.customer_id
    AND c.branch_id = t.branch_id AND c.issued_at BETWEEN t.paid_at - interval '1 second' AND t.paid_at + interval '1 second';
CREATE INDEX customer_coupons_topup ON customer_coupons (topup_id) WHERE topup_id IS NOT NULL;

-- ---- L1: a refund's record keeps what it was ----
-- Changing a refund into a store balance into cash afterwards (revoke) no
-- longer loses what went in: revoked_credit grows by what to_credit loses.
ALTER TABLE order_refunds ADD COLUMN revoked_credit BIGINT NOT NULL DEFAULT 0 CHECK (revoked_credit >= 0),
    ADD COLUMN revoked_at TIMESTAMPTZ, ADD COLUMN revoked_by BIGINT REFERENCES users(id);
UPDATE order_refunds r SET revoked_credit = x.back, revoked_at = x.at FROM (
    SELECT j.refund_id, -sum(p.amount) AS back, max(j.created_at) AS at FROM wallet_journals j
    JOIN wallet_postings p ON p.journal_id = j.id JOIN wallet_accounts w ON w.id = p.account_id
    WHERE j.kind = 'revoke' AND w.kind = 'store_credit' GROUP BY j.refund_id) x WHERE x.refund_id = r.id;
-- +goose StatementBegin
CREATE FUNCTION order_refunds_kept() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.amount <> OLD.amount OR NEW.cash_part <> OLD.cash_part OR NEW.principal_part <> OLD.principal_part
        OR NEW.bonus_part <> OLD.bonus_part OR NEW.order_id <> OLD.order_id OR NEW.branch_id <> OLD.branch_id
        OR NEW.lines::text <> OLD.lines::text THEN
        RAISE EXCEPTION 'a refund keeps its amounts';
    END IF;
    IF NEW.to_credit + NEW.revoked_credit <> OLD.to_credit + OLD.revoked_credit OR NEW.revoked_credit < OLD.revoked_credit THEN
        RAISE EXCEPTION 'a refund into a balance only changes by a revoke, which is kept';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER order_refunds_kept BEFORE UPDATE ON order_refunds FOR EACH ROW EXECUTE FUNCTION order_refunds_kept();
CREATE TRIGGER order_refunds_not_deleted BEFORE DELETE ON order_refunds FOR EACH ROW EXECUTE FUNCTION wallet_frozen_rows();

-- ---- M5: the books at the end of each day, and what each check found ----
-- One row per account and day (the store's day, in its time zone; UTC for
-- the platform's accounts): opening + credits - debits = closing.
CREATE TABLE wallet_daily_balances (
    account_id BIGINT NOT NULL REFERENCES wallet_accounts(id),
    day        DATE NOT NULL,
    opening    BIGINT NOT NULL,
    credits    BIGINT NOT NULL CHECK (credits >= 0),
    debits     BIGINT NOT NULL CHECK (debits >= 0),
    closing    BIGINT NOT NULL,
    postings   INT NOT NULL CHECK (postings >= 0),
    PRIMARY KEY (account_id, day),
    CHECK (closing = opening + credits - debits)
);
CREATE INDEX wallet_daily_balances_day ON wallet_daily_balances (day);
-- The last day closed for each store (0 = the platform).
CREATE TABLE wallet_days_closed (
    scope_id   BIGINT PRIMARY KEY,
    day        DATE NOT NULL,
    closed_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER wallet_daily_balances_kept BEFORE UPDATE OR DELETE ON wallet_daily_balances
    FOR EACH ROW EXECUTE FUNCTION wallet_frozen_rows();

-- An hourly check looks at what was written since the last one; once a day
-- the whole books. digest fingerprints every account's chain head.
ALTER TABLE wallet_check_runs ADD COLUMN full_check BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN last_journal_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN last_posting_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN digest TEXT NOT NULL DEFAULT '',
    ADD COLUMN warnings JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN alerted_at TIMESTAMPTZ;

-- ---- H2: the application's own database role ----
-- waimai_app may read everything and change everything else, but writes the
-- books only through wallet_post_journal. It is created here when this role
-- may create roles; the operator gives it a password (APP_DB_PASSWORD, see
-- docs/DEPLOY.md) and points DATABASE_URL at it, keeping the owner's URL for
-- migrations only.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'waimai_app')
        AND (SELECT rolsuper OR rolcreaterole FROM pg_roles WHERE rolname = current_user) THEN
        CREATE ROLE waimai_app NOLOGIN;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'waimai_app') THEN
        EXECUTE format('GRANT CONNECT ON DATABASE %I TO waimai_app', current_database());
        GRANT USAGE ON SCHEMA public TO waimai_app;
        GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO waimai_app;
        GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO waimai_app;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO waimai_app;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO waimai_app;
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON wallet_journals, wallet_postings, wallet_journal_rules,
            customer_wallet_entries, wallet_daily_balances FROM waimai_app;
        REVOKE UPDATE, DELETE, TRUNCATE ON wallet_accounts FROM waimai_app;
        GRANT UPDATE (frozen, frozen_note, updated_at) ON wallet_accounts TO waimai_app;
        REVOKE ALL ON FUNCTION wallet_post_journal(TEXT, TEXT, BIGINT, TEXT, BIGINT, BIGINT, BIGINT, BIGINT, TEXT, BIGINT,
            BIGINT, TEXT, BIGINT, BIGINT[], BIGINT[], TIMESTAMPTZ) FROM PUBLIC;
        GRANT EXECUTE ON FUNCTION wallet_post_journal(TEXT, TEXT, BIGINT, TEXT, BIGINT, BIGINT, BIGINT, BIGINT, TEXT, BIGINT,
            BIGINT, TEXT, BIGINT, BIGINT[], BIGINT[], TIMESTAMPTZ) TO waimai_app;
    END IF;
END $$;
-- +goose StatementEnd
-- The daily balances are written by the books' owner only.
-- +goose StatementBegin
CREATE FUNCTION wallet_close_day(p_scope BIGINT, p_day DATE) RETURNS INT
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE
    tz TEXT;
    n INT;
BEGIN
    IF p_scope = 0 THEN
        tz := 'UTC';
    ELSE
        SELECT timezone INTO tz FROM branches WHERE id = p_scope;
    END IF;
    IF EXISTS (SELECT 1 FROM wallet_days_closed WHERE scope_id = p_scope AND day >= p_day) THEN
        RETURN 0;
    END IF;
    INSERT INTO wallet_daily_balances (account_id, day, opening, credits, debits, closing, postings)
    SELECT w.id, p_day, COALESCE(prev.closing, 0), COALESCE(mv.credits, 0), COALESCE(mv.debits, 0),
        COALESCE(prev.closing, 0) + COALESCE(mv.credits, 0) - COALESCE(mv.debits, 0), COALESCE(mv.n, 0)
    FROM wallet_accounts w
    LEFT JOIN LATERAL (SELECT d.closing FROM wallet_daily_balances d WHERE d.account_id = w.id AND d.day < p_day
        ORDER BY d.day DESC LIMIT 1) prev ON true
    LEFT JOIN LATERAL (SELECT sum(p.amount) FILTER (WHERE p.amount > 0) AS credits,
            -sum(p.amount) FILTER (WHERE p.amount < 0) AS debits, count(*) AS n
        FROM wallet_postings p JOIN wallet_journals j ON j.id = p.journal_id
        WHERE p.account_id = w.id AND (j.created_at AT TIME ZONE tz)::date = p_day) mv ON true
    WHERE COALESCE(w.branch_id, 0) = p_scope AND (mv.n > 0 OR prev.closing IS NOT NULL);
    GET DIAGNOSTICS n = ROW_COUNT;
    INSERT INTO wallet_days_closed (scope_id, day) VALUES (p_scope, p_day)
        ON CONFLICT (scope_id) DO UPDATE SET day = EXCLUDED.day, closed_at = now();
    RETURN n;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'waimai_app') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON wallet_days_closed FROM waimai_app;
        REVOKE ALL ON FUNCTION wallet_close_day(BIGINT, DATE) FROM PUBLIC;
        GRANT EXECUTE ON FUNCTION wallet_close_day(BIGINT, DATE) TO waimai_app;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- The books only move forward: journals written under these rules (the
-- platform wallet, the reclassifications, every hash) cannot be taken back.
-- Restore the backup taken before the upgrade instead (docs/DEPLOY.md).
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '00085 cannot be undone: restore the backup taken before upgrading';
END $$;
-- +goose StatementEnd
