# CrossDesk 全部能力原生并入 iFlyGo（路线图）

> 目标：把 CrossDesk 的远程桌面能力从「独立服务」升级为 iFlyGo 内置能力。
> 本文是实施蓝图，不代表已在单次提交中完成。

## 1. 为什么不能一次完成

CrossDesk 不是单一服务，而是完整的跨平台远控产品：

| 层 | 现有实现 | 原生并入难度 |
|---|---|---|
| 信令/设备/在线状态/TURN 凭据 | C++ + SQLite | 可用 Go 重写，工作量中等 |
| Web 客户端 | 静态 HTML/JS + WebRTC | 可内置 iFlyGo 静态资源 |
| 桌面采集/编码/解码/音频 | C++ / Slint / 平台 API | 采集/输入可用纯 Go；H.264/音频需原生编码器 |
| 媒体传输 | MiniRTC / ICE / DTLS / SRTP | 可用现有 WebRTC 库，工作量大 |
| 文件传输 / 剪贴板 / 多显示器 | C++ 客户端 | 需与媒体层一起重写 |
| DeskHub 桌面墙 / 群控 | C++ GUI | 可改为 iFlyGo Web + 客户端 Agent |
| CLI / MCP | 独立客户端 | 可 Go 重写 |
| iOS / Android / HarmonyOS | 原生客户端 | 各自独立工程 |
| 可信设备 / 登录前远控 / 无头 | C++/平台服务 | 需平台原生实现 |

因此必须分阶段，先服务端/Web，再媒体与客户端。

## 2. 许可证

- iFlyGo：MIT
- CrossDesk 客户端：GPL-3.0
- CrossDesk Server：LGPL-3.0
- CrossDesk Web：LGPL-3.0

代码级并入必须确认许可证兼容性；更稳妥的方式是：
- 信令/设备/管理面用 Go 重写（不复制 C++ 代码）；
- 媒体/采集层保持独立原生进程或动态库，单独分发并附源码；
- 或整体转为 GPL/LGPL 兼容发布。

## 3. 分阶段路线

### Phase 1：原生 Go 信令与设备管理（建议先做）

在 iFlyGo 内实现：

- WSS 信令端点（兼容 CrossDesk 客户端协议）；
- 设备注册、设备 ID/密码、在线状态；
- 登录、Join、Offer/Answer/ICE Candidate 转发；
- TURN 临时凭据（HMAC）；
- 会话恢复、设备列表、`/stats`；
- 管理后台：设备/会话查询、断开、清理；
- 复用 iFlyGo PostgreSQL/SQLite、RBAC、审计。

验收：

- CrossDesk 官方桌面/移动/Web 客户端能登录自建 iFlyGo 信令；
- 两个客户端可完成 P2P/TURN 会话；
- `/stats` 与后台数据一致；
- 断线重连、密码验证、设备列表正常。

### Phase 2：内置 Web 客户端 + SSO

- 将 CrossDesk Web 客户端作为 iFlyGo 静态资源（独立目录/许可证声明）；
- 注入 `CROSSDESK_CONFIG` 指向 iFlyGo 原生 WSS；
- iFlyGo 登录后免二次输入设备密码（可选 SSO 绑定）；
- Web 端文件传输/剪贴板/多显示器与官方 Web 对齐。

### Phase 3：纯 Go 静态 Agent（已实现初始版）

在不引入 CGO 的前提下，`iflygo-client crossdesk host` 提供主机端：

- 平台屏幕采集：
  - Linux：X11 `GetImage`（需 X11 会话；XWayland 仅能捕获 X11 窗口）；
  - Windows：GDI `BitBlt` + `GetDIBits`（虚拟桌面）；
  - macOS：CoreGraphics（purego 动态调用，无需 CGO）；
- 画面编码：`image/jpeg` + `x/image/draw` 缩放，分片后经
  `data-stream` DataChannel 发送（自定义 `CDS1` 分片协议）；
- 键鼠注入：
  - Linux：XTEST；
  - Windows：`SetCursorPos` + `mouse_event` / `keybd_event`；
  - macOS：`CGEventPost`；
- 文本剪贴板双向同步（`web-clipboard`，16 KiB 分片、128 KiB 上限）；
- 画质/帧率协商（`type 11/12`，质量 0/1/2 映射 JPEG 45/62/78）；
- 受限目录文件传输：
  - `--file-root` 可重复，未指定则完全关闭；
  - 目录浏览（分页/快照）、文件/目录下载、文件/目录上传；
  - 目录下载先向浏览器发送 `prepare` 预留子目录；
  - 路径穿越与符号链接拒绝，上传永不覆盖同名文件；
  - FileChunk/ACK 协议兼容内置 Web 客户端；
- 多显示器枚举与切换：
  - Linux：RandR CRTC；
  - Windows：EnumDisplayMonitors；
  - macOS：CGGetActiveDisplayList；
  - 键鼠坐标随当前显示器区域映射；Web 客户端显示选择器自动填充；
- 与原生 iFlyGo 信令登录、Join、Offer/Answer/ICE 全流程对接；
- 首次运行自动注册设备，生成并保存 Ed25519 设备密钥（`0600`）；
  已有设备首次绑定密钥时要求一次密码，之后仅签名登录；
- 一个 Agent 可同时服务多个 Guest（桌面墙）；
- 内置 Web 客户端新增 canvas 画面渲染，自动适配 JPEG 帧通道。

当前取舍：

- 未使用 H.264/AV1（纯 Go 无成熟编码器），采用分片 JPEG；
- 由 iFlyGo 内置 Web 客户端渲染，官方 CrossDesk 客户端不识别该画面通道；
- 音频与 Wayland 原生截图仍需后续原生层。

后续可演进：

- 接入 WebRTC 视频轨道（需 CGO/原生编码器或硬件编码 SDK）；
- 平台音频采集与 Opus；
- 硬件编码；
- 隐私屏、登录前远控、服务安装与自动升级。

### Phase 4：DeskHub / 群控 / 移动端 / CLI / MCP

- iFlyGo Web 实现桌面墙、分组、群控、批量文件分发；
- Android/iOS/HarmonyOS 控制端；
- Go CLI 与 MCP Server；
- 可信设备、会话审计、权限策略统一到 iFlyGo RBAC。

## 4. 建议的技术选择

- **信令/设备/管理**：Go（iFlyGo 原生）。
- **Web 客户端**：内置静态资源 + WebRTC。
- **媒体/采集**：纯 Go 静态 Agent（已实现屏幕/输入/JPEG 通道）；
  需要 H.264/AV1/音频/硬件加速时再引入 C++/Rust 编码层。
- **移动端**：复用 CrossDesk 原生客户端或单独 HAP/APK。
- **许可证**：原生 Agent/客户端独立分发，iFlyGo 保持 MIT；
  或整体采用 GPL/LGPL 兼容许可。

## 5. 当前进度

### 已完成（Phase 1 初始）

- `internal/crossdesk` 原生 Go 信令包：
  - 设备 ID / 六位密码注册与校验；
  - `web` / `cli` 临时控制器身份；
  - `login`、`ping/pong`、`join_transmission`、`user_leave_transmission`；
  - `offer` / `answer` / `new_candidate` / `new_candidate_mid` 转发；
  - `query_user_id_list`、`turn_credentials`、`recent_connections_presence`；
  - 重连令牌（30 天）与 `reconnect_token` 登录；
  - `change_password`（6 位、request_id 幂等）；
  - `disconnect_peer`、`revoke_reconnect_credential`；
  - Ed25519 设备密钥登录：`login_challenge` + 签名校验，绑定后禁止密码登录；
  - `session_ticket` / `session_report` / `session_resumed` 会话恢复；
  - 会话开始/结束写入审计（`crossdesk.session_start/end`）；
- MCP 管理工具：状态、设备/会话列表、免密票据、桌面墙、断开（写工具需 AI 令牌允许）；
- CLI 管理：`iflygo-client crossdesk status/devices/connect/deskhub`（管理面）；
  - TURN 临时凭据（HMAC-SHA1，兼容 Coturn REST API）；
  - `/crossdesk/stats` 统计；
  - Hub 并发/连接替换/离线清理测试。
- 迁移 `0021_crossdesk_devices`（PG/SQLite）。
- 设置项：原生信令开关、信令路径、TURN 主机/端口/密钥/有效期。
- 路由：`/crossdesk/signal`（WSS）、`/crossdesk/stats`。

### 已完成（Phase 2 初始）

- 内置 CrossDesk Web 客户端静态资源：`/crossdesk/web/`；
- 动态注入 `window.CROSSDESK_CONFIG`：
  - `signalingUrl` 指向 iFlyGo 原生 WSS；
  - STUN 使用配置的 TURN 主机/端口；
  - `clientTag=web`；
- 原生信令页面直接 iframe 内置 Web 客户端；
- 设备列表「连接」按钮可带 `device_id` 打开并预填 Web 客户端；
- Web 桌面墙：最多 9 台设备 iframe 网格（独立会话，群控待后续）；
- iFlyGo 管理员「连接」会生成 60 秒一次性 join_ticket，Web 客户端免输设备密码；
- 设备「授权」可把免密连接权限授予普通 iFlyGo 用户（`crossdesk_device_users`）；
- 票据记录签发人、来源 IP 与使用 IP；
- 资源通过 `go:embed` 打包进服务端二进制；
- 保留 CrossDesk Web 的 LGPL-3.0 LICENSE。

尚未完成：

- 更细粒度的角色/分组授权（当前按用户名授权设备）；
- 官方 Web 客户端完整功能回归；
- 纯 Go Agent 的音频、Wayland 原生截图与官方客户端视频兼容。

### 尚未完成（后续阶段）

- 官方 CrossDesk 客户端与本 Agent 的视频互通（需 H.264/VP8 编码）；
- 音频采集/播放；
- Wayland 原生截图；
- Android/iOS/HarmonyOS NEXT 原生控制端；
- 媒体层的 MCP/CLI 远程操作（当前仅为管理面）。

### 已完成（Phase 3 初始）

- `internal/crossdesk/agent`：纯 Go 主机端（信令、Pion DataChannel、采集/输入）；
- `cmd/client crossdesk host`：静态二进制内建 Agent；
- 平台实现：Linux X11 / Windows GDI+SendInput / macOS CoreGraphics；
- Web 客户端 `frame_view.js`：分片 JPEG 重组与 canvas 渲染；
- 单元测试：分片协议、缩放编码、URL 规范化、键位映射；
- 集成测试：真实 Hub + Pion Guest，验证 Offer/Answer、画面帧与键鼠注入。

## 6. 近期可交付

1. 用官方桌面/Web 客户端做 Phase 1 协议兼容性回归；
2. 补齐官方客户端与纯 Go Agent 的视频互通（H.264/VP8）；
3. 音频；
4. 移动端/CLI/MCP 的媒体操作。

> 当前 CrossDesk 官方客户端仍使用 WebRTC 视频；纯 Go Agent 提供 JPEG 画面通道，
> 由 iFlyGo 内置 Web 客户端渲染。两者信令兼容，媒体层暂不互通。
