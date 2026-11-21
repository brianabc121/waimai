# 员工工作台 App 上架材料

这份文档是把“员工工作台”提交到 Google Play 和 App Store 时要填的内容草稿。
带【】的地方要换成你自己的信息。代码里没有任何一处会自动提交到商店，上架都由你手动操作。

## 1. 基本信息

| 项目 | 中文 | English |
|---|---|---|
| 应用名称 | 【店名】员工工作台 | 【Store】 Staff Workbench |
| 一句话简介（80 字以内） | 门店员工接单、备餐、配送用的工作台 | Workbench for store staff: prepare, pack and deliver orders |
| 分类 | 商务 | Business |
| 包名 / Bundle ID | `app.waimai.workbench` | 同左 |
| 隐私政策网址 | `https://【你的域名】/privacy.html` | 同左 |
| 联系邮箱 | 【邮箱】 | 同左 |

### 完整介绍（中文）

【店名】员工工作台是给本店员工使用的内部工具，不面向顾客。

- 一个账号可以有多个身份：店内岗位（如油炸炉、炒菜、打包）和骑手，随时切换。
- 店内身份：只看到分给自己岗位的菜，做好后点一下即可。
- 骑手身份：上班后接收派单提醒，查看取餐和送达地址，一键导航、联系顾客、拍照留证。
- 信号不好时操作先存在手机里，恢复信号后自动补发。
- 下班后不再采集位置。

账号由店长在商家后台的“员工”页面创建，应用内不能自行注册。

### Full description (English)

Staff Workbench is an internal tool for the employees of 【Store】. It is not for customers.

- One account, several roles: kitchen stations, packing, and rider. Switch at any time.
- Station role: see only the dishes assigned to your station and mark them done.
- Rider role: clock on to receive delivery assignments, see pickup and drop-off addresses, navigate, call the customer and take a proof photo.
- Works with poor signal: steps are saved on the phone and sent when the connection returns.
- Location is not collected after you clock off.

Accounts are created by the store manager. There is no sign-up inside the app.

## 2. 权限说明（审核一定会问）

| 权限 | 用途 | 给审核员的说明（英文，可直接粘贴） |
|---|---|---|
| 位置（使用期间） | 骑手上班时上报位置 | The rider's location is shown to the store dispatcher and to the customer of the order being delivered. |
| 位置（后台 / 始终） | 骑手把手机放口袋或切到导航时仍能上报 | Riders navigate with another maps app while delivering, so the app is in the background for most of a delivery. Location is collected only between "Clock on" and "Clock off", and a persistent notification is shown the whole time. |
| 通知 | 新派单、订单取消的提醒 | Riders are notified when an order is assigned, reassigned or cancelled. |
| 相机 | 送达拍照 | Optional proof-of-delivery photo taken by the rider. |

Google Play 的后台定位要另外填一份声明并上传一段演示视频（30 秒左右）：
打开应用，登录骑手账号，点“上班”，系统弹出定位授权，切到后台，展示常驻通知，再点“下班”。

## 3. 数据安全表（Google Play “Data safety” / App Store “App Privacy”）

| 数据 | 是否收集 | 是否与第三方共享 | 用途 | 是否可删除 |
|---|---|---|---|---|
| 精确位置 | 是，仅骑手上班期间 | 否（顾客只看到自己那一单的骑手位置） | 应用功能 | 轨迹 7 天自动删除 |
| 姓名、手机号 | 是（由店长录入） | 否 | 账号管理 | 店长删除员工即删除 |
| 照片 | 是，骑手主动拍摄 | 否 | 应用功能 | 30 天自动删除 |
| 设备推送标识 | 是 | 发送给 Firebase 或苹果推送服务以送达通知 | 应用功能 | 退出登录即删除 |
| 广告、分析、跟踪 | 否 | 否 | | |

传输全程加密（HTTPS）。不用于广告，不出售。

## 4. 审核用测试账号

审核员需要一个能登录的账号，否则会直接拒绝。上架前在商家后台“员工”页面建一个：

- 账号：【review】 密码：【自己设】
- 岗位：骑手 + 任意一个店内岗位
- 在“审核备注”里写明：This is an internal tool for store employees. Use the account above. To see a delivery, the store assigns an order to this rider from the back office; we have prepared one test order for you.

审核期间最好保持有一张已派给该账号的测试订单。

## 5. 截图清单

手机竖屏，中英文各一套，建议 5 张：

1. 登录后的身份切换
2. 店内岗位的任务列表
3. 骑手的配送列表（已上班）
4. 配送详情（地址、导航、联系顾客）
5. 送达拍照

Google Play 另需 512×512 图标和 1024×500 横幅；App Store 需要 6.7 英寸和 6.5 英寸两种尺寸的截图。

## 6. iOS 的额外风险

现在的安卓 App 是一个“壳”，内容来自你的网站。这种做法在 Google Play 没问题，
但苹果审核指南 4.2 条可能以“只是把网站包了一层”为由拒绝。降低风险的做法：

- 把页面资源打包进 App（不依赖远程网址启动），这需要改打包方式；
- 强调原生能力：后台定位、推送、相机；
- 或者不上架 App Store，用苹果的企业内部分发（需要企业开发者账号），
  或让 iPhone 员工直接用浏览器“添加到主屏幕”（已支持，但 iOS 网页不能后台定位）。

上架 App Store 还需要：苹果开发者账号（每年 99 美元）和一台 Mac（或云端 Mac 打包服务）。
