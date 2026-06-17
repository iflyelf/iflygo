# iFlyGo Docker 🚀

[![Docker Pulls](https://img.shields.io/docker/pulls/iflyelf/iflygo?style=flat-square)](https://hub.docker.com/r/iflyelf/iflygo)
[![Docker Image Size](https://img.shields.io/docker/image-size/iflyelf/iflygo/latest?style=flat-square)](https://hub.docker.com/r/iflyelf/iflygo)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square)](LICENSE)

**iFlyGo** 是一款可扩展的 overlay 网络隧道工具，专注于**性能**、**简洁**和**安全**。通过 Docker 多阶段构建，支持多架构（amd64/arm64）部署，适用于跨公网、跨数据中心的安全组网场景。

---

## ✨ 特性

- 🛡️ **安全加密**: 基于 Noise Protocol Framework 的相互认证，默认使用 Curve25519 + AES-256-GCM
- 🌍 **跨平台**: Docker 镜像支持 Linux amd64/arm64，可在云服务器、树莓派、NAS 等设备运行
- 🏗️ **多阶段构建**: 从 Go 源码编译，生成精简运行镜像
- 📦 **即开即用**: 提供 server (lighthouse) / client 两种运行模式，通过环境变量自动生成配置
- 🔧 **灵活配置**: 支持证书自动签发、防火墙规则、分组、中继等高级特性
- 📝 **中文注释**: 所有配置模板均包含详尽的中文注释

---

## 📦 快速开始

### 1. 拉取镜像

```bash
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

| 变量名 | 说明 | 默认值 | 示例 |
|--------|------|--------|------|
| `RUNMODE` | 运行模式 | `client` | `server` / `client` |
| `NODE` | 节点名称（用于证书） | `iflygo-node` | `lighthouse1` / `client1` |
| `IFLYGO_IP` | 此节点的内网 IP | `192.168.100.1` | `192.168.100.2` |
| `IFLYGO_NETMASK` | 内网子网掩码 | `24` | `24` |
| `LIGHTHOUSE_IP` | Lighthouse 内网 IP | `192.168.100.1` | `192.168.100.1` |
| `LIGHTHOUSE_PUBLIC` | Lighthouse 公网入口 | `127.0.0.1:6688` | `1.2.3.4:6688` |
| `LISTEN_HOST` | 监听地址 | `::` | `0.0.0.0` |
| `LISTEN_PORT` | 监听端口 | `6688` | `0` (client 推荐 0) |
| `TUN_DEV` | TUN 设备名 | `iflygo` | `iflygo` |
| `TUN_MTU` | TUN MTU | `1300` | `1300` |
| `LOG_LEVEL` | 日志级别 | `info` | `debug` / `trace` |
| `LOG_FORMAT` | 日志格式 | `text` | `json` |
| `GROUPS` | 证书分组 | (空) | `laptop,home,ssh` |
| `CERT_DURATION` | 证书有效期 | `26280h` (3年) | `8760h` (1年) |
| `AUTO_GEN_CA` | 自动生成 CA | `true` | `false` |

---

## 🏗️ 项目结构

```
iflygo-docker/
├── Dockerfile                 # 多阶段构建(Go 源码编译 + 运行镜像)
├── docker-compose.yml         # Docker Compose 编排文件
├── init.sh                    # 配置初始化脚本(自动生成证书与配置)
├── entrypoint.sh              # 容器入口脚本
├── conf/
│   ├── server/
│   │   └── config.yml         # Server 配置模板(含中文注释)
│   └── client/
│       └── config.yml         # Client 配置模板(含中文注释)
├── .github/workflows/
│   └── docker-publish.yml     # GitHub Actions 自动构建工作流
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
- **节点证书** (`host.crt` / `host.key`): 根据 `NODE` / `IFLYGO_IP` / `GROUPS` 自动签发

### 手动管理

如果需要在多个节点间共享 CA 或预先签发证书：

```bash
# 1. 生成 CA (仅在 server 上执行一次)
docker exec iflygo-server iflygo-cert ca -name "My iFlyGo CA"

# 2. 签发节点证书
docker exec iflygo-server iflygo-cert sign \
  -name "client2" \
  -ip "192.168.100.3/24" \
  -groups "laptop,home,ssh" \
  -out-crt client2.crt \
  -out-key client2.key

# 3. 复制证书到客户端
docker cp iflygo-server:/etc/iflygo/ca.crt ./data/client2/config/
docker cp iflygo-server:/etc/iflygo/client2.crt ./data/client2/config/host.crt
docker cp iflygo-server:/etc/iflygo/client2.key ./data/client2/config/host.key
```

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
iflygo-cert sign -name client1 -ip 192.168.100.2/24 -groups "laptop,home,ssh"
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

# 2. 挂载自定义配置到容器（init.sh 检测到已存在会跳过生成）
docker run -d \
  --name iflygo-server \
  -v $(pwd)/my-custom-config.yml:/etc/iflygo/config.yml:ro \
  -v $(pwd)/ca.crt:/etc/iflygo/ca.crt:ro \
  -v $(pwd)/host.crt:/etc/iflygo/host.crt:ro \
  -v $(pwd)/host.key:/etc/iflygo/host.key:ro \
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
# 在 lighthouse 上签发网关节点证书时声明子网
iflygo-cert sign \
  -name gateway1 \
  -ip 192.168.100.99/24 \
  -subnets "172.16.1.0/24,10.0.9.0/24,10.0.88.0/24" \
  -groups "gateway"
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

### 2. 无法 ping 通其他节点

```bash
# 检查日志
docker logs iflygo-client

# 常见原因:
# - lighthouse 公网地址配置错误
# - UDP 6688 端口未开放
# - CA 证书不一致
# - 防火墙规则阻止
```

### 3. 查看隧道状态

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
