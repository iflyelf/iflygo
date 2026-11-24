# CrossDesk 全部能力原生并入 iFlyGo（路线图）

> 目标：把 CrossDesk 的远程桌面能力从「独立服务」升级为 iFlyGo 内置能力。
> 本文是实施蓝图，不代表已在单次提交中完成。

## 1. 为什么不能一次完成

CrossDesk 不是单一服务，而是完整的跨平台远控产品：

| 层 | 现有实现 | 原生并入难度 |
|---|---|---|
| 信令/设备/在线状态/TURN 凭据 | C++ + SQLite | 可用 Go 重写，工作量中等 |
| Web 客户端 | 静态 HTML/JS + WebRTC | 可内置 iFlyGo 静态资源 |
| 桌面采集/编码/解码/音频 | C++ / Slint / 平台 API | 需原生 C++/Rust + 各平台权限 |
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

### Phase 3：原生桌面 Agent / 媒体层

- 独立原生 Agent（C++/Rust）负责：
  - 屏幕采集、H.264/AV1 编解码、硬件加速；
  - 音频采集/播放；
  - 键鼠注入、剪贴板、多显示器；
  - 文件传输、隐私屏、登录前远控。
- 与 iFlyGo 原生信令对接；
- 优先 Windows/Linux，再 macOS；
- Agent 可由 iFlyGo 安装/升级/配置下发。

### Phase 4：DeskHub / 群控 / 移动端 / CLI / MCP

- iFlyGo Web 实现桌面墙、分组、群控、批量文件分发；
- Android/iOS/HarmonyOS 控制端；
- Go CLI 与 MCP Server；
- 可信设备、会话审计、权限策略统一到 iFlyGo RBAC。

## 4. 建议的技术选择

- **信令/设备/管理**：Go（iFlyGo 原生）。
- **Web 客户端**：内置静态资源 + WebRTC。
- **媒体/采集**：C++/Rust 原生 Agent，不做 Go 重写。
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
  - TURN 临时凭据（HMAC-SHA1，兼容 Coturn REST API）；
  - `/crossdesk/stats` 统计；
  - Hub 并发/连接替换/离线清理测试。
- 迁移 `0021_crossdesk_devices`（PG/SQLite）。
- 设置项：原生信令开关、信令路径、TURN 主机/端口/密钥/有效期。
- 路由：`/crossdesk/signal`（WSS）、`/crossdesk/stats`。

### 尚未完成（后续阶段）

- CrossDesk 设备密钥登录（Ed25519/配对）；
- 完整管理后台（设备/会话查询、断开、清理）；
- 官方客户端兼容性回归（需真机/客户端联调）；
- Web 客户端内置与 SSO；
- 桌面采集/媒体/文件传输/DeskHub/移动端/CLI/MCP。

## 6. 近期可交付

1. 用官方桌面/Web 客户端做 Phase 1 协议兼容性回归；
2. 补齐设备密钥/重连/改密；
3. 内置 Web 客户端；
4. 再进入媒体层。

> 在 Phase 3 完成前，CrossDesk 官方客户端仍负责桌面采集与媒体，
> iFlyGo 负责信令、设备、入口、状态与管理。
