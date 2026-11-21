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
| `VAPID_PUBLIC_KEY`、`VAPID_PRIVATE_KEY`、`VAPID_SUBJECT` | 选填。员工工作台的浏览器推送，生成方法见第 14 节。不填也能正常运行，只是骑手锁屏时收不到新单提醒 |
| `FCM_SERVICE_ACCOUNT` | 选填。给安卓 App 发推送用的 Firebase 密钥，见第 15 节。不填时 App 能用，只是收不到推送 |

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

## 14. 员工工作台（上下班、定位、推送）

员工用手机浏览器打开 `https://你的地址/work` 登录即可，不需要安装任何东西。在浏览器菜单里选“添加到主屏幕”后，可以像 App 一样从桌面打开，没有信号时也能打开并继续点取餐、送达，联网后自动补交。

### 14.1 必须是 HTTPS

定位、添加到主屏幕、推送这三样，浏览器只在 HTTPS 地址下才允许。按第 5 节配好 HTTPS 就可以，用 `http://IP:端口` 直接访问时这些功能不会出现。

### 14.2 定位

骑手在工作台点“上班”后，浏览器会问是否允许定位，选允许。之后只在“上班中、当前是骑手身份、这个页面开着”时上报位置。点“下班”、退出登录、连续 12 小时没下班，都会停止并清掉最新位置。历史轨迹保留 7 天后自动删除。

网页版在手机锁屏或切到别的 App 后不能继续上报，这是浏览器的限制，要等安卓和 iPhone 的 App 做好才能解决。

### 14.3 浏览器推送

推送用来在骑手锁屏时提醒“有新配送单、被改派、被取消、可以取餐了”。不配置也不影响使用，页面开着时任务照样实时出现。

1. 生成一对密钥（只做一次）：

   ```bash
   docker compose run --rm --no-deps admin admin vapid
   ```

   它会打印两行，`VAPID_PUBLIC_KEY=...` 和 `VAPID_PRIVATE_KEY=...`。

2. 把这两行原样写进 `.env`，再加一行联系方式（浏览器厂商的推送服务出问题时用来联系你）：

   ```bash
   VAPID_SUBJECT=mailto:你的邮箱
   ```

3. 重启：`docker compose up -d`。

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

把第 4 步下载的私钥文件压成一行，写进服务器的 `.env`：

```bash
# 在放着私钥文件的电脑上执行，把输出的那一行复制下来
python3 -c "import json,sys; print(json.dumps(json.load(open(sys.argv[1]))))" 下载的文件.json
```

```bash
# 服务器 .env 里加一行，注意两边是单引号
FCM_SERVICE_ACCOUNT='这里粘贴那一行'
```

然后 `docker compose up -d` 重启。

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

