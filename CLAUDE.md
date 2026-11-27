@AGENTS.md

# Claude 补充

上面的 AGENTS.md 是本仓库的规则，Claude 和 codex 共用。最重要的一条再说一遍：

**涉及商品价格、订单金额的任何改动，都必须按 docs/PRICING_CHECK.md 验算（加手算样例、场景测试每步 `checked`、`make pricing-check` 全过、PR 写“金额验算”一节），通过才能合并；验算发现现有算法有错，先告诉 brian。**

**涉及余额（店铺余额、储值、平台余额、余额账本）的任何改动，都必须按 docs/BALANCE_CHECK.md 重新验算（加手算样例、`make balance-check` 全过、PR 写“余额验算”一节）；发现现有的账不对，先告诉 brian。**
