-- +goose Up
-- Payment types, online payment methods and discounts at checkout
-- (docs/CHECKOUT_DISCOUNT_DESIGN.md §5, §6, §9).

-- The platform's online payment methods, by country. "Online" is only a kind
-- of payment the cashier records: nothing is charged through them.
CREATE TABLE pay_channels (
    id         BIGSERIAL PRIMARY KEY,
    country    TEXT NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
    names      JSONB NOT NULL DEFAULT '{}',   -- language code -> name, empty = name everywhere
    icon_url   TEXT NOT NULL DEFAULT '',
    sort       INT NOT NULL DEFAULT 0,
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (country, name)
);
CREATE INDEX pay_channels_country ON pay_channels (country, sort, id);

INSERT INTO pay_channels (country, name, sort) VALUES
    ('PH','GCash',1), ('PH','Maya',2),
    ('TH','PromptPay',1), ('TH','TrueMoney',2),
    ('VN','MoMo',1), ('VN','ZaloPay',2),
    ('MY','Touch ''n Go',1), ('MY','GrabPay',2),
    ('SG','PayNow',1), ('SG','GrabPay',2),
    ('ID','GoPay',1), ('ID','OVO',2),
    ('MM','KBZPay',1), ('MM','WavePay',2),
    ('KH','ABA',1), ('KH','Wing',2),
    ('LA','BCEL One',1),
    ('CN','微信支付',1), ('CN','支付宝',2),
    ('HK','八达通',1), ('HK','PayMe',2),
    ('TW','LINE Pay',1), ('TW','街口支付',2),
    ('JP','PayPay',1);
UPDATE pay_channels SET names = '{"en":"WeChat Pay","zh-Hant":"微信支付"}' WHERE country='CN' AND name='微信支付';
UPDATE pay_channels SET names = '{"en":"Alipay","zh-Hant":"支付寶"}' WHERE country='CN' AND name='支付宝';
UPDATE pay_channels SET names = '{"en":"Octopus","zh-Hant":"八達通"}' WHERE country='HK' AND name='八达通';
UPDATE pay_channels SET names = '{"en":"JKOPAY"}' WHERE country='TW' AND name='街口支付';
-- Every country with a store, and every one above, also gets a bank transfer.
INSERT INTO pay_channels (country, name, names, sort)
SELECT c, 'Bank transfer', '{"zh":"银行转账","zh-Hant":"銀行轉帳","th":"โอนเงินผ่านธนาคาร","vi":"Chuyển khoản","fr":"Virement bancaire","ar":"تحويل بنكي","es":"Transferencia bancaria","ru":"Банковский перевод","ja":"銀行振込","my":"ဘဏ်လွှဲ","km":"ផ្ទេរតាមធនាគារ"}', 99
FROM (SELECT DISTINCT country FROM pay_channels
      UNION SELECT DISTINCT upper(country_code) FROM branches WHERE country_code ~* '^[a-z]{2}$') x(c);

-- What a store accepts: {"cash": bool, "card": bool, "off": [channel ids it does not take]}.
-- Empty means cash, card and every online method of its country.
ALTER TABLE branches ADD COLUMN pay_settings JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN discount_reasons TEXT[];

-- An order paid online before delivery: the rider collects nothing.
ALTER TABLE orders DROP CONSTRAINT orders_payment_method_check,
    ADD CONSTRAINT orders_payment_method_check CHECK (payment_method IN ('cod','pay_at_pickup','pay_at_counter','prepaid'));

ALTER TABLE orders
    -- how it was paid: cash, card or online (pay_channel_id says which); '' = not recorded
    ADD COLUMN pay_type TEXT NOT NULL DEFAULT '' CHECK (pay_type IN ('', 'cash', 'card', 'online')),
    ADD COLUMN pay_channel_id BIGINT REFERENCES pay_channels(id),
    -- the discount staff gave at checkout (§5): a percentage off the dishes, then
    -- a round-off or a set amount to pay, both taken off the total last
    ADD COLUMN adj_percent_bp INT NOT NULL DEFAULT 0 CHECK (adj_percent_bp >= 0 AND adj_percent_bp < 10000),
    ADD COLUMN staff_discount BIGINT NOT NULL DEFAULT 0 CHECK (staff_discount >= 0),
    ADD COLUMN adj_mode TEXT NOT NULL DEFAULT '' CHECK (adj_mode IN ('', 'round', 'set_total')),
    ADD COLUMN round_off BIGINT NOT NULL DEFAULT 0 CHECK (round_off >= 0),
    ADD COLUMN adj_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN adj_by BIGINT REFERENCES users(id),
    ADD COLUMN adj_approved_by BIGINT REFERENCES users(id),
    ADD COLUMN adj_at TIMESTAMPTZ;
CREATE INDEX orders_adjusted ON orders (branch_id, adj_at) WHERE adj_at IS NOT NULL;

-- Existing records: a dine-in bill paid with a method that is now a channel.
UPDATE orders o SET pay_type = 'online', pay_channel_id = c.id
FROM branches b, pay_channels c
WHERE o.branch_id = b.id AND o.pay_method <> '' AND c.country = upper(b.country_code) AND c.name = o.pay_method;

-- Discounts at checkout and the reconciliation page are new permissions:
-- owners and managers have them, order takers do not.
UPDATE branch_role_perms SET perms = array_append(perms, 'checkout.discount')
    WHERE role = 'manager' AND NOT 'checkout.discount' = ANY(perms);
UPDATE branch_role_perms SET perms = array_append(perms, 'reconcile')
    WHERE role = 'manager' AND NOT 'reconcile' = ANY(perms);

-- +goose Down
UPDATE branch_role_perms SET perms = array_remove(array_remove(perms, 'checkout.discount'), 'reconcile');
DROP INDEX orders_adjusted;
ALTER TABLE orders DROP COLUMN pay_type, DROP COLUMN pay_channel_id, DROP COLUMN adj_percent_bp,
    DROP COLUMN staff_discount, DROP COLUMN adj_mode, DROP COLUMN round_off, DROP COLUMN adj_reason,
    DROP COLUMN adj_by, DROP COLUMN adj_approved_by, DROP COLUMN adj_at;
UPDATE orders SET payment_method = 'cod' WHERE payment_method = 'prepaid';
ALTER TABLE orders DROP CONSTRAINT orders_payment_method_check,
    ADD CONSTRAINT orders_payment_method_check CHECK (payment_method IN ('cod','pay_at_pickup','pay_at_counter'));
ALTER TABLE branches DROP COLUMN pay_settings, DROP COLUMN discount_reasons;
DROP TABLE pay_channels;
