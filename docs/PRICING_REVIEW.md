# 订单金额计算评审：现状、业内做法、风险和目标架构

版本 v0.1 · 2026-11-24 · 只做评审和调研，没有改代码。是否改造、按哪一步改，等 brian 决定（第 8 节）。

---

## 0. 结论先看

1. **地基是对的。** 金额全部用整数（int64，“分”），没有浮点；下单时服务器用实时菜单重新算一遍，和顾客确认的报价逐项比对；活动名额、券、库存都在事务里加锁领取；订单上存了税率、包装费、配送费、活动名称这些快照；改价都有审计日志。这些是金钱系统最要紧的几条，已经做到了。
2. **问题出在“改单”这一层。** 下单只算一次，问题不大；但加单、退菜、结账打折、抹零、按菜退款这几个线程是分别加的，每个都自己拼一套“这张单现在该收多少”，同一件事在代码里有 **9 处**各写一遍，从不同的地方（订单列、优惠明细表、活动表的现在状态、门店的现在设置）凑数据。凑法不一致，就出了错。
3. **用测试在真库上复现了 5 个算错钱的问题**（第 3 节，附测试代码）。最严重的两个：**堂食有满减时按菜退款，系统建议退款金额是 0，退不了**；**外卖/自取订单打过折再退菜，建议退款也是 0**。
4. **将来要加新活动类型（第二份半价、买一送一、指定菜品打折、会员价、阶梯满减……）现在很难加**：优惠只分“菜品”和“配送费”两个桶，没有分摊到每道菜，活动种类写死在十几处 switch 和 SQL 里。
5. **建议**：先用一个小 PR 修掉 5 个错（第 7 节第 0 步，1 到 2 天），再分 4 步把计算收拢成“一个纯函数的计价引擎 + 一张优惠明细表（分摊到每道菜）+ 一本只追加不修改的金额流水”。每一步都能单独上线、单独回滚。

---

## 1. 现在是怎么算的

### 1.1 计算顺序（外卖、自取）

```
菜品原价 × 数量（含规格加价）                       quote.go:priceLines
  └─ 小计 subtotal
① 店铺活动（满减 / 折扣 / 免配送费，自动挑最省的一个） promos.go:bestPromotion
② 店铺券：满减券 → 无门槛券 → 配送费券              coupons.go:pickCoupons（固定顺序 couponOrder）
③ 结账打折（只打菜品，打“活动和券之后还剩的”）      checkout.go:adjustTotals
④ 服务费 = (菜品 − ①②③的菜品部分) × 服务费率          pricing.go:ComputeTotals
   包装费（固定）、配送费（区域或店员改价）
⑤ 税 = (菜品净额 + 包装费 + 服务费) × 税率；配送费不计税；含税/不含税两种
⑥ 平台券（平台出钱，从应付里减，不影响店铺的税和服务费） pricing.go:applyPlatform
⑦ 抹零 / 改实收（只减应付，不影响税）                 checkout.go:adjustTotals
  = 应付 total
```

堂食少 ② 和 ⑥（不用券、没有配送费），活动只看堂食活动，计价在 `dinein.go:dineTotals`。

### 1.2 改单时谁在重算

| 场景 | 入口 | 自己的重算函数 | 读哪些数据 |
|---|---|---|---|
| 下单报价 | `quote.go:114 computeQuote` | `applyCoupons` → `ComputeTotals` | 门店现在设置、实时菜单、活动表、顾客钱包 |
| 下单确认 | `order.go:480 PlaceOrder` | 再跑一次 `computeQuote`，`sameQuote` 比对 | 同上 |
| 代客下单 | `members.go:466 staffAdjust` | `adjBase.price` | 报价 + **活动表现在的 kind** |
| 结账打折/抹零 | `checkout.go:392 orderAdjBase` | `adjustTotals` | 订单列 + `order_discounts` + **门店现在的服务费率** + **活动表现在的 kind** |
| 加单 | `addon.go:152 priceWithAddon`、`:422 priceWithAddonOnly`（两份几乎一样的代码） | `ComputeTotals` + `reapplyAdjust` | 订单列 + `order_discounts` + 活动表 + **门店现在的服务费率** |
| 外卖退菜 | `voids.go:446 voidPrice`、`:499 repriceAfterVoid` | `ComputeTotals` + `reapplyAdjust` | 同上 |
| 堂食加菜/退菜 | `dinein.go:424 recomputeBillKeep`、`:462 dineTotals` | `ComputeTotals` | 订单行 + **门店现在的税率、服务费率** |
| 按菜退款 | `voids.go:709 Refund` | 堂食：`ComputeTotals(…, Discount{})`；外卖：`voidPrice` | 见第 3 节问题 1、2 |
| 对账 | `reconcile.go:97` | SQL 现场推算 | `orders.total`，活动金额用“总优惠 − 平台券 − 券”倒推 |
| 账单 | `bills.go:240` | SQL 汇总 | `orders.merchant_discount` 和 `order_discounts` 两套各算一遍 |

### 1.3 金额类型、舍入、币种、快照

| 项目 | 现状 | 评价 |
|---|---|---|
| 金额类型 | `int64`，存“百分之一单位”（pricing.go:3） | 好，业内标准做法 |
| 比例 | 基点 bp（1200 = 12%），`mulDivRound` 四舍五入 | 好；每个费用各自舍入一次，没有“先加再舍”误差 |
| 多币种 | 门店国家定币种（PR #63），订单存 `currency` | 好；但**所有币种都存 1/100**，日元、越南盾、韩元也一样（见问题 6） |
| 下单快照 | 税率、含税、包装费、配送费、活动名、各项优惠金额 | 好；但**服务费率、活动规则（kind/门槛/面额）没存**（问题 4、5） |
| 报价防篡改 | 报价存库 10 分钟，下单时重算并逐项比对 | 好，和 Shopify/Square 的“服务器说了算”一致 |
| 并发 | `FOR UPDATE` 锁订单、条件 UPDATE 领名额和券、幂等键 | 好 |
| 审计 | `audit()` 有 54 处，记录前后总额 | 有，但金额是**覆盖写**在 `orders` 上，没有可加总的金额流水（问题 8） |

---

## 2. 做得好的地方（改造时要保留）

- 整数分、bp、四舍五入集中在一个函数。
- 服务器重新算价，客户端传的金额只用来做“你看到的和现在一样吗”的比对（`ExpectedTotal`、`sameQuote`）。
- 出资方分开：`merchant_discount`（店铺出）和 `platform_subsidy`（平台出），平台券在税后减，店铺的税基不受平台补贴影响，这个处理是对的。
- 活动名额、每人限用、券的领取都是数据库条件更新，压测过并发。
- 退菜区分“顾客原因 / 店铺原因”决定满减降不降档，加单“只升不降”，这些业务规则本身合理，问题只在实现分散。

---

## 3. 找到的问题（按严重程度）

下面 1 到 5 都在本地 PostgreSQL 上用测试复现过，测试代码在附录 A，金额单位是分（36800 = 368.00）。测试店：税 12%、服务费 5%、包装费 10.00、配送费 69.00；鸡 169.00、水 30.00。

### 问题 1（高）：堂食有满减，按菜退款建议金额是 0

- 位置：`voids.go:790`
  ```go
  after = ComputeTotals(max(sub-q.Dishes, 0), 0, b.dineInTax(), Discount{})
  ```
  退款试算时把剩下的菜按**没有任何优惠**重新算，再拿“原来付的 − 试算”当退款额。
- 复现：堂食 8 折活动，点 2 份鸡 + 1 瓶水，小计 368.00，优惠 73.60，实付 309.12。结账后按店铺原因退那瓶水（30.00）：**建议退款 = 0**。因为“剩下的鸡不打折”算出来比原来付的还多，相减为负，被截成 0。系统规定退款不能超过建议额，所以**这瓶水根本退不了**。
- 同样的错：只要堂食账单有活动或结账打折，退款建议都会偏低或为 0；而且用的是门店**现在**的税率。

### 问题 2（高）：结账打过折的外卖/自取单，按菜退款建议金额是 0

- 位置：`voids.go:801` 调用 `voidPrice`，而 `voidPrice`（`voids.go:446`）只算活动和券，**不算结账打折**。
- 复现：自取单小计 368.00，店员打 8 折（减 73.60），实付 319.12。取走后退那瓶水（店铺原因）：**建议退款 = 0**。
- 抹零也类似：试算不含抹零，退款建议会多出抹掉的那几毛。

### 问题 3（中高）：店铺原因退菜后，优惠明细加起来比总优惠多

- 位置：`voids.go:465`（活动保持原额，只封顶到剩余小计）和 `voids.go:560`（券只封顶到剩余小计，**没扣掉活动已经占掉的部分**）。`ComputeTotals` 会把总优惠截到小计，但 `order_discounts` 里的每一行没跟着截。
- 复现：满 300 减 100 + 无门槛券 50，点 2 份鸡 + 1 瓶水（368.00）。店铺原因退掉 2 份鸡，剩 30.00：
  - 订单 `discount` = 30.00，`merchant_discount` = 30.00
  - `order_discounts` 明细：活动 30.00 + 券 30.00 = **60.00**
- 影响：商家账单页“按券种汇总”（`bills.go`，读明细）和“店铺优惠合计”（读 `merchant_discount`）对不上；对账页的活动金额是倒推的（`reconcile.go:139`），又是第三个数。之后再打折，`orderAdjBase` 按“总优惠 − 券”倒推活动 = 0，又是第四个说法。

### 问题 4（中）：服务费率没有存在订单上，改单时用门店现在的费率

- 位置：`checkout.go:403`、`addon.go:182`、`addon.go:441`、`voids.go:484`、`members.go:470`、`dinein.go:441/473`，都是 `b.taxConfig()` 或 `b.ServiceChargeBP`。订单表有 `tax_rate_bp` 却没有服务费率。
- 复现：自取单下单时服务费 5%，小计 338.00，服务费 16.90。商家把服务费改成 10%，店员给这张单加一瓶水（30.00）：服务费变成 **36.80**，即原来那 338.00 的菜也按 10% 重新收了，顾客多付 16.90。
- 堂食更进一步：每次加菜都用门店现在的税率和服务费率重算整张账单，并把新税率写回订单。

### 问题 5（中）：活动被编辑后，老订单的打折重算会变

- 位置：`checkout.go:415`、`members.go:475` 用 `SELECT kind FROM promotions WHERE id=…` 读活动**现在**的类型，来判断订单上的活动金额是减菜品还是减配送费；加单、退菜（`addon.go:160`、`voids.go:452`）也读活动现在的门槛和面额。而活动在没结束前可以改类型（`promos.go:321`）。
- 复现：外卖单用“免配送费”活动（减 69.00），预览抹零 0.01 应付 408.68。商家把这个活动改成“满 69 减 69”，同一张单再预览：应付变成 **396.53**，服务费、税都变了（活动金额被当成菜品折扣）。

### 问题 6（中）：无小数货币也存 1/100，会算出“半日元”

- 位置：`pricing.go:59` 的舍入只舍到 1/100；`web/src/i18n.ts:441` 显示时对日元等再四舍五入到整数。
- 例子：日元店菜品 ¥1,005，服务费 10% → 存 10050（¥100.50），税、折扣同理都可能带零头。页面上每一项各自四舍五入显示，**各项加起来可能和合计差 ¥1**；现金收款时实际收不了 ¥0.50。越南盾、韩元、印尼盾一样。业内做法是按币种的实际小数位存（Stripe：日元 500 就传 500），或至少在每一步把结果舍到该币种的最小单位。

### 问题 7（中）：按菜退款没有进对账和账单

- 位置：`reconcile.go:97-150` 和 `bills.go:240` 都只看 `orders.total`；`order_refunds` 只用于退菜记录页。退款后，对账“应收”和钱箱“应有现金”都没扣（退菜线程已在记忆里注明“没做”）。
- 影响：有退款的日子，对账一定对不上，商家会以为丢钱。

### 问题 8（结构）：同一件事写了 9 遍，数据有 4 个“真相”

- “这张单现在该收多少”在 `computeQuote`、`staffAdjust`、`orderAdjBase`、`priceWithAddon`、`priceWithAddonOnly`、`voidPrice`、`recomputeBillKeep`/`dineTotals`、`Refund`、`reapplyAdjust` 各写一遍，参数各不相同。问题 1 到 5 都是“某一处漏了一步”。
- 活动优惠金额同时存在 `order_discounts` 活动行、`promotion_redemptions.amount`、“`discount − platform_subsidy − 券`”倒推值、`Totals.PromotionDiscount` 四处，改单时要逐个同步。
- 金额改动直接覆盖 `orders` 的列；历史只在 `audit` 的 JSON、`order_batches.total_before/after`、`order_voids.total_before/after` 里，散而且不能直接加总。查“这单为什么从 408 变成 319”要翻多张表。

### 问题 9（结构）：优惠没有分摊到每道菜，新活动很难加

- `Discount` 只有 `Goods` 和 `Delivery` 两个数（`promos.go`），没有“这 73.60 里每道菜各分多少”。
- 后果：
  - 按菜退款只能“整单重算再相减”，遇到抹零、封顶、降档这些非线性规则就会出问题 1、2 那样的错；
  - 没法开出按菜的退款小票或红字发票（每道菜的税是多少不知道）；
  - **加不了菜品级活动**：第二份半价、买一送一、指定菜品 8 折、套餐价、会员价，这些都需要“优惠落在哪几道菜上”；
  - 活动种类写死：`couponOrder`、`readOrderMoney` 的 switch、`orderAdjBase` 的 `kind IN (…)`、`loadDiscounts` 的 `array_position`、`bills.go` 的 `billKinds`、`reconcile.go` 的 `funded_by` 过滤……加一种券要改十来处。

### 其他小点

- `voidPrice`（`voids.go:476`）找“更好的活动”时没按 `scope` 过滤，靠 `why()` 里的判断挡住堂食活动，能用但容易被后来的人改坏。
- 堂食活动的名额要到结账才扣（`dinein.go:755`），名额用完时整张账单会被重算并要求收银员重新确认，这是有意设计，但流程上会让收银员看到金额跳动。
- 配送费改价（代客下单 `DeliveryFee`）没有单独记录原区域运费，账单上看不出这单配送费被改过。

---

## 4. 业内是怎么做的

| 做法 | 谁在用 | 要点 |
|---|---|---|
| 金额用最小货币单位整数 | Stripe、Square、Shopify、commercetools（`centAmount`） | 每种币种有自己的小数位；日元 500 就是 500，不乘 100 |
| 优惠分“整单”和“单行”两种作用域，整单优惠**分摊到每一行** | Square Orders API（`scope = ORDER / LINE_ITEM`，整单折扣按行小计分摊，行上有 `applied_discounts` 和分摊额）；Toast（整单折扣按各菜占比分摊到菜上，菜品级折扣一道菜最多一个） | 分摊后，退一道菜就退“这道菜的净额 + 它的税”，不需要整单重算 |
| 优惠按类别分开计算、有明确的叠加规则 | Shopify Discount Functions（product / order / shipping 三类，叠加规则配在优惠上）；commercetools（`sortOrder` 决定先后，`stackingMode` 决定是否继续叠加，“同组只取最优”） | 先后顺序和能不能叠加是**数据**，不是写在代码里的 if |
| 计价引擎是纯函数 | Shopify 的 Function 只吃购物车输入、输出优惠操作；commercetools 由平台统一重算购物车 | 输入一样输出就一样，可以离线回放、单元测试、比对新旧版本 |
| 订单上存规则快照 | Square 订单上的折扣对象带名称、类型、比例/金额，之后目录里改了不影响老订单；作用域创建后不可改 | 改单重算只读快照，不读“活动现在长什么样” |
| 只追加的金额流水（复式记账） | Modern Treasury 等账本：每笔交易至少两条分录、借贷平衡、错了用反向分录更正而不是改原记录 | 每次下单、加单、退菜、打折、收款、退款都是一条流水；订单上的总额只是流水的汇总缓存 |
| 退款单独成单 | Square/Toast 的 refund 是独立对象，关联到被退的行和支付 | 对账从“收款 − 退款”算，不从订单当前总额算 |

---

## 5. 目标架构

### 5.1 一个计价引擎（纯函数）

```go
// internal/pricing（新包，不碰数据库）
type Input struct {
    Currency   Currency          // 含小数位 exponent、现金最小单位
    Lines      []Line            // 每道菜：单价、数量、分类、标签、是否已退
    Fees       FeeConfig         // 税率、含税、服务费率、包装费、配送费 —— 全部来自订单快照
    Rules      []Rule            // 本单适用的活动/券/打折/抹零的规则快照，带 stage 和叠加规则
    Policy     Policy            // 改单策略：活动“只升不降”的保底额、店铺原因不降档等
}
type Result struct {
    Lines       []LineResult     // 每道菜：原价、各项优惠分摊、净额、税、服务费分摊
    Adjustments []Applied        // 每条规则实际减了多少、谁出钱、分摊到哪些行
    Totals      Totals           // 由上面加总得出，不单独算
}
func Price(in Input) (Result, error)
```

固定的阶段（每条规则声明自己在哪个阶段）：

1. 单品价（规格、会员价、时段价）
2. **菜品级优惠**（第二份半价、买一送一、指定菜品折扣）→ 直接落在行上
3. **整单菜品优惠**（满减、折扣、店铺券）→ 按行净额比例分摊，余数用“最大余数法”分，保证分摊之和等于优惠额
4. 结账打折（同样分摊到行）
5. 服务费、包装费、配送费，及配送费优惠
6. 税（按行算，再加总；含税/不含税两种）
7. 平台出资的优惠（平台券）
8. 舍入：抹零、改实收、按币种现金舍入

所有入口（报价、下单、代客下单、加单、退菜、结账打折、堂食账单、退款试算）都只做一件事：**组装 Input → 调 Price → 保存 Result**。现在的 9 处重算全部删掉。

### 5.2 规则可插拔

```go
type Rule interface {
    Stage() Stage
    Eligible(ctx EvalContext) (ok bool, why string)   // 门槛、时段、服务类型、名额（名额在引擎外领取）
    Apply(ctx EvalContext, cart *Working) Applied     // 返回减了多少、落在哪些行
}
```

- 现有的 `amount_off / percent_off / free_delivery`、四种券、结账打折、抹零、改实收，各写成一个 Rule。
- 叠加规则（能否和券同用、同组取最优、优先级）放在规则配置上，不放在代码顺序里。
- **以后加一种活动 = 加一个 Go 文件 + 一份配置表单**。比如“第二份半价”只要实现 stage 2 的 Apply，退款、对账、账单、小票都自动对。

### 5.3 订单上的数据

- `orders` 增加 `pricing_snapshot jsonb`：下单时的费率、币种小数位、每条规则的完整参数（类型、门槛、面额、出资方）。改单只读它。
- 新表 `order_adjustments` 取代 `order_discounts` 和 `orders.adj_* / staff_discount / round_off` 这些散列：一行一条规则，含 `kind`、`stage`、`funded_by`、`amount`、`rule_snapshot`。
- 新表 `order_line_allocations`：每条优惠在每道菜上分了多少、每道菜的税和服务费。
- 新表 `order_money_events`（只追加）：`placed / addon / void / adjust / payment / refund / correction`，每条记 `delta_total`、`delta_store_funded`、`delta_platform_funded`、`delta_tax` 和引擎版本号。`orders.total` 等列保留，作为“流水加总”的缓存，并用测试保证两者相等。
- 对账和账单改为读流水：收款 − 退款，按出资方汇总，问题 7 自然消失。

### 5.4 币种

- 加一张币种表：`code, exponent（小数位）, cash_unit（现金最小面额）`。
- 新订单按实际小数位存（日元存 1 = ¥1）；老订单带 `amount_scale` 字段区分，显示和对账都看它。这一步要做数据迁移，放在最后。

### 5.5 守住正确性的测试

- **不变式测试**（每次计价后都检查）：各项优惠分摊之和 = 该优惠金额；行净额之和 + 费用 + 税（不含税时）− 平台优惠 − 舍入 = 应付；任何一项不为负；退款累计 ≤ 已付。
- **黄金用例**：把现在 `*_test.go` 里所有金额断言整理成表格驱动的用例，新引擎上线前逐条比对，确保除了修掉的错以外一分不差。
- **随机测试**：随机菜品、活动、券、加单、退菜序列，检查不变式。

---

## 6. 和现有业务规则的对应

| 现有规则 | 新架构里怎么表达 |
|---|---|
| 活动自动取最优，堂食/外卖分开 | 规则的 `scope` + “同组取最优” |
| 券每种最多一张，固定顺序 | 券规则的 stage 和组 |
| 加单活动只升不降，券保留 | `Policy.PromoFloor` = 原活动金额 |
| 退菜顾客原因降档、店铺原因不降 | `Policy.KeepPromo`，按原因设置 |
| 券封顶 | 分摊时自然封顶到行净额，不会再出现问题 3 |
| 结账打折跟着加单/退菜走，抹零清掉 | 打折规则保留在快照里，抹零规则在改单时移除 |
| 按菜退款 | 退款额 = 被退那几行的净额 + 税 − 已退；店铺原因不重算活动，顾客原因按 Policy 重算差额 |

---

## 7. 分步改造建议

每一步一个 PR，可以单独上线、单独回滚。

| 步 | 内容 | 解决 | 估计 |
|---|---|---|---|
| **0. 先修错** | ① 退款试算带上活动和结账打折（堂食用 `recomputeBillKeep` 同样的口径，外卖在 `voidPrice` 后套 `adjBase`）；② `repriceAfterVoid` 封顶券时扣掉活动占的部分；③ 订单加 `service_charge_bp`、`promotion_kind` 快照列，迁移补老订单，所有改单读快照；④ 对账、账单扣掉 `order_refunds`；⑤ 附录 A 的 5 个测试改成断言正确值，加进仓库 | 问题 1、2、3、4、5、7 | 1 到 2 天 |
| 1. 抽出计价引擎 | 新建 `internal/pricing` 纯函数，先只复刻现有逻辑（两桶优惠），9 处入口改为调用它；黄金用例对齐 | 问题 8 的重复代码 | 3 到 4 天 |
| 2. 分摊到行 + 优惠明细表 | `order_adjustments`、`order_line_allocations`；退款改为按行净额；`order_discounts` 保留只读视图兼容账单页 | 问题 3、9 的前半 | 3 到 5 天 |
| 3. 金额流水 | `order_money_events`，对账和账单改读流水；后台订单详情加“金额变动记录” | 问题 7、8 的后半 | 2 到 3 天 |
| 4. 规则插件 + 第一个新活动 | Rule 接口，现有活动改写成插件；上线第一个菜品级活动（建议“指定菜品折扣”或“第二份半价”）验证 | 问题 9，将来的新活动 | 3 到 5 天 |
| 5. 币种小数位 | 币种表、新订单按真实小数位、现金舍入 | 问题 6 | 2 到 3 天（含迁移） |

如果近期只有菲律宾比索的店，第 5 步可以等到真有日元、越南盾等门店时再做。

---

## 8. 需要 brian 决定的

1. **第 0 步（修 5 个错）现在做吗？** 推荐：现在做。这些错会让商家退不了款、账对不上。
2. **后面的改造做到哪一步？** 推荐：至少做到第 2 步（引擎 + 分摊到行）。如果近期要推第二份半价、买一送一这类活动，就一路做到第 4 步，否则每加一种活动都要在 9 处打补丁。
3. **商家改活动规则时，已下单但没完成的订单怎么办？** 推荐：一律按下单时的规则（快照），这也是 Square 的做法。
4. **堂食开台后商家改了税率/服务费率，这桌按哪个？** 推荐：按开台时的。现在是按最新的。
5. **日元、越南盾等门店什么时候会有？** 决定第 5 步的时间。

---

## 附录 A：复现测试

需要本地 PostgreSQL（`TEST_DATABASE_URL`）。放进 `internal/app/` 后运行
`go test ./internal/app/ -run TestRepro -v`。这几个测试只打印金额，第 0 步修复时改成断言。

```go
package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func pickupDone(t *testing.T, o *app.Order) *app.Order {
	t.Helper()
	ctx, a, staff := context.Background(), testApp, app.Actor{Type: "staff", ID: 1}
	o, err := a.Accept(ctx, o.ID, 0, staff)
	noErr(t, err)
	for _, it := range o.Items {
		o, err = a.SetItemStatus(ctx, o.ID, it.ID, "ready", app.Actor{Type: "system"})
		noErr(t, err)
	}
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, staff)
	noErr(t, err)
	o, err = a.StaffHandoff(ctx, o.ID, staff)
	noErr(t, err)
	return o
}

// 问题 1：堂食有活动时按菜退款
func TestReproDineRefundPromo(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Scope: "dine_in", Kind: "percent_off", Value: 2000})
	tbl, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "R1", Active: true})
	noErr(t, err)
	bill, err := a.OpenTable(ctx, tbl, 2, nil, s.staff)
	noErr(t, err)
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"),
		batch(app.QuoteLineInput{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}},
			app.QuoteLineInput{ItemID: s.water, Qty: 1}), s.staff)
	noErr(t, err)
	bill = finishKitchen(t, bill)
	o := bill.Order
	bill, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: o.ManifestVersion,
		AmountReceived: o.Totals.Total, PayType: "cash"}, s.staff)
	noErr(t, err)
	o = bill.Order
	rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "water").ID, 1),
		ReasonKind: "store", Dry: true}, s.staff)
	noErr(t, err)
	t.Logf("total=%d dishes=%d suggested=%d", o.Totals.Total, rq.Dishes, rq.Suggested)
}

// 问题 2：结账打折后按菜退款
func TestReproRefundStaffDiscount(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}},
		{ItemID: s.water, Qty: 1}})
	o, err := a.SetOrderAdjust(ctx, o.ID, 0, app.Adjust{PercentBP: 8000}, s.staff)
	noErr(t, err)
	o = pickupDone(t, o)
	rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "water").ID, 1),
		ReasonKind: "store", Dry: true}, s.staff)
	noErr(t, err)
	t.Logf("total=%d dishes=%d suggested=%d", o.Totals.Total, rq.Dishes, rq.Suggested)
}

// 问题 3：店铺原因退菜后优惠明细之和 ≠ 总优惠
func TestReproVoidDiscountLines(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Kind: "amount_off", Threshold: 30000, Value: 10000, AppliesDelivery: true, AppliesPickup: true})
	cust, phone := customer(t)
	s.give(t, cust, app.CouponStoreFree, 5000, 0, 7)
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{
		{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.water, Qty: 1}}}, phone)
	noErr(t, err)
	p, err := a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)
	o, err := a.OrderByToken(ctx, p.AccessToken)
	noErr(t, err)
	o, err = a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	r, err := a.VoidLine(ctx, itemByName(o, "chicken").ID, app.VoidInput{Reason: "沽清", ReasonKind: "store"}, s.staff, true)
	noErr(t, err)
	var lines int64
	for _, d := range r.Order.Discounts {
		lines += d.Amount
	}
	t.Logf("discount=%d lines=%d", r.Order.Totals.Discount, lines)
}

// 问题 4：服务费率改了以后加单
func TestReproServiceChargeDrift(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}})
	t.Logf("placed svc=%d", o.Totals.ServiceCharge)
	s.settings(t, func(in *app.SettingsInput) { in.TaxInclusive = false; in.ServiceChargeBP = 1000 })
	res, err := a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}}, s.staff)
	noErr(t, err)
	t.Logf("after add-on svc=%d", res.Order.Totals.ServiceCharge)
}

// 问题 5：活动类型被编辑后预览结账优惠
func TestReproPromoKindDrift(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	id := s.promo(t, app.PromotionInput{Kind: "free_delivery", AppliesDelivery: true})
	o := s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}})
	pv, err := a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{Mode: "round", Amount: 1})
	noErr(t, err)
	t.Logf("before edit total=%d", pv.Total)
	_, err = a.SavePromotion(ctx, s.branchID, id, app.PromotionInput{Name: "x", Kind: "amount_off", Threshold: 6900,
		Value: 6900, AppliesDelivery: true}, s.staff)
	noErr(t, err)
	pv, err = a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{Mode: "round", Amount: 1})
	noErr(t, err)
	t.Logf("after edit total=%d", pv.Total)
}
```

运行结果（2026-11-24，main 分支 226c364）：

```
问题1  total=30912 dishes=3000 suggested=0
问题2  total=31912 dishes=3000 suggested=0
问题3  discount=3000 lines=6000
问题4  placed svc=1690   after add-on svc=3680
问题5  before edit total=40868   after edit total=39653
```

## 附录 B：参考资料

- Square Orders API：[Order Discounts](https://developer.squareup.com/docs/orders-api/discounts)、[Apply Taxes, Discounts, and Service Charges](https://developer.squareup.com/docs/orders-api/apply-taxes-and-discounts)、[OrderLineItemDiscountScope](https://developer.squareup.com/reference/square/enums/OrderLineItemDiscountScope)
- Toast：[Discounting orders](https://doc.toasttab.com/doc/devguide/apiDiscountingOrders.html)、[Effect of discounts on prices](https://doc.toasttab.com/doc/platformguide/adminDiscountPricing.html)、[Externally-priced orders and discounts](https://doc.toasttab.com/doc/apiordersdraftdoc/devExternallyPricedOrdersandDiscounts.html)
- Shopify：[Discount Function API](https://shopify.dev/docs/api/functions/latest/discount)
- commercetools：[How Cart Discounts work](https://docs.commercetools.com/learning-price-and-discount-your-products/cart-discounts/how-cart-discounts-work)、[Cart Discounts API](https://docs.commercetools.com/api/projects/cartDiscounts)
- Stripe：[Supported currencies（最小货币单位、无小数货币）](https://docs.stripe.com/currencies)
- Modern Treasury：[Accounting for Developers](https://www.moderntreasury.com/resources/ebooks/accounting-for-developers)、[Enforcing immutability in your double-entry ledger](https://www.moderntreasury.com/journal/enforcing-immutability-in-your-double-entry-ledger)
