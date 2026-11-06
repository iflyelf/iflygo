# iFlyGo 🚀

高可用 P2P 隧道 + 升级控制平台 + 远程访问/管理的一体化平台。目标：用**两个自包含的 Go 静态二进制**（`iflygo-server` / `iflygo-client`）构建一个可水平扩展、数据面去中心化的安全隧道服务，并完整复刻 `upgrade_mgr` 的主机/组件/制品/任务/RBAC/审计/AI 管理能力，附远程访问（DDNSTO）与远程管理套件（Liaison）。

## ✨ 特性

### 隧道（数据面去中心化）
- ✅ **P2P 优先**：UDP 打洞成功走直连，性能不受服务端带宽限制
- ✅ **自动兜底**：打洞失败走 Relay（UDP），UDP 被封锁时走 TCP Relay
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

## 🚀 发布流程

- 推 tag `v*` 触发：Docker 多架构发布（Docker Hub + 华为云 SWR）+ 全平台二进制 Release
- `workflow_dispatch` 手动触发构建
- `update-version.yml` 每天检测上游 Nebula 最新版本并自动开 PR

## 🔗 上游子模块同步

本项目以 git 子模块引用上游，跟随其改动：

| 子模块 | 路径 | 用途 | 跟随方式 |
|---|---|---|---|
| Nebula | `third_party/nebula`（`go.mod replace`） | 数据面引擎 | 自动：`update-version.yml` 检测最新 release → 更新子模块 + go.mod + `NEBULA_VERSION` → 开 PR |
| upgrade_mgr | `third_party/upgrade_mgr` | 控制面/前端/CI 对齐参考 | 自动更新子模块开 PR，功能按 diff 手动移植（其 `internal/` 不可跨模块导入） |
| grdp | `third_party/grdp`（`go.mod replace`，本地 fork） | Web RDP 客户端 | 本地 fork：已移除 CGO（`plugin/channel.go` 的 `import "C"`）与 broken 的 `client/rfb.go`，以支持 `CGO_ENABLED=0` 静态编译 |

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
```

> 升级 Nebula 后如遇编译/行为不兼容，CI 会在 PR 中暴露，请审查后合并。

## 🔐 安全

凭据（Docker Hub / 华为云 SWR）只存放于 GitHub Actions Secrets，**不写入代码**。

## 📄 许可证

[MIT](./LICENSE)
