-- +goose Up
-- Stored value: "top up X, get Y free" (docs/STORED_VALUE_DESIGN.md). A
-- customer pays the store (cash or a scan at the counter, the staff confirm)
-- and gets the money as stored principal plus a bonus from the store. Both
-- live in the double-entry books of store credit (00083): two more customer
-- accounts and three more store-side accounts.
ALTER TABLE wallet_accounts DROP CONSTRAINT wallet_accounts_kind_check,
    ADD CONSTRAINT wallet_accounts_kind_check CHECK (kind IN ('store_credit', 'store_refund', 'store_sales',
        'stored_principal', 'stored_bonus', 'store_topup', 'store_bonus', 'store_stored_sales'));
ALTER TABLE wallet_accounts DROP CONSTRAINT wallet_accounts_check,
    ADD CONSTRAINT wallet_accounts_customer CHECK ((kind IN ('store_credit', 'stored_principal', 'stored_bonus')) = (customer_id IS NOT NULL));
ALTER TABLE wallet_accounts DROP CONSTRAINT wallet_accounts_check1,
    ADD CONSTRAINT wallet_accounts_not_negative CHECK (kind NOT IN ('store_credit', 'stored_principal', 'stored_bonus') OR balance >= 0);

ALTER TABLE wallet_journals DROP CONSTRAINT wallet_journals_kind_check,
    ADD CONSTRAINT wallet_journals_kind_check CHECK (kind IN ('refund', 'spend', 'return', 'revoke',
        'topup', 'topup_revoke', 'stored_spend', 'stored_return', 'stored_refund', 'stored_cashout'));

-- The platform turns stored value on per country and caps what one customer
-- may hold at one store (§9, Q9). cap is in the country's currency, minor units.
CREATE TABLE stored_countries (
    country_code TEXT PRIMARY KEY CHECK (country_code ~ '^[A-Z]{2}$'),
    enabled      BOOLEAN NOT NULL DEFAULT false,
    cap          BIGINT NOT NULL DEFAULT 0 CHECK (cap >= 0),
    note         TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO stored_countries (country_code, enabled, cap, note) VALUES
    ('PH', true, 5000000, 'RA 10962: stored value may not expire'),
    ('SG', true, 150000, 'limited purpose e-money'),
    ('MY', true, 300000, 'check the 2024 exemption order before going live');

-- A campaign: the tiers on offer for a while (§4.3). At most one runs at a
-- time per store (checked by the app, which knows the dates).
CREATE TABLE stored_campaigns (
    id           BIGSERIAL PRIMARY KEY,
    branch_id    BIGINT NOT NULL REFERENCES branches(id),
    name         TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
    names        JSONB NOT NULL DEFAULT '{}',
    starts_at    TIMESTAMPTZ,
    ends_at      TIMESTAMPTZ,
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
    -- any amount from custom_min up, with no bonus
    custom_ok    BOOLEAN NOT NULL DEFAULT false,
    custom_min   BIGINT NOT NULL DEFAULT 0 CHECK (custom_min >= 0),
    -- the brands whose pages show it (empty = all); the balance is the store's
    brand_ids    BIGINT[] NOT NULL DEFAULT '{}',
    note         TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);
CREATE INDEX stored_campaigns_branch ON stored_campaigns (branch_id, id DESC);

CREATE TABLE stored_tiers (
    id            BIGSERIAL PRIMARY KEY,
    campaign_id   BIGINT NOT NULL REFERENCES stored_campaigns(id),
    pay           BIGINT NOT NULL CHECK (pay > 0),
    bonus         BIGINT NOT NULL DEFAULT 0 CHECK (bonus >= 0),
    -- a coupon given with the tier (Q1): kind, amount, threshold, days, how many
    coupon_kind   TEXT NOT NULL DEFAULT '' CHECK (coupon_kind IN ('', 'store_free', 'store_threshold')),
    coupon_value  BIGINT NOT NULL DEFAULT 0 CHECK (coupon_value >= 0),
    coupon_min    BIGINT NOT NULL DEFAULT 0 CHECK (coupon_min >= 0),
    coupon_days   INT NOT NULL DEFAULT 30 CHECK (coupon_days BETWEEN 1 AND 365),
    coupon_qty    INT NOT NULL DEFAULT 0 CHECK (coupon_qty BETWEEN 0 AND 10),
    label         TEXT NOT NULL DEFAULT '' CHECK (length(label) <= 20),
    per_customer  INT CHECK (per_customer > 0),
    first_only    BOOLEAN NOT NULL DEFAULT false,
    quota         INT CHECK (quota > 0),
    sort          INT NOT NULL DEFAULT 0,
    -- a tier taken off a campaign stays for the top-ups that used it
    removed       BOOLEAN NOT NULL DEFAULT false,
    CHECK ((coupon_kind = '') = (coupon_qty = 0))
);
CREATE INDEX stored_tiers_campaign ON stored_tiers (campaign_id, sort, id);

-- A top-up: asked for by the customer (a code they show at the counter) or
-- by the staff (the customer's texted code), paid when the staff confirm
-- they took the money. It keeps a copy of the tier.
CREATE TABLE topups (
    id           BIGSERIAL PRIMARY KEY,
    branch_id    BIGINT NOT NULL REFERENCES branches(id),
    brand_id     BIGINT REFERENCES brands(id),
    customer_id  BIGINT NOT NULL REFERENCES customers(id),
    campaign_id  BIGINT REFERENCES stored_campaigns(id),
    tier_id      BIGINT REFERENCES stored_tiers(id),
    currency     TEXT NOT NULL CHECK (length(currency) = 3),
    pay          BIGINT NOT NULL CHECK (pay > 0),
    bonus        BIGINT NOT NULL DEFAULT 0 CHECK (bonus >= 0),
    coupon_kind  TEXT NOT NULL DEFAULT '',
    coupon_value BIGINT NOT NULL DEFAULT 0,
    coupon_min   BIGINT NOT NULL DEFAULT 0,
    coupon_days  INT NOT NULL DEFAULT 30,
    coupon_qty   INT NOT NULL DEFAULT 0,
    source       TEXT NOT NULL CHECK (source IN ('customer', 'staff')),
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'cancelled', 'revoked')),
    code         TEXT NOT NULL CHECK (code ~ '^[0-9]{6}$'),
    token        TEXT NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    pay_type     TEXT NOT NULL DEFAULT '' CHECK (pay_type IN ('', 'cash', 'card', 'online')),
    pay_channel_id BIGINT REFERENCES pay_channels(id),
    pay_method   TEXT NOT NULL DEFAULT '',
    requested_by BIGINT REFERENCES users(id),
    received_by  BIGINT REFERENCES users(id),
    approved_by  BIGINT REFERENCES users(id),
    paid_at      TIMESTAMPTZ,
    revoked_by   BIGINT REFERENCES users(id),
    revoked_at   TIMESTAMPTZ,
    revoke_note  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((status IN ('paid', 'revoked')) = (paid_at IS NOT NULL AND pay_type <> ''))
);
-- a pending code is unique at its store
CREATE UNIQUE INDEX topups_pending_code ON topups (branch_id, code) WHERE status = 'pending';
CREATE INDEX topups_branch ON topups (branch_id, created_at DESC);
CREATE INDEX topups_customer ON topups (customer_id, branch_id, created_at DESC);
CREATE INDEX topups_tier ON topups (tier_id) WHERE status = 'paid';

-- A customer's stored value paid back in money (§8.3).
CREATE TABLE stored_cashouts (
    id          BIGSERIAL PRIMARY KEY,
    branch_id   BIGINT NOT NULL REFERENCES branches(id),
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    currency    TEXT NOT NULL CHECK (length(currency) = 3),
    reason      TEXT NOT NULL CHECK (reason IN ('customer', 'store')),
    principal   BIGINT NOT NULL CHECK (principal >= 0), -- what was left
    bonus       BIGINT NOT NULL CHECK (bonus >= 0),     -- what was left, void
    clawback    BIGINT NOT NULL CHECK (clawback >= 0),  -- bonus already used, kept back
    paid_out    BIGINT NOT NULL CHECK (paid_out >= 0),
    pay_type    TEXT NOT NULL CHECK (pay_type IN ('cash', 'transfer', 'online', 'other')),
    consent     TEXT NOT NULL DEFAULT '' CHECK (consent IN ('', 'in_person', 'phone', 'chat')),
    note        TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    done_by     BIGINT REFERENCES users(id),
    approved_by BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (paid_out + clawback = principal)
);
CREATE INDEX stored_cashouts_branch ON stored_cashouts (branch_id, created_at DESC);

ALTER TABLE wallet_journals ADD COLUMN topup_id BIGINT REFERENCES topups(id),
    ADD COLUMN cashout_id BIGINT REFERENCES stored_cashouts(id);

-- The part of credit_paid that came from stored value (credit_paid is
-- everything paid from balances at the store, so that what is still to
-- collect stays total - credit_paid everywhere).
ALTER TABLE orders ADD COLUMN stored_principal_paid BIGINT NOT NULL DEFAULT 0 CHECK (stored_principal_paid >= 0),
    ADD COLUMN stored_bonus_paid BIGINT NOT NULL DEFAULT 0 CHECK (stored_bonus_paid >= 0),
    ADD CONSTRAINT orders_stored_within_credit CHECK (stored_principal_paid + stored_bonus_paid <= credit_paid);

-- A refund's share of stored value goes back to it, principal and bonus
-- apart (§8.2); they are part of to_credit.
ALTER TABLE order_refunds ADD COLUMN principal_part BIGINT NOT NULL DEFAULT 0 CHECK (principal_part >= 0),
    ADD COLUMN bonus_part BIGINT NOT NULL DEFAULT 0 CHECK (bonus_part >= 0),
    ADD CONSTRAINT order_refunds_stored_parts CHECK (principal_part + bonus_part <= amount - cash_part);

-- The customer's texted code for a top-up the staff started.
ALTER TABLE wallet_verifications ALTER COLUMN order_id DROP NOT NULL,
    ADD COLUMN topup_id BIGINT REFERENCES topups(id),
    DROP CONSTRAINT wallet_verifications_purpose_check,
    ADD CONSTRAINT wallet_verifications_purpose_check CHECK (purpose IN ('spend', 'refund', 'topup')),
    ADD CONSTRAINT wallet_verifications_subject CHECK ((order_id IS NULL) <> (topup_id IS NULL));
CREATE INDEX wallet_verifications_topup ON wallet_verifications (topup_id, created_at DESC) WHERE topup_id IS NOT NULL;

-- A top-up above this needs a manager (§6).
ALTER TABLE branches ADD COLUMN topup_approve_over BIGINT NOT NULL DEFAULT 500000 CHECK (topup_approve_over >= 0),
    ADD COLUMN stored_terms_at TIMESTAMPTZ;

-- Coupons can come with a top-up.
ALTER TABLE coupon_batches DROP CONSTRAINT coupon_batches_source_check,
    ADD CONSTRAINT coupon_batches_source_check CHECK (source IN ('repurchase', 'claim', 'new_customer', 'manual', 'topup'));

-- Who may take top-ups (everyone at the counter) and pay stored value back
-- or undo a top-up (managers), by default.
UPDATE branch_role_perms SET perms = perms || ARRAY['stored.topup']
    WHERE role IN ('manager', 'cashier') AND NOT 'stored.topup' = ANY(perms);
UPDATE branch_role_perms SET perms = perms || ARRAY['stored.cashout']
    WHERE role = 'manager' AND NOT 'stored.cashout' = ANY(perms);

-- +goose Down
UPDATE branch_role_perms SET perms = array_remove(array_remove(perms, 'stored.topup'), 'stored.cashout');
ALTER TABLE coupon_batches DROP CONSTRAINT coupon_batches_source_check,
    ADD CONSTRAINT coupon_batches_source_check CHECK (source IN ('repurchase', 'claim', 'new_customer', 'manual'));
ALTER TABLE branches DROP COLUMN topup_approve_over, DROP COLUMN stored_terms_at;
DELETE FROM wallet_verifications WHERE topup_id IS NOT NULL;
ALTER TABLE wallet_verifications DROP CONSTRAINT wallet_verifications_subject,
    DROP CONSTRAINT wallet_verifications_purpose_check,
    ADD CONSTRAINT wallet_verifications_purpose_check CHECK (purpose IN ('spend', 'refund')),
    DROP COLUMN topup_id, ALTER COLUMN order_id SET NOT NULL;
ALTER TABLE order_refunds DROP CONSTRAINT order_refunds_stored_parts, DROP COLUMN principal_part, DROP COLUMN bonus_part;
ALTER TABLE orders DROP CONSTRAINT orders_stored_within_credit, DROP COLUMN stored_principal_paid, DROP COLUMN stored_bonus_paid;
ALTER TABLE wallet_journals DROP COLUMN topup_id, DROP COLUMN cashout_id;
DROP TABLE stored_cashouts;
DROP TABLE topups;
DROP TABLE stored_tiers;
DROP TABLE stored_campaigns;
DROP TABLE stored_countries;
ALTER TABLE wallet_journals DROP CONSTRAINT wallet_journals_kind_check,
    ADD CONSTRAINT wallet_journals_kind_check CHECK (kind IN ('refund', 'spend', 'return', 'revoke'));
ALTER TABLE wallet_accounts DROP CONSTRAINT wallet_accounts_not_negative,
    ADD CONSTRAINT wallet_accounts_check1 CHECK (kind <> 'store_credit' OR balance >= 0);
ALTER TABLE wallet_accounts DROP CONSTRAINT wallet_accounts_customer,
    ADD CONSTRAINT wallet_accounts_check CHECK ((kind = 'store_credit') = (customer_id IS NOT NULL));
ALTER TABLE wallet_accounts DROP CONSTRAINT wallet_accounts_kind_check,
    ADD CONSTRAINT wallet_accounts_kind_check CHECK (kind IN ('store_credit', 'store_refund', 'store_sales'));
