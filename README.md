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
- ✅ **多种部署**：二进制/systemd、Windows 服务（SCM）、Docker Compose、Nginx 反向代理示例
- ✅ **移动客户端**：Android APK（完整 VpnService 隧道，兼容 HarmonyOS 4/4.2 及以下）

### 升级控制平台（完整复刻 upgrade_mgr）
- ✅ 主机清单 / 组件 / 制品 / 升级任务（灰度编排、批次、重试、取消、回滚）
- ✅ Agent 注册 / 心跳 / 指令下发 / 进度上报 / 自升级
- ✅ 多租户 + RBAC（用户/团队/角色/服务组/组织架构）+ 审计日志
- ✅ AI 接入（MCP/令牌/助手）+ 外部文件白名单 + 快速上手引导
- ✅ 主机自动合并（隧道 IP / 出口 IP+主机名）、在线删除保护与强制删除、手工新增主机
- ✅ Casdoor/FlyIAM 用户目录与用户管理（创建/更新/重置密码/管理员标记/本地授权）
- ✅ 用户字段定义自动同步（FlyIAM → 本地，配置/进度/日志）与 consul_mgr 按服务名称精确同步
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
- [x] 双数据库（PostgreSQL + SQLite）全量支持（58 handler + 192 路由 + 44 迁移）
- [x] DDNSTO 远程访问：子域名自动分配 + edge 穿透
- [x] Liaison 远程管理套件：Casbin RBAC / Web SSH / 文件管理 / Web DB / LLM 网关
- [x] 数据面模式 auto/tcp/udp + TCP Relay（UDP 被封锁时经 TCP 承载 overlay 流量）
- [x] 服务端/客户端配置文件热加载；服务端中继配置与网络 Nebula 设置可在页面配置
- [x] 主机自动合并 / 在线删除保护与强制删除 / 手工新增主机
- [x] Casdoor 用户目录/用户管理、用户字段自动同步、consul_mgr 按服务名称同步
- [x] Android APK 客户端：gomobile + VpnService 完整隧道（兼容 HarmonyOS 4）
- [x] 部署文档与模板：systemd / Windows SCM / Docker Compose / Nginx

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

### 主机 / 用户目录（upgrade_mgr 同步新增）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/hosts` | 手工新增主机（租户隔离，IP 冲突 409） |
| GET | `/api/hosts/deletable-ids` | 按当前筛选返回可删除主机 ID（`force=1` 可包含在线主机） |
| GET | `/api/consul-mgr/services` | 列出 consul_mgr 服务组内服务名称（按服务同步） |
| GET | `/api/users/directory` | Casdoor 用户目录（分页/搜索，合并本地租户/角色） |
| POST | `/api/casdoor/users` | 创建 Casdoor 用户并开通本地授权 |
| PUT | `/api/casdoor/users/{username}` | 更新 Casdoor 用户资料 |
| POST | `/api/casdoor/users/{username}/reset-password` | 重置密码 |
| POST | `/api/casdoor/users/{username}/admin` | 设置/取消 Casdoor 管理员 |
| GET/PUT | `/api/flyiam/sync-user-fields/config` | 用户字段自动同步配置 |
| GET | `/api/flyiam/sync-user-fields/progress` | 当前同步进度 |
| GET/DELETE | `/api/flyiam/sync-user-fields/logs` | 同步历史/清理日志 |

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

## 🚀 部署

> 发布产物：GitHub Releases 的 `latest` 提供 7 个平台的
> `iflygo-server-*` / `iflygo-client-*`（Linux/macOS 为 `.tar.gz`，Windows 为 `.zip`，
> zip 内已包含 `wintun.dll`）。
> 容器镜像：`iflyelf/iflygo:latest`、`swr.cn-east-3.myhuaweicloud.com/iflyelf/iflygo:latest`
>（`linux/amd64`、`linux/arm64`）。

### 部署拓扑

| 组件 | 典型位置 | 说明 |
|---|---|---|
| `iflygo-server`（console） | 控制面主机 + PostgreSQL | Web/API、IPAM、PKI、配置生成、DDNSTO/Liaison |
| `iflygo-server`（lighthouse/relay/edge/gateway） | 独立节点/容器 | Nebula 数据面；每个节点从控制台下载自己的配置包 |
| `iflygo-client` | Linux / Windows / macOS | enroll 后运行；需要 TUN 与 `NET_ADMIN` |
| PostgreSQL / SQLite | console 依赖 | 生产推荐 PostgreSQL；SQLite 适合单机/体验 |

### 前置条件

- Linux：内核支持 TUN/TAP，存在 `/dev/net/tun`；客户端与数据面节点需要 root 或 `CAP_NET_ADMIN`。
- Windows 客户端：Windows 10/11 或 Server 2016+，管理员权限；使用 zip 包（内含 `wintun.dll`）。
- 端口（默认值，均可配置）：

| 端口 | 协议 | 用途 |
|---|---|---|
| 8090 | TCP | 控制面 Web/API（`server.port`） |
| 8091 | TCP | TCP 中继（默认控制面端口 +1；`tcp_relay.listen`） |
| 443 | TCP/UDP | Edge HTTPS / Nebula（数据面节点） |
| 6688 | UDP | Lighthouse / Relay |
| 80 | TCP | 可选：ACME HTTP-01 |
| 5432 / 6379 | TCP | 外部 PostgreSQL / Redis（不对外暴露） |

- 生产建议 HTTPS。`server.external_url` 必须填**浏览器/客户端实际访问地址**，不是监听地址。

---

### 方式一：二进制部署（Linux systemd，推荐）

#### 1. 下载并安装

```bash
# 按需替换 ARCH：amd64 / arm64 / arm
ARCH=amd64
BASE=https://github.com/iflyelf/iflygo/releases/latest/download

wget -c -O /tmp/iflygo-server.tar.gz  "$BASE/iflygo-server-linux-${ARCH}.tar.gz"
wget -c -O /tmp/iflygo-client.tar.gz  "$BASE/iflygo-client-linux-${ARCH}.tar.gz"
tar -xzf /tmp/iflygo-server.tar.gz -C /tmp
tar -xzf /tmp/iflygo-client.tar.gz -C /tmp

install -m 0755 "/tmp/iflygo-server-linux-${ARCH}" /usr/local/bin/iflygo-server
install -m 0755 "/tmp/iflygo-client-linux-${ARCH}" /usr/local/bin/iflygo-client
```

> 国内访问慢时可使用代理前缀（可选）：
> `https://proxy.xiaonuo.live/proxy?url=<原始 GitHub URL>`。

#### 2. 准备数据库

生产推荐 PostgreSQL 12+：

```bash
sudo -u postgres psql <<'SQL'
CREATE USER iflygo WITH PASSWORD 'change-me';
CREATE DATABASE iflygo OWNER iflygo;
SQL
```

单机/体验也可用 SQLite：

```bash
export DATABASE_DRIVER=sqlite
export SQLITE_PATH=/var/lib/iflygo/iflygo.db
```

> 首次启动会自动执行迁移，无需手工导入 SQL。SQLite 仅建议单实例。

#### 3. 目录与配置

```bash
install -d -m 0755 /etc/iflygo /var/lib/iflygo/artifacts
```

`/etc/iflygo/config.yaml`：

```yaml
server:
  host: 0.0.0.0
  port: 8090
  external_url: https://iflygo.example.com
  web_embedded: true
  cookie_secure: auto
  trust_proxy: true
  # Nginx 在其它机器时追加代理 IP/CIDR
  trusted_proxies: ["127.0.0.1", "::1"]

database:
  driver: postgres
  dsn: postgresql://iflygo:change-me@127.0.0.1:5432/iflygo?sslmode=disable
  max_open_conns: 50
  max_idle_conns: 10

auth:
  provider: builtin
  admin_username: admin
  admin_password: ""          # 建议用 ADMIN_PASSWORD 环境变量覆盖

artifact:
  dir: /var/lib/iflygo/artifacts
  storage_type: local
  max_concurrent_uploads: 4
  lock_wait: 30m

tcp_relay:
  mode: auto                  # auto / tcp / udp（客户端默认数据面模式）
  listen: ""                  # 留空 = 8091（控制面端口 +1）
  advertise: ""               # 反向代理/端口映射时填对外地址，如 iflygo.example.com:8091
  token: ""                   # 可选共享令牌，客户端自动获取

client:
  dns: "10.77.0.1"            # 客户端默认 DNS（网络级 DNS 优先）
  mtu: 1280                   # 客户端默认 MTU（网络级 MTU 优先）
  interval: 30s               # 客户端心跳间隔
  dns_route: false            # 公网 DNS 自动下发 /32 主机路由（需网关 NAT）
  auto_nat: true              # 网关匹配 Agent 后自动应用 NAT，健康切换自动重建
  auto_nat_max_retries: 5     # NAT 失败最大自动重试次数（达到后停止并告警）
  auto_nat_webhook: ""        # NAT 失败/恢复告警 Webhook（可选）
  egress_exclude: ""          # 全量出口额外排除网段（逗号分隔 CIDR）

log:
  level: info
```

敏感项建议放入环境变量文件 `/etc/iflygo/server.env`：

```bash
ADMIN_PASSWORD=change-me
SESSION_SECRET=$(openssl rand -hex 32)
DATABASE_URL=postgresql://iflygo:change-me@127.0.0.1:5432/iflygo?sslmode=disable
SERVER_EXTERNAL_URL=https://iflygo.example.com
# TCP_RELAY_ADVERTISE=iflygo.example.com:8091
# TCP_RELAY_TOKEN=change-me
# CLIENT_DNS=10.77.0.1
# CLIENT_MTU=1280
# CLIENT_INTERVAL=30s
# CLIENT_DNS_ROUTE=false
# CLIENT_AUTO_NAT=true
# CLIENT_AUTO_NAT_MAX_RETRIES=5
# CLIENT_AUTO_NAT_WEBHOOK=https://hooks.example.com/iflygo
# CLIENT_EGRESS_EXCLUDE=10.0.0.0/8,192.168.0.0/16
```

> 优先级：**页面设置 > 环境变量 > 配置文件 > 内置默认值**。
> 管理员密码为空时首次启动会生成随机密码并打印一次日志。

#### 4. systemd 启动

```bash
# 从仓库复制 deploy/systemd/iflygo-server.service（仅下载二进制时也可按文件内容手写）
install -m 0644 deploy/systemd/iflygo-server.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now iflygo-server
journalctl -u iflygo-server -f
```

首次登录：`http://<server>:8090`，用户名 `admin`，密码为 `ADMIN_PASSWORD`
（未设置时见首次启动日志）。

> 仅部署 console 时，可从 service 文件删除 `AmbientCapabilities` / `CapabilityBoundingSet`。
> 数据面角色（lighthouse/relay/edge/gateway）需要 `CAP_NET_ADMIN`，edge 绑定 443 需要 `CAP_NET_BIND_SERVICE`。

#### 5. 接入 Linux 客户端

1. 页面 → **隧道 → 客户端 → 创建客户端**，复制 enroll token 或安装命令；
2. 在目标主机执行：

```bash
sudo iflygo-client enroll \
  --server https://iflygo.example.com \
  --token '<ENROLL_TOKEN>' \
  --state-dir /var/lib/iflygo-client

# 前台运行（需要 root / TUN）
sudo iflygo-client run --state-dir /var/lib/iflygo-client

# 或安装为 systemd 服务
sudo iflygo-client service install \
  --state-dir /var/lib/iflygo-client \
  --config /etc/iflygo/client.yaml
```

> `enroll` 会自动把服务端下发的全部文件写入状态目录：
> `ca.crt`、`node.crt`、`node.key`、`config.yml`、`routes.json`、`client.yaml`。
> 注册完成后直接 `run` 即可，无需手工维护配置。

客户端配置文件 `/etc/iflygo/client.yaml`（支持约 2 秒热加载；默认由服务端 Bundle 自动写入）：

```yaml
interval: 30s
data_plane:
  mode: auto                 # auto / tcp / udp
  tcp_relay: ""              # 留空自动从服务端发现
  tcp_relay_token: ""        # 留空自动获取
```

查看状态 / 重新拉取配置：

```bash
iflygo-client status --state-dir /var/lib/iflygo-client
iflygo-client config --state-dir /var/lib/iflygo-client
```

> 客户端只需要出站 TCP 8090/8091（以及出站 UDP，用于 P2P/打洞）；不需要固定的入站 UDP 端口。
> TCP 中继模式说明见下方「数据面与 TCP Relay」。
>
> 服务端页面配置的 DNS/MTU/心跳间隔/数据面模式/TCP 中继会随 enroll/config
> 自动写入 `client.yaml`，客户端无需手工填写。

#### 6. 接入 Windows 客户端

1. 下载 `iflygo-client-windows-amd64.zip`（或 `arm64`）并解压；
2. **保持目录结构**：`iflygo-client-windows-amd64.exe` 与
   `dist\windows\wintun\bin\amd64\wintun.dll` 在同一父目录下；
3. 以管理员身份打开 PowerShell：

```powershell
.\iflygo-client-windows-amd64.exe enroll `
  --server https://iflygo.example.com `
  --token "<ENROLL_TOKEN>" `
  --state-dir C:\ProgramData\iflygo

.\iflygo-client-windows-amd64.exe run --state-dir C:\ProgramData\iflygo

# 安装为 Windows 服务（SCM，开机自启）
.\iflygo-client-windows-amd64.exe service install `
  --state-dir C:\ProgramData\iflygo `
  --config C:\ProgramData\iflygo\client.yaml
```

> Windows 服务以 LocalSystem 运行，需要管理员权限以加载 wintun 驱动。
> 若使用裸 `.exe`（不下载 zip），需手动补齐 `dist\windows\wintun\bin\<arch>\wintun.dll`。

#### 7. 升级 / 卸载

```bash
# 升级：替换二进制后重启，迁移自动执行
install -m 0755 /tmp/iflygo-server-linux-amd64 /usr/local/bin/iflygo-server
systemctl restart iflygo-server

# 卸载
systemctl disable --now iflygo-server
rm -f /etc/systemd/system/iflygo-server.service
rm -rf /opt/iflygo /var/lib/iflygo   # 确认已备份 CA/数据库/制品后再删
```

---

### 方式二：Docker / Docker Compose（快速体验/控制面）

#### 镜像

- Docker Hub：`iflyelf/iflygo:latest`
- 华为云 SWR：`swr.cn-east-3.myhuaweicloud.com/iflyelf/iflygo:latest`

镜像内同时包含 `iflygo-server` 与 `iflygo-client`，默认入口是 `iflygo-server`。

#### Compose 一键启动（Console + PostgreSQL）

```bash
cd deploy/docker
cp .env.example .env
cp config.example.yaml config.yaml
# 编辑 .env：至少设置 POSTGRES_PASSWORD / ADMIN_PASSWORD / SESSION_SECRET
# 编辑 config.yaml：external_url、tcp_relay.advertise 等
docker compose up -d
docker compose logs -f server
```

默认映射：

- `8090/tcp` 控制面 Web/API
- `8091/tcp` TCP 中继

访问 `http://<host>:8090`，用 `admin` + `ADMIN_PASSWORD` 登录。

> 数据卷：`pgdata`（PostgreSQL）、`iflygo-data`（`/data/artifacts`）。
> 配置文件以只读方式挂载；修改后 `docker compose restart server`（`tcp_relay` 支持热加载）。

#### 最小 docker run（SQLite，单机体验）

```bash
docker run -d --name iflygo \
  -p 8090:8090 -p 8091:8091 \
  -e TZ=Asia/Shanghai \
  -e SERVER_EXTERNAL_URL=http://127.0.0.1:8090 \
  -e ADMIN_USERNAME=admin \
  -e ADMIN_PASSWORD=change-me \
  -e SESSION_SECRET="$(openssl rand -hex 32)" \
  -e DATABASE_DRIVER=sqlite \
  -e SQLITE_PATH=/data/iflygo.db \
  -e ARTIFACT_DIR=/data/artifacts \
  -v iflygo-data:/data \
  iflyelf/iflygo:latest
```

#### 数据面角色容器

lighthouse/relay/edge 需要 Nebula 配置包、UDP 与 TUN，推荐 **host 网络**：

```bash
docker run -d --name iflygo-lighthouse \
  --network host \
  --cap-add NET_ADMIN \
  --device /dev/net/tun \
  --workdir /etc/iflygo/node \
  -v /etc/iflygo/node:/etc/iflygo/node:ro \
  -e TZ=Asia/Shanghai \
  iflyelf/iflygo:latest \
  -role lighthouse \
  -overlay-config /etc/iflygo/node/config.yml
```

edge 示例：

```bash
docker run -d --name iflygo-edge \
  --network host \
  --cap-add NET_ADMIN \
  --device /dev/net/tun \
  --workdir /etc/iflygo/node \
  -v /etc/iflygo/node:/etc/iflygo/node:ro \
  -v /etc/iflygo/edge-routes.json:/etc/iflygo/edge-routes.json:ro \
  iflyelf/iflygo:latest \
  -role edge \
  -overlay-config /etc/iflygo/node/config.yml \
  -edge-addr :443 \
  -edge-routes /etc/iflygo/edge-routes.json
```

> 控制面与 edge 不要同时占用宿主机 443；生产建议 console 与 edge 分机部署，或使用独立 IP/端口。

#### 客户端容器（可选）

客户端需要 TUN 与 host 网络（P2P 打洞/源地址更稳定）：

```bash
# enroll（一次性）
docker run --rm --network host \
  -v iflygo-client:/var/lib/iflygo-client \
  --entrypoint /usr/local/bin/iflygo-client \
  iflyelf/iflygo:latest enroll \
    --server https://iflygo.example.com \
    --token '<ENROLL_TOKEN>' \
    --state-dir /var/lib/iflygo-client

# 长期运行
docker run -d --name iflygo-client \
  --network host \
  --cap-add NET_ADMIN \
  --device /dev/net/tun \
  -v iflygo-client:/var/lib/iflygo-client \
  --entrypoint /usr/local/bin/iflygo-client \
  iflyelf/iflygo:latest run --state-dir /var/lib/iflygo-client
```

> 客户端容器需要覆盖 `--entrypoint`（镜像默认入口是 server）；容器内不建议用
> `service install`，直接 `run` 即可。

---

### 方式三：数据面节点（lighthouse / relay / edge / gateway）

> 数据面节点使用 `-role <角色> -overlay-config ...` 启动时进入**纯数据面模式**：
> 不连接数据库、不启动 8090 控制面，只运行 Nebula/Edge。需要同时提供控制面时，
> 把 `console` 加入 `-role`（如 `-role console,lighthouse`）。

1. 控制台创建网络；
2. **服务端节点**中创建节点，填写 `roles`（如 `lighthouse`、`relay`、`edge`）与
   `public_host` / `public_port`（必须与节点实际监听/映射一致）；
3. 在页面下载该节点的配置包（或调用
   `GET /api/server-nodes/{id}/bundle`），保存为：

```text
/etc/iflygo/node/ca.crt
/etc/iflygo/node/node.crt
/etc/iflygo/node/node.key
/etc/iflygo/node/config.yml
```

4. 按角色启动：

```bash
# lighthouse（UDP 发现 / 打洞协调）
iflygo-server -role lighthouse -overlay-config /etc/iflygo/node/config.yml

# relay（UDP 中继）
iflygo-server -role relay -overlay-config /etc/iflygo/node/config.yml

# edge（HTTPS/域名入口；路由表从页面下载或 GET /api/server-nodes/{id}/edge-routes）
iflygo-server -role edge \
  -overlay-config /etc/iflygo/node/config.yml \
  -edge-addr :443 \
  -edge-routes /etc/iflygo/edge-routes.json

# gateway（不安全路由/NAT/子网探测）
iflygo-server -role gateway -overlay-config /etc/iflygo/node/config.yml
```

> 使用仓库中的 `deploy/systemd/iflygo-node.service`（已设置
> `WorkingDirectory=/etc/iflygo/node`，config.yml 内的相对 pki 路径可直接使用），
> 按角色修改 `ExecStart` 后安装即可。也可把 `pki.ca/cert/key` 改为绝对路径：
> ```yaml
> pki:
>   ca: /etc/iflygo/node/ca.crt
>   cert: /etc/iflygo/node/node.crt
>   key: /etc/iflygo/node/node.key
> ```
>
> 一个进程可用逗号组合角色（如 `-role lighthouse,relay`），共用同一份 overlay 配置；
> 控制台（console）与数据面建议分开部署，避免重启/升级互相影响；
> 纯数据面节点无需 PostgreSQL，也不需要 `-c` 配置文件。
>
> edge 的 TLS 可用 `-edge-tls-cert/-edge-tls-key` 或 ACME：
> `-edge-acme-email ops@example.com -edge-acme-cache /var/lib/iflygo/acme`。

---

### 移动客户端（Android / HarmonyOS 4）

带界面的 Android 客户端位于 `mobile/android/`，Go 核心位于 `mobile/`：

- **形态**：完整 VpnService/TUN 隧道（不是仅管理页面）
- **功能**：填写控制面地址 + enroll token → 注册 → 启动/停止 VPN → 状态/IP/日志
- **兼容性**：APK 可安装在 **HarmonyOS 4 / 4.2 及以下**（Android 兼容）；
  HarmonyOS NEXT（5.x）不兼容 Android APK，需要原生 ArkTS HAP（本仓库暂未构建）
- **构建**：`.github/workflows/android.yml` 自动执行
  `gomobile bind` + `gradle assembleDebug`；构建产物为 `app-debug.apk`，
  发布资产命名为 `iflygo-android.apk`（附 `.sha256`），手动触发/tag 时附加到 Release
- **下载**：
  ```bash
  wget -O iflygo-android.apk \
    https://github.com/iflyelf/iflygo/releases/latest/download/iflygo-android.apk
  ```
  当前为 debug 签名（可直接安装）；正式分发建议配置自有 release keystore 后重新签名。
- **本地构建**：见 `mobile/android/README.md`（需要 JDK 17、Android SDK 34、
  NDK 26.1、Gradle 8.7、gomobile）
- **使用**：
  1. 控制台「隧道 → 客户端」创建客户端，复制 enroll token；
  2. 安装 APK，填写控制面地址与 token，点「注册」；
  3. 点「启动」，允许 Android VPN 授权；
  4. 状态显示「运行中」，日志可查看 Nebula 输出。
- **参数下发**：服务端「设置 → 客户端下发」（DNS/MTU/interval）与「数据面 / TCP 中继」、
  「网络 → 编辑」、客户端「编辑」弹窗与路由页的参数会随 enroll/config 自动下发；
  VpnService 每 30 秒检查配置版本，IP/证书/DNS/MTU/路由变化会自动重启隧道，
  也可点「刷新」立即应用。
- **网络范围**：VpnService 只添加 overlay CIDR 与服务端下发的不安全路由；
  Nebula 自身 UDP 通过 `addDisallowedApplication` 排除，避免回环。
  当前版本不提供全局 0/0 代理；需要全局时须配合 socket protect/全隧道策略。

### Nginx / HTTPS 反向代理（可选）

参考 `deploy/nginx/iflygo.conf`，关键点：

- 必须转发 `Upgrade` / `Connection`，并给 `/api/liaison/ws`、`/api/agent/*` 设置较长
  `proxy_read_timeout`（Web SSH/桌面、Agent 长轮询）；
- 制品上传按需增大 `client_max_body_size`（示例 2g）；
- `server.external_url` 填 HTTPS 域名；`trusted_proxies` 加入 Nginx 地址；
- TCP 中继 8091 默认直连；若经 Nginx `stream` 转发，设置
  `TCP_RELAY_ADVERTISE=<域名>:8091`，确保客户端能连到该端口。

---

### 备份 / 升级 / 排障

**备份**（至少包含数据库与制品目录；CA/证书在数据库中）：

```bash
pg_dump -Fc -U iflygo iflygo > iflygo-$(date +%F).dump
tar -czf artifacts-$(date +%F).tar.gz -C /var/lib/iflygo artifacts
# SQLite：
sqlite3 /var/lib/iflygo/iflygo.db ".backup '/var/lib/iflygo/iflygo-$(date +%F).db'"
```

**升级**：替换二进制 → `systemctl restart iflygo-server`；Docker 用
`docker compose pull && docker compose up -d`。迁移在启动时自动执行；回滚时换回旧二进制/镜像。

**常见问题**：

- 页面打不开：默认端口是 **8090**（不是 8080）；确认 `server.host: 0.0.0.0` 与防火墙。
- 客户端连不上 TCP 中继：放通 8091/TCP；反向代理场景设置 `TCP_RELAY_ADVERTISE`；
  未设置 `tcp_relay.token` 时中继不校验令牌，建议设置并限制来源。
- Windows 客户端启动失败：使用 zip 包（含 wintun.dll），以管理员运行；
  确认 `dist\windows\wintun\bin\<arch>\wintun.dll` 与 exe 的相对位置。
- enroll 401：token 一次性/已过期，在页面重新生成。
- 多副本：必须使用 PostgreSQL + 共享 `SESSION_SECRET`；SQLite 不支持多副本。
- 数据面 443 与 Nginx 冲突：console 与 edge 分机部署，或使用不同 IP/端口。

---

## 📡 端口

| 端口 | 协议 | 用途 |
|---|---|---|
| 8090 | TCP | 控制面 Web/API（默认 `server.port`） |
| 8091 | TCP | TCP 中继（默认控制面端口 +1，可配置/关闭） |
| 443 | TCP/UDP | Edge 入口 / Nebula 数据面 |
| 6688 | UDP | Lighthouse / Relay（`public_port` 可覆盖） |
| 80 | TCP | 可选：ACME HTTP-01 |
| 5432 / 6379 | TCP | 外部 PostgreSQL / Redis（不对外暴露） |

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

### 客户端参数下发（服务端页面）

所有客户端运行参数都可以在服务端页面配置，并随 enroll token / 配置刷新自动下发：

| 页面位置 | 配置项 | 下发方式 |
|---|---|---|
| 设置 → 客户端下发 | `client.dns`、`client.mtu`、`client.interval`、`client.dns_route`、`client.auto_nat`、`client.egress_exclude`（全局默认） | Bundle → `client.yaml` / 客户端 State |
| 隧道 → 参数下发 | 聚合全局默认 + 网络级 Nebula + 每主机覆盖；NAT 脚本预览/Agent 自动执行/全量出口默认路由 | 同上 |
| 设置 → 数据面 / TCP 中继 | `tcp_relay.mode`（客户端默认 auto/tcp/udp）、中继令牌 | Bundle `client.data_plane_mode` + 心跳 |
| 隧道 → 网络 → 编辑 | cipher、punch、relays、TUN、握手、日志、统计、网络 DNS/MTU | Nebula `config.yml` / Bundle DNS/MTU |
| 隧道 → 客户端 → 编辑 | 名称、IPv4/IPv6、分组、子网、**每主机 DNS/MTU/心跳/数据面/DNS 开关** | Bundle / State |
| 隧道 → 路由 → 编辑 | 不安全路由 CIDR、网关、权重 | `routes.json` / Nebula `unsafe_routes` |
| 隧道 → 客户端 | enroll token / 安装命令 | Bundle / State |

推荐入口：**隧道 → 参数下发**。该页把全局默认、网络级 Nebula、每主机覆盖
聚合到一个页面，所有编辑仍在弹窗内完成：

- **全局默认**：DNS、MTU、心跳、数据面模式、公网 DNS 主机路由；
- **网络级**：网络 DNS/MTU、cipher、punch、relays、TUN、握手、日志、统计等；
- **每主机**：DNS/MTU/心跳/数据面/DNS 开关/DNS 主机路由与 IP；
- **网关 NAT 脚本**：按当前网络与公网 DNS 生成转发 + `MASQUERADE` 脚本。

每台主机都可以在「隧道 → 客户端 → 编辑」弹窗中单独覆盖参数：

- **服务端 DNS**：跟随网络/全局、启用、关闭三态；关闭后 Bundle 不下发 DNS；
- **DNS**：覆盖网络级与全局 DNS；
- **公网 DNS 主机路由**：跟随全局、启用、关闭三态；
- **DNS 网关**：可单独覆盖网络级 DNS 网关（多网关场景）；
- **MTU**：0 表示跟随网络/全局；
- **心跳间隔**：如 `30s`，留空跟随默认；
- **数据面模式**：auto / tcp / udp，留空跟随默认。

### 公网 DNS 走 VPN（主机路由 + 网关 NAT）

如果客户端 DNS 填的是公网解析器（如 `8.8.8.8`），默认情况下该流量不会进入
VPN。开启 **公网 DNS 主机路由** 后：

1. 服务端为每个不在 overlay 网段内的 DNS 生成 `/32`（IPv6 为 `/128`）主机路由；
2. 路由网关按 **网络 DNS 网关 > 默认路由 > `gateway` 角色节点 > 网络网关 > 任意服务端节点** 选择；
3. 路由同时写入客户端 `routes.json` 与 Nebula `unsafe_routes`，Android/CLI 均生效；
4. 在 **隧道 → 参数下发 → DNS NAT / 全量出口** 中可先预览脚本，再选择：
   - **手动执行**：复制脚本到网关节点以 root 执行；
   - **Agent 自动执行**：选择已安装 Agent 的主机（留空则按网关节点 `public_host` 自动匹配），
     服务端创建并立即启动命令任务，Agent 下一次心跳自动执行；
5. 网关角色节点的部署包（ServerNodeBundle）会直接携带 NAT 脚本；
6. 只要网关节点已安装 Agent，服务端会自动应用 NAT：
   - Agent 注册成功后**立即触发**一次对账，之后每分钟巡检；
   - 无需手工点击，NAT 与 Agent 安装流程自动衔接；
   - 网关离线/恢复时自动切换，并递增客户端 `config_rev` 让客户端自动拉取新网关；
   - 失败任务按 10 分钟退避自动重试，默认最多 5 次；达到上限后停止并告警；
   - 告警写入审计日志（`gateway.nat_failed` / `gateway.nat_recovered`），
     可配置 `client.auto_nat_webhook` POST JSON；参数下发页展示重试次数与最近错误；
7. 执行后客户端解析公网域名即可正常走 overlay 网关；
8. 脚本会开启转发、放行 overlay → DNS 的 FORWARD，并对回包做 `MASQUERADE`。

**多网关显式选择**：网络编辑弹窗与客户端编辑弹窗均新增「DNS 网关（overlay IP）」，
可分别指定网络级/每主机公网 DNS 主机路由的下一跳；留空自动选择。该字段影响
DNS 主机路由与全量出口的下一跳；不会修改已有路由表条目。

**网关健康检查与自动切换**：服务端通过网关节点 `public_host` 匹配 Agent，读取
`agents.status`；显式网关离线时自动切换到下一个健康网关，全部离线时保留最后候选
并在恢复后自动重建 NAT。服务节点列表会展示 `online / unknown / offline` 健康状态。

**全量公网出口**：网络编辑弹窗新增「出口模式」：
- `off`：关闭；
- `dns`：仅公网 DNS；
- `full`：全量公网出口。

全量出口不会下发单一 `0.0.0.0/0`，而是按排除策略拆分公网前缀：
- 自动排除 RFC1918 内网、回环、链路本地、CGNAT、组播、保留地址；
- 自动排除 overlay 网段；
- 自动排除控制面 `external_url` 解析到的地址（也可在 `client.egress_exclude` /
  网络「全量出口排除网段」中追加）；
- 网关 NAT 脚本同时写入 `RETURN` 规则，避免对排除网段做源 NAT。

这样全量出口只会把**公网流量**送入隧道，内网、控制面、局域网访问保持直连。

> Agent 自动执行需要：`agent.allow_exec_mode=true`，且目标主机已安装并在线运行 Agent。
> 手动模式仍可直接复制脚本执行。
>
> 网关需具备出公网能力；脚本按 iptables 通用语法生成，Debian/Ubuntu 可用
> `iptables-persistent`、RHEL/CentOS 可用 `iptables-services` 持久化。
> 若 DNS 本身在 overlay 网段内（如 `10.77.0.1`），无需主机路由与 NAT。

全局/网络/主机级参数保存后会自动递增客户端 `config_rev`，客户端下一次心跳自动拉取。

优先级：

1. **主机级覆盖** > **网络级** > 全局「客户端下发」默认值；
2. **命令行参数** 优先于 `client.yaml`；
3. `client.yaml` 由服务端自动写入，优先级高于心跳自动发现；
4. 页面所有创建/编辑操作均在弹窗中完成，不再在列表页展开表单；
5. 修改主机 IP 后服务端会自动重新签发证书、更新 IPAM 绑定并递增 `config_rev`；
6. 修改页面后，客户端心跳/`config` 拉取会自动更新 `client.yaml`、证书与 Nebula 配置：
   - CLI/服务端安装的客户端：检测到 IP/证书变化会自动重启 overlay；
   - Android 客户端：VpnService 每 30 秒检查一次配置版本，有更新会自动重启隧道，
     也可以点「刷新」立即应用；无需手工修改任何客户端文件。

> **DNS 建议填 overlay 内可达的解析器**（如 `10.77.0.1`）。Android VpnService 会把该 DNS
> 下发给系统；由于 VPN 只接管 overlay CIDR 与不安全路由，公网 DNS 能否访问取决于
> 网关是否提供转发/NAT。若不需要服务端 DNS，可在主机编辑弹窗中选择「关闭服务端 DNS」。

### 客户端配置文件（支持热加载）

默认路径 `<state-dir>/client.yaml`（可用 `--config` 或 `IFLYGO_CLIENT_CONFIG` 指定）：

```yaml
interval: 30s                # 服务端 client.interval
data_plane:
  mode: auto                 # 服务端 tcp_relay.mode
  tcp_relay: ""              # 服务端下发的可达地址
  tcp_relay_token: ""        # 服务端下发的令牌
```

> 该文件默认由 `enroll`/`config` 自动生成；如需本地覆盖，可直接编辑（约 2 秒热加载），
> 但下一次服务端配置刷新会用下发值覆盖。命令行参数优先级最高。

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
- Windows 发布包（zip）内置 `wintun.dll`，解压即用
- Android APK（`iflygo-android.apk`）由 `android.yml` 构建；手动触发/tag 时附加到 Release
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
