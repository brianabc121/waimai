# 余额架构评审和业内做法调研 v1.0（2026-11-27，brian 已确认，第一到三批已实施）

> 范围：店铺余额（退款存入，PR #152/#153/#154）、充值送储值（PR #155/#156）、余额页改版和按比例退回（PR #157），以及“我的”页里早期的平台“余额/奖励金”（`customer_wallet_entries`，PR #48）。平台余额（按国家）还没做，放在第 6 节一起规划。
>
> brian 2026-11-27 回复“按照设计方案进行优化”，Q1–Q5 全按推荐（Q4 的邮件换成 webhook 推送，因为系统没有发邮件的服务）。实施记录见第 8 节；怎么验算见 [BALANCE_CHECK.md](BALANCE_CHECK.md)，以后所有余额改动都要按它重新验算。

---

## 0. 结论先看

1. **钱算得对。** `make pricing-check` 在真库上全部通过（引擎样例、5 万组随机、真库每张测试订单重放、W1–W15）。逐行读了 `wallet.go`、`stored.go`、`wallet_check.go`、`voids.go` 的退款部分，**没有发现现有算法算错钱**：扣余额、按比例退回、按菜退款四份拆分、退储值扣回赠送，都和 PRICING_CHECK 第 4 节的手算样例一致。
2. **底子是业内标准做法。** 复式记账、凭证和明细只能追加、余额只能由记账触发器改、每行明细记“变动后余额”、凭证幂等键、每天全账核对。这和 Square、Stripe、Modern Treasury 公开的做法是同一路子。
3. **但离“绝对可靠、所有动向都看得清”还差 3 个高优先级问题：**
   - **H1 账本只记“欠顾客多少”，没记“钱实际去了哪”。** 充值收的现金、退储值付出去的现金、扣回的已用赠送、作废的赠送，在账本里分不出来，只能去业务表拼。
   - **H2 数据库保护能被一行 SQL 绕过。** `set_config('waimai.wallet_posting','on',true)` 后就能直接改余额（已在测试库复现）；应用的数据库账号是表的主人，也能直接关掉触发器。现在只有每天一次的核对能发现。
   - **H3 “我的”页首屏那个“余额/奖励金”是另一套单式账。** 平台后台可以凭空加钱、没有对方科目、记录能改能删（已复现）、删顾客会连账一起删，也不进每日核对。顾客看到的“余额”有两套，口径不一样。
4. **中、低优先级 9 条**（第 3 节），大多是账目分类不一致、核对不报警、并发下可能报错（钱不会错）这类问题。
5. **建议分四批改**（第 5 节）：先堵 H2、H3（不改金额算法），再补 H1 的科目和“余额流水”总页，最后做报警、日终快照和平台余额。

---

## 1. 怎么查的

| 做了什么 | 结果 |
|---|---|
| 读迁移 00029、00083、00084，代码 `wallet.go`、`wallet_check.go`、`wallet_views.go`、`wallet_verify.go`、`stored.go`、`voids.go`（退款）、`order.go`（下单用余额、取消退回）、`me.go`、`me_admin.go`、`reconcile.go`、`bills.go` | 下面的问题都标了出处 |
| `TEST_DATABASE_URL=... make pricing-check`（本地 PostgreSQL 16） | 全部通过：`internal/pricing` ok，`internal/app` ok（57.8 秒），前端 dishpromo 2/2 |
| 测试跑完后在测试库上做 6 个探测（全部在事务里做完回滚，不留数据） | 见下表 |

| 探测 | 结果 | 说明 |
|---|---|---|
| 直接 `UPDATE wallet_accounts SET balance=...` | 被拒：`a wallet balance only changes by a posting` | 触发器有效 |
| 先 `set_config('waimai.wallet_posting','on',true)` 再改余额 | **成功**，余额从 352 改成 100352 | H2 |
| 应用账号是不是表的主人 | 是（部署里是 `waimai` 账号建的表） | 主人可以 `ALTER TABLE ... DISABLE TRIGGER`，H2 |
| 一张门店 A 的凭证，一行记门店 A 顾客余额、一行记门店 B 的店铺科目 | **数据库收下了** | M1：只有夜里的 W1 能发现 |
| 一张 `spend`（消费）凭证记到“赠送支出”科目 | **数据库收下了**，W1–W15 都不会报 | M1：凭证类型和科目没有对应规则 |
| `customer_wallet_entries` 改金额、删记录 | **都成功** | H3 |

另外用测试库的数据核对了 M2（退款余额那份退回时记到哪个科目），结果见 M2。

---

## 2. 现在的架构一页看懂

### 2.1 账户（`wallet_accounts`，一个账户一种币种）

| 谁的 | 科目 | 含义 | 正常余额 |
|---|---|---|---|
| 顾客 × 门店 | `store_credit` | 退款余额（门店退款存进来的） | ≥ 0（数据库 CHECK） |
| 顾客 × 门店 | `stored_principal` | 储值本金（顾客自己充的钱） | ≥ 0 |
| 顾客 × 门店 | `stored_bonus` | 储值赠送金 | ≥ 0 |
| 门店 | `store_refund` | 门店退进余额的累计（负数） | ≤ 0 |
| 门店 | `store_sales` | 用退款余额付掉的累计 | ≥ 0 |
| 门店 | `store_topup` | 收到的充值本金（负数 = 欠着顾客）；退储值时冲回 | ≤ 0 |
| 门店 | `store_bonus` | 送出的赠送金（负数） | ≤ 0 |
| 门店 | `store_stored_sales` | 用储值付掉的累计 | ≥ 0 |

### 2.2 每种业务怎么记账（凭证 `wallet_journals` + 明细 `wallet_postings`，每张凭证相加 = 0）

| 业务 | 凭证类型 | 明细 |
|---|---|---|
| 退款存入余额 | `refund` | 顾客 `store_credit` +X，门店 `store_refund` −X |
| 撤回（改退现金） | `revoke` | 上面反过来 |
| 用退款余额付款 | `spend` | `store_credit` −X，`store_sales` +X |
| 订单变小 / 取消退回 | `return` | `store_credit` +X，`store_sales` −X |
| 充值 | `topup` | `stored_principal` +P，`store_topup` −P；`stored_bonus` +B，`store_bonus` −B |
| 当天撤销充值 | `topup_revoke` | 上面反过来 |
| 用储值付款 | `stored_spend` | 本金 −p、赠送 −b，`store_stored_sales` +(p+b) |
| 储值退回（订单变小） | `stored_return` | 上面反过来 |
| 按菜退款回储值 | `stored_refund` | 本金 +p、赠送 +b，`store_stored_sales` −(p+b) |
| 退储值（提现） | `stored_cashout` | 本金 −P，`store_topup` +P；赠送 −B，`store_bonus` +B |

订单上另外存 `credit_paid`、`stored_principal_paid`、`stored_bonus_paid`（余额付了多少，是账本的“缓存”），退款记录存 `cash_part`、`to_credit`、`principal_part`、`bonus_part`。W4–W13 每次 `checked` 都核这些缓存和账本一致。

### 2.3 已经做对的地方（和业内一致，保留）

| 做法 | 现在怎么做的 | 业内对照 |
|---|---|---|
| 复式记账，每张凭证相加为 0 | `postJournal` 检查 + 提交时数据库约束触发器再查一次 | Square Books、Modern Treasury、TigerBeetle 都是 |
| 只追加，不改不删 | 凭证、明细、账户都有触发器拦 UPDATE/DELETE | Stripe Ledger“immutable append-only log” |
| 余额是缓存，只能由记账改 | `wallet_post` 触发器加余额，`wallet_balance_guard` 拦其他改法 | Modern Treasury 的 balance cache + 行锁 |
| 每行明细记“变动后余额” | `balance_after` | 支付宝类账务系统的“余额流水”字段 |
| 顾客余额不能为负 | CHECK 约束，并发时由数据库兜底 | TigerBeetle 的 `debits_must_not_exceed_credits` |
| 金额全用整数分，比例用向下取整、零头落在不能变现金的那份 | `MulDivFloor`、`storedTake`、`creditGiveBack`、`storedRefundSplit` | 通用做法 |
| 余额是付款方式，不进计价引擎 | 订单应付不变，只记余额付了多少 | 美团、饿了么、Grab 的储值卡都这样 |
| 每天全账核对 | `wallet_check` 任务：借贷平衡、余额 = 明细合计 = 最后一行 balance_after、W8、W14、W15 | 日终试算平衡 |
| 大额要授权、每人每天有上限、代客单要顾客短信验证码 | `creditRefundLimits`、`topup_approve_over`、`wallet_verifications` | 内控“四眼原则” |

---

## 3. 发现的问题（按严重程度）

严重程度：**高** = 影响“绝对可靠”或“看得清每笔钱”的根基；**中** = 账目分类或运维上的缺口，钱目前不会错；**低** = 边角问题。

> 说明：按项目铁规则，验算发现**现有算法算错钱**要先停下告诉 brian。这次**没有发现算错钱的地方**。下面 M2 是“记到哪个科目”的分类问题，顾客余额和订单金额都是对的。

### H1（高）账本只记“欠顾客多少”，看不到钱的完整去向

现在的账本只有“负债”这一面：顾客余额 vs 门店欠款科目。真正的现金进出不在账上：

- **充值**：顾客付给门店的现金/扫码，只记在 `topups.pay_type`，账上是 `stored_principal +P / store_topup −P`，没有“门店收到现金 +P”这一行。
- **退储值**（`StoredCashout`，`stored.go`）：账上一行 `stored_principal −P / store_topup +P`。但这 P 实际分成两份：真正付给顾客的现金 `paid_out` 和扣回已用赠送 `clawback`（门店留下的收入）。两份记进了**同一个科目**，只有 `stored_cashouts` 表知道怎么分。
- **作废的赠送金**：冲回 `store_bonus`，和“当天撤销充值”冲回的是同一个科目，分不出是“作废”还是“撤销”。
- **按菜退款退现金的那份**：不进账本（只在 `order_refunds.cash_part`）。

后果：问“门店今天收了多少储值现金、付出去多少、扣回多少、赠送作废多少”，要把 `topups`、`stored_cashouts`、`order_refunds` 和账本拼起来算（对账页 `reconcile.go` 正是这样拼的）。拼的逻辑一旦写错，账本自己的平衡检查发现不了。这正是 brian 要的“清楚知道所有余额的动向”的缺口。

业内做法：账本里有**内部账户**（现金/待清算、收入、损益），每笔业务都是完整的借贷，钱从哪来、到哪去都在一张凭证里。

### H2（高）数据库保护能被绕过

- `wallet_balance_guard` 靠一个会话变量判断“是不是记账触发器在改”。任何连上数据库的人执行 `SELECT set_config('waimai.wallet_posting','on',true)` 就能直接改余额。**测试库已复现**：余额 352 → 100352，没有报错。
- 应用连数据库用的账号就是表的主人（部署文档里是 `waimai`）。主人能 `ALTER TABLE wallet_postings DISABLE TRIGGER ALL`，也能 `DROP TRIGGER`。
- 发生了之后，只有第二天的 `wallet_check` 会发现（W2：余额 ≠ 明细合计），而且只在后台页面显示红条（见 M5）。

这条防的不是代码 bug（代码不会这么写），而是：手工上数据库“修数据”、被入侵后改余额、将来别人写的脚本。brian 要的是“绝对可靠”，这一层要补。

### H3（高）“我的”页上的平台“余额/奖励金”是另一套单式账

`customer_wallet_entries`（迁移 00029，PR #48 的最小版本）：

- 每行一个数，没有对方科目。平台后台“顾客和钱包”可以给任何顾客加任意金额（≤ 10 亿），钱从哪来没记。
- 表上没有防改防删的触发器：**测试库已复现** UPDATE 改金额、DELETE 删记录都成功。
- `customer_id ... ON DELETE CASCADE`：删顾客会把账一起删掉。
- 不在 `wallet_check` 每日核对里，也不在 PRICING_CHECK 的 W 规则里。
- 顾客端“我的”首页（`MeHome.vue` 的 `me-balance`）和 `/me/wallet/balance`、`/me/wallet/bonus` 显示的就是它；而店铺余额页（PR #157）显示“现金余额 / 充值赠送金”。顾客眼里有**两个“余额”**，口径不一样。

现在它不能付款、不能提现，所以还没造成损失；但以后做“平台余额”如果在这张表上加功能，就是在单式账上走钱。

### M1（中）数据库只验“相加为 0”，不验“记到对的地方”

- 一张凭证的明细可以记到**别的门店**的账户（只验了币种）。**已复现**，只有夜里 W1 能发现。
- 凭证类型和科目没有对应规则：`spend` 应该只动 `store_credit` 和 `store_sales`，但记到 `store_bonus` 数据库也收。**已复现**，而且 W1–W15 全都不会报（顾客那边对得上，门店那边只看合计）。

业内叫“交易码驱动记账”：每种业务预先定义好借哪个科目、贷哪个科目，记账时只能按模板写。

### M2（中）退款余额那份退回时，记到了“退进余额”科目而不是冲回“余额消费”

按菜退款时，这张单原来用**退款余额**付的那份退回去，`creditRefund` 记成 `store_credit + / store_refund −`；而**储值**那份记成冲回 `store_stored_sales`。两种余额的处理不一致。测试库数据：

| 退款 | 金额 | 现金那份 | 退款余额那份 | 实际明细 |
|---|---|---|---|---|
| #15 | 158.99 | 0 | 8.21 | 储值部分冲回 `store_stored_sales`；退款余额那份记 `store_refund −8.21` |
| #20 | 178.86 | 158.17 | 20.69 | `store_refund −20.69` |

影响：顾客余额完全正确；但 `store_sales`（“顾客用余额付了多少”）没被冲回，`store_refund`（“门店退进余额多少”）被放大。将来按科目出报表或做风控（例如“这个店员今天退进余额多少”）会多算。`creditRefundLimits` 用的是 `order_refunds` 字段，不受影响。

### M3（中）消费和退回的“幂等键”其实是流水号

`spend:<订单>:<第几次>`、`return:<订单>:<第几次>` 是按这张单已有凭证数现编的，同一个请求重试会得到新编号，挡不住重复。现在不会多扣，是因为另有兜底：订单行锁 + “最多扣到应付 − 已用余额”；按菜退款有真正的请求幂等键（`idemSave`）。但幂等靠“业务上限”碰巧兜住，不是靠键本身；以后加“部分用余额”“多次付款”时容易出问题。

### M4（中）锁的顺序没守住，可能死锁；事务也不重试

`walletAccount` 的注释说“先锁顾客账户，再锁门店账户”。但按菜退款里 `storedRefund` 先锁本金、赠送、门店储值消费，接着 `creditRefund` 才锁顾客退款余额。同一顾客同一时刻：店员给 A 单退款（拿着本金锁、等退款余额锁），顾客在下 B 单用余额（拿着退款余额锁、等本金锁），PostgreSQL 会判死锁、杀掉一个。`inTx` 不重试，用户看到一次报错。

钱不会错（被杀的事务整体回滚），属于可用性问题；这是读代码推出来的，没有专门复现。

### M5（中）核对一天一次、不报警；没有日终余额快照

- `wallet_check` 24 小时一次，发现问题只写进 `wallet_check_runs` 和日志，在平台后台“后台任务”页显示红条，不会主动通知 brian。
- 每次全表扫描，数据多了会越来越慢。
- 没有“每天结束时每个账户的余额”快照，也没有日报“期初 + 发生额 = 期末”。想知道“上个月底某店欠顾客多少”要从头累加明细。

### M6（中）撤销充值时作废赠券，靠“发券时间 ±1 秒”去找

`RevokeTopup` 用 `issued_at` 在 `paid_at` 前后 1 秒内来认定哪几张券是这次充值送的，券和充值之间没有关联字段。同一秒有别的来源 `topup` 券就会误作废，慢了超过 1 秒就漏掉。已经用掉的券也不会追回（目前设计如此，应写明）。

### L1（低）撤回退款会改退款记录本身

`RevokeCreditRefund` 会 `UPDATE order_refunds SET to_credit=..., pay_type='cash'`。账本凭证保留了历史，但业务记录原值没了，看退款记录时不知道它曾经是“存入余额”。

### L2（低）门店改币种后，旧币种余额用不了也退不了

账户按币种分开是对的；但下单、充值、退储值都用门店**当前**币种，旧币种余额只能留着。门店改币种很少见，但应有处理办法（例如改币种前要求余额清零，或平台按汇率转一次并留凭证）。

### L3（低）门店那边的科目是热点行

每笔消费都要锁门店的 `store_sales` / `store_stored_sales` 行，同一家店的余额付款排队执行。单店规模现在完全够用；做平台余额（一个国家共用几个平台科目）时就会成为瓶颈。

### L4（低）平台没有一张“所有余额流水”的总表

商家页有本店流水，顾客页有自己的流水，储值页有储值流水，都是各自从明细现算，字段不统一。平台没有一页能按“任意顾客 / 门店 / 业务单号 / 操作人 / 时间段”查所有余额变动。

---

## 4. 业内怎么做（调研）

### 4.1 公开资料里的共识

| 公司 / 资料 | 要点 | 我们的现状 |
|---|---|---|
| **Square Books**（2019 工程博客） | 不可变的复式记账服务；每个“账本”的余额只能通过参与记账交易来改；多租户 | 一致 |
| **Stripe Ledger** | 不可变、只追加的日志作为资金流的唯一事实来源；把上游系统的每个操作记成交易；用“清算、及时、完整”几个指标衡量数据质量，发现上下游不一致 | 账本有；**上游业务表（充值、退储值、退款）和账本的完整性**只靠夜里 W7/W8/W13/W14 部分核对（H1、M5） |
| **Modern Treasury《How to Scale a Ledger》** | 应用层强制借贷相等；已记账的明细不可改，更正用新凭证；余额缓存 + 行锁保证串行；可给明细加“余额条件”（不足就不记）；版本号乐观锁 | 一致；余额条件用的是 CHECK 约束 |
| **TigerBeetle**（专用记账数据库） | 只有“账户”和“转账”；每笔转账一借一贷、同一账本；账户上可设“借方不得超过贷方”；两阶段转账（先冻结、再确认或作废）；转账 id 就是幂等键 | 我们没有“先冻结再确认”（M3）；账本隔离只验币种，不验门店（M1） |
| **Airbnb Orpheus**（避免重复付款） | 每个请求一个幂等键，幂等记录和业务写在同一个事务提交；错误分“可重试/不可重试” | 按菜退款有；消费/退回没有真正的请求幂等键（M3）；事务不重试（M4） |
| **国内支付账务系统**（支付宝类，人人都是产品经理、掘金的总结） | 科目分级、客户账户 vs 内部账户；**交易码驱动记账**；会计日 + 日切；**日终试算平衡**（期初 + 发生额 = 期末）；热点账户用缓冲记账 | 没有交易码模板（M1）、没有内部现金/收入账户（H1）、没有日切快照（M5）、热点账户（L3） |
| **预付卡监管** | 中国：北京 2026 年办法要求预收资金余额 ≥ 20 万的商家备案并存管（不低于上季末余额的 20%），上海征求意见稿 40% 专户；菲律宾 RA 10962 禁止礼品卡过期 | 我们按门店记“欠顾客多少”，能随时报出每家店的预收余额（`branchCreditOwed`），合规上有基础；本金不过期已按 RA 10962 做 |

### 4.2 归纳成一套“可靠余额”的标准件

1. **完整的科目表**：顾客账户、门店负债、内部账户（现金/待清算、收入、赠送费用、作废收益），每笔业务一张完整凭证。
2. **交易码模板**：每种业务的借贷科目事先定好，数据库或记账函数按模板校验。
3. **数据库只开一个入口**：应用账号对账本表没有 INSERT/UPDATE/DELETE 权限，只能调一个记账函数；账本表的主人是另一个账号。
4. **真正的幂等**：每个改余额的请求带唯一键，键和凭证同事务。
5. **两层核对**：提交时核（约束触发器，已经有）+ 日终核（试算平衡、快照、业务表 vs 账本），有问题主动报警。
6. **防篡改的证据**：凭证做哈希链（每张凭证带上一张的哈希），每天把最新哈希存到外部（例如发到 brian 邮箱/日志），事后能证明账没被动过。
7. **一张总流水表**：任何一分钱都能按顾客、门店、业务单号、操作人查到来龙去脉。

---

## 5. 改进建议（按批次，等 brian 确认）

每一批涉及金额或账本的都要按 PRICING_CHECK 第 5 节走：加手算样例、场景测试每步 `checked`、`make pricing-check` 全过、PR 写“金额验算”。

### 第一批：堵住绕过和单式账（H2、H3、M1；不改任何金额算法）

1. **数据库分两个账号**：`waimai_owner` 建表、拥有账本；应用用 `waimai_app`，对 `wallet_*` 只有 SELECT；记账走 `SECURITY DEFINER` 函数 `wallet_post_journal(...)`。去掉 `waimai.wallet_posting` 会话变量的判断，改成“只有这个函数能改余额”。部署文档和 docker compose 同步改，老库迁移时自动建账号和授权。
2. **凭证校验加两条**（触发器，提交时查）：所有明细的账户都属于凭证的门店；按凭证类型只能动规定的科目（交易码模板表 `wallet_journal_rules`）。
3. **凭证哈希链**：`wallet_journals` 加 `prev_hash`、`hash`，每天核对时重算整条链，最新哈希写进核对记录。
4. **平台“余额/奖励金”处理**（见 Q2）：推荐先把现有记录按原值迁进复式账本（平台科目），旧表停写、加防改触发器、去掉级联删除；顾客端先隐藏这两个入口，等平台余额正式做再开。

### 第二批：钱的去向全进账本（H1、M2、M6、L1）

1. **加内部账户**：门店“储值现金收款”、“退储值付现”、“扣回已用赠送（收入）”、“赠送作废（收益）”；平台以后同理。充值凭证变成“收现金 → 本金 + 赠送”，退储值凭证拆成“付现 / 扣回 / 作废”三行。
2. **统一冲回规则**：按菜退款里退款余额那份冲回 `store_sales`（和储值一致），只有“顾客同意把现金那份存余额”才记 `store_refund`。历史凭证不改，用一张调整凭证把科目分类对齐（金额不变）。
3. 充值送的券加 `topup_id` 关联，撤销时按关联作废。
4. 撤回退款不再改原退款记录，改为新增一条“撤回”记录。
5. 验算：W 规则加“每家店现金类科目 = 对账页钱箱里的储值现金”“每张退储值凭证三行 = stored_cashouts 三个数”，`CheckWallet` 加交易码校验。

### 第三批：看得清、早发现（M3、M4、M5、L4）

1. **余额流水总页**（平台后台“余额流水”，商家后台同样的页面只看本店）：每行 = 时间、交易码、顾客、账户、变动、变动后余额、对方科目、业务单号（订单/退款/充值/退储值，可点开）、操作人和授权人、备注；可按顾客/门店/单号/操作人/时间筛选，可导出。顾客页、商家页现有流水改用同一个查询。
2. **日终快照和试算平衡**：每天按门店时区结束时记每个账户余额；日报“期初 + 发生额 = 期末”，按科目、按门店。
3. **核对改成两档**：每次提交（已有）+ 每小时增量核对（只查新凭证）+ 每天全量；有问题推送给平台管理员（站内消息 + 邮件/短信，见 Q4）。
4. **真正的幂等键**：下单用余额、充值确认、退储值都由前端带请求键，和凭证同事务；消费/退回凭证键改成“请求键”而不是流水号。
5. **固定锁顺序**：所有改余额的地方先锁顾客行，再按账户 id 从小到大锁；`inTx` 遇到死锁/序列化失败自动重试一次。

### 第四批：平台余额（按国家），等以上三批做完再出方案

用同一套账本，加“平台”层科目（按国家、币种）；平台余额是平台的负债，和门店余额完全分开；涉及电子货币监管（菲律宾 BSP、新加坡 MAS、马来西亚 BNM），出方案时单独调研。平台科目是热点账户，要考虑按日汇总记账。

---

## 6. 需要 brian 决定的问题

| # | 问题 | 推荐 |
|---|---|---|
| Q1 | 按第 5 节分四批做，先做第一批？ | 是。第一批不改任何金额算法，风险最小、收益最大 |
| Q2 | 现在“我的”页的平台“余额/奖励金”怎么办？A 迁进复式账本并先隐藏入口；B 原样保留只加防改；C 直接停用（先确认线上有没有数据） | A |
| Q3 | 第二批改历史凭证的科目分类（M2），用“调整凭证”对齐（不改旧凭证、金额不变）？ | 是 |
| Q4 | 核对发现问题时怎么通知你？站内 + 邮件 / 站内 + 短信 / 只在后台页面 | 站内 + 邮件 |
| Q5 | 余额流水总页给谁看？平台管理员看全部，店主和有 `wallet.view` 的经理看本店 | 按推荐 |

---

## 7. 来源

- Square Engineering：[Books, an immutable double-entry accounting database service](https://developer.squareup.com/blog/books-an-immutable-double-entry-accounting-database-service/)
- Stripe：[Ledger: Stripe’s system for tracking and validating money movement](https://stripe.dev/blog/ledger-stripe-system-for-tracking-and-validating-money-movement)
- Modern Treasury：[How to Scale a Ledger Part II](https://moderntreasury.com/journal/how-to-scale-a-ledger-part-ii)、[Part IV](https://www.moderntreasury.com/journal/how-to-scale-a-ledger-part-iv)、[Part V](https://moderntreasury.com/journal/how-to-scale-a-ledger-part-v)、[How and why homegrown ledgers break](https://www.moderntreasury.com/journal/how-and-why-homegrown-ledgers-break)
- TigerBeetle：[Transfers](https://docs.tigerbeetle.com/reference/transfer)、[Two-Phase Transfers](https://docs.tigerbeetle.com/coding/two-phase-transfers/)、[Accounts](https://docs.tigerbeetle.com/reference/accounts)
- Airbnb Engineering：[Avoiding Double Payments in a Distributed Payments System](https://medium.com/airbnb-engineering/avoiding-double-payments-in-a-distributed-payments-system-2981f6b070bb)
- 国内账务系统设计：[人人都是产品经理 · 账务系统](https://www.woshipm.com/pd/4440125.html)、[掘金 · 账务系统设计](https://juejin.cn/post/7409850315204542483)
- 预付卡监管：[北京单用途预付卡备案及预收资金管理（新华网报道）](https://www.news.cn/20260528/929d95d62bec4ee785d54b9fb7fb6ba1/c.html)、[上海“以案释法”单用途预付卡](https://sww.sh.gov.cn/assets/RECP/y_06.html)
- 项目内：docs/REFUND_BALANCE_DESIGN.md、docs/STORED_VALUE_DESIGN.md、docs/PRICING_CHECK.md 第 2.3 节

---

## 8. 实施记录（2026-11-27）

第一到三批一起做在迁移 `00085_ledger_hardening.sql` 和同一个 PR 里，**没有改任何金额算法**：订单该付多少、退多少、储值怎么拆，全部和以前一样；改的是“怎么记账、怎么保护、怎么查”。验算方法和全部规则写进了新文档 [BALANCE_CHECK.md](BALANCE_CHECK.md)（W1–W20、手算样例 B-S1 到 B-S6），AGENTS.md / CLAUDE.md 加了铁规则：余额改动必须重新验算。

| 问题 | 怎么改的 | 规则 / 测试 |
|---|---|---|
| H1 钱的去向 | 新科目 `store_cashout`（退出去的钱）、`store_clawback`（扣回）、`store_bonus_void`（作废赠送）；退储值按三个去向记账；历史退储值用重分类凭证 `reclass_cashout:<id>` 对齐 | W14；B-S1、B-S2；`TestCashoutBooksWhereItWent` |
| H2 绕过保护 | 唯一入口数据库函数 `wallet_post_journal`（SECURITY DEFINER）；余额保护改看 `pg_trigger_depth()`，会话变量开不了门；新角色 `waimai_app` 只能读账本、调记账函数；每个账户的明细编号 + 哈希链 + 指纹 | W2、W18；`TestBooksGuards`、`TestAppRoleCannotWriteBooks` |
| H3 平台余额单式账 | `customer_wallet_entries` 按原值、原时间迁进复式账本（`platform_adjust` 凭证、`platform_balance / platform_bonus / platform_issued` 科目），旧表停写、不能删；后台调整改走记账函数并带幂等键；顾客端“余额/奖励金”入口隐藏，只有还有老余额的顾客能看到 | W17；B-S5；`TestPlatformWalletBooks` |
| M1 记到对的地方 | 规则表 `wallet_journal_rules`：每种凭证能动哪些科目；记账函数检查门店、币种、科目 | W1；`TestBooksGuards` |
| M2 退款余额那份 | 退款时原本用退款余额付的那份冲回 `store_sales`，只有现金那份存进余额才记 `store_refund`；历史退款用 `reclass_refund:<id>` 重分类 | W16；B-S3；`TestRefundBalanceShareReverses` |
| M3 幂等键 | 订单派生凭证用 `postNew`（同一个键已存在就报错，不会悄悄当成重放）；充值确认、退储值、平台调整接受 `Idempotency-Key`，前端每次操作带一个 | W20；B-S6 |
| M4 锁顺序 | 先锁顾客，再按固定顺序锁门店科目；记账函数内部按账户 id 加锁；`inTx` 遇死锁、序列化冲突重试一次 | 全部测试 |
| M5 核对和日终 | 每小时增量 + 每天全量检查，结果记在 `wallet_check_runs`（含指纹、安全警告）；有问题平台后台“余额账本”红色数字 + `WALLET_ALERT_WEBHOOK` 推送；每天关账写 `wallet_daily_balances`，商家和平台都能看日终试算 | W19；B-S4；`TestWalletDaysAndAlarm` |
| M6 撤销充值作废券 | 券记下来自哪张充值单（`customer_coupons.topup_id`），撤销时按它作废；历史数据按 ±1 秒补一次 | `TestTopupRevokeVoidsItsCoupons` |
| L1 撤回改退款记录 | `order_refunds` 加 `revoked_credit / revoked_at / revoked_by`，金额列不能改，存进 + 撤回 恒定，记录不能删 | W7；`TestStoreCreditLimitsAndRevoke` |
| L4 总流水 | 商家后台“店铺余额 → 全部流水 / 日终试算”（本店），平台后台“余额账本”（全部门店 + 平台余额），可按手机、单号、业务、科目、员工、日期筛，导出 CSV | `TestStoreCreditRoutes` |

**没做的**：

- L2（门店改币种后旧币种余额）、L3（门店科目热点行）：目前单店量级不需要，等真的出现再做。
- **第四批平台余额（按国家）**：需要单独出方案（钱从哪来、能不能跨店用、谁出钱、怎么结算给门店），brian 确认后再做。现在平台余额只是迁进了复式账本、对顾客隐藏。

**上线注意**：00085 不能回退（会搬数据、补哈希），升级前一定备份；正式服务器要按 DEPLOY.md 第 8 节把应用改成用 `waimai_app` 连接，并设 `WALLET_ALERT_WEBHOOK`。
