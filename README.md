# iFlyGo 🚀

高可用 P2P 隧道 + Web 控制面。目标：用**两个自包含的 Go 静态二进制**（`iflygo-server` / `iflygo-client`）构建一个可水平扩展、数据面去中心化的安全隧道服务。

## ✨ 特性（规划）

- ✅ **P2P 优先**：UDP 打洞成功走直连，性能不受服务端带宽限制
- ✅ **自动兜底**：打洞失败走 Relay（UDP），UDP 被封锁时走 TCP Relay
- ✅ **端到端加密**：即使经 Relay/Edge 转发，也无法解密内容
- ✅ **高可用**：多 Lighthouse / Relay / Edge，一个挂掉自动切换
- ✅ **不安全路由 HA**：多网关加权 ECMP + 健康检查 + 自动故障切换
- ✅ **Web 管理**：Token 接入、IPAM、证书签发、配置下发、自定义域名/二级前缀
- ✅ **跨平台**：`CGO_ENABLED=0` 交叉编译，Linux / Windows / macOS / 树莓派 / NAS

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
- [ ] edge + 自定义域名
- [ ] 完整配置热加载 / 证书续期

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

## 🔐 安全

凭据（Docker Hub / 华为云 SWR）只存放于 GitHub Actions Secrets，**不写入代码**。

## 📄 许可证

[MIT](./LICENSE)
