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
| `APP_SECRET` | `openssl rand -hex 32` 生成。上线后不要再改，改了会让已发出的订单链接失效 |
| `PUBLIC_BASE_URL` | 你的地址，例如 `https://order.example.com`，结尾不带斜杠 |
| `COOKIE_SECURE` | 保持 `true` |
| `OTP_DEV_ECHO` | 保持 `false`。设成 `true` 会把验证码直接显示在页面上，只能用于演示 |
| `HTTP_PORT` | 保持 `127.0.0.1:8000`，只让本机的 Caddy 访问 |
| `GOOGLE_MAPS_API_KEY` | 选填。配送地图用的 Google 地图密钥，申请方法见第 13 节。不填也能正常运行，只是没有地图 |

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

打开 `https://order.example.com/login`，用平台管理员登录。

## 6. 短信

`SMS_PROVIDER` 有三个选项，改完 `.env` 后执行 `docker compose up -d` 生效：

- `twilio`：填 `TWILIO_ACCOUNT_SID`、`TWILIO_AUTH_TOKEN`，再填 `TWILIO_FROM`（发信号码）或 `TWILIO_MESSAGING_SERVICE_SID`。
- `webhook`：系统把 `{"to":"号码","message":"内容"}` 以 JSON POST 到 `SMS_WEBHOOK_URL`，可以接本地短信服务商的网关。`SMS_WEBHOOK_AUTH` 会放进 `Authorization` 请求头。
- `log`：只写日志，不发短信。这时顾客收不到验证码，无法下单。

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
2. 在“API 和服务”里启用两项：**Maps JavaScript API**（显示地图）和 **Geocoding API**（搜索地址）。
3. 在“凭据”里创建一个 API 密钥。
4. 给密钥加限制，这一步不要省。密钥会出现在浏览器里，不限制的话别人可以拿去用，费用算在你头上：
   - 应用限制选“网站”，填你的地址，例如 `https://order.example.com/*`。
   - API 限制选“限制密钥”，只勾上面两项。
5. 建议在结算里设一个预算提醒。

### 13.2 配置

把密钥写进 `.env`：

```bash
GOOGLE_MAPS_API_KEY=你的密钥
```

然后重启：

```bash
docker compose up -d
```

### 13.3 检查

1. 用店主账号登录，打开“配送地图”，应当看到真实的地图。
2. 如果页面提示“Google 地图拒绝了密钥”，依次检查：密钥有没有抄错，网站限制里的地址和你实际访问的地址是否一致（包括 `https`），两项 API 是否都启用了，项目是否绑定了结算账号。改完限制后要等几分钟才生效。
3. 设好店铺位置，画好至少一个配送区，再把“顾客怎么选配送区”切到“地图定位”，然后用手机打开门店链接试下一单。

### 13.4 测试底图

`GOOGLE_MAPS_API_KEY=stub` 会显示一张不联网的格子底图，功能和真地图一样，只是没有街道。它只用于自动化测试和演示（`e2e/mapzones.cjs` 需要它），正式环境不要用。

Google 地图在中国大陆不能正常访问，在大陆打开时地图会加载失败。
