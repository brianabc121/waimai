# 给 AI 编码助手（Claude、codex 等）的仓库规则

用户用中文交流；文档、提交说明、PR 描述用中文写。

## 铁规则：金额改动必须验算

**凡是新增或改动涉及商品价格、订单金额的功能（菜价、规格、加料、活动、券、结账打折/抹零/改实收、服务费、包装费、配送费、税、加单、退菜、退款、对账、账单、金额流水、币种），都必须按 [docs/PRICING_CHECK.md](docs/PRICING_CHECK.md) 验算，验算全部通过才能合并。这是项目负责人 brian 定的规则，不能出错，没有例外。**

具体要做（详见 PRICING_CHECK.md 第 5 节）：

1. 所有算钱的地方只走 `internal/app/money.go` 的 `loadMoney → choosePromo → priceMoney → saveMoney` 和 `internal/pricing.Price`，不要另写一份金额计算；改单只读订单自己的快照。
2. 在 `internal/pricing/check_test.go` 的 `TestPricingCheckSamples` 加手算样例（数字手算，不许照着程序输出抄），并写进 PRICING_CHECK.md 第 4 节。
3. 在 `internal/app` 写真库场景测试，每一步调用 `checked(t, "步骤", orderID)`；对照第 3 节的场景清单补齐。
4. 引擎加新阶段或新规则时，同步更新 `pricing.Check` 和 PRICING_CHECK.md 第 2 节；改了后端算法，同步 `web/src/dishPromo.ts` 和它的测试。
5. 跑 `make pricing-check`（需要 `TEST_DATABASE_URL`）全部通过，PR 描述里写“金额验算”一节和运行结果。
6. 验算发现**现有**算法有错时，先停下告诉 brian（哪张单、哪条规则、差多少），等他决定；不要改断言或样例数字去凑结果，不要跳过或删掉验算测试。

## 铁规则：余额改动必须重新验算

**凡是新增或改动涉及余额的功能（店铺退款余额、储值“充值送”、平台余额和奖励金、余额账本、对账和账单里的余额部分），都必须按 [docs/BALANCE_CHECK.md](docs/BALANCE_CHECK.md) 重新验算，验算全部通过才能合并。这也是 brian 定的规则，没有例外。**

具体要做（详见 BALANCE_CHECK.md 第 6 节）：

1. 先写清楚记账：哪种凭证、动哪几个账户、各记多少，写进 BALANCE_CHECK.md 第 2 节；新凭证种类要加进规则表 `wallet_journal_rules`。
2. 只用 `postJournal` / `postNew`（数据库函数 `wallet_post_journal`）记账，不直接写账本表；订单派生的凭证用固定幂等键，按钮触发的写操作前端带 `Idempotency-Key`。
3. 在 `internal/app/balance_check_test.go` 加手算样例（数字手算，不许照着程序输出抄），写进 BALANCE_CHECK.md 第 4 节；场景测试每一步 `checked`，最后 `walletClean`。
4. 跑 `make balance-check`（包含 `make pricing-check`）全部通过，PR 描述里写“余额验算”一节和运行结果。
5. 验算或线上告警发现**现有**的账不对时，先停下告诉 brian（哪个账户、哪条规则、差多少），等他决定；不要改断言或样例数字，不要跳过或删掉检查，更正只能加新凭证。

## 其他

- 后端 Go（`internal/`、`cmd/`），前端 Vue 3（`web/`），数据库 PostgreSQL，迁移在 `internal/db/migrations/`。
- 测试需要真库：`TEST_DATABASE_URL=postgres://.../waimai_test`，`go test -count=1 -p 1 ./...`；CI 见 `.github/workflows/ci.yml`。提交前 `gofmt -l .` 为空、`go vet ./...` 通过。
- 设计文档都在 `docs/`，改功能前先读对应的设计文档。
