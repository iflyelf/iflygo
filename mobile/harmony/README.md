# HarmonyOS NEXT 原生 HAP（规划 / 阻塞说明）

当前仓库的 Android APK **不能在 HarmonyOS NEXT（5.x）安装**。NEXT 已移除 Android
兼容层，必须单独构建原生 HAP。

## 当前状态

**未构建。** 当前环境没有 HarmonyOS NEXT SDK/NDK、DevEco Studio、ohpm/hvigor 与
华为签名材料，无法生成可安装、可验证的 HAP。本文档记录已确认的技术路线和阻塞项，
避免用不可运行的占位工程冒充“已实现”。

## 技术路线

```text
ArkUI 页面（注册/启动/停止/状态/日志）
        │
        ▼
VpnExtensionAbility（module.json5: type = "vpn.extension"）
        │  TUN fd / 生命周期
        ▼
NAPI 桥接（libiflygo_core.so）
        │
        ▼
Nebula 数据面（Go 核心或 C 核心）
```

### 1. 页面与 VPN 扩展

- HarmonyOS NEXT API 11+ 提供 `VpnExtensionAbility`，与 Android `VpnService` 类似；
- 在 `module.json5` 的 `extensionAbilities` 中声明 `type: "vpn.extension"`；
- 仅允许启动/停止本应用声明的 VPN 扩展；
- 首次启动会弹系统 VPN 授权。

### 2. 数据面核心

这是主要阻塞项：

- 当前 Android 客户端复用 `mobile/` 的 Go 核心（gomobile bind）；
- Go **没有官方 `GOOS=openharmony` 目标**，不能直接把现有 gomobile AAR 思路套到 HAP；
- 可行方案：
  1. 用 OpenHarmony NDK（`aarch64-linux-ohos`）交叉编译 Nebula C 核心 + NAPI 包装；
  2. 将 `third_party/nebula` 的 Go 核心移植到 OpenHarmony（需要自定义 libc/系统调用适配，工作量大且上游无保证）；
  3. 以 ArkTS 重写数据面（不现实：Nebula 的加密、打洞、路由、证书链复杂度高）。
- 推荐方案：**Nebula C 核心 + OHOS NDK + NAPI**，并由 `VpnExtensionAbility` 把 TUN fd
  传给 native 层。

### 3. 构建与签名

需要：

- DevEco Studio NEXT / HarmonyOS SDK（含 API 12+ SDK、NDK、hvigor、ohpm）；
- 华为开发者账号与应用签名证书（.p12/.cer/.p7b）；
- `hvigorw assembleHap` 或 DevEco 构建；
- 真机安装验证（模拟器对 VPN 扩展支持有限）。

建议 CI 使用自托管 runner 或带华为 SDK 的构建机，并把签名材料放入 Secrets；
当前 GitHub 托管 runner 不包含 HarmonyOS SDK，无法直接构建。

## 建议实施顺序

1. 在 DevEco Studio 创建空的 Native HAP，跑通 ArkUI 页面；
2. 声明 `VpnExtensionAbility`，验证系统授权弹窗与生命周期；
3. 用 OHOS NDK 编译一个最小 NAPI `.so`，验证 TUN fd 传递；
4. 移植/编译 Nebula 核心，接入 enroll Bundle（证书/配置/routes）；
5. 对齐现有服务端参数下发与 `config_rev` 自动刷新；
6. 配置签名、真机回归、发布 HAP（AppGallery 或企业分发）。

在完成以上步骤前，HarmonyOS NEXT 用户请使用系统内置 VPN/其他兼容客户端，
Android APK 仅适用于 HarmonyOS 4/4.2 及以下。
