# iFlyGo 🚀

高可用 P2P 隧道 + 升级控制平台 + 远程访问/管理的一体化平台。目标：用**两个自包含的 Go 静态二进制**（`iflygo-server` / `iflygo-client`）构建一个可水平扩展、数据面去中心化的安全隧道服务，并完整复刻 `upgrade_mgr` 的主机/组件/制品/任务/RBAC/审计/AI 管理能力，附远程访问（DDNSTO）与远程管理套件（Liaison）。

## ✨ 特性

### 隧道（数据面去中心化）
- ✅ **P2P 优先**：UDP 打洞成功走直连，性能不受服务端带宽限制
- ✅ **智能数据面**：`auto`（UDP 优先，自动检测并切换 TCP 中继）/ `tcp`（强制 TCP）/ `udp`（仅 UDP）
- ✅ **零配置**：服务端自动开启 TCP 中继并通过心跳下发，客户端无需手工指定协议或地址
- ✅ **配置热加载**：服务端/客户端配置文件修改后自动生效；服务端设置可在页面配置
- ✅ **密钥可见可控**：设置页密钥默认脱敏；superadmin 可点「显示完整值」查看明文（服务端写审计）
- ✅ **端到端加密**：即使经 Relay/Edge 转发，也无法解密内容
- ✅ **高可用**：多 Lighthouse / Relay / Edge，一个挂掉自动切换
- ✅ **不安全路由 HA**：多网关加权 ECMP + 健康检查 + 自动故障切换
- ✅ **Web 管理**：Token 接入、IPAM、证书签发、配置下发、自定义域名/二级前缀
- ✅ **跨平台**：`CGO_ENABLED=0` 交叉编译，Linux / Windows / macOS / 树莓派 / NAS

### 升级控制平台（完整复刻 upgrade_mgr）
- ✅ 主机清单 / 组件 / 制品 / 升级任务（灰度编排、批次、重试、取消、回滚）
- ✅ Agent 注册 / 心跳 / 指令下发 / 进度上报 / 自升级
- ✅ 多租户 + RBAC（用户/团队/角色/服务组/组织架构）+ 审计日志
- ✅ AI 接入（MCP/令牌/助手）+ 外部文件白名单 + 快速上手引导
- ✅ 三主题（🌞暖沙米 / 🌊冷蓝 / 🌙暗黑）+ Mac 圆角 + H5 自适应

### 远程访问（DDNSTO）
- ✅ 给客户端自动分配子域名 `<slug>.<ddnsto_domain>`，一键穿透到 overlay 目标
- ✅ 复用 edge 数据面（域名 → overlay 反向代理），无带宽套餐限制

### 远程管理套件（Liaison）
- ✅ Casbin RBAC 细粒度授权（策略存库，默认 superadmin/admin 全权限）
- ✅ Web SSH 终端（xterm.js + WebSocket 桥接 x/crypto/ssh）
- ✅ 文件管理 / Web 数据库查询 / LLM 网关（OpenAI 兼容）
- ✅ Web VNC 远程桌面（纯 Go RFB 客户端 + canvas 渲染，Raw/Hextile/CopyRect/Tight/ZRLE）
- ✅ Web RDP 远程桌面（fork grdp 移除 CGO + canvas 渲染）

### 双数据库
- ✅ **PostgreSQL + SQLite 双库**：同一套代码全功能支持，`$N`/`NOW()`/数组/时间戳/并发原语跨库适配

## 🏛️ 角色划分

| 二进制 | 角色 | 职责 |
|---|---|---|
| `iflygo-server` | `console` | Web/API、数据库、IPAM、PKI、Token、配置生成、域名 |
| | `lighthouse` | UDP 发现、NAT 打洞协调 |
| | `relay` | UDP 中继 |
| | `tcp-relay` | TCP 中继（UDP over TCP 兜底，配置 `tcp_relay.listen` 后由 console 进程启动） |
| | `edge` | 443 入口、Host/SNI 路由、ACME |
| | `gateway` | 不安全路由网关（转发/NAT/子网探测） |
| `iflygo-client` | — | Token 注册、overlay、L7 隧道、服务守护、自升级 |

## 📦 当前状态

仓库骨架 + 控制面基础已就绪，业务持续开发中：

- [x] 两个二进制入口 + 版本注入
- [x] 多阶段 Dockerfile（builder=`iflyelf/ubuntu:latest`，runtime=`iflyelf/ubuntu:lite`）
- [x] GitHub Actions：CI / Docker 多架构发布 / 全平台二进制 / 上游版本更新
- [x] 控制面基础：store 双数据库（PostgreSQL + SQLite）、认证、IPAM、PKI、Token 注册、配置下发
- [x] Web 基础页面：登录 / 网络 / 客户端（响应式，原生 JS）
- [x] 客户端 Agent：enroll / run / config / status / service
- [x] overlay：嵌入 Nebula（证书、完整配置生成、启停、热加载、隧道状态）
- [x] 服务端节点：lighthouse / relay / edge / gateway 配置生成
- [x] 不安全路由 HA：多网关加权 + 健康探测 + 动态热加载（active-active / active-passive / fail-closed）
- [x] edge + 自定义域名：Host/SNI 路由反向代理（HTTP/TLS）、域名→overlay 目标
- [x] ACME 自动证书（Let's Encrypt，HTTP-01 + TLS-ALPN-01）
- [x] 证书续期 / 吊销（pki.blocklist）
- [x] Web 管理页面：客户端 / 网络 / 节点 / 路由 / 域名
- [x] 前端合并：upgrade_mgr 完整控制台（主机/组件/制品/任务/AI/RBAC/审计/设置）+ 隧道管理 + DDNSTO 远程访问 + Liaison 远程管理
- [x] 双数据库（PostgreSQL + SQLite）全量支持（81 handler + 153 路由 + 39 迁移）
- [x] DDNSTO 远程访问：子域名自动分配 + edge 穿透
- [x] Liaison 远程管理套件：Casbin RBAC / Web SSH / 文件管理 / Web DB / LLM 网关
- [x] 数据面模式 auto/tcp/udp + TCP Relay（UDP 被封锁时经 TCP 承载 overlay 流量）
- [x] 服务端/客户端配置文件热加载；服务端中继配置与网络 Nebula 设置可在页面配置

## 🔌 控制面 API（已实现）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/health` `/ready` | 健康检查 |
| POST | `/api/auth/login` `/logout` | 登录/登出 |
| GET | `/api/auth/userinfo` | 当前用户 |
| GET/POST | `/api/networks` | 网络列表/创建（自动生成 Nebula CA + 可选 nebula 设置） |
| DELETE | `/api/networks/{id}` | 删除网络 |
| GET/POST | `/api/server-nodes` | 服务端节点（lighthouse/relay/edge/gateway） |
| GET | `/api/server-nodes/{id}/bundle` | 服务端节点配置包（含 Nebula 配置） |
| DELETE | `/api/server-nodes/{id}` | 删除服务端节点 |
| GET/POST | `/api/clients` | 客户端列表/创建（分配 IP + 签发 Nebula 证书 + enroll token） |
| GET/DELETE | `/api/clients/{id}` | 客户端详情/删除 |
| POST | `/api/clients/{id}/enroll-token` | 重新生成安装 token |
| GET | `/api/clients/{id}/bundle` | 下载配置包 |
| POST | `/api/agent/register` | Agent 用 token 注册 |
| POST | `/api/agent/heartbeat` | Agent 心跳 |
| GET | `/api/agent/config` | Agent 拉取配置 |
| GET/POST | `/api/networks/{id}/routes` | 不安全路由列表/创建（加权网关） |
| DELETE | `/api/routes/{id}` | 删除路由 |
| GET/POST | `/api/edge-services` | 边缘服务（自定义域名 → overlay 目标） |
| DELETE | `/api/edge-services/{id}` | 删除边缘服务 |
| GET | `/api/server-nodes/{id}/edge-routes` | 边缘节点路由表 |

### 远程访问（DDNSTO）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/ddnsto/access` | 远程访问条目列表 |
| POST | `/api/ddnsto/access` | 开通远程访问（自动分配子域名 + 创建 edge 映射） |
| DELETE | `/api/ddnsto/access/{id}` | 删除远程访问 |

### 远程管理套件（Liaison）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/liaison/ws` | WebSocket 通道（SSH/RDP/VNC 终端） |
| GET/PUT/DELETE | `/api/liaison/files*` | 文件管理（列目录/读/写/上传/删除） |
| GET/POST/DELETE | `/api/liaison/db/connections*` | Web 数据库连接管理 |
| POST | `/api/liaison/db/query` | 执行 SQL 查询 |
| GET/POST | `/api/liaison/llm/*` | LLM 网关（模型/对话） |
| GET/POST/DELETE | `/api/liaison/policies` | Casbin 策略管理（superadmin） |

## 🔧 本地构建

```bash
make build       # 构建当前平台 server/client 到 bin/
make cross       # 交叉编译全部平台到 dist/
make test        # 单元测试
make test-race   # 并发竞态检查（含 Nebula 测试补丁）
make vet         # 静态检查
```

构建命令等价于：

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/iflygo-server ./cmd/server
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/iflygo-client ./cmd/client
```

## 🐳 Docker

```bash
docker build -t iflyelf/iflygo:latest .
docker run --rm iflyelf/iflygo:latest -version
```

镜像基于 `iflyelf/ubuntu:latest` 构建、运行于 `iflyelf/ubuntu:lite`，支持多架构 `linux/amd64,linux/arm64`。

## 📡 端口

| 端口 | 协议 | 用途 |
|---|---|---|
| 8080 | TCP | 控制面 Web/API |
| 443 | TCP/UDP | Edge 入口 / TCP 隧道 |
| 6688 | UDP | Lighthouse/Relay |
| 控制面端口 +1（默认，可配置） | TCP | TCP 中继（默认自动开启；`tcp_relay.listen=off` 关闭） |

## 🧵 数据面与 TCP Relay

Nebula 原生只有 UDP。当网络**只允许 TCP**（UDP 全部被拦截）时，iflygo 在客户端
本地做一层「UDP 桥」，把 overlay 数据报封装进 TCP 隧道：

```
Nebula ──UDP──▶ 127.0.0.1:<随机端口>（本地桥）
                        │ TCP（长度前缀帧）
                        ▼
              iflygo-server: tcp-relay
                        │ UDP（每个隧道独立 socket）
                        ▼
              lighthouse / relay 真实 endpoint
```

### 三种数据面模式

| 模式 | 行为 | 适用场景 |
|---|---|---|
| `auto`（默认） | UDP 直连优先；连续无响应自动切 TCP 中继；直连恢复自动切回 | 绝大多数网络，兼顾性能与可达性 |
| `tcp` | 始终经 TCP 中继，不回落直连 | UDP 被完全封锁、或 UDP 时通时断 |
| `udp` | 仅原生 UDP，不使用中继 | 明确禁止中继/带宽受限 |

模式优先级：**命令行 > 客户端配置文件 > 服务端心跳下发 > 内置默认**。

### 零配置（推荐）

- 服务端默认自动监听「控制面端口 +1」的 TCP 中继，并把模式/地址/令牌随心跳下发。
- 客户端直接 `iflygo-client run` 即可：自动发现中继、自动判断是否需要 TCP。
- 页面路径：**设置 → 数据面 / TCP 中继**，可配置模式、监听地址、对外地址与共享令牌，保存即时生效。

服务端配置文件（可选，支持热加载）：

```yaml
tcp_relay:
  mode: auto            # auto / tcp / udp（下发给客户端作为默认）
  listen: ":8443"       # 留空 = 控制面端口 +1；off = 关闭
  advertise: ""         # 对外地址，留空按 external_url/请求 Host 自动推断
  token: "change-me"    # 可选共享令牌（客户端自动获取）
```

环境变量：`TCP_RELAY_MODE`、`TCP_RELAY_LISTEN`、`TCP_RELAY_ADVERTISE`、`TCP_RELAY_TOKEN`。

### 客户端配置文件（支持热加载）

默认路径 `<state-dir>/client.yaml`（可用 `--config` 或 `IFLYGO_CLIENT_CONFIG` 指定）：

```yaml
interval: 30s
data_plane:
  mode: auto                 # auto / tcp / udp
  tcp_relay: ""              # 留空自动发现；也可填 relay.example.com:8443
  tcp_relay_token: ""        # 留空自动从服务端获取
```

修改后约 2 秒内自动生效；数据面模式变化会自动重启 overlay（隧道短暂重连），
心跳间隔变化立即生效。

命令行仍可临时覆盖：

```bash
iflygo-client run --data-plane tcp
iflygo-client run --tcp-relay relay.example.com:8443 --tcp-relay-token change-me
iflygo-client run --tcp-relay off   # 等价 --data-plane udp
```

### 服务端配置文件热加载

`iflygo-server -c etc/config.yaml` 会每 3 秒检查文件变化：

- `tcp_relay`：即时重建监听并生效；
- 其它 section（数据库、监听端口、Redis 等）：日志提示「需重启生效」；
- 页面已保存的 `tcp_relay.*` 优先级更高，文件变更不会覆盖页面配置。

### 原理与安全

- 客户端把 Nebula `static_host_map` 与 `relay.relays` 中每个真实上游 `host:port`
  改写为本地桥端口；桥用「4 字节大端长度 + 载荷」帧写入 TCP 隧道；服务端按握手目标
  建立独立 UDP socket 转发，响应原路返回。纯 overlay IP（无端口）条目原样保留。
- 本地桥默认直连 UDP（与原生等价）；只有判定直连不可用时才启用 TCP，
  避免长期承担额外一跳与 TCP 队头阻塞的代价。
- `token` 非空时服务端用常量时间比较校验握手令牌，客户端通过已认证心跳自动获取；
  未设置令牌时启动会打印告警，建议用防火墙限制来源。

## 🚀 发布流程

- 推 tag `v*` 触发：Docker 多架构发布（Docker Hub + 华为云 SWR）+ 全平台二进制 Release
- `workflow_dispatch` 手动触发构建
- `update-version.yml` 每天检测上游 Nebula 最新版本并自动开 PR

## 🔗 上游子模块同步

本项目以 git 子模块引用上游，跟随其改动：

| 子模块 | 路径 | 用途 | 跟随方式 |
|---|---|---|---|
| Nebula | `third_party/nebula`（`go.mod replace`） | 数据面引擎 | 自动：`update-version.yml` 检测最新 release → 更新子模块 + go.mod + `NEBULA_VERSION` → 开 PR；`scripts/patch_nebula.py` 修复 disabledTun 的测试 data race（幂等） |
| upgrade_mgr | `third_party/upgrade_mgr` | 控制面/前端/CI 对齐参考 | 自动更新子模块开 PR，功能按 diff 手动移植（其 `internal/` 不可跨模块导入） |
| grdp | `third_party/grdp`（`go.mod replace`） | Web RDP 客户端 | 子模块 + `scripts/patch_grdp.py` 构建时应用补丁（移除 CGO `import "C"`、删除 broken VNC `client/rfb.go`，以支持 `CGO_ENABLED=0`） |

**手动同步命令：**

```bash
# 克隆时初始化子模块
git clone --recurse-submodules https://github.com/iflyelf/iflygo.git

# 更新 Nebula 到指定 tag（或最新 release）
cd third_party/nebula && git fetch --depth 1 origin tag v1.11.1 && git checkout v1.11.1 && cd ../..
go mod edit -require="github.com/slackhq/nebula@v1.11.1" && go mod tidy
git add third_party/nebula go.mod go.sum NEBULA_VERSION && git commit

# 更新 upgrade_mgr 到最新 main（仅作参考）
cd third_party/upgrade_mgr && git fetch --depth 1 origin main && git checkout FETCH_HEAD && cd ../..
git add third_party/upgrade_mgr && git commit

# 更新 grdp 到最新（上游 2018 年已停更，通常无需更新）
cd third_party/grdp && git fetch --depth 1 origin master && git checkout FETCH_HEAD && cd ../..
python3 scripts/patch_grdp.py third_party/grdp   # 重新应用 iflygo 补丁
git add third_party/grdp && git commit
```

> 升级 Nebula / grdp 后如遇编译/行为不兼容，CI 会在 PR 中暴露，请审查后合并。
> 注：grdp 子模块在克隆后是未打补丁的原始代码，**编译前必须运行 `make prepare`**（应用 grdp 补丁 + 前端注入）；CI/Docker 已自动执行。

## 🔐 安全

凭据（Docker Hub / 华为云 SWR）只存放于 GitHub Actions Secrets，**不写入代码**。

## 📄 许可证

[MIT](./LICENSE)
