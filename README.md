# waimai · 外卖点餐系统

面向海外华人餐厅的点餐系统，覆盖自营外卖、自取和堂食。外卖闭环：顾客点餐下单，商家接单，后厨制作和打包，骑手配送和代收现金，门店核款。堂食闭环：店员开台，顾客扫桌码或用桌上平板点菜和加菜，后厨按工作台制作，上桌，柜台收款关台。

设计依据是 [docs/DESIGN.md](docs/DESIGN.md)。部署到服务器见 [docs/DEPLOY.md](docs/DEPLOY.md)。 配送地图（商家画配送区、顾客地图定位）的设计见 [docs/MAP_ZONES_DESIGN.md](docs/MAP_ZONES_DESIGN.md)，需要 Google 地图密钥，见部署文档第 13 节。

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

顾客和商家的聊天默认是关的，要另外启动 OpenIM，做法见 `docs/DEPLOY.md` 第 19 节。

用 Docker Compose 启动全部服务：

```bash
# 注意：根目录有 .env 时 Compose 会读取它，里面的 DATABASE_URL 不能是 localhost
docker compose up --build -d
docker compose run --rm seed    # 可选：载入演示数据
# 打开 http://localhost:8000
```

## 入口和登录方式

正式部署用两个域名（见 `docs/DEPLOY.md` 第 20 节）：主域名放企业官网，`order.` 子域名放点餐系统（商城、点餐、商家后台、员工工作台、平台后台和接口）。下表的路径都接在对应域名后面；本地开发时点餐系统是 `http://localhost:8080`（`make run`）或 `http://localhost:8000`（Docker Compose），官网是 `http://localhost:4321`（`cd site && npm run dev`）或 `http://localhost:8001`（Docker Compose）。

| 入口 | 网址 | 谁用 | 怎么登录 | 演示账号（`make seed` 后，密码都是 `demo1234`） |
|---|---|---|---|---|
| 企业官网 | 主域名 `/`（按浏览器语言跳到 `/zh/`、`/zh-Hant/`、`/en/`） | 想了解和入驻的餐厅老板 | 不用登录。“预约演示”表单进平台后台“顾客服务 → 入驻申请”；页面上的“登录”链接到点餐系统的 `/login` | 无 |
| 外卖商城首页 | `order.` 子域名 `/` | 顾客 | 逛店不用登录 | 任意 `+63` 手机号，见下方“短信验证码” |
| 门店点餐页 | `/s/门店标识`，例如 `/s/makati`、`/s/bgc` | 顾客 | 看菜单、加购物车不用登录；点“去结算”时没登录会跳到 `/account/login`，用手机短信验证码登录（只有验证码，没有密码），登录后回到原页面 | 同上 |
| 顾客“我的”、订单 | `/me`、`/orders`，订单详情 `/o/订单令牌` | 顾客 | 同上，`/account/login` 短信验证码，登录 30 天有效。订单详情链接自带令牌，不登录也能看 | 同上 |
| 堂食扫码点菜 | `/t/桌码`（桌台页每张桌的“桌码”里有链接和二维码） | 堂食顾客 | 不用登录，桌子要先开台 | 无 |
| 堂食平板点菜 | `/ipad` | 桌上的平板 | 不用账号：店员在桌台页 `/merchant/tables` 生成配对码，平板上输入 | 用 `owner1` 或 `cashier1` 生成配对码 |
| 商家后台 | `/login` 选“商家后台”标签，进入 `/merchant/orders` | 店主、经理、接单员 | 用户名加密码。账号在多家店或多个身份时，登录后到 `/login/pick` 选身份；侧栏“切换身份”可以换到员工工作台。从工作台切回后台要再输一次密码。只有岗位、没有后台身份的员工登不进后台，会被引到工作台 | 店主 `owner1`（马卡蒂小馆）、`owner2`（BGC 面馆，验证商户隔离）、`owner3` 到 `owner9`（商城里的其他演示店）；接单员 `cashier1`。演示数据里没有经理账号，用 `owner1` 在 `/merchant/staff` 新建一个角色选“经理”即可 |
| 员工工作台 | `/login` 选“员工工作台”标签，进入 `/work` | 有岗位的员工（后厨工位、打包、骑手），以及有岗位的店主、经理、接单员 | 用户名加密码，和商家后台同一个登录页 | `kitchen1`（后厨各工位和打包）、`zhang1`（炸台、热厨）、`wang1`（打包台加骑手，可切换岗位）、`rider1`、`rider2`（只有骑手） |
| 后厨大屏 | `/kitchen` | 后厨 | 算员工工作台页面，用工作台登录后打开 | `kitchen1` |
| 员工工作台 App（安卓） | App 打开的是这套部署的 `/work` | 员工，主要是骑手（锁屏推送、后台定位） | App 里的登录页只有“员工工作台”，用户名加密码。打包见 `mobile/README.md` 和 `docs/DEPLOY.md` 第 15 节 | 同员工工作台 |
| 平台管理后台 | `/admin/login`，进入 `/admin/dashboard` | 平台管理员 | 单独的登录页和单独的 Cookie，`/login` 登不进平台后台，平台账号也登不进商家后台 | `admin`。生产环境不要跑 seed，用 `cmd/admin` 创建：`docker compose run --rm -e ADMIN_PASSWORD='强密码' admin`（见 `docs/DEPLOY.md` 第 4 节） |

商家后台常用页面（都用 `/login` 的商家后台身份）：订单台 `/merchant/orders`、代客下单 `/merchant/orders/new`、桌台 `/merchant/tables`、菜单 `/merchant/menu`、员工 `/merchant/staff`、角色权限 `/merchant/staff/roles`（只有店主能打开）、骑手状态 `/merchant/riders`、模板 `/merchant/templates`。接单员能看到哪些页面由店主在“角色权限”里配置，见 `docs/STAFF_ACCESS_DESIGN.md`。

### 短信验证码

顾客登录只用短信验证码，验证码 5 分钟有效，同一个手机号一天最多发 10 次。登录页的手机号目前固定 `+63`（菲律宾，9 开头 10 位，例如 `917 555 0123`）。

- **开发和演示环境**：在 `.env` 里设 `OTP_DEV_ECHO=true`（`.env.example` 默认就是），验证码会直接显示在登录页手机号下面（“演示模式验证码”），并自动填进输入框，点登录即可。任何符合格式的号码都能用。
- **没开 `OTP_DEV_ECHO` 又没配短信通道时**（`SMS_PROVIDER=log`），服务端日志只记一行“message for phone ending xxxx not sent”，不写验证码本身，所以拿不到验证码，开发时一定要开 `OTP_DEV_ECHO`。
- **生产环境**：必须 `OTP_DEV_ECHO=false`，并用 `SMS_PROVIDER` 选短信服务商（`twilio`，或 `webhook` 接本地短信网关，见 `.env.example`；也可以在平台后台 `/admin/settings` 填密钥），验证码发到顾客手机上。

### 登录限制

- 员工和管理员登录每个地址每分钟最多 10 次，超过会提示稍后再试。
- 商家后台、员工工作台的登录 14 天有效；顾客登录 30 天有效。
- 经理授权码（接单员没权限取消订单或售后时请经理输入的 6 位码）由店主、经理在后台自己设置，演示数据里没有预设。

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

`e2e/responsive.cjs` 在电脑（1440）、竖放的平板（820）和手机（390）三种宽度下把顾客端、商家端、平台后台和员工工作台各走一遍：页面不能横向滚动，导航和表格要换成对应的形态。

## 三种屏幕

所有页面都按屏幕宽度自动换布局，不需要单独的地址：

| | 手机（小于 768） | 平板（768 到 1023） | 电脑（1024 以上） |
|---|---|---|---|
| 顾客点餐 | 单列，底部购物车条，选规格从底部弹出 | 菜品多列，底部购物车条，选规格是居中的对话框 | 菜品多列，购物车常驻在菜单右边 |
| 结算、订单 | 单列 | 900 以上分两栏 | 表单在左，明细和下单按钮在右 |
| 商家端、平台后台 | 顶栏加抽屉导航，表格变成一行一张卡片 | 顶栏加抽屉导航，表格 | 左侧导航栏（可收起），表格 |
| 员工工作台 | 单列，底部三个标签 | 内容居中，标签浮在底部 | 同平板 |

断点写在 `web/src/styles.css`；导航的收放在 `web/src/shell.ts`；手机上表格卡片的列名由 `web/src/tables.ts` 从表头带过去。画配送区需要平板或电脑，手机上只能查看和改名称、金额。

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
