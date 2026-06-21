# iFlyGo Docker 🚀

[![Docker Pulls](https://img.shields.io/docker/pulls/iflyelf/iflygo?style=flat-square)](https://hub.docker.com/r/iflyelf/iflygo)
[![Docker Image Size](https://img.shields.io/docker/image-size/iflyelf/iflygo/latest?style=flat-square)](https://hub.docker.com/r/iflyelf/iflygo)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square)](LICENSE)

**iFlyGo** 是一款可扩展的 overlay 网络隧道工具，专注于**性能**、**简洁**和**安全**。通过 Docker 多阶段构建，支持多架构（amd64/arm64）部署，适用于跨公网、跨数据中心的安全组网场景。

---

## ✨ 特性

- 🛡️ **安全加密**: 基于 Noise Protocol Framework 的相互认证，默认使用 Curve25519 + AES-256-GCM
- 🌍 **全平台支持**: 
  - **Linux**: amd64/arm64 容器镜像，支持云服务器、树莓派、NAS
  - **macOS**: 支持 Apple Silicon (M1/M2/M3)，可从容器提取 ARM64 原生二进制
  - **Windows**: amd64/arm64 原生二进制容器，支持 x64 和 ARM64 (Copilot+ PC)
- 🏗️ **多阶段构建**: 从 Go 源码编译，生成精简运行镜像
- 📦 **即开即用**: 提供 server (lighthouse) / client 两种运行模式，通过环境变量自动生成配置
- 🔧 **灵活配置**: 支持证书自动签发、防火墙规则、分组、中继等高级特性
- 📝 **中文注释**: 所有配置模板均包含详尽的中文注释

---

## 🌐 跨平台支持

### Linux (amd64/arm64)

Docker 镜像支持 Linux amd64 和 arm64 平台，可直接使用：

```bash
docker pull iflyelf/iflygo:latest
```

Docker 会自动拉取匹配当前系统架构的镜像。

---

### macOS (Apple Silicon / M 系列芯片)

**macOS M1/M2/M3 芯片使用 Linux ARM64 容器**。由于 Docker Desktop for Mac 运行 Linux VM，需要从容器中复制二进制到宿主机直接运行。

#### 方法一：从容器提取二进制（推荐）

```bash
# 1. 拉取 Linux arm64 镜像
docker pull --platform linux/arm64 iflyelf/iflygo:latest

# 2. 创建临时容器
docker create --name iflygo-temp --platform linux/arm64 iflyelf/iflygo:latest

# 3. 复制二进制到 macOS 宿主机
docker cp iflygo-temp:/usr/local/bin/iflygo /usr/local/bin/iflygo
docker cp iflygo-temp:/usr/local/bin/iflygo-cert /usr/local/bin/iflygo-cert

# 4. 添加执行权限
chmod +x /usr/local/bin/iflygo /usr/local/bin/iflygo-cert

# 5. 验证版本
iflygo -version

# 6. 删除临时容器
docker rm iflygo-temp
```

#### 方法二：在容器内运行（需配置网络）

```bash
# 使用 host 网络模式 (需 Docker Desktop 最新版本)
docker run -d \
  --name iflygo-client \
  --cap-add NET_ADMIN \
  --device /dev/net/tun \
  --network host \
  -e RUNMODE=client \
  -e NODE=macos-client \
  -e IFLYGO_IP=192.168.100.100 \
  -e LIGHTHOUSE_IP=192.168.100.1 \
  -e LIGHTHOUSE_PUBLIC=your-server-ip:6688 \
  -v $(pwd)/data/macos/config:/etc/iflygo:rw \
  iflyelf/iflygo:latest
```

#### 如何使用 iflygo

复制二进制到宿主机后，参考 [快速开始](#📦-快速开始) 章节：

1. 从 server 节点获取 `ca.crt`
2. 在 server 上签发 macOS 客户端证书（使用 `sign` 命令）
3. 创建配置文件 `~/.config/iflygo/config.yml`（参考 `conf/client/config.yml` 模板）
4. 运行 iflygo：

```bash
sudo iflygo -config ~/.config/iflygo/config.yml
```

---

### Windows (amd64/arm64)

**Windows 平台使用 Windows 容器镜像**，包含原生 `iflygo.exe` 和 `iflygo-cert.exe`。

#### 拉取镜像

```bash
# Windows amd64 (x64 CPU)
docker pull iflyelf/iflygo:windows-amd64

# Windows arm64 (ARM64 Surface / Copilot+ PC)
docker pull iflyelf/iflygo:windows-arm64

# 自动选择架构
docker pull iflyelf/iflygo:windows-latest
```

#### 方法一：从容器提取二进制（推荐）

```powershell
# 1. 创建临时容器
docker create --name iflygo-temp iflyelf/iflygo:windows-latest

# 2. 复制二进制到 Windows 宿主机
docker cp iflygo-temp:C:\iflygo\bin\iflygo.exe C:\iflygo\
docker cp iflygo-temp:C:\iflygo\bin\iflygo-cert.exe C:\iflygo\

# 3. 复制配置模板（可选）
docker cp iflygo-temp:C:\iflygo\config\client-config-template.yml C:\iflygo\config.yml

# 4. 验证版本
C:\iflygo\iflygo.exe -version

# 5. 删除临时容器
docker rm iflygo-temp
```

#### 方法二：在容器内运行（查看帮助）

```powershell
# 运行容器并查看帮助信息
docker run --rm iflyelf/iflygo:windows-latest
```

#### 如何使用 iflygo.exe

提取二进制后，在 PowerShell 或 CMD 中运行：

1. 从 server 节点获取 `ca.crt`
2. 在 server 上签发 Windows 客户端证书（使用 `sign` 命令）
3. 创建配置文件 `C:\iflygo\config.yml`（参考 `conf/client/config.yml` 模板）
4. 管理员权限运行 iflygo：

```powershell
# 以管理员权限打开 PowerShell
C:\iflygo\iflygo.exe -config C:\iflygo\config.yml
```

**注意事项**：

- Windows 需要创建 TUN/TAP 虚拟网卡，首次运行需安装 [Wintun](https://www.wintun.net/) 驱动
- 需要管理员权限运行 iflygo.exe
- 防火墙可能拦截 UDP 6688 端口，需手动添加入站规则

---

### 架构对比表

| 平台 | 架构 | Docker 镜像 Tag | 原生运行 | 推荐方式 |
|------|------|-----------------|----------|----------|
| **Linux** | amd64 | `iflyelf/iflygo:latest` | ✅ 容器内直接运行 | 容器部署 |
| **Linux** | arm64 | `iflyelf/iflygo:latest` | ✅ 容器内直接运行 | 容器部署 |
| **macOS** | Apple Silicon (M1/M2/M3) | `--platform linux/arm64` | ✅ 复制二进制到宿主机 | 提取二进制 |
| **Windows** | amd64 (x64) | `iflyelf/iflygo:windows-amd64` | ✅ 复制二进制到宿主机 | 提取二进制 |
| **Windows** | arm64 (ARM) | `iflyelf/iflygo:windows-arm64` | ✅ 复制二进制到宿主机 | 提取二进制 |

---

## 🏛️ 架构与通信原理

iFlyGo 是一个基于 [Noise Protocol Framework](https://noiseprotocol.org/) 的 P2P（点对点）overlay 网络。它由三类角色组成：**Lighthouse（灯塔/服务端）**、**Relay（中继）** 和 **Client（客户端）**。

### 整体架构图（高可用生产部署）

```
                          ┌──── 互联网云端 (公网区域) ────┐
                          │                              │
                   ┌──────┴──────┐              ┌───────┴──────┐
                   │  Lighthouse1 │              │ Lighthouse2  │
                   │  (上海机房)   │              │  (新加坡机房) │
                   │─────────────│              │──────────────│
                   │ am_lighthouse│              │am_lighthouse │
                   │    = true    │              │   = true     │
                   │ am_relay     │              │am_relay      │
                   │    = true    │              │   = true     │
                   │─────────────│              │──────────────│
                   │公网固定IP/域名│◄────互相──────►│公网固定IP/域名│
                   │sh.example.com│    同步发现   │sg.example.com│
                   │    :6688     │              │    :6688     │
                   │─────────────│              │──────────────│
                   │OverlayIP:    │              │OverlayIP:    │
                   │ 10.88.0.1    │              │ 10.88.0.2    │
                   │fd88::a58:1   │              │fd88::a58:2   │
                   └──────┬──────┘              └───────┬──────┘
                          │                              │
                          │  ① 注册自己的公网地址           │
                          │  ② 查询对方节点的公网地址        │
                          │     (支持多lighthouse容错)      │
              ┌───────────┴───────────┬──────────────────┴─────────────┐
              │                       │                                │
              ▼                       ▼                                ▼
    ┌─────────────────┐    ┌─────────────────┐              ┌─────────────────┐
    │   Client A      │    │   Client B      │              │   Client C      │
    │ (办公室电脑)      │    │  (家庭路由器)    │              │  (移动设备)      │
    │─────────────────│    │─────────────────│              │─────────────────│
    │ NAT后 动态公网IP │    │ NAT后 动态公网IP │              │ 4G/5G 运营商NAT  │
    │ port: 0 (动态)   │    │ port: 0 (动态)   │              │ port: 0 (动态)   │
    │─────────────────│    │─────────────────│              │─────────────────│
    │ 10.88.0.100     │    │ 10.88.0.101     │              │ 10.88.0.102     │
    │ fd88::a58:100   │    │ fd88::a58:101   │              │ fd88::a58:102   │
    └────────┬────────┘    └────────┬────────┘              └────────┬────────┘
             │                      │                                │
             │  ③ UDP 打洞 + P2P 直连 (端到端加密，不经过 lighthouse) │
             │      ═══════════════════════════════════════════════  │
             │     ║  AES-256-GCM / ChachaPoly 加密流量              ║
             │      ═══════════════════════════════════════════════  │
             │                      │                                │
             └──────────────────────┼────────────────────────────────┘
                                    │
                    ④ 若 P2P 打洞失败(双方都在对称NAT后)
                       流量自动切换到 Relay 中转 (经 Lighthouse1/2)
                                    │
                    ┌───────────────┴────────────────┐
                    │  Relay 中继 (灯塔兼任)           │
                    │  • 仅转发无法直连的流量           │
                    │  • 保持端到端加密不变             │
                    │  • Lighthouse1/2 同时作为中继    │
                    └─────────────────────────────────┘


    ┌─────────────────────────────────────────────────────────────────┐
    │  📊 关键特性说明                                                  │
    ├─────────────────────────────────────────────────────────────────┤
    │  ✅ 高可用: 多 Lighthouse 互为备份, 一个挂掉自动切换            │
    │  ✅ P2P 优先: 90% 流量走直连, 性能不受 Lighthouse 带宽限制      │
    │  ✅ 自动兜底: 对称 NAT 等无法直连场景, 自动启用 Relay 中转      │
    │  ✅ 端到端加密: 即使经 Relay 转发, Lighthouse 也无法解密内容    │
    │  ✅ 跨平台: Docker 容器统一部署, 支持 amd64/arm64/树莓派/NAS   │
    └─────────────────────────────────────────────────────────────────┘
```

### 通信流程详解

| 步骤 | 阶段 | 说明 |
|------|------|------|
| ① | **注册与发现** | 每个 client 启动后，定期（`LIGHTHOUSE_INTERVAL` 秒）向 lighthouse 上报自己的公网地址。当 A 想连接 B 时，先向 lighthouse 查询 B 的公网地址。 |
| ② | **UDP 打洞** | A 和 B 拿到对方公网地址后，同时向对方发送 UDP 包进行 NAT 打洞（`punchy`），尝试穿透各自的 NAT/防火墙。 |
| ③ | **P2P 直连** | 打洞成功后，A 和 B 建立**端到端加密隧道**，数据直接传输，**不经过 lighthouse**（lighthouse 只负责牵线，不转发数据）。 |
| ④ | **中继兜底** | 若 P2P 打洞失败（如双方都在对称 NAT 后），流量改由 `relay` 节点转发。中继节点需 `am_relay: true`，客户端需 `use_relays: true`。 |

### 三类角色对比

| 角色 | RUNMODE | am_lighthouse | am_relay | 公网 IP | 监听端口 | 典型部署位置 |
|------|---------|---------------|----------|---------|----------|--------------|
| **Lighthouse** | `server` | `true` | `true`（可兼任中继） | ✅ 必须 | 固定 `6688` | 云服务器（有公网IP） |
| **Relay** | `server` | 可选 | `true` | ✅ 必须 | 固定 `6688` | 云服务器（通常由 lighthouse 兼任） |
| **Client** | `client` | `false` | `false` | ❌ 不需要 | 动态 `0` | NAT 后的内网设备 |

### 关键设计要点

- **Lighthouse 不转发数据**：它只是"地址簿"，帮节点发现彼此的公网地址。真实流量走 P2P 直连，性能不受 lighthouse 带宽限制。
- **Relay 是兜底方案**：仅在 P2P 直连失败时启用。生产环境通常让 lighthouse 同时兼任 relay（`am_relay: true`）。
- **所有节点 overlay IP 同网段**：示例中 `10.88.0.0/16`（IPv4）和 `fd88::/64`（IPv6），由证书 `-networks` 字段绑定，不可伪造。
- **加密算法必须统一**：所有节点的 `CIPHER` 必须一致（默认 `chachapoly`），否则无法握手。

---

## 📦 快速开始

> **💡 跨平台提示**：本章节适用于 **Linux 平台**容器部署。macOS / Windows 用户请先查看 [🌐 跨平台支持](#🌐-跨平台支持) 章节了解如何提取原生二进制。

### 1. 拉取镜像

```bash
# Linux 平台 (自动选择 amd64 或 arm64)
docker pull iflyelf/iflygo:latest
```

### 2. 启动 Server (Lighthouse 灯塔节点)

```bash
docker run -d \
  --name iflygo-server \
  --privileged \
  --cap-add NET_ADMIN \
  --cap-add SYS_MODULE \
  --device /dev/net/tun \
  --network host \
  -e RUNMODE=server \
  -e NODE=lighthouse1 \
  -e CERT_HOSTNAME=lighthouse1 \
  -e IFLYGO_IP=192.168.100.1 \
  -e LIGHTHOUSE_PUBLIC=your-server-ip:6688 \
  -e LISTEN_PORT=6688 \
  -v $(pwd)/data/server/config:/etc/iflygo:rw \
  -v $(pwd)/data/server/logs:/var/log/iflygo:rw \
  iflyelf/iflygo:latest
```

### 3. 启动 Client (客户端节点)

```bash
docker run -d \
  --name iflygo-client \
  --privileged \
  --cap-add NET_ADMIN \
  --cap-add SYS_MODULE \
  --device /dev/net/tun \
  --network host \
  -e RUNMODE=client \
  -e NODE=client1 \
  -e CERT_HOSTNAME=client1 \
  -e IFLYGO_IP=192.168.100.2 \
  -e LIGHTHOUSE_IP=192.168.100.1 \
  -e LIGHTHOUSE_PUBLIC=your-server-ip:6688 \
  -e LISTEN_PORT=0 \
  -v $(pwd)/data/client/config:/etc/iflygo:rw \
  -v $(pwd)/data/client/logs:/var/log/iflygo:rw \
  iflyelf/iflygo:latest
```

### 4. 使用 Docker Compose（推荐）

```bash
# 编辑 docker-compose.yml 中的 LIGHTHOUSE_PUBLIC 为你的服务器公网 IP
vim docker-compose.yml

# 启动 server 和 client
docker-compose up -d

# 查看日志
docker-compose logs -f

# 停止服务
docker-compose down
```

---

## 🔧 环境变量说明

### 基础环境变量

| 变量名 | 说明 | 默认值 | 示例 |
|--------|------|--------|------|
| **🚀 基础运行** | | | |
| `RUNMODE` | 运行模式 | `client` | `server` / `client` |
| `NODE` | 节点名称（用于证书 `-name`） | `iflygo-node` | `lighthouse1` / `client1` |
| `CERT_HOSTNAME` | 证书文件主机名（命名 `<名>.crt/.key`） | 同 `NODE` | `uola-servers-lead-01` |
| **🌐 节点网络** | | | |
| `IFLYGO_IP` | 此节点的内网 IPv4 地址 | `10.88.0.1` | `10.88.0.2` |
| `IFLYGO_IP_V6` | 此节点的内网 IPv6 地址（留空则不签发 IPv6） | `fd88::ffff:a58:1` | `fd88::ffff:a58:2` |
| `IFLYGO_NETMASK` | IPv4 子网掩码（CIDR 位数） | `16` | `16` / `24` |
| `IFLYGO_NETMASK_V6` | IPv6 子网掩码（CIDR 位数） | `64` | `64` |
| **🏠 Lighthouse（单节点）** | | | |
| `LIGHTHOUSE_IP` | Lighthouse 内网 IPv4 | `10.88.0.1` | `10.88.0.1` |
| `LIGHTHOUSE_IP_V6` | Lighthouse 内网 IPv6（留空则不生成） | `fd88::ffff:a58:1` | `fd88::ffff:a58:1` |
| `LIGHTHOUSE_PUBLIC` | Lighthouse 公网入口 | `127.0.0.1:6688` | `1.2.3.4:6688` |
| `LIGHTHOUSE_INTERVAL` | 向 lighthouse 报告间隔（秒） | `3` | `60` |
| **🏘️ Lighthouse（多节点）** | 见下方[多 Lighthouse 章节](#多-lighthouse-环境变量高可用场景) | | |
| `LIGHTHOUSE_IP_N` | 第 N 个 lighthouse 内网 IP | (空) | `10.88.0.1` |
| `LIGHTHOUSE_PUBLIC_N` | 第 N 个 lighthouse 公网入口 | (空) | `shanghai.example.com:6688` |
| `LIGHTHOUSE_IP_N_V6` | 第 N 个 lighthouse IPv6（可选） | (空) | `fd88::ffff:a58:1` |
| **🔌 监听（Listen）** | | | |
| `LISTEN_HOST` | 监听地址 | `[::]` | `0.0.0.0` |
| `LISTEN_PORT` | 监听端口 | server: `6688`<br>client: `0` | `0` (动态端口) |
| `READ_BUFFER` | UDP 读缓冲区大小（字节） | `20000000` | `30000000` |
| `WRITE_BUFFER` | UDP 写缓冲区大小（字节） | `20000000` | `30000000` |
| `SEND_RECV_ERROR` | recv_error 数据包模式 | `always` | `always` / `never` / `private` |
| **🔐 加密与证书（PKI）** | | | |
| `CIPHER` | 加密算法（**所有节点必须一致**） | `chachapoly` | `chachapoly` / `aes` |
| `PKI_INITIATING_VERSION` | 证书版本（推荐 v2） | `2` | `1` / `2` |
| `CERT_GROUPS` | 证书分组（**勿用 `GROUPS`**，与 bash 内置变量冲突） | server: `lead`<br>client: (空) | `laptop,home,ssh` |
| `SUBNETS` | 网关证书子网路由（用于 unsafe_routes） | (空) | `10.8.1.0/24` |
| `CA_DURATION` | CA 证书有效期 | `876000h` (100年) | `876000h` |
| `CERT_DURATION` | 节点证书有效期（**留空则自动跟随 CA 剩余有效期**） | (空，自动跟随) | `26280h` (3年) |
| `AUTO_GEN_CA` | 自动生成 CA | `true` | `false` |
| **🌉 中继（Relay）** | | | |
| `RELAYS` | 中继服务器列表（逗号分隔内网 IP） | (空，用模板) | `10.88.0.1,10.88.0.2` |
| `AM_RELAY` | 是否作为中继节点 | (空，用模板) | `true` / `false` |
| `USE_RELAYS` | 是否使用中继连接 | (空，用模板) | `true` / `false` |
| **🔗 NAT 打洞（Punchy）** | | | |
| `PUNCH` | 是否持续打洞 | `true` | `true` / `false` |
| `PUNCH_RESPOND` | 响应模式（对称 NAT 穿透） | `true` | `true` / `false` |
| `PUNCH_DELAY` | 打洞响应延迟 | `1s` | `2s` |
| **🤝 握手（Handshakes）** | | | |
| `HANDSHAKE_TRY_INTERVAL` | 握手重试间隔 | `100ms` | `200ms` |
| `HANDSHAKE_RETRIES` | 握手超时次数 | `10` | `20` |
| `HANDSHAKE_TRIGGER_BUFFER` | 握手缓冲通道大小 | `64` | `128` |
| **🛡️ TUN 设备** | | | |
| `TUN_DEV` | TUN 设备名 | `iflygo` | `iflygo` |
| `TUN_MTU` | TUN MTU | `1300` | `1300` |
| `TUN_DISABLED` | 是否禁用 TUN（lighthouse 可禁用） | `false` | `true` / `false` |
| `DROP_LOCAL_BROADCAST` | 是否转发本地广播 | `true` | `true` / `false` |
| `DROP_MULTICAST` | 是否转发组播 | `true` | `true` / `false` |
| `TX_QUEUE` | 传输队列长度 | `1500` | `2000` |
| **🛣️ 路由配置** | | | |
| `PREFERRED_RANGES` | 本地网络偏好范围（逗号分隔 CIDR） | `10.88.0.0/16,fd88::/64` | `172.16.0.0/24,10.99.0.0/16` |
| `UNSAFE_ROUTES` | 不安全路由（[格式说明](#unsafe_routes-环境变量格式)） | (空，用模板默认) | `route=10.8.1.0/24,via=10.88.1.1` |
| **🌍 静态映射（DNS）** | | | |
| `STATIC_MAP_CADENCE` | DNS 缓存时间 | `30s` | `60s` |
| `STATIC_MAP_NETWORK` | 网络地址类型 | `ip` | `ip4` / `ip6` / `ip` |
| `STATIC_MAP_LOOKUP_TIMEOUT` | DNS 查询超时 | `250ms` | `500ms` |
| **📝 日志** | | | |
| `LOG_LEVEL` | 日志级别 | `info` | `debug` / `trace` / `warn` / `error` |
| `LOG_FORMAT` | 日志格式 | `text` | `text` / `json` |
| **⚙️ 配置管理** | | | |
| `FORCE_REGEN` | 强制用环境变量重新生成配置（旧配置自动备份为 `.bak`） | `false` | `true` / `false` |

> 提示：大部分高级配置保持默认值即可，仅在特殊场景下调整（如高流量扩大缓冲区、特定 NAT 环境调整打洞参数、统一证书加密算法等）。

> ⚠️ **配置更新机制**：首次启动会根据环境变量生成 `config.yml`，**之后再修改环境变量默认不会生效**（避免覆盖用户手动修改）。如需让新环境变量生效，设置 `FORCE_REGEN=true` 强制重新生成（旧配置会自动备份为 `config.yml.bak.<时间戳>`）。

#### UNSAFE_ROUTES 环境变量格式

通过环境变量配置不安全路由（无需修改配置文件模板）。格式说明：

- **多条路由**用分号 `;` 分隔
- **每条路由**字段用逗号 `,` 分隔：`route=<CIDR>,via=<网关>[,mtu=<MTU>][,metric=<N>]`
- **单网关**：`via=10.88.1.1`
- **多网关 ECMP**（加权负载均衡）：`via=网关1:权重1|网关2:权重2`（用 `|` 分隔网关，`:` 后跟权重）

**示例**：

```yaml
# docker-compose.yml
environment:
  # 单网关路由
  - UNSAFE_ROUTES=route=10.8.1.0/24,via=10.88.1.1,mtu=1300,metric=100

  # 多网关 ECMP 加权负载均衡
  - UNSAFE_ROUTES=route=10.0.9.0/24,via=10.88.2.1:10|10.88.2.2:5

  # 多条路由组合（分号分隔）
  - UNSAFE_ROUTES=route=10.8.1.0/24,via=10.88.1.1;route=10.0.9.0/24,via=10.88.2.1:10|10.88.2.2:5;route=10.0.88.0/24,via=10.88.2.1:10|10.88.2.2:5,mtu=1300
```

上述最后一个示例会生成：

```yaml
tun:
  unsafe_routes:
    - route: 10.8.1.0/24
      via: 10.88.1.1
    - route: 10.0.9.0/24
      via:
        - gateway: 10.88.2.1
          weight: 10
        - gateway: 10.88.2.2
          weight: 5
    - route: 10.0.88.0/24
      via:
        - gateway: 10.88.2.1
          weight: 10
        - gateway: 10.88.2.2
          weight: 5
      mtu: 1300
```

> ⚠️ 网关节点（`via` 指向的节点）的证书必须用 `SUBNETS` 声明对应子网，否则路由不生效。

### 多 Lighthouse 环境变量（高可用场景）

当部署多个 lighthouse 节点时，使用以下编号变量（自动检测，优先级高于单节点变量）：

| 变量名 | 说明 | 示例 |
|--------|------|------|
| `LIGHTHOUSE_IP_1` | 第 1 个 lighthouse 内网 IP | `10.88.0.1` |
| `LIGHTHOUSE_PUBLIC_1` | 第 1 个 lighthouse 公网入口 | `shanghai.example.com:6688` |
| `LIGHTHOUSE_IP_1_V6` | 第 1 个 lighthouse IPv6 地址（可选） | `fd88::ffff:a58:1` |
| `LIGHTHOUSE_IP_2` | 第 2 个 lighthouse 内网 IP | `10.88.0.2` |
| `LIGHTHOUSE_PUBLIC_2` | 第 2 个 lighthouse 公网入口 | `singapore.example.com:6688` |
| `LIGHTHOUSE_IP_2_V6` | 第 2 个 lighthouse IPv6 地址（可选） | `fd88::ffff:a58:2` |
| ... | 依此类推（最多支持 64 个） | ... |

**使用方式**：

```yaml
# docker-compose.yml 中配置多 lighthouse
environment:
  - LIGHTHOUSE_IP_1=10.88.0.1
  - LIGHTHOUSE_PUBLIC_1=shanghai.example.com:6688
  - LIGHTHOUSE_IP_1_V6=fd88::ffff:a58:1  # 可选 IPv6
  - LIGHTHOUSE_IP_2=10.88.0.2
  - LIGHTHOUSE_PUBLIC_2=singapore.example.com:6688
  - LIGHTHOUSE_IP_2_V6=fd88::ffff:a58:2
  - LIGHTHOUSE_IP_3=10.88.0.3
  - LIGHTHOUSE_PUBLIC_3=tokyo.example.com:6688
```

**自动检测逻辑**：
- 如果检测到 `LIGHTHOUSE_IP_1` 和 `LIGHTHOUSE_PUBLIC_1` 存在，自动进入**多 lighthouse 模式**
- 否则使用 `LIGHTHOUSE_IP` 和 `LIGHTHOUSE_PUBLIC`（**单 lighthouse 模式**，向后兼容）
- IPv6 地址可选，不配置则仅生成 IPv4 条目

---

## 🏗️ 项目结构

```
iflygo-docker/
├── Dockerfile                 # 多阶段构建(Linux: Go 源码编译 + 运行镜像)
├── Dockerfile.windows         # Windows 容器镜像(打包原生 iflygo.exe)
├── docker-compose.yml         # Docker Compose 编排文件
├── init.sh                    # 配置初始化脚本(自动生成证书与配置)
├── entrypoint.sh              # 容器入口脚本
├── sign-client.sh             # 客户端证书签发脚本
├── conf/
│   ├── server/
│   │   └── config.yml         # Server 配置模板(含中文注释)
│   └── client/
│       └── config.yml         # Client 配置模板(含中文注释)
├── .github/workflows/
│   ├── docker-publish.yml          # Linux 多架构(amd64/arm64)自动构建
│   └── docker-publish-windows.yml  # Windows 多架构(amd64/arm64)自动构建
├── .gitignore
└── README.md
```

---

## 📚 详细说明

### Server (Lighthouse) 节点

Lighthouse 是 iFlyGo 网络中的核心节点，负责协助其他节点发现彼此的公网地址并建立 P2P 连接。特点：

- **固定公网 IP**: 必须有稳定的公网地址（或固定域名）
- **固定端口**: 监听端口必须固定（默认 6688）
- **am_lighthouse: true**: 配置中必须启用 lighthouse 功能
- **防火墙规则**: 通常允许任意入站（由客户端证书分组控制访问）

#### 部署示例

1. 在云服务器上启动 server 容器
2. 确保防火墙开放 UDP 6688 端口
3. 修改 `LIGHTHOUSE_PUBLIC` 为服务器公网 IP:6688
4. 复制 `/etc/iflygo/ca.crt` 到客户端（所有节点必须共享同一个 CA）

### Client 节点

Client 节点连接到 lighthouse 并与其他客户端建立 P2P 隧道。特点：

- **动态端口**: `LISTEN_PORT=0`，系统自动分配（适合 NAT 后的客户端）
- **am_lighthouse: false**: 配置中必须禁用 lighthouse 功能
- **hosts**: 必须填写 lighthouse 的内网 IP（如 `192.168.100.1`）
- **static_host_map**: 必须填写 lighthouse 的公网入口（如 `1.2.3.4:6688`）

#### 部署示例

1. 从 server 节点复制 `ca.crt` 到客户端的 `/etc/iflygo/` 目录
2. 修改 `LIGHTHOUSE_PUBLIC` 和 `LIGHTHOUSE_IP` 为 server 的地址
3. 修改 `IFLYGO_IP` 为此客户端的唯一内网 IP（如 `192.168.100.2`）
4. 启动容器后通过 `ping 192.168.100.1` 测试连通性

---

## 🔐 证书管理

### 自动生成（推荐）

容器启动时，`init.sh` 会自动检查并生成缺失的证书：

- **CA 证书** (`ca.crt` / `ca.key`): 首次启动时自动生成
- **节点证书** (`<CERT_HOSTNAME>.crt` / `<CERT_HOSTNAME>.key`): 根据 `NODE` / `IFLYGO_IP` / `IFLYGO_IP_V6` / `CERT_GROUPS` 自动签发（使用 v2 证书的 `-networks` 语法，同时支持 IPv4 + IPv6 双栈）

节点证书的文件名由 `CERT_HOSTNAME` 环境变量决定（默认取 `NODE` 的值），便于在共享卷或集中管理时按主机名区分不同节点的证书。例如：

```bash
# 设置 CERT_HOSTNAME=uola-servers-lead-01
# 生成的证书文件: uola-servers-lead-01.crt / uola-servers-lead-01.key
# 配置文件 pki 段会自动指向这两个文件:
#   cert: /etc/iflygo/uola-servers-lead-01.crt
#   key:  /etc/iflygo/uola-servers-lead-01.key
docker run -d \
  -e NODE=uola-servers-lead-01 \
  -e CERT_HOSTNAME=uola-servers-lead-01 \
  -e IFLYGO_IP=10.88.0.1 \
  ... \
  iflyelf/iflygo:latest
```

> 提示：如果不设置 `CERT_HOSTNAME`，证书文件名默认与 `NODE` 相同（如 `NODE=client1` 则生成 `client1.crt` / `client1.key`）。

### 在服务端签发客户端证书（快捷方式）

使用 `docker run --rm` 可以在服务端便捷地为客户端签发证书，无需手动输入 `iflygo-cert` 命令（需挂载服务端的证书目录，以使用已有的 CA）：

```bash
# 基础用法: 签发 IPv4 单栈证书
docker run --rm \
  -v $(pwd)/data/server/config:/etc/iflygo \
  iflyelf/iflygo:latest sign \
  -name client2 \
  -ip 10.88.0.101

# 完整用法: 签发 IPv4+IPv6 双栈 + 分组 + 网关子网路由
docker run --rm \
  -v $(pwd)/data/server/config:/etc/iflygo \
  iflyelf/iflygo:latest sign \
  -name uola-home-gw-01 \
  -ip 10.88.1.1 \
  -ip6 fd88::ffff:a58:101 \
  -groups home \
  -subnets 10.8.1.0/24 \
  -duration 876000h

# 也可用环境变量传参(与命令行等价)
docker run --rm \
  -v $(pwd)/data/server/config:/etc/iflygo \
  -e NODE=client3 \
  -e IFLYGO_IP=10.88.0.102 \
  -e IFLYGO_IP_V6=fd88::ffff:a58:102 \
  -e CERT_GROUPS=laptop,ssh \
  iflyelf/iflygo:latest sign
```

> 💡 提示：`-v $(pwd)/data/server/config:/etc/iflygo` 挂载是必需的，签发脚本需要读取该目录下已有的 `ca.crt`/`ca.key` 来签发，并将新证书输出到同一目录。

**输出文件**：
- 证书：`data/server/config/client2.crt`
- 私钥：`data/server/config/client2.key`
- CA：`data/server/config/ca.crt`（客户端需要）

**分发到客户端**：

```bash
# 将 3 个文件复制到客户端的配置目录
cp data/server/config/ca.crt data/client/config/
cp data/server/config/client2.crt data/client/config/
cp data/server/config/client2.key data/client/config/

# 启动客户端时指定证书文件名
docker compose up -d iflygo-client \
  -e NODE=client2 \
  -e CERT_HOSTNAME=client2 \
  -e IFLYGO_IP=10.88.0.101
```

**参数说明**：

| 参数 | 必填 | 说明 | 示例 |
|------|------|------|------|
| `-name` | ✅ | 节点名称（证书 CN，也是输出文件名前缀） | `client2` |
| `-ip` / `-ipv4` | ✅ | 节点内网 IPv4 地址 | `10.88.0.101` |
| `-ip6` / `-ipv6` | ❌ | 节点内网 IPv6 地址（留空则不签发 IPv6） | `fd88::ffff:a58:101` |
| `-netmask` | ❌ | IPv4 子网掩码（CIDR 位数） | `16`（默认） |
| `-netmask6` | ❌ | IPv6 子网掩码（CIDR 位数） | `64`（默认） |
| `-groups` | ❌ | 证书分组（逗号分隔，用于防火墙规则） | `laptop,home,ssh` |
| `-subnets` | ❌ | 网关子网路由（逗号分隔，用于 unsafe_routes） | `10.8.1.0/24` |
| `-duration` | ❌ | 证书有效期（留空则跟随 CA 剩余有效期） | `876000h`（100年） |

> ⚠️ **注意**：签发的证书会输出到服务端的挂载卷（`data/server/config/`），如果证书已存在会自动跳过（避免误覆盖）。如需重签，请先删除旧证书。

### 手动管理

如果需要在多个节点间共享 CA 或预先签发证书：

```bash
# 1. 生成 CA (仅在 server 上执行一次, 默认有效期 100 年)
docker exec iflygo-server iflygo-cert ca \
  -name "My iFlyGo CA" \
  -duration 876000h \
  -out-key ca.key \
  -out-crt ca.crt

# 2. 签发节点证书（使用 v2 -networks 语法支持 IPv4+IPv6 双栈）
docker exec iflygo-server iflygo-cert sign \
  -name "uola-office-dns-01" \
  -networks "10.88.0.100/16,fd88::ffff:a58:100/64" \
  -groups "laptop,home,ssh" \
  -duration 876000h \
  -out-crt uola-office-dns-01.crt \
  -out-key uola-office-dns-01.key

# 3. 签发网关证书（带 -subnets 子网路由, 用于 unsafe_routes）
docker exec iflygo-server iflygo-cert sign \
  -name "uola-home-gw-01" \
  -networks "10.88.1.1/16,fd88::ffff:a58:101/64" \
  -groups "home" \
  -subnets "10.8.1.0/24" \
  -duration 876000h \
  -out-crt uola-home-gw-01.crt \
  -out-key uola-home-gw-01.key

# 4. 复制证书到客户端（保持文件名与 CERT_HOSTNAME 对应）
docker cp iflygo-server:/etc/iflygo/ca.crt ./data/client2/config/
docker cp iflygo-server:/etc/iflygo/uola-office-dns-01.crt ./data/client2/config/
docker cp iflygo-server:/etc/iflygo/uola-office-dns-01.key ./data/client2/config/
```

> ⚠️ **重要**：
> - CA 有效期必须**大于等于**节点证书有效期，否则签发会报错 `certificate expires after signing certificate`
> - **推荐方案**：不设置 `CERT_DURATION`（留空），自动跟随 CA 剩余有效期，避免超期问题
> - **手动指定场景**：如果所有证书统一 100 年，手动设置 `CA_DURATION=876000h` + `CERT_DURATION=876000h`
> - **旧 CA 兼容**：如果挂载卷里有旧 CA（短有效期），删除旧 CA 让 init.sh 重新生成，或不设置 `CERT_DURATION`

### 查看证书信息

使用 `iflygo-cert print` 命令查看证书详细信息（网络、分组、有效期等）：

```bash
# 查看 CA 证书信息
docker exec -it iflygo-server iflygo-cert print -path /etc/iflygo/ca.crt

# 查看节点证书信息
docker exec -it iflygo-server iflygo-cert print -path /etc/iflygo/uola-servers-lead-01.crt

# 在本地查看（如果证书已挂载到宿主机）
docker run --rm -v $(pwd)/data/server/config:/certs \
  iflyelf/iflygo:latest iflygo-cert print -path /certs/ca.crt
```

**输出示例**（CA 证书）：
```
iFlyGo Certificate (version 2)
  Name: iFlyGo Root CA
  Not before: 2024-01-01 00:00:00 +0000 UTC
  Not after: 2123-12-31 23:59:59 +0000 UTC
  Is CA: true
```

**输出示例**（节点证书）：
```
iFlyGo Certificate (version 2)
  Name: uola-servers-lead-01
  Networks:
    - 10.88.0.1/16
    - fd88::ffff:a58:1/64
  Groups:
    - lead
  Is CA: false
  Not before: 2024-01-01 00:00:00 +0000 UTC
  Not after: 2123-12-31 23:59:59 +0000 UTC
```

> 💡 提示：通过 `print` 命令可以快速确认证书的网络地址、分组和有效期，排查配置问题。

---

## 🔥 防火墙规则

iFlyGo 使用基于证书 `groups` 的防火墙规则。示例：

### Server 配置 (允许所有入站)

```yaml
firewall:
  inbound:
    - port: any
      proto: any
      host: any
```

### Client 配置 (仅允许特定组访问)

```yaml
firewall:
  inbound:
    # 允许 ICMP
    - port: any
      proto: icmp
      host: any
    # 允许 laptop + home 组访问 443
    - port: 443
      proto: tcp
      groups:
        - laptop
        - home
```

签发证书时通过 `-groups` 参数指定分组：

```bash
iflygo-cert sign -name client1 -networks "192.168.100.2/24" -groups "laptop,home,ssh" -duration 876000h
```

---

## 🚀 高级特性

### 0. 自定义配置（修改模板）

**init.sh 现在基于完整配置模板生成运行时配置**，这意味着你可以通过修改 `conf/server/config.yml` 或 `conf/client/config.yml` 来自定义所有高级特性，而不仅限于环境变量支持的基础字段。

#### 方法一：修改模板并重新构建镜像（推荐）

```bash
# 1. 克隆仓库并修改配置模板
git clone https://github.com/iflyelf/iflygo-docker.git
cd iflygo-docker

# 2. 编辑配置模板（以 server 为例）
vim conf/server/config.yml
# 修改 unsafe_routes、static_map、cipher、read_buffer、handshakes 等任意字段

# 3. 重新构建镜像
docker build -t iflygo:custom .

# 4. 使用自定义镜像启动容器
docker run -d --name iflygo-server ... iflygo:custom
```

#### 方法二：直接挂载自定义配置（跳过 init.sh）

```bash
# 1. 从模板复制并自定义配置
cp conf/server/config.yml my-custom-config.yml
vim my-custom-config.yml
# 注意: 确认配置中 pki.cert / pki.key 指向的文件名与下方挂载一致

# 2. 挂载自定义配置到容器（init.sh 检测到已存在会跳过生成）
#    证书文件名需与 my-custom-config.yml 里 pki 段声明的路径一致
docker run -d \
  --name iflygo-server \
  -v $(pwd)/my-custom-config.yml:/etc/iflygo/config.yml:ro \
  -v $(pwd)/ca.crt:/etc/iflygo/ca.crt:ro \
  -v $(pwd)/lighthouse1.crt:/etc/iflygo/lighthouse1.crt:ro \
  -v $(pwd)/lighthouse1.key:/etc/iflygo/lighthouse1.key:ro \
  ... \
  iflyelf/iflygo:latest
```

#### 支持的高级配置

模板包含以下高级特性，修改后会完整保留：

- **unsafe_routes**: 不安全路由（将非 iFlyGo 子网路由到网关节点）
- **static_map**: DNS 缓存时间、网络类型（ip4/ip6/ip）、查询超时
- **cipher**: 加密算法（chachapoly / aes）
- **read_buffer / write_buffer**: UDP 缓冲区大小（高流量优化）
- **handshakes**: 握手超时、重试次数、缓冲通道大小
- **relay**: 中继服务器列表、是否作为中继节点
- **remote_allow_list / local_allow_list**: IP 范围白名单
- **preferred_ranges**: 本地网络范围提示
- **punchy**: 打洞延迟、响应模式
- **firewall**: 完整的防火墙规则（conntrack、出站/入站规则）

**注意**: 环境变量仅覆盖以下基础字段（证书路径、lighthouse 地址、端口、TUN 设备名、日志级别），其他字段必须通过修改模板配置。

---

### 1. 不安全路由 (Unsafe Routes)

将 iFlyGo 网络外的子网通过特定节点暴露，实现跨数据中心或 VPC 的网络互通。

#### 单网关路由（最简形式）

在 **server 模板** 的 `tun.unsafe_routes` 中添加：

```yaml
tun:
  dev: iflygo
  mtu: 1300
  routes: []
  unsafe_routes:
    - route: 172.16.1.0/24
      via: 192.168.100.99      # 网关节点的 iFlyGo 内网 IP
      mtu: 1300
      metric: 100
```

#### 多网关加权 ECMP（负载均衡 + 冗余）

```yaml
tun:
  unsafe_routes:
    # 通过两个网关节点访问 10.0.9.0/24，按权重分配流量
    - route: 10.0.9.0/24
      via:
        - gateway: 192.168.100.11
          weight: 10               # 大部分流量走此网关
        - gateway: 192.168.100.12
          weight: 5                # 少量流量走此网关（冗余）
      mtu: 1300
    
    # 监控网段：等权重负载均衡
    - route: 10.0.88.0/24
      via:
        - gateway: 192.168.100.11
        - gateway: 192.168.100.12

    # 大规模子网（如整个 10.7.0.0/16）
    - route: 10.7.0.0/16
      via:
        - gateway: 192.168.100.11
          weight: 10
        - gateway: 192.168.100.12
          weight: 5
```

#### 网关节点证书要求

⚠️ **重要**：作为 `via` 网关的节点，其证书必须在 `-subnets` 字段声明该子网，否则路由不生效：

```bash
# 在 lighthouse 上签发网关节点证书时声明子网（使用 v2 -networks 语法）
iflygo-cert sign \
  -name gateway1 \
  -networks "192.168.100.99/24" \
  -subnets "172.16.1.0/24,10.0.9.0/24,10.0.88.0/24" \
  -groups "gateway" \
  -duration 876000h
```

#### 路由生效验证

```bash
# 进入客户端容器
docker exec -it iflygo-client bash

# 查看路由表，应看到 unsafe_routes 已注入
ip route | grep iflygo

# 测试连通性
ping 172.16.1.10                # 应通过 iFlyGo 网关到达
traceroute 10.0.9.5             # 查看流量经过哪个网关
```

> 提示：完整的多网关 ECMP 模板示例可参考 `conf/server/config.yml` 中的 `unsafe_routes` 段落。

### 2. 中继 (Relay)

当两个节点无法直接建立 P2P 连接时，可通过第三方节点中继：

```yaml
# 中继节点配置
relay:
  am_relay: true

# 客户端配置
relay:
  relays:
    - 192.168.100.1
```

### 3. Prometheus 监控

```yaml
stats:
  type: prometheus
  listen: 127.0.0.1:8080
  path: /metrics
```

---

## 🛠️ 故障排查

### 1. 容器无法启动

**错误**: `/dev/net/tun` 不存在

```bash
# 解决方法: 确保宿主机加载 tun 模块
sudo modprobe tun
```

### 2. 证书签发失败 `certificate expires after signing certificate`

**原因**: 挂载卷里残留了旧的 CA（有效期较短），但节点证书指定了更长的有效期，导致超过 CA 有效期。

```bash
# 解决方法 1: 删除旧 CA, 让 init.sh 重新生成 100 年的 CA
docker compose down
rm -f data/server/config/ca.crt data/server/config/ca.key
rm -f data/server/config/*.crt data/server/config/*.key
docker compose up -d

# 解决方法 2: 不设置 CERT_DURATION (推荐)
# 留空则自动跟随 CA 剩余有效期, 避免超期问题
# docker-compose.yml 中删除 CERT_DURATION 环境变量即可
```

### 3. 无法 ping 通其他节点

```bash
# 检查日志
docker logs iflygo-client

# 常见原因:
# - lighthouse 公网地址配置错误
# - UDP 6688 端口未开放
# - CA 证书不一致
# - 防火墙规则阻止
```

### 4. 修改环境变量后配置不更新

**原因**: `init.sh` 默认在 `config.yml` 已存在时跳过生成，避免覆盖用户的手动修改。

```bash
# 方法 1: 设置 FORCE_REGEN=true 强制重新生成（推荐）
# 在 docker-compose.yml 中添加:
#   environment:
#     - FORCE_REGEN=true
docker compose up -d   # 旧配置会备份为 config.yml.bak.<时间戳>

# 方法 2: 手动删除旧配置后重启
rm data/server/config/config.yml
docker compose restart iflygo-server

# 方法 3: 一次性命令
docker compose run --rm -e FORCE_REGEN=true iflygo-server
```

> 💡 **建议**：首次部署或调试阶段可设置 `FORCE_REGEN=true` 让每次重启都用最新环境变量；生产稳定后改回 `false` 避免误覆盖。

### 5. 查看隧道状态

```bash
# 进入容器
docker exec -it iflygo-client bash

# 查看 TUN 接口
ip addr show iflygo

# 查看路由表
ip route | grep iflygo

# 查看连接
iflygo -config /etc/iflygo/config.yml -print-tunnel-info
```

---

## 📄 许可证

MIT License - 详见 [LICENSE](LICENSE)

---

## 📧 联系

- 作者: iflyelf
- 博客: [https://www.xiaonuo.live](https://www.xiaonuo.live)
- Issues: [https://github.com/iflyelf/iflygo-docker/issues](https://github.com/iflyelf/iflygo-docker/issues)
