# 员工工作台 App 外壳

这里是安卓 App 的工程（Capacitor）。App 本身不带页面：它打开某一套部署的 `/work`，页面、登录和以后的更新都来自服务器。外壳只提供浏览器做不到的两件事：

- 手机自带的推送（Firebase Cloud Messaging），锁屏也能收到新配送单；
- 后台定位，骑手上班时锁屏或切到别的应用也继续上报位置。

页面里和 App 打交道的代码在 `web/src/views/work/native.ts`、`duty.ts`、`push.ts`。

## 打包

打包和发给员工的完整步骤见 [docs/DEPLOY.md](../docs/DEPLOY.md) 第 15 节。一般不用在自己电脑上打包，GitHub 上的 “Android app” 工作流会打好。

本机打包需要 Node 22、JDK 21 和 Android SDK（`ANDROID_HOME`）：

```bash
cd mobile
npm ci
# 推送：把 Firebase 的 google-services.json 放到 android/app/ 下（不要提交）
WORKBENCH_URL=https://order.example.com npx cap sync android
cd android && ./gradlew assembleDebug        # app/build/outputs/apk/debug/app-debug.apk
```

打包时读的环境变量：

| 变量 | 用途 |
|---|---|
| `WORKBENCH_URL` | 必填。这套部署的地址，结尾不带斜杠。必须是 HTTPS |
| `ANDROID_KEYSTORE_FILE`、`ANDROID_KEYSTORE_PASSWORD` | 正式签名用的密钥文件和密码。设了以后用 `assembleRelease` |
| `ANDROID_KEY_ALIAS`、`ANDROID_KEY_PASSWORD` | 选填，默认别名 `workbench`，密码同上 |
| `BUILD_NUMBER` | 版本号，每次要比上一次大，否则手机不让覆盖安装 |

包名固定是 `app.waimai.workbench`，在 Firebase 里添加安卓应用时要填这个。

## 改了外壳之后

改了 `capacitor.config.js` 或加了插件后，运行 `npx cap sync android`，把 `android/` 下的变化一起提交。`android/app/src/main/assets/` 下的文件是打包时生成的，不提交。
