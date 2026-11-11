# iFlyGo Android 客户端

带界面的 Android VPN 客户端，Go 核心通过 `gomobile bind` 生成 AAR。

## 功能

- 填写控制面地址 + enroll token，注册并保存证书/Nebula 配置
- 一键启动/停止 VpnService（完整 TUN 隧道）
- 状态、IPv4/IPv6、运行日志
- 配置刷新
- 同一 APK 可安装在 **HarmonyOS 4 / 4.2 及以下**（Android 兼容）
- HarmonyOS NEXT（5.x）不兼容 Android APK，需要单独的 ArkTS HAP

## 构建

```bash
# 依赖：JDK 17、Android SDK 34、NDK 26.1、Gradle 8.7、Go
sdkmanager "platforms;android-34" "build-tools;34.0.0" "ndk;26.1.10909125"
go install golang.org/x/mobile/cmd/gomobile@latest
export ANDROID_NDK_HOME="$ANDROID_SDK_ROOT/ndk/26.1.10909125"
gomobile init
mkdir -p mobile/android/app/libs
gomobile bind -target=android -androidapi 24 -javapkg=com.iflyelf.iflygo.core -o mobile/android/app/libs/iflygo.aar ./mobile
gradle -p mobile/android assembleDebug
```

APK 输出：`mobile/android/app/build/outputs/apk/debug/app-debug.apk`

发布资产：`iflygo-android.apk`（由 CI 重命名并附加 `.sha256`），可从 latest Release 下载：
```bash
wget -O iflygo-android.apk \
  https://github.com/iflyelf/iflygo/releases/latest/download/iflygo-android.apk
```
当前为 debug 签名（可直接安装）；正式分发建议使用自有 release keystore 重新签名。

CI：`.github/workflows/android.yml` 自动构建并上传；tag / 手动触发时附加到 Release。

## 使用

1. 在控制台「隧道 → 客户端」创建客户端，复制 enroll token；
2. 安装 APK，填写控制面地址与 token，点「注册」；
3. 点「启动」，首次会弹出 Android VPN 授权，允许即可；
4. 状态显示「运行中」，日志可查看 Nebula 输出。

> VpnService 只接管 overlay CIDR 与不安全路由；Nebula 自身的 UDP 通过
> `addDisallowedApplication` 排除，避免回环。若需要全局代理（0/0），需额外实现
> socket protect 或全隧道策略，当前版本未开启。
