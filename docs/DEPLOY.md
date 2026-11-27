# 部署教程

这份教程把系统部署到一台 Linux 服务器上，用 Docker Compose 运行，前面用 Caddy 提供 HTTPS。适合一家或几家门店的试点。

> 实测情况（2026-11-21）：这份教程在一台装了 Docker 29 和 Compose v5 的 Linux 机器上从头走过一遍。镜像构建、迁移、Compose 启动、创建管理员、Caddy 的 HTTPS 和 HTTP/2、Nginx 转发和实时推送、短信 webhook、重启恢复、备份和恢复、托管数据库的接法（TLS 加事务级连接池，见第 10 节）都验证过，外卖、堂食、模板三条浏览器流程也在这套环境上全部通过。没有验证的部分：真实域名的证书签发（测试用的是 Caddy 给 `localhost` 签的本地证书）、Twilio 真实账号、具体某家云厂商的托管数据库。

## 1. 整体结构

```
浏览器 ──HTTPS──> Caddy（宿主机，自动证书，HTTP/2）
                    └─> web 容器（Nginx：前端静态文件，/api 转发）
                          └─> api 容器（Gin 接口和 SSE）
                    worker 容器（后台任务：接单超时、短信、菜单导入、预约单放行）
                    postgres 容器（或托管 PostgreSQL）
```

`api`、`worker`、`migrate`、`seed`、`admin` 是同一个镜像的不同命令。每次启动时 `migrate` 先执行数据库迁移，成功后 `api` 和 `worker` 才启动。

## 2. 准备

- 一台 Linux 服务器。试点规模 2 核 CPU、2 GB 内存、20 GB 硬盘够用。
- 一个域名（下面用 `order.example.com` 举例），A 记录指向服务器。
- 防火墙放行 80 和 443，其他端口不对外。
- 服务器上装好 Git、Docker Engine 和 Compose 插件（`docker compose version` 能输出版本）。
- 顾客下单要验证手机号，所以正式营业前需要一个短信渠道，见第 6 节。

## 3. 获取代码并配置

```bash
git clone https://github.com/brianabc121/waimai.git
cd waimai
cp deploy/env.production.example .env
chmod 600 .env
```

编辑 `.env`，把标了 `CHANGE` 的值都换掉：

| 变量 | 填什么 |
|---|---|
| `POSTGRES_PASSWORD` | `openssl rand -hex 24` 生成。只在数据库第一次创建时生效 |
| `DATABASE_URL` | 把里面的 `CHANGE` 换成同一个数据库密码 |
| `APP_SECRET` | `openssl rand -hex 32` 生成。上线后不要再改，改了会让已发出的订单链接失效，后台“密钥设置”页保存的密钥也会读不出来，要重填 |
| `PUBLIC_BASE_URL` | 你的地址，例如 `https://order.example.com`，结尾不带斜杠 |
| `COOKIE_SECURE` | 保持 `true` |
| `OTP_DEV_ECHO` | 保持 `false`。设成 `true` 会把验证码直接显示在页面上，只能用于演示 |
| `HTTP_PORT` | 保持 `127.0.0.1:8000`，只让本机的 Caddy 访问 |

地图、推送、短信这些外部服务的密钥不用写在 `.env` 里。系统跑起来以后，用平台管理员登录后台，在“密钥设置”页填写，保存后马上生效，见第 17 节。`.env` 里同名的变量仍然有效，作为后台没填时的兜底。

注意：不要把仓库根目录的 `.env.example` 直接用于 Compose。它是给本机开发用的，里面的数据库地址是 `localhost`，在容器里连不上。

## 4. 启动

```bash
docker compose up -d --build
docker compose ps
```

正常情况下 `postgres`、`api`、`worker`、`web` 是 running，`migrate` 是 exited (0)。确认接口和数据库都通：

```bash
docker compose exec api wget -qO- http://localhost:8080/readyz   # 应返回 {"ok":true}
curl -I http://127.0.0.1:8000/                                    # 应返回 200
```

创建平台管理员（用来创建商户）。密码至少 10 位，再次执行同一条命令可以重置密码：

```bash
docker compose run --rm -e ADMIN_PASSWORD='换成你的强密码' admin
```

默认用户名是 `admin`，要换的话加 `-e ADMIN_USERNAME=名字`。

想先看演示数据，可以执行 `docker compose run --rm seed`。它会创建密码都是 `demo1234` 的演示账号，其中包括把 `admin` 的密码也设成 `demo1234`。**正式环境不要执行**。如果已经执行过，请重新运行上面的 `admin` 命令改密码，并在后台停用演示账号。

## 5. 配置 HTTPS

在宿主机安装 Caddy（见 Caddy 官方安装文档），把 `/etc/caddy/Caddyfile` 写成：

```
order.example.com {
    reverse_proxy 127.0.0.1:8000 {
        flush_interval -1
    }
}
```

```bash
sudo systemctl reload caddy
```

Caddy 会自动申请和续期证书，并启用 HTTP/2。HTTP/2 是必要的：每个打开的页面会保持一条实时推送连接，HTTP/1.1 下浏览器对同一域名最多 6 条连接，多开几个页面就会卡住。`flush_interval -1` 让推送不被缓冲。

容器里的 Nginx 已配置为信任来自本机和内网代理的 `X-Forwarded-For`，所以限流看到的是顾客的真实地址。如果你用的是别的代理或云负载均衡，要确认它会带上这个请求头。

### 5.1 店铺专属页面（每家店自己的网址）

每家店有一个只看得到本店的专属页面，网址是“店铺号.主域名”，例如 `https://10001.example.com`；商家也可以绑定自己的域名（docs/ORDER_TYPES_DESIGN.md 第 5 节）。要开通：

1. 在域名商那里加一条通配记录：`*.example.com` 的 A 记录指向服务器（和 `order.example.com` 同一台）。
2. Caddyfile 再加一段。每个店铺网址、商家自己的域名第一次被打开时，Caddy 才去签证书；签之前先问系统这个网址是不是真有这家店，不是就拒绝，防止被人拿乱七八糟的域名刷证书：

```
{
    on_demand_tls {
        ask http://127.0.0.1:8000/internal/tls-ask
    }
}

https:// {
    tls {
        on_demand
    }
    reverse_proxy 127.0.0.1:8000 {
        flush_interval -1
    }
}
```

   `https://` 这一段接住所有其他网址（店铺号子域名和商家自己的域名），`order.example.com` 那一段保持不变。改完 `sudo systemctl reload caddy`。

3. 平台后台“密钥设置”页的“店铺专属页面”一栏，“店铺网址的主域名”填 `example.com`（不带 `order.`），保存后马上生效。也可以在 `.env` 里设 `STORE_SITE_DOMAIN=example.com`。

商家在后台“店铺装修 → 店铺链接”看到自己的专属网址和二维码。商家绑定自己的域名时，按页面提示到域名商那里加一条 CNAME 指向 `店铺号.example.com`；系统每几分钟检查一次，指对了就自动生效，Caddy 随后自动签证书。商家申请代买的域名在平台后台“域名申请”里处理：买好后把它的 CNAME 指向表里的“指向”，再把状态改成“已生效”。

没设主域名时，商家的店铺链接还是老的 `/s/店铺` 链接，打开后进入商城里的本店。本机开发不用配：`http://10001.localhost:8080` 就是店铺号 10001 的专属页面。

打开 `https://order.example.com/admin/login`（平台管理员有自己单独的登录页，商家和员工的 `/login` 登不进平台后台），用平台管理员登录。

## 6. 短信

在平台后台“密钥设置”页的“短信”一栏选短信通道并填写密钥，保存后马上生效（见第 17 节）。通道有三个选项，括号里是对应的变量名：

- `twilio`：填 `TWILIO_ACCOUNT_SID`、`TWILIO_AUTH_TOKEN`，再填 `TWILIO_FROM`（发信号码）或 `TWILIO_MESSAGING_SERVICE_SID`。
- `webhook`：系统把 `{"to":"号码","message":"内容"}` 以 JSON POST 到 `SMS_WEBHOOK_URL`，可以接本地短信服务商的网关。`SMS_WEBHOOK_AUTH` 会放进 `Authorization` 请求头。
- `log`：只写日志，不发短信。这时顾客收不到验证码，无法下单。

顾客现在只能用短信验证码登录，所以短信通道是必须的。为了防止被人刷短信，系统有三道限制：同一个手机号 60 秒只能发一条、一天最多 10 条；同一个网络地址一天最多 30 条。“密钥设置”页的短信一栏会显示最近 24 小时发了多少条，数字突然变大就要留意。顾客登录页可以选国家或地区区号，任何国家的手机号都能登录（号码按 E.164 存成 `+区号号码`）。所以短信通道要能发国际短信：用 Twilio 时，要在 Twilio 控制台 Messaging → Settings → Geo permissions 里勾上要发的国家，没勾的国家会发送失败（顾客看到“暂时发不了验证码”）；用 webhook 接本地网关时，先确认网关支持哪些国家。国际短信比本地贵，“密钥设置”页的发送数量要多留意。

顾客登录页下方的《用户服务协议》在 `web/public/terms.html`，黄色标出的地方（公司名、联系方式）需要你填好再上线。

webhook 方式在容器环境里对着模拟网关实测过，Twilio 只做过模拟测试。配置好后请用真实手机号完整下一单，并查看 `docker compose logs worker` 里有没有发送失败。

## 7. 开通门店

1. 平台管理员登录后进入平台后台的“商户”页，点“新建商户”：填商户名、门店名、门店链接标识（例如 `makati`，顾客地址就是 `/s/makati`）、时区、币种和店长账号。
2. 店长登录，在“设置”里填营业时间、税率、服务费、包装费、起送金额、配送区和后厨工作台，再到“员工”页添加员工账号并分配岗位（工作台、打包台、骑手）和后台权限（收银、店长）。想让顾客收到接单和配送的状态短信，在这里打开短信通知，新门店默认是关的。
3. 在“菜单”里建分类和菜品，或用 Excel 导入，给菜品选工作台，然后发布。
4. 堂食：在“桌台”里新增桌台，点每张桌的“桌码”打印二维码贴在桌上。平板在浏览器打开 `/ipad`，输入桌台页生成的配对码。
5. 在“模板”里给外卖、扫码、平板各选一个布局并发布。
6. 把顾客链接 `https://order.example.com/s/门店标识` 发给顾客或做成二维码。

设备建议：后厨屏和平板用较新的 Chrome 或 Safari，关闭自动休眠，把页面添加到主屏幕。

## 8. 日常运维

**看日志**

```bash
docker compose logs -f --tail=100 api worker
```

**备份**（建议每天定时执行，并把文件传到另一台机器或对象存储）

```bash
docker compose exec -T postgres pg_dump -U waimai -Fc waimai > backup-$(date +%F).dump
```

**恢复**（会覆盖现有数据，先停掉接口和后台任务）

```bash
docker compose stop api worker
docker compose exec -T postgres pg_restore -U waimai -d waimai --clean --if-exists < backup-2026-11-21.dump
docker compose start api worker
```

上线前请实际做一次恢复演练，确认备份文件可用。

**升级**

```bash
docker compose exec -T postgres pg_dump -U waimai -Fc waimai > before-upgrade.dump
git pull
docker compose up -d --build
docker compose ps
```

迁移会自动执行。升级时接口会重启几秒钟，打开的页面会自动重连并刷新。尽量在非营业时间升级。

**回滚**：数据库迁移只向前，不自动回退。如果新版本有问题，先 `git checkout` 回上一个版本并重新 `docker compose up -d --build`。如果新版本的迁移和旧代码不兼容，用升级前的备份恢复。

**余额账本**（docs/BALANCE_CHECK.md）：顾客的退款余额、储值、平台余额都是钱，账本只能追加，只能通过数据库函数 `wallet_post_journal` 记账。不要用 SQL 直接改 `wallet_accounts`、`wallet_journals`、`wallet_postings`；要调整走商家后台的退款、撤回、退储值，或平台后台的调整。`APP_SECRET` 也用来给短信验证码做签名，换了以后没用完的余额验证码会失效（5 分钟内重发即可）。

*1）应用用受限的数据库角色连接*。迁移会建一个角色 `waimai_app`：能读写业务表，但账本只能读、只能调记账函数，连改都改不了。正式服务器在 `.env` 里这样设（`APP_DB_PASSWORD` 用 `openssl rand -hex 24` 生成）：

```bash
MIGRATE_DATABASE_URL=postgres://waimai:数据库密码@postgres:5432/waimai?sslmode=disable
APP_DB_PASSWORD=应用角色密码
DATABASE_URL=postgres://waimai_app:应用角色密码@postgres:5432/waimai?sslmode=disable
```

`migrate` 用 `MIGRATE_DATABASE_URL`（表的所有者）跑迁移，并给 `waimai_app` 设上密码、允许登录；`api`、`worker` 用 `DATABASE_URL`。用了托管数据库并且有 `DIRECT_DATABASE_URL` 时，它也换成 `waimai_app`，`MIGRATE_DATABASE_URL` 填所有者的直连地址。托管数据库不允许建角色时迁移会跳过这一步：请在数据库控制台手动建一个叫 `waimai_app` 的用户，再重跑 `docker compose up -d migrate`。没改的话系统照样能用（触发器也会拦），但平台后台“余额账本”页会一直显示安全警告。

*2）自动检查和告警*。后台任务每小时检查新记的账、每天检查全部账本，每家店过了当地午夜关一次账（日终试算）。结果在平台后台左侧“余额账本”，有问题显示红色数字。想在手机上收到提醒，在 `.env` 设 `WALLET_ALERT_WEBHOOK` 为企业微信/钉钉/Slack/Telegram 机器人的地址，出问题会推一条（6 小时内不重复）。收到后照 docs/BALANCE_CHECK.md 第 7 节处理。

*3）指纹*。“余额账本”页显示账本的指纹（一串字母数字）。建议每天把它抄到账本之外（例如截图发到群里）；以后有人改过历史数据，指纹就对不上。

*4）升级到 00085 迁移*（余额账本加固）：迁移会把老的平台钱包流水搬进复式账本、给历史数据补哈希链和重分类凭证，**不能回退**。升级前务必备份；升级后跑一次 `docker compose exec api pricecheck -days 0` 或在平台后台“余额账本”页确认 0 个问题。

**磁盘**：旧镜像会占空间，可以定期执行 `docker image prune -f`。

## 9. 上线前检查

- [ ] `https://你的域名/` 能打开，证书有效，浏览器开发者工具里协议显示为 h2。
- [ ] `.env` 里 `OTP_DEV_ECHO=false`、`COOKIE_SECURE=true`，`APP_SECRET` 和数据库密码是随机值。
- [ ] 没有执行过 `seed`，或者演示账号已停用、`admin` 密码已重置。
- [ ] 用真实手机号收到验证码，完成一单外卖：下单、接单、后厨备好、打包、骑手送达、核款。
- [ ] 订单工作台和后厨屏在新订单进来时会自动刷新（说明实时推送穿过了代理）。
- [ ] 堂食走一遍：开台、扫码点菜、平板加菜、上桌、收款关台。
- [ ] 重启一次 `docker compose restart api worker`，页面能自动恢复，订单都在。
- [ ] 做过一次备份和恢复演练，并设置了每天自动备份。
- [ ] 平台后台“余额账本”页显示账本检查没有问题，并且没有安全警告（应用用的是 `waimai_app`）。
- [ ] 设置了 `WALLET_ALERT_WEBHOOK`，或者每天有人看“余额账本”页。
- [ ] 防火墙只开放 80 和 443，8000 端口从外网访问不到。

## 10. 使用托管数据库

正式营业建议用云厂商的托管 PostgreSQL 16，自带备份和时间点恢复。下面的做法在模拟环境里实测过：一个只接受 TLS 连接的独立 PostgreSQL，前面加一层事务模式的 PgBouncer，应用通过连接池访问。

### 10.1 两种地址

云厂商通常给两个地址：

- **直连地址**：直接连到数据库，一般是 5432 端口。
- **连接池地址**：经过事务级连接池（PgBouncer 的 transaction 模式，或标着 pooled 的地址），一般是 6432 或 6543 端口。

只有直连地址时，把它填进 `DATABASE_URL` 就行，其他不用设。

用连接池地址时，**必须同时提供直连地址**：

```
DATABASE_URL=postgres://用户:密码@连接池地址:6432/waimai?sslmode=require
DIRECT_DATABASE_URL=postgres://用户:密码@直连地址:5432/waimai?sslmode=require
```

原因有三个，系统在设置了 `DIRECT_DATABASE_URL` 后会自动处理：

1. 实时推送靠数据库的 LISTEN，它需要一条固定的连接，经过事务级连接池收不到通知。接口进程会用直连地址监听。
2. 数据库迁移要持有会话级的锁，也走直连地址。
3. 经过事务级连接池时不能用缓存的预编译语句。系统会自动把普通查询切换到对连接池安全的模式，不需要手工改连接串。

如果只填了连接池地址而没填 `DIRECT_DATABASE_URL`，现象是接口大量返回 500，日志里有 `prepared statement ... already exists` 或 `does not exist`，并且页面不会自动刷新。

`sslmode=require` 会加密连接但不校验证书。云厂商使用公共 CA 签发证书时可以改成 `verify-full`，这一点没有实测。

### 10.2 停用本地数据库容器

新建 `docker-compose.override.yml`，内容如下，然后 `docker compose up -d`：

```yaml
services:
  postgres:
    profiles: ["local-db"]
  migrate:
    depends_on: !reset []
```

`!reset` 需要较新的 Compose（2.24 及以上）。启动后 `docker compose ps -a` 里应该只有 `api`、`worker`、`web` 在运行，`migrate` 是 exited (0)。

### 10.3 备份和恢复

托管数据库优先用云厂商自带的备份和时间点恢复。需要手工导出时，用直连地址：

```bash
docker run --rm postgres:16-alpine pg_dump '直连地址' -Fc > backup-$(date +%F).dump

docker compose stop api worker
docker run --rm -i postgres:16-alpine pg_restore -d '直连地址' --clean --if-exists --no-owner < backup-2026-11-21.dump
docker compose start api worker
```

### 10.4 实测结果（2026-11-21）

- 迁移、启动、演示数据都能通过 TLS 和连接池完成。
- 数据库端看到的连接全部是加密的。只有接口进程的监听连接是直连，其余都经过连接池。
- 业务测试（包括并发下单、名额、关台和加菜并发）经过连接池全部通过。外卖、堂食、模板三条浏览器流程也全部通过。
- 备份后删掉数据再恢复，数据回来了。
- 重启数据库模拟故障切换：接口几秒内恢复，实时推送自动重连。

没有验证的是具体某家云厂商的产品，各家连接池的行为可能有细微差别。

## 11. 常见问题

| 现象 | 排查 |
|---|---|
| `migrate` 退出码不是 0 | `docker compose logs migrate`。多半是 `DATABASE_URL` 里的密码和 `POSTGRES_PASSWORD` 不一致。数据库卷已创建后再改 `POSTGRES_PASSWORD` 不会生效 |
| 登录后马上又掉回登录页 | 用 http 访问但 `COOKIE_SECURE=true`。请用 https 地址 |
| 顾客收不到验证码 | `SMS_PROVIDER` 是不是还是 `log`，再看 `docker compose logs worker api` |
| 页面不自动刷新，要等半分钟 | 实时推送被代理缓冲或被连接池挡住。检查 Caddy 的 `flush_interval -1` 和第 10 节的 `DIRECT_DATABASE_URL` |
| 升级或改配置后短暂出现 502 | 接口容器重启需要几秒，页面会自动重连。如果持续超过半分钟，看 `docker compose logs api` |
| 接口大量 500，日志有 `prepared statement ... already exists` | `DATABASE_URL` 经过了事务级连接池，但没设置 `DIRECT_DATABASE_URL`，见第 10 节 |
| 很多顾客同时被提示请求太频繁 | 代理没有传真实地址，所有人被当成同一个来源。检查代理是否带 `X-Forwarded-For` |
| 开很多页面后卡住 | 没有走 HTTP/2。确认是通过 Caddy 的 https 地址访问 |
| 短信里没有订单链接 | 没设置 `PUBLIC_BASE_URL` |

## 12. 不用 Docker 的单机运行

只想在一台机器上快速跑起来（例如内部演示），可以直接用二进制，需要 Go 1.24、Node 22 和 PostgreSQL 16：

```bash
export DATABASE_URL='postgres://USER:PASS@localhost:5432/waimai?sslmode=disable'
export APP_SECRET=$(openssl rand -hex 32)
make web build
./bin/migrate
ADMIN_PASSWORD='换成你的强密码' ./bin/admin
STATIC_DIR=web/dist EMBED_WORKER=true ./bin/api      # 接口、后台任务和前端都在 :8080
```

对外提供服务时同样要在前面放一个 HTTPS 代理，并设置 `COOKIE_SECURE=true` 和 `PUBLIC_BASE_URL`。

## 13. Google 地图（配送地图）

商家端的“配送地图”页面和顾客端的“送到哪里”要用 Google 地图。整个平台共用一个密钥，商户不用自己申请。

不配密钥时系统照常运行：配送地图页退回成文字表格，顾客从列表里选配送区，和以前一样。已经切到“地图定位”的门店也会自动按列表方式接单，等密钥配好后恢复。

### 13.1 申请密钥

1. 打开 [Google Cloud Console](https://console.cloud.google.com/)，新建一个项目，并给项目绑定结算账号。Google 地图必须绑定付款方式才能用，有免费额度，超出后按用量收费，单价以 Google 当时的价目表为准。
2. 在“API 和服务”里启用三项：**Maps JavaScript API**（显示地图）、**Geocoding API**（搜索地址）和 **Places API (New)**（顾客选收货地址时的搜索建议和图钉附近的地点，见 13.6）。
3. 在“凭据”里创建一个 API 密钥。
4. 给密钥加限制，这一步不要省。密钥会出现在浏览器里，不限制的话别人可以拿去用，费用算在你头上：
   - 应用限制选“网站”，填你的地址，例如 `https://order.example.com/*`。
   - API 限制选“限制密钥”，只勾上面三项。
5. 建议在结算里设一个预算提醒。

### 13.2 配置

用平台管理员登录后台，打开“密钥设置”，把密钥粘贴到“Google 地图 > 地图浏览器密钥”，点保存。不用重启，已经打开的页面刷新一下就有地图了。（也可以像以前一样在 `.env` 里写 `GOOGLE_MAPS_API_KEY=你的密钥` 再 `docker compose up -d`，后台填了的话以后台为准。）

### 13.3 检查

1. 用店主账号登录，打开“配送地图”，应当看到真实的地图。
2. 如果页面提示“Google 地图拒绝了密钥”，依次检查：密钥有没有抄错，网站限制里的地址和你实际访问的地址是否一致（包括 `https`），三项 API 是否都启用了，项目是否绑定了结算账号。改完限制后要等几分钟才生效。
3. 设好店铺位置，画好至少一个配送区，再把“顾客怎么选配送区”切到“地图定位”，然后用手机打开门店链接试下一单。

### 13.4 测试底图

`GOOGLE_MAPS_API_KEY=stub` 会显示一张不联网的格子底图，功能和真地图一样，只是没有街道。它只用于自动化测试和演示（`e2e/mapzones.cjs` 需要它），正式环境不要用。

Google 地图在中国大陆不能正常访问，在大陆打开时地图会加载失败。

### 13.5 Google 推荐路线（骑手工作汇总）

“员工 > 骑手工作汇总”里每一趟的“系统推荐路线”，配了路线密钥后由 Google 按真实道路计算；不配时是“店到各送餐点再回店”的直线估算，页面其他部分照常可用。

**这是另一个密钥，不能用上面那个。** 上面的地图密钥会出现在浏览器里，限制的是网站地址；路线密钥只放在服务器上，不能让浏览器看到。

1. 在同一个 Google Cloud 项目的“API 和服务”里启用 **Routes API**。
2. 在“凭据”里再创建一个 API 密钥。API 限制选“限制密钥”，只勾 **Routes API**。应用限制选“IP 地址”，填你服务器的出口 IP；出口 IP 不固定的话应用限制留空，但 API 限制一定要设。
3. 在平台后台“密钥设置”的“Google 地图 > 路线服务器密钥”里粘贴并保存。不用重启，最多等 15 秒后新出发的趟就按 Google 路线算。（也可以写在 `.env` 的 `GOOGLE_ROUTES_API_KEY` 里。）

**怎么收费**（以 Google 当时的价目表为准，下面是 2026 年 11 月核对的全球价）：每一趟只在骑手出发时调用一次，之后看多少次页面都不再调用。我们的请求用了摩托车路线，按 Google 的 “Compute Routes Enterprise” 计费：每月前 1,000 次免费，之后每 1,000 次 15 美元（用量很大时单价递减）。一家店一天 50 趟，一个月约 1,500 趟，超出 500 次，约 7.5 美元。建议在结算里设预算提醒。

**几点说明**

- 路线优先按摩托车算（Google 的摩托车路线覆盖菲律宾，目前标为测试版）。某个请求 Google 不给摩托车路线时，自动改按汽车算，页面上会写明“按汽车算”。
- 送餐顺序由 Google 排。预计用时按出发那一刻的路况。
- Google 调用失败时每分钟重试，一趟最多 3 次，之后这一趟退回直线估算。一趟超过 25 个送餐点时直接用直线估算。有订单没有在地图上定点时，这一趟没有推荐路线。
- 配密钥之前已经出发的趟不会补算，仍然是直线估算。
- 发给 Google 的只有店铺和送餐点的经纬度，没有姓名、电话和地址文字。隐私政策里已写明。
- 检查：配好后让骑手送一趟，出发一两分钟后在路线页应当看到“（Google 路线，摩托车）”和沿着街道走的蓝色虚线。如果仍是“（直线估算）”，看 worker 的日志里 `planned route` 开头的那一行，常见原因是 Routes API 没启用、密钥的 IP 限制不对、项目没绑定结算账号。
- `GOOGLE_ROUTES_API_KEY=stub` 是不联网的替身，只用于自动化测试和演示。

### 13.6 顾客选收货地址（Places API）

顾客新增收货地址时在地图上选位置：输入大楼或街道名会出现搜索建议，拖动地图会列出图钉附近的地点。这两样用的是 **Places API (New)**，和地图用同一个密钥，不用另外填。

- 在 Google Cloud Console 的“API 和服务”里启用 **Places API (New)**（注意不是旧的 “Places API”），并在密钥的 API 限制里把它勾上。
- 没启用时不会报错，功能自动退回简化版：搜索变成按整句地址查找（Geocoding），图钉附近只给一个街道地址。顾客仍然可以拖动地图选位置、保存地址、下单。
- 顾客的搜索只在店铺所在国家里找、优先店铺附近。店铺所在国家是商家保存店铺位置时由 **Geocoding API** 解析出来的，所以 Geocoding API 必须启用。
- 这项按用量收费，单价以 Google 当时的价目表为准。为了少花钱，搜索是顾客停止输入 0.3 秒后才查一次，至少输入 2 个字才查。
- 这部分是按 Google 官方文档写的，还没有用真实密钥实测。启用后请用手机实际加一个地址试一下：搜索一个大楼名，看有没有建议；拖动地图，看下面的列表有没有变。如果建议不出来，先看浏览器控制台里 Google 的报错，多半是没启用这项 API 或密钥限制里没勾。

顾客看到的地图上不画配送区，也不显示区名，只告诉顾客这个位置能不能送和配送费。

## 14. 员工工作台（上下班、定位、推送）

员工用手机浏览器打开 `https://你的地址/work` 登录即可，不需要安装任何东西。在浏览器菜单里选“添加到主屏幕”后，可以像 App 一样从桌面打开，没有信号时也能打开并继续点取餐、送达，联网后自动补交。

### 14.1 必须是 HTTPS

定位、添加到主屏幕、推送这三样，浏览器只在 HTTPS 地址下才允许。按第 5 节配好 HTTPS 就可以，用 `http://IP:端口` 直接访问时这些功能不会出现。

### 14.2 定位

骑手在工作台点“上班”后，浏览器会问是否允许定位，选允许。之后只在“上班中、当前是骑手身份、这个页面开着”时上报位置。点“下班”、退出登录、连续 12 小时没下班，都会停止并清掉最新位置。原始位置点保留 7 天后自动删除；每一趟配送的路线图（压缩后的）保留 90 天，里程和单数长期保留。

网页版在手机锁屏或切到别的 App 后不能继续上报，这是浏览器的限制，要等安卓和 iPhone 的 App 做好才能解决。

### 14.3 浏览器推送

推送用来在骑手锁屏时提醒“有新配送单、被改派、被取消、可以取餐了”。不配置也不影响使用，页面开着时任务照样实时出现。

1. 用平台管理员登录后台，打开“密钥设置”，在“浏览器推送”一栏点“生成一对新密钥”（只做一次）。公钥和私钥会自动填好并生效。

2. 在同一栏的“联系方式”里填 `mailto:你的邮箱`（浏览器厂商的推送服务出问题时用来联系你），点保存。

3. 不用重启。（以前写在 `.env` 里的 `VAPID_PUBLIC_KEY`、`VAPID_PRIVATE_KEY`、`VAPID_SUBJECT` 继续有效；命令 `docker compose run --rm --no-deps admin admin vapid` 也还能用来生成密钥。）

4. 骑手在工作台“我的”里点“新任务推送”的“开启”，浏览器询问时选允许。每台手机各开一次。

注意：

- 密钥上线后不要更换。换了以后所有骑手都要重新点一次“开启”。
- 私钥和数据库密码一样要保密，不要提交到代码仓库。
- 安卓的 Chrome 直接支持。iPhone 要求 iOS 16.4 以上，并且必须先“添加到主屏幕”，从桌面图标打开后才能开启推送。
- 推送只是提醒，内容只有单号和地址。推送丢了不会丢单，打开工作台就能看到任务。
- 发送情况可以在平台后台的 Worker 页面看到，推送失败会自动重试。

## 15. 安卓 App（员工工作台）

安卓 App 是给骑手用的：锁屏也能收到新配送单，上班时切到别的应用也继续上报位置。店内员工用浏览器版就够了。

App 只是一个外壳，打开的还是你服务器上的 `/work` 页面，所以以后页面更新不用重新装 App。因为每家的服务器地址和推送密钥都不一样，安装包要你自己打一次，全程在 GitHub 网页上点，不需要装开发工具。

你需要准备：系统已经按前面的步骤上线，有一个 HTTPS 地址；一个 Google 账号（建 Firebase 项目用，免费）。

### 15.1 建 Firebase 项目（推送用）

1. 打开 <https://console.firebase.google.com>，用 Google 账号登录，点“创建项目”，名字随意，Google Analytics 可以关掉。
2. 在项目首页点安卓图标“添加应用”。“Android 软件包名称”必须填 `app.waimai.workbench`，其余不用填。
3. 下载它给你的 `google-services.json`，后面的“下一步”一路点完即可。这个文件给 App 用。
4. 点左上角齿轮进“项目设置”，打开“服务账号”标签，点“生成新的私钥”，会下载一个 `.json` 文件。这个文件给服务器用，等同于密码，不要发给别人，也不要提交到代码仓库。

暂时不想弄推送可以跳过这一小节，App 照样能用，之后补上再重新打包一次。

### 15.2 服务器这边

用记事本打开第 4 步下载的私钥文件，全选复制。用平台管理员登录后台，打开“密钥设置”，粘贴到“App 推送（Firebase）> Firebase 服务账号”，点保存。不用重启，页面上会显示项目名和服务账号邮箱，私钥本身不再显示。

（也可以像以前一样把文件内容压成一行写进 `.env` 的 `FCM_SERVICE_ACCOUNT`，后台填了的话以后台为准。）

### 15.3 做一个签名密钥（只做一次）

安卓要求安装包带签名，而且以后的新版本必须用同一个密钥签名才能覆盖安装。在任何一台装了 Java 的电脑上执行（服务器上没有 Java 的话，用 Docker 也行）：

```bash
docker run --rm -v "$PWD":/out eclipse-temurin:21 keytool -genkeypair \
  -keystore /out/workbench.jks -alias workbench -keyalg RSA -keysize 2048 -validity 10000 \
  -storepass '换成你的密码' -keypass '换成你的密码' -dname "CN=Workbench"
base64 -w0 workbench.jks > workbench.jks.txt
```

把 `workbench.jks` 和密码存到安全的地方。**丢了以后就没法给已经装了 App 的手机发更新，只能让大家卸载重装。**

### 15.4 在 GitHub 上填三样东西

打开代码仓库的 Settings，左边选 Secrets and variables 下的 Actions。

在 **Variables** 标签里新建：

| 名字 | 值 |
|---|---|
| `WORKBENCH_URL` | 你的地址，例如 `https://order.example.com`，结尾不带斜杠 |

在 **Secrets** 标签里新建：

| 名字 | 值 |
|---|---|
| `GOOGLE_SERVICES_JSON` | 15.1 第 3 步下载的 `google-services.json` 的全部内容 |
| `ANDROID_KEYSTORE_BASE64` | `workbench.jks.txt` 的全部内容 |
| `ANDROID_KEYSTORE_PASSWORD` | 15.3 里你设的密码 |

### 15.5 打包和下载

1. 打开仓库的 Actions 标签，左边选 “Android app”，点 “Run workflow”。
2. 大约五分钟后这次运行变成绿色。点进去，页面上会写明这次打包用的服务器地址、有没有推送、是不是正式签名。
3. 在页面底部的 Artifacts 里下载 `workbench-android-数字`，解压得到 `app-release.apk`。

没有填签名密钥时打出来的是 `app-debug.apk`，可以先装上看看，但以后换成正式签名的版本时要先卸载。

### 15.6 发给骑手

1. 把 `app-release.apk` 发给骑手（聊天软件、网盘都行）。
2. 骑手点开安装。手机会提示“不允许安装未知来源的应用”，按提示给发文件的那个软件开一次权限即可。
3. 打开 App，用自己的账号登录，切到骑手身份，点“上班”。手机会依次问通知权限和定位权限，都选允许。定位那一步请选“使用应用时允许”，上班期间状态栏会一直有一条“正在上报位置”的提示，这是正常的，下班后消失。
4. 国产手机（小米、华为、OPPO、vivo 等）省电管得很严，可能锁屏几分钟后就把 App 停掉。在 App 的“我的”里点“打开应用设置”，把电池改成“无限制”，并允许自启动。

### 15.7 更新

页面和功能的更新都在服务器上，骑手不用做任何事。只有外壳本身变了（比如加了新的手机权限）才需要重新打包，届时再点一次 “Run workflow”，把新的安装包发给骑手覆盖安装。

### 15.8 目前的限制

- 这个 App 还没有在真手机上验证过。云端只能确认安装包能打出来、签名和权限正确；锁屏推送和后台定位在各品牌手机上的实际表现，要请你或骑手装上跑一两天，把遇到的问题告诉我。
- 国内不能访问 Google 服务的手机收不到 Firebase 推送。在这类手机上，App 开着时任务仍然实时出现，只是锁屏时没有提醒。
- iPhone 版在后面的批次做。

## 16. 隐私政策和上架材料

- 隐私政策草稿在 `web/public/privacy.html`，部署后网址是 `https://你的域名/privacy.html`（中英文在同一页）。
- 上线前把页面里所有黄色标出的地方（`class="todo"`）换成你的公司名称、联系方式、短信服务商和托管商，
  并请熟悉当地法律的人看一遍。改完重新构建前端即可。
- 应用商店要填的名称、介绍、权限说明、数据安全表、审核账号和截图清单见 `docs/APP_STORE_LISTING.md`。

## 17. 密钥设置页（平台后台）

地图、推送、短信这些外部服务的密钥，都在平台后台左侧菜单“系统 > 密钥设置”里填。只有平台管理员能打开这个页面。

| 栏目 | 填什么 | 申请方法 |
|---|---|---|
| Google 地图 | 地图浏览器密钥、路线服务器密钥 | 第 13 节 |
| 浏览器推送 | 点“生成一对新密钥”，再填联系方式 | 14.3 节 |
| App 推送（Firebase） | Firebase 服务账号 JSON 文件的全部内容 | 15.1 节 |
| 短信 | 短信通道，以及 Twilio 或自建网关的密钥 | 第 6 节 |

**怎么用**

- 粘贴密钥，点这一栏的“保存”，马上生效，不用重启服务器。API 进程立即换用新密钥，单独运行的 worker 和其他 API 进程最多 15 秒后换用。
- 保存前系统会检查这一栏能不能用，例如选了 Twilio 却没填 Token、Firebase 内容不是服务账号文件，会提示并且不保存，原来的设置不受影响。它不会联网验证密钥本身对不对，填完请按各节的“检查”实际试一次。
- 保存后页面只显示密钥末尾 4 位，完整密钥不会再显示，也不会发回浏览器。要换密钥就直接粘贴新的；点“清除”则删掉后台保存的值。
- 每次修改都记在“操作记录”里，只记改了哪一项和谁改的，不记密钥内容。

**和 `.env` 的关系**

- 后台填了的以后台为准；后台没填的，用 `.env` 里同名变量的值；两边都没有就是未启用。页面上每一项都标着“后台设置”“来自服务器 .env”或“未设置”。
- 已经写在 `.env` 里的密钥不用动，照常有效。想搬到后台，就在页面上重新粘贴一次，然后可以把 `.env` 里那一行删掉。
- `DATABASE_URL`、`APP_SECRET`、`PUBLIC_BASE_URL` 这些是服务器启动时就要用的，仍然只在 `.env` 里设置。

**安全**

- 密钥用 `APP_SECRET` 派生的密钥加密（AES-256-GCM）后存进数据库的 `platform_settings` 表，数据库备份里没有明文。所以 `APP_SECRET` 必须是随机值；它还是开发默认值时页面会有黄色提醒。
- 换了 `APP_SECRET`，或者把数据库恢复到另一台 `APP_SECRET` 不同的服务器上，后台保存的密钥就读不出来了：服务器照常启动，改用 `.env` 里的值，页面上这些项会标“读不出来，请重新填写”，重填即可。备份数据库时请把 `.env` 一起妥善保存。
- 地图浏览器密钥本来就会出现在顾客的浏览器里，靠 Google 那边的网站限制保护（13.1 节）；其余密钥只在服务器上使用。
- 平台管理员账号能改所有密钥，请用强密码，不要多人共用。

## 18. 外卖商城（网站首页）

网站的首页（`https://你的域名/`）现在是外卖商城：顾客打开后按自己的位置看到能送到的店，点进去就是那家店的点餐页。商家和员工的登录入口还是 `/login`，商城页脚也有“商家登录”。

**上线前检查这几样**

- 要有地图密钥（第 13 节）。没有的话顾客仍然可以自动定位、用已保存的地址、按城市逛，但不能搜索地址和在地图上选位置，左上角也只显示地区名，不显示街道名。
- 自动定位只在 HTTPS 下可用（和 14.1 节一样）。
- 每家店要在“配送地图”里设好店铺位置、画好配送范围，才会出现在商城里。只开自取的店出现在“附近可自取”。
- 商城不需要新的环境变量，升级后数据库会自动加表（`00031_mall.sql`）。

**谁管什么**

- 商家：商家后台“设置”里的“商城展示”，可以关掉展示、选最多 2 个分类、写一句话介绍。头像和封面图在“店铺装修”里传。
- 平台：平台后台“商城”，管顶部活动图（最多 5 张，图片不超过 1 MB）、分类的名字和顺序，可以把某家店下架。下架只影响商城，店铺自己的链接照常能下单。

**顾客看不到配送范围**

商城和它的接口只告诉顾客某个位置能不能送、配送费、起送价和大概多久，不会给出配送范围的形状或名字。

## 19. 聊天（顾客和商家发消息）

聊天是可选的，基于开源的 OpenIM。不开的话系统和以前一样，页面上不出现任何聊天入口。方案见 `docs/CHAT_DESIGN.md`。

**需要什么**

- 服务器多出约 1.5 GB 内存（实测空载 1.05 GB）。整台建议 4 核 8 GB。
- 多出 6 个容器：`openim-server` 和它用的 `im-mongo`、`im-redis`、`im-kafka`、`im-etcd`、`im-minio`。都不对外开端口。

**开通步骤**

1. 在 `.env` 里加四行（后三个各自用 `openssl rand -hex 24` 生成，不要用示例值）：

   ```
   OPENIM_API_URL=http://openim-server:10002
   OPENIM_SECRET=……
   OPENIM_HOOK_KEY=……
   IM_STORE_PASSWORD=……
   ```

2. 启动时带上 `im` 这一组：

   ```
   docker compose --profile im up -d --build
   ```

   第一次启动 OpenIM 要一两分钟。`docker compose --profile im ps` 里 `openim-server` 显示 `healthy` 就好了。

3. 打开任意一家店的点餐页，里面出现“联系商家”就是开通了。商家后台左边多一个“消息”，平台后台多一个“所有会话”。

以后每次 `docker compose up` 都要带 `--profile im`，否则聊天的容器不会跟着启动（页面会提示“聊天暂时不可用”，下单不受影响）。

**本机开发时开通聊天**

上面是用 Docker Compose 跑整套系统的做法（本机也一样，开 http://localhost:8000）。如果你在本机用 `make run` 或 `go run ./cmd/api` 跑接口、用 `npm run dev` 跑页面，OpenIM 还是用 Docker 起，但要多带一个 `deploy/im-local.yml`，让它在本机开出 10001、10002 两个端口，并把回调打回本机的 8080：

1. 在 `.env` 里加：

   ```
   OPENIM_SECRET=……（至少 16 位，openssl rand -hex 24）
   OPENIM_HOOK_KEY=……（至少 16 位）
   ```

2. 只启动 OpenIM 和它用的几个容器：

   ```
   docker compose -f docker-compose.yml -f deploy/im-local.yml --profile im up -d openim-server
   ```

   等 `docker compose ps openim-server` 显示 `healthy`（一两分钟）。

3. 启动接口时带上这三个变量（和 `.env` 里的值一样），端口要是 8080：

   ```
   export OPENIM_API_URL=http://localhost:10002
   export OPENIM_SECRET=……
   export OPENIM_HOOK_KEY=……
   make run
   ```

4. 另开一个终端 `cd web && npm run dev`，打开 http://localhost:5173 。聊天要从这里进：`make run` 自己在 8080 提供的页面不会转发 `/im/`，聊天会显示连接不上。

聊天页显示“聊天功能暂时没有开通”，说明接口启动时没读到 `OPENIM_API_URL`；显示“聊天暂时不可用”，说明读到了但连不上 OpenIM（没启动、还没 healthy，或地址写错）。

**要知道的几件事**

- `OPENIM_SECRET` 是给聊天登录签名的，`OPENIM_HOOK_KEY` 保护 OpenIM 回调我们后端的入口。两个都至少 16 位，太短的话 api 容器会拒绝启动。改了 `OPENIM_SECRET` 之后已经打开的聊天页要刷新一次。
- 如果删掉了 OpenIM 的数据卷重新装，要重启一次 api 容器，让它重新在 OpenIM 里建好顾客和门店的账号。
- `IM_STORE_PASSWORD` 只在第一次创建数据卷时生效，之后改它不会改已有数据库的密码。
- 如果前面有 Caddy 之类的 TLS 代理，不用加配置：聊天的两个路径 `/im/ws` 和 `/im/api` 都在同一个域名下，由 web 容器转发。代理要支持 WebSocket（Caddy 默认支持）。
- 聊天内容我们自己的数据库里有一份副本（平台“所有会话”读的就是它），随 PostgreSQL 一起备份。OpenIM 那边的原件在数据卷 `im-mongo` 里，要完整恢复聊天页里的历史就也备份它：`docker compose exec im-mongo mongodump -u root -p "$IM_STORE_PASSWORD" --authenticationDatabase admin --archive > im.archive`。
- 浏览器端用的 OpenIM 网页 SDK 是 GPL-3.0 授权，见 `web/NOTICE.md`。
- **提醒**：对方没在看页面时用浏览器推送提醒，用的是平台后台“密钥设置”里的浏览器推送密钥（和员工工作台同一套）。没配密钥就没有“开启提醒”的入口，聊天本身不受影响。顾客有进行中订单又没开推送时会发一条短信，要门店设置里开着“短信通知顾客”，并且配了短信服务。短信里的链接用 `PUBLIC_BASE_URL`。
- **保留时间**：消息 180 天，图片 90 天，由每天的清理任务删除；被举报的会话保留到平台处理完再加 90 天。
- **是否正常**：平台后台“总览”有一张“聊天服务”卡片，变红说明 OpenIM 没有应答，先看 `docker compose --profile im ps` 和 `docker compose logs openim-server`。

**测试**

```
BASE_URL=http://localhost:8000 node e2e/chat.cjs
```

没开聊天的环境会直接提示跳过。


## 20. 企业官网（主域名）

企业官网（`site/`，方案见 `docs/WEBSITE_DESIGN.md`）是纯静态页面，用单独的 `site` 容器提供，放在主域名上；点餐系统（商城、商家后台、员工工作台、接口）照旧在 `order.` 子域名上。

1. 两个域名的 A 记录都指向这台服务器，例如 `example.com`、`www.example.com` 和 `order.example.com`。
2. `.env` 里填好：
   - `PUBLIC_BASE_URL=https://order.example.com`（点餐系统，官网的“登录”和“我是顾客，去点餐”链接到这里）
   - `SITE_URL=https://example.com`（官网自己的地址，用于规范链接、多语言链接和站点地图）
   - `SITE_PORT=127.0.0.1:8001`
3. `/etc/caddy/Caddyfile` 再加两段，然后 `sudo systemctl reload caddy`：

   ```
   example.com {
       reverse_proxy 127.0.0.1:8001
   }
   www.example.com {
       redir https://example.com{uri} permanent
   }
   ```

4. `docker compose up -d --build site`。改了官网文案或 `.env` 里的这两个地址后，也要重新执行这一条（地址是构建时写进页面的）。

说明：

- 打开主域名会按浏览器语言跳到 `/zh/`、`/zh-Hant/` 或 `/en/`，用户切换过语言就按他选的（和点餐系统共用 `lang` cookie）。
- 官网上的“预约演示”表单提交到官网自己的 `/api/v1/public/leads`，由 `site` 容器转给 `api`，不需要跨域设置。提交的申请出现在平台后台“顾客服务 → 入驻申请”，来源标为“官网”，侧栏“顾客服务”上的数字是还没联系的申请数。同一个手机号一天内重复提交只更新原来那条；每个 IP 连续提交 5 次后每分钟只能再提交 1 次；全站每小时最多收 60 条新申请。
- 访问统计可选：自建 Umami 后在 `.env` 填 `UMAMI_SRC`、`UMAMI_ID` 再重新构建 `site`。不填就不加载任何统计脚本。
- 上线后到 Google Search Console 添加主域名，提交 `https://example.com/sitemap.xml`。
- 本地预览：`cd site && npm ci && npm run dev`（表单需要本机 `make run` 起着接口，或设 `API_URL`）。
