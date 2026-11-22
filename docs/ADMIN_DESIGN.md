# 平台管理员后台 · 设计方案 v1.0

2026-11-21 · brian 已确认，第 9 节的问题全部按推荐做法

| 批次 | 状态 |
|---|---|
| 第一批：商户、门店、账号、商品、操作记录、只读进入门店 | 已实现 |
| 第二批：Worker 和任务队列 | 已实现 |
| 第三批：API 监控和总览 | 已实现 |

本稿是 [DESIGN.md](DESIGN.md) 的补充，只讲平台管理员（`platform_admin`）这一端。现在的 `/admin` 页面只有“商户列表 + 新建商户”一张表单（`web/src/views/AdminView.vue`，接口 `GET｜POST /api/v1/admin/merchants`）。本稿把它扩成一个完整后台。

效果图在 [admin-screens/](admin-screens/)，可点开的 HTML 原型在 [admin-prototype/](admin-prototype/)。图里的数字都是示意数据。

## 1. 目标和范围

| 你要的功能 | 本稿对应 | 页面 |
|---|---|---|
| 监控 API 请求 | 请求量、错误率、耗时分位、按接口排行、最近错误 | A02 |
| 监控 Worker 运行状态 | Worker 心跳、周期任务、任务队列、失败任务重试 | A03 |
| 商家入驻的增删改查 | 商户和门店的新建、编辑、暂停、删除、店主账号管理 | A04、A05 |
| 商品信息的增删改查 | 跨商户搜索商品，进入任一门店改分类和商品 | A06 |
| （补充）总览 | 一屏看全系统是否正常 | A01 |
| （补充）操作记录 | 管理员做过的每个写操作 | A07 |

不在本稿范围：线上支付、GrabFood、foodpanda 的接入管理，账单和抽成结算，管理员分级（见第 9 节第 5 条）。

## 2. 页面

后台用左侧导航的桌面布局，沿用现有配色和组件样式（`web/src/styles.css`），中英双语。入口仍是 `/admin`，只有 `is_platform_admin` 的账号能进。

| 编号 | 路径 | 页面 | 效果图 |
|---|---|---|---|
| A01 | `/admin` | 总览 | [01-dashboard.png](admin-screens/01-dashboard.png) |
| A02 | `/admin/api` | API 监控 | [02-api.png](admin-screens/02-api.png) |
| A03 | `/admin/worker` | Worker 和任务队列 | [03-worker.png](admin-screens/03-worker.png) |
| A04 | `/admin/merchants` | 商户列表和新建 | [04-merchants.png](admin-screens/04-merchants.png) · [04b-merchant-new.png](admin-screens/04b-merchant-new.png) |
| A05 | `/admin/merchants/:id` | 商户详情（门店、账号） | [05-merchant-detail.png](admin-screens/05-merchant-detail.png) |
| A06 | `/admin/items` | 商品管理 | [06-items.png](admin-screens/06-items.png) |
| A07 | `/admin/audit` | 操作记录 | 无图，普通表格 |

### A01 总览

- 四张健康卡：API、Worker、数据库、任务队列。每张卡显示“正常／注意／异常”和判定依据（规则见 5.3）。
- 当日指标：请求数、错误率、P95 耗时、今日订单数、营业中的门店数。
- 最近一小时请求量小图、最近的 5 条服务端错误、失败任务数。任何一项点进去到对应的明细页。
- 每 10 秒自动刷新（轮询，不占 SSE 连接）。

### A02 API 监控

- 时间范围：最近 1 小时、6 小时、24 小时、7 天。
- 三张图，各自一个纵轴：每分钟请求数（按 2xx、4xx、5xx 分）、错误率、耗时 P50 和 P95。
- 接口排行表：方法、路由模板、请求数、4xx、5xx、P50、P95、最慢一次。可按任一列排序。
- 最近错误表：时间、接口、状态码、错误码、耗时、`request_id`。`request_id` 和日志里的一致，可直接拿去查日志。
- 限流命中次数（`RATE_LIMITED`）单独列出，方便看有没有人在刷接口。
- 当前打开的 SSE 连接数（顾客、门店、骑手各多少）。

### A03 Worker 和任务队列

- **实例：** 每个 Worker 进程一行，显示主机名、启动时间、最后心跳、上一轮循环耗时、是否内嵌在 API 进程里（`EMBED_WORKER`）。超过 20 秒没心跳标为失联。
- **周期任务：** 现有四个，各显示上次运行时间、耗时、结果和最近一次错误。

  | 任务 | 代码 | 周期 |
  |---|---|---|
  | 待接单超时取消 | `ExpirePendingOrders` | 5 秒 |
  | 预约单到点放单 | `ReleaseScheduled` | 5 秒 |
  | 任务队列处理 | `RunJobs` | 5 秒 |
  | 过期数据清理 | `Cleanup` | 10 分钟 |

- **任务队列（`jobs` 表）：** 按类型（`sms`、`menu_import`）统计等待、执行中、失败、24 小时内完成的数量，以及最老一条等待任务等了多久。
- **任务列表：** 按状态和类型筛选。失败的任务可以“重试”（重置次数，立即排队）或“丢弃”。短信任务的手机号打码显示（`+63 917 ••• ••89`），短信内容只在点开详情时显示。

### A04 商户列表

- 搜索（商户名、门店名、链接 slug、联系电话），按状态筛选（营业中、已暂停、已删除）。
- 每行：商户名、门店数、状态、今日订单、近 7 天订单、入驻时间、操作。
- “新建商户”抽屉就是现在的表单（商户、首家门店、店主账号），加联系人和备注。
- 行内操作：编辑、暂停或恢复、删除。

### A05 商户详情

- 基本信息：名称、联系人、联系电话、备注、状态。可编辑。
- 门店：列表加增删改。字段沿用 `branches` 表（名称、英文名、slug、时区、货币、地址、电话）。slug 改了以后旧链接失效，保存前提示。
- 账号：本商户下所有员工账号和角色。管理员可以新建店主、重置密码、停用账号。重置密码会让该账号的所有会话下线。
- “进入门店后台”：见 6.2。
- 近 7 天订单数和营业额小计，只读。

### A06 商品管理

- 顶部先选商户和门店，或者直接跨商户搜索商品名。
- 列表：图片、中英文名、所属门店、分类、价格、上架、售罄、库存、更新时间。
- 增改：用和商家端同一张商品表单（价格、规格组、工位、可配送和可自取等）。分类的增改也在这里。
- 删除：商品是“归档”（`menu_items.archived`），历史订单不受影响，和商家端现在的做法一致。分类只有在没有商品时才能删。
- 批量操作：批量上架、下架、标记售罄。
- Excel 导入已有（商家端），后台里给一个入口跳过去，不重做。

### A07 操作记录

管理员每个写操作一行：时间、管理员、动作、对象、改动前后、来源 IP。只读，不能删。

## 3. 数据模型

新增一个迁移 `00007_admin.sql`。

### 3.1 商户和门店

```sql
ALTER TABLE merchants
    ADD COLUMN status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
    ADD COLUMN contact_name  TEXT NOT NULL DEFAULT '',
    ADD COLUMN contact_phone TEXT NOT NULL DEFAULT '',
    ADD COLUMN note          TEXT NOT NULL DEFAULT '',
    ADD COLUMN updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN deleted_at    TIMESTAMPTZ;
ALTER TABLE branches ADD COLUMN deleted_at TIMESTAMPTZ;
```

- **暂停（suspended）：** 顾客打开店铺链接看到“暂停营业”，不能报价和下单。店员仍能登录，把已经在做的订单做完。和门店自己的“暂停接单”（`branches.paused`）是两回事，商家自己解不开。
- **删除：** 软删除，只打 `deleted_at`。条件是商户已暂停，且没有进行中的订单。删除后店铺链接返回 404，该商户的员工账号全部停用并下线，订单和核款记录保留。slug 继续占用，避免旧二维码指到别的店。30 天内可以在“已删除”里恢复。
- 不做物理删除。订单、现金核款有外键引用，而且是对账依据。

### 3.2 API 指标

```sql
CREATE TABLE api_metrics (
    bucket      TIMESTAMPTZ NOT NULL,          -- 对齐到分钟
    instance    TEXT NOT NULL,                 -- 进程实例，多实例时各写各的
    method      TEXT NOT NULL,
    route       TEXT NOT NULL,                 -- 路由模板，如 /api/v1/merchant/orders/:id
    status      INT  NOT NULL,                 -- 2、3、4、5（状态码百位）
    count       INT  NOT NULL,
    sum_ms      BIGINT NOT NULL,
    max_ms      INT  NOT NULL,
    hist        INT[] NOT NULL,                -- 固定分桶的计数，用来跨实例合并算分位
    PRIMARY KEY (bucket, instance, method, route, status)
);

CREATE TABLE api_errors (
    id          BIGSERIAL PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    request_id  TEXT NOT NULL,
    method      TEXT NOT NULL,
    route       TEXT NOT NULL,
    status      INT  NOT NULL,
    code        TEXT NOT NULL,                 -- 业务错误码，如 INTERNAL、RATE_LIMITED
    duration_ms INT  NOT NULL,
    user_id     BIGINT,                        -- 员工接口才有
    message     TEXT NOT NULL DEFAULT ''       -- 只记 5xx 的内部错误文本，截断到 500 字
);
CREATE INDEX api_errors_at_idx ON api_errors (at DESC);
```

- 采集在一个 Gin 中间件里（`internal/httpapi/metrics.go`），放在 `requestID` 之后。每个请求只在内存里给对应的计数器加一，不写库。
- 每分钟由一个后台协程把上一分钟的计数批量写入 `api_metrics`。写库失败只记日志，不影响请求。进程退出时补写最后一段。
- 耗时分桶固定为 5、10、25、50、100、250、500、1000、2500、5000 毫秒和以上，共 11 格。P50 和 P95 从合并后的分桶估算，精度够看趋势。
- 路由用模板（`c.FullPath()`），不带订单号和令牌，基数小，也不泄露个人信息。匹配不到路由的请求统一记成 `unmatched`。
- SSE 长连接（`…/events`）不计入耗时，只统计当前连接数，否则 P95 永远是几分钟。
- `/healthz`、`/readyz` 和静态文件不计。
- `api_errors` 记所有 5xx，以及 429。普通 4xx（校验失败、未登录等）只进计数，不逐条记。
- 保留期：`api_metrics` 7 天，`api_errors` 7 天，由 Worker 的 `Cleanup` 清理。按现在的接口数量（约 90 条路由），一天最多十几万行，7 天在百万行量级，PostgreSQL 足够。

### 3.3 Worker 状态

```sql
CREATE TABLE worker_instances (
    instance      TEXT PRIMARY KEY,            -- 启动时生成的随机 ID
    hostname      TEXT NOT NULL,
    embedded      BOOLEAN NOT NULL,            -- 是否内嵌在 API 进程
    started_at    TIMESTAMPTZ NOT NULL,
    last_beat_at  TIMESTAMPTZ NOT NULL,
    last_loop_ms  INT NOT NULL DEFAULT 0
);

CREATE TABLE worker_tasks (
    task             TEXT PRIMARY KEY,         -- expire_pending、release_scheduled、jobs、rider_trips、cleanup
    last_started_at  TIMESTAMPTZ,
    last_ok_at       TIMESTAMPTZ,
    last_duration_ms INT NOT NULL DEFAULT 0,
    last_count       INT NOT NULL DEFAULT 0,   -- 这一轮处理了几条，文字由页面按语言拼
    last_error       TEXT NOT NULL DEFAULT '',
    last_error_at    TIMESTAMPTZ,
    runs             BIGINT NOT NULL DEFAULT 0,
    failures         BIGINT NOT NULL DEFAULT 0
);
```

- `RunWorker`（现在在 `internal/app/menu.go`，顺手挪到新文件 `internal/app/worker.go`）每轮循环结束时更新自己的心跳，每个周期任务跑完更新一行 `worker_tasks`。
- 超过 1 天没心跳的实例行由 `Cleanup` 删除。
- 任务队列不加表，直接查现有的 `jobs`。“执行中”就是 `status='pending' AND locked_until > now()`。加一个索引方便按状态翻页：`CREATE INDEX jobs_status_idx ON jobs (status, id DESC)`。
- 给 `jobs` 加一个 `'discarded'` 状态用于“丢弃”，保留记录，14 天后随现有清理规则删除。

### 3.4 操作记录

现有的 `audit_logs` 要求有 `branch_id`，是订单级的。管理员操作另建一张：

```sql
CREATE TABLE admin_audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    admin_id     BIGINT NOT NULL REFERENCES users(id),
    action       TEXT NOT NULL,                -- merchant.create、merchant.suspend、item.update、job.retry …
    target_type  TEXT NOT NULL,
    target_id    BIGINT,
    detail       JSONB NOT NULL DEFAULT '{}',  -- 改动前后，不含密码
    ip           TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX admin_audit_logs_created_idx ON admin_audit_logs (created_at DESC);
```

审计行和业务改动在同一个事务里写。

### 3.5 实现时的调整（第一批）

写代码时有几处和上面的草案不同，以这里为准：

- **删除商户不改账号的启用状态。** 被删商户和被删门店的角色在登录时直接不可见，已有会话当场清掉。这样恢复商户时员工原样回来，不用记谁原来是停用的。
- **暂停原因和时间单独存。** `merchants` 多了 `suspend_reason`、`suspended_at`，列表里能看到。
- **最近登录。** `users` 多了 `last_login_at`，登录时更新。
- **分类也是归档。** 已归档的商品仍然引用分类，所以分类加了 `archived` 列，删除分类是打标记。
- **商品改动的操作记录。** 商户、门店、账号的操作记录和业务改动在同一个事务里。商品和分类的保存复用商家端的用例，操作记录在保存成功后紧接着写，不在同一个事务里。批量操作和删除分类仍在同一个事务里。
- **新增门店时**，该商户现有的店主自动成为新门店的店主。
- **只读进入门店** 时，平台管理员的写请求返回 `403 READ_ONLY`。

### 3.6 实现时的调整（第二批）

- **周期任务存数量不存文字。** `worker_tasks.last_count` 记这一轮处理了几条，“取消 2 单”这样的文字由页面按中英文拼。
- **正常退出的 Worker 会删掉自己的心跳行**，所以重启或发版之后不会留下一条“失联”。只有崩溃或被强杀的实例才显示失联，1 天后清理。
- **一个 Worker 都不在线时**，页面顶部有红色提示。
- **任务详情里的短信内容会隐去订单链接**（`/o/…`），因为拿到链接就能看到顾客的订单。列表里只有打码的手机号。
- **重试** 适用于失败和已丢弃的任务，次数从 0 重新算。**丢弃** 适用于失败和等待中的任务，执行中的不能丢弃。“全部重试”可以按类型筛选。三个动作都写操作记录。
- **侧栏的失败数** 每分钟刷新一次，Worker 页面本身每 10 秒刷新，浏览器标签页不在前台时暂停。
- **演示数据** 里带两条失败的短信任务，方便试重试和丢弃。正式环境不跑 seed，不会有。

### 3.7 实现时的调整（第三批）

- **每 10 秒写一次库，不是每分钟。** 每个 API 进程把这 10 秒的计数加到当前分钟的那一行上（同一行累加），所以总览和 API 监控最多晚 10 秒。进程退出时补写最后一段。
- **多了 `api_instances` 表。** 每个 API 进程一行，记心跳和它当前持有的 SSE 连接数（顾客、桌台、门店、骑手）。多个进程时页面显示的是各进程之和，30 秒没有心跳的不算。
- **`api_metrics` 多了 `limited` 列**，单独数 429。限流命中次数从这里来，逐条的 429 记录每 10 秒最多留 200 条，被刷接口时不会把表撑大。
- **panic 也记成错误**，错误码是 `PANIC`。
- **监控页面自己的轮询不计入。** `/admin/overview`、`/admin/metrics/*`、`/admin/worker` 每 10 秒被页面请求一次，算进去会让没有顾客的平台看起来很忙。其他后台接口照常统计。
- **总览的“当日指标”改成近 24 小时。** 平台没有统一时区，“今天”对不同门店不是同一段时间。订单数仍按各门店当地的今天。
- **时间粒度。** 1 小时每点 1 分钟，6 小时每点 5 分钟，24 小时每点 15 分钟，7 天每点 2 小时。请求量统一换算成每分钟。
- **图表是手写的 SVG**，没有引入图表库。鼠标悬停显示该时刻所有序列的数值，键盘左右键也能移动，另有一份给读屏软件的表格。
- 健康判定的阈值在 `internal/app/apistats.go` 开头的常量里。

## 4. 接口

都在 `/api/v1/admin` 下，沿用现有的 `requireUser` 加 `requireAdmin` 中间件和统一错误格式。列表接口统一 `?q=&status=&page=&page_size=`，返回 `{items, total}`。

| 分组 | 接口 |
|---|---|
| 总览 | `GET /admin/overview` |
| API 监控 | `GET /admin/metrics/api?range=1h｜6h｜24h｜7d`（时间序列加接口排行） · `GET /admin/metrics/errors?route=&status=` |
| Worker | `GET /admin/worker`（实例、周期任务、队列统计） · `GET /admin/jobs?status=&kind=` · `GET /admin/jobs/{id}` · `POST /admin/jobs/{id}/retry` · `POST /admin/jobs/{id}/discard` · `POST /admin/jobs/retry-failed` |
| 商户 | `GET /admin/merchants` · `POST /admin/merchants` · `GET /admin/merchants/{id}` · `PUT /admin/merchants/{id}` · `POST /admin/merchants/{id}/suspend｜resume｜restore` · `DELETE /admin/merchants/{id}` |
| 门店 | `GET /admin/branches`（全部门店，给选择框用） · `GET /admin/branches/{bid}` · `POST /admin/merchants/{id}/branches` · `PUT /admin/branches/{bid}` · `DELETE /admin/branches/{bid}` |
| 账号 | 账号列表在 `GET /admin/merchants/{id}` 里 · `POST /admin/branches/{bid}/users` · `PATCH /admin/users/{id}`（启用、停用） · `POST /admin/users/{id}/reset-password` |
| 商品 | `GET /admin/items?q=&merchant_id=&branch_id=&status=`（跨商户搜索） · `GET /admin/branches/{bid}/menu` · `POST｜PUT /admin/branches/{bid}/categories[/{id}]` · `DELETE /admin/branches/{bid}/categories/{id}` · `POST｜PUT｜PATCH /admin/branches/{bid}/items[/{id}]` · `POST /admin/branches/{bid}/items/bulk` |
| 操作记录 | `GET /admin/audit?admin_id=&action=&from=&to=` |

`GET｜POST /admin/merchants` 现在已有，返回结构会变（加状态、统计、分页），前端一起改。

商品接口不重写业务逻辑。`internal/app/menu.go` 里的 `AdminMenu`、`SaveCategory`、`SaveItem`、`PatchItem` 本来就只按 `branchID` 工作，权限在 HTTP 层判断。后台的处理函数直接调同一批用例，只是把“校验该用户在门店的角色”换成“校验平台管理员”，并多写一条操作记录。

## 5. 监控口径

### 5.1 API

| 指标 | 算法 |
|---|---|
| 请求量 | 每分钟 `count` 之和，按状态码百位分色 |
| 错误率 | 5xx 数 ÷ 总数。4xx 另列，不算进错误率（多数是顾客输错或未登录） |
| P50、P95 | 合并各实例的 `hist` 后按分桶线性插值 |
| 慢接口 | 所选时间范围内 P95 最高的 10 条路由 |

### 5.2 Worker

| 指标 | 算法 |
|---|---|
| 在线 | `last_beat_at` 距今不超过 20 秒（循环周期 5 秒，容忍 3 轮） |
| 队列积压 | `jobs` 里到期未执行的条数，和其中最老一条的等待时间 |
| 失败任务 | `status='failed'` 的条数（重试 5 次仍失败） |

### 5.3 健康判定（总览的四张卡）

| 卡 | 正常 | 注意 | 异常 |
|---|---|---|---|
| API | 近 5 分钟 5xx 低于 1% 且 P95 低于 1 秒 | 5xx 在 1% 到 5% 之间，或 P95 在 1 到 3 秒之间 | 5xx 超过 5%，或 P95 超过 3 秒 |
| Worker | 至少一个实例在线，四个周期任务最近一次都成功 | 有任务最近一次失败 | 没有实例在线 |
| 数据库 | `Ping` 成功且连接池有空闲 | 连接池用满 | `Ping` 失败 |
| 任务队列 | 最老等待不到 1 分钟，没有失败任务 | 最老等待 1 到 10 分钟，或有失败任务 | 最老等待超过 10 分钟 |

阈值先写成常量，集中在 `internal/app/apistats.go` 一处，方便调。

**本期只做“看”，不做“推送告警”。** 管理员打开后台才看得到异常。要不要加短信或邮件告警见第 9 节第 2 条。

## 6. 权限和安全

### 6.1 基本规则

- 所有 `/admin` 接口要求 `is_platform_admin`，非管理员返回 403。前端路由同样拦一层。
- 管理员账号仍然只能用命令行创建（`cmd/admin`），后台里不提供“新建管理员”。
- 写操作沿用现有的 `X-Requested-With` 请求头校验。删除商户要求手输商户名确认。
- 重置密码时由系统生成一次性的随机密码，只在响应里显示一次，不进日志和操作记录。
- 监控数据里不放手机号、地址、令牌。短信任务的手机号打码。
- 管理员登录沿用登录限流（每 IP 10 次）。

### 6.2 进入门店后台

排查问题时，管理员常要看商家看到的界面。两种做法：

| 做法 | 说明 |
|---|---|
| **只读查看（推荐）** | 管理员可以打开任一门店的商家后台页面，所有 `GET` 接口放行，写接口仍然拒绝。要改商品就回 A06 改，会留操作记录 |
| 完全代管 | 管理员在任一门店等同店主，能接单、改设置、核款 |

推荐只读。接单、核款这些动作涉及钱和履约，由平台代做容易说不清责任。实现上是在 `allowed()`（`internal/httpapi/server.go`）里对平台管理员的 `GET` 请求放行。

## 7. 前端结构

```
web/src/views/admin/
  AdminLayout.vue          左侧导航、顶栏、语言切换
  AdminDashboard.vue       A01
  AdminApi.vue             A02
  AdminWorker.vue          A03
  AdminMerchants.vue       A04（含新建抽屉）
  AdminMerchantDetail.vue  A05
  AdminItems.vue           A06（商品表单复用 MenuView 里拆出来的 ItemForm.vue）
  AdminAudit.vue           A07
  charts/LineChart.vue     手写 SVG 折线图和柱图，不引入图表库
```

- 现在的 `AdminView.vue` 删掉，表单并入 `AdminMerchants.vue`。
- 图表只有折线和堆叠柱两种，自己用 SVG 画，约 150 行，不增加依赖。带悬停提示。
- 监控页每 10 秒轮询一次，页面切到后台标签时暂停。
- 文案加进 `web/src/i18n.ts`，中英文都写。

## 8. 落地计划

分三个 PR，每个都能单独上线，测试通过后合并到 main。

| 批次 | 内容 | 验证 |
|---|---|---|
| 第一批 | 后台框架和导航、商户和门店增删改查、账号管理、商品管理、操作记录、只读进入门店 | Go 测试覆盖暂停后不能下单、删除条件、跨商户改商品、非管理员 403。新增一条浏览器端到端脚本 `e2e/admin.cjs` |
| 第二批 | Worker 心跳、周期任务状态、任务队列页面、失败任务重试和丢弃 | 测试覆盖心跳失联判定、重试后任务被重新领取、丢弃后不再执行 |
| 第三批 | API 指标中间件、分钟汇总入库、API 监控页面、总览页面和健康判定 | 测试覆盖分桶和分位估算、多实例合并、SSE 不计入、保留期清理。压一轮请求确认中间件开销可以忽略 |

监控放在后面是因为它依赖新表里攒出数据才有东西看，而商户和商品管理马上就能用。如果你更急着要监控，顺序可以倒过来。

## 9. 需要你确认的地方

每条都给了推荐做法。不回复的话按推荐做。

| # | 问题 | 推荐 |
|---|---|---|
| 1 | 监控数据存哪里 | 存在自己的 PostgreSQL 里，后台直接出图，不用另外装 Prometheus 和 Grafana。以后流量大了再加 `/metrics` 接口对接外部监控 |
| 2 | 要不要主动告警（短信或邮件） | 本期不做，先把页面做出来。告警需要短信账号，现在真实短信账号还没验证过 |
| 3 | 删除商户 | 软删除，30 天内可恢复，订单和核款记录永久保留，slug 不释放 |
| 4 | 管理员进入门店后台 | 只读（见 6.2） |
| 5 | 管理员要不要分级（比如只能看监控的运维、只能管商户的运营） | 本期不分，所有管理员权限相同。人多了再加 |
| 6 | 暂停商户后店员能不能登录 | 能登录，把在途订单做完，但顾客不能再下新单 |
| 7 | 指标保留多久 | 分钟级指标和错误明细都保留 7 天 |
| 8 | 落地顺序 | 先商户和商品管理，再 Worker，最后 API 监控（见第 8 节） |
