# waimai · 外卖点餐系统

面向海外华人餐厅的点餐系统，覆盖自营外卖、自取和堂食。外卖闭环：顾客点餐下单，商家接单，后厨制作和打包，骑手配送和代收现金，门店核款。堂食闭环：店员开台，顾客扫桌码或用桌上平板点菜和加菜，后厨按工作台制作，上桌，柜台收款关台。

设计依据是 [docs/DESIGN.md](docs/DESIGN.md)。部署到服务器见 [docs/DEPLOY.md](docs/DEPLOY.md)。

## 技术栈

- 后端：Go、Gin、PostgreSQL（pgx）、Goose 迁移。`cmd/api` 提供接口和 SSE，`cmd/worker` 跑后台任务。
- 前端：Vue 3、TypeScript、Vite，中英双语。
- 部署：Docker Compose 加 Nginx。

## 本地运行

需要 Go 1.24、Node 22 和一个 PostgreSQL 16。

```bash
cp .env.example .env            # 按需修改
export DATABASE_URL='postgres://USER:PASS@localhost:5432/waimai?sslmode=disable'

make migrate                    # 执行数据库迁移
make seed                       # 载入演示数据，可重复执行
make web                        # 构建前端
make run                        # 单进程启动：接口、后台任务和前端都在 :8080
```

前端开发时可以另开一个终端运行 `cd web && npm run dev`，Vite 会把 `/api` 代理到 `:8080`。

用 Docker Compose 启动全部服务：

```bash
# 注意：根目录有 .env 时 Compose 会读取它，里面的 DATABASE_URL 不能是 localhost
docker compose up --build -d
docker compose run --rm seed    # 可选：载入演示数据
# 打开 http://localhost:8000
```

## 演示账号

`make seed` 之后所有账号的密码都是 `demo1234`。

| 入口 | 地址 | 账号 |
|---|---|---|
| 顾客点餐 | `/s/makati` | 不需要账号，下单前验证手机号 |
| 商家订单台 | `/login` | `cashier1` 或 `owner1` |
| 模板（各入口的布局和主题色） | `/merchant/templates` | `owner1` |
| 桌台（开台、账单、收款） | `/merchant/tables` | `cashier1` 或 `owner1` |
| 堂食扫码点菜 | 桌台页每张桌的“桌码”里有链接和二维码 | 不需要账号，要先开台 |
| 堂食平板点菜 | `/ipad` | 在桌台页生成配对码后输入 |
| 后厨、打包和上菜 | `/login` | `kitchen1` |
| 骑手 | `/login` | `rider1`、`rider2` |
| 平台管理后台（商户、商品、Worker 和任务队列、操作记录，见 docs/ADMIN_DESIGN.md） | `/login` | `admin` |
| 第二个商户（验证隔离） | `/login` | `owner2`，门店 `/s/bgc` |

开发时设置 `OTP_DEV_ECHO=true`，验证码会直接显示在结账页。生产环境必须关闭，并用 `SMS_PROVIDER` 选择短信服务商（`twilio`，或用 `webhook` 接本地短信网关，见 `.env.example`）。

## 测试

后端测试需要一个真实的空 PostgreSQL 数据库，不设置时数据库相关测试会跳过。

```bash
export TEST_DATABASE_URL='postgres://USER:PASS@localhost:5432/waimai_test?sslmode=disable'
make test
```

浏览器端到端流程（需要已启动服务、已载入演示数据、开启 `OTP_DEV_ECHO`，并安装 Playwright）：

```bash
BASE_URL=http://localhost:8080 make e2e
```

对着自签名证书的 https 地址跑时加上 `E2E_INSECURE_TLS=1`。

`make e2e` 先跑外卖流程，再跑堂食演示（08 桌：手机点炸物，平板加饮料，炸台和饮品台各自备好，上桌，模拟回应丢失后重试，收款关台，下一桌从空账单开始），最后验证模板的草稿、预览和分入口发布。

## 目录

```
cmd/api  cmd/worker  cmd/migrate  cmd/seed  cmd/admin
internal/app        业务用例和规则
internal/httpapi    路由、权限、SSE、限流
internal/db         连接池和迁移 SQL
web/                Vue 前端
deploy/             Nginx 配置和前端镜像
docs/DESIGN.md      设计稿
docs/reference/     Codex 原始资料
e2e/                浏览器端到端脚本
```

## 上线前必须处理

- 把 `APP_SECRET` 换成随机值，`COOKIE_SECURE=true`，`OTP_DEV_ECHO=false`。
- 设置 `SMS_PROVIDER` 和对应凭证，并用真实手机号实测一次。默认的 `log` 只写日志，不会真的发短信。Twilio 和 webhook 两种实现目前只对着模拟服务测试过，没有用真实账号发过短信。
- 设置 `PUBLIC_BASE_URL`，否则状态短信里没有订单链接。
- 在 Nginx 上配置 HTTPS 并启用 HTTP/2。
- 数据库开启自动备份和时间点恢复，并做一次恢复演练。
- 如果数据库地址经过事务级连接池，必须设置 `DIRECT_DATABASE_URL` 为直连地址，见部署教程第 10 节。
