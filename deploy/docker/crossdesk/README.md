# CrossDesk 远程桌面（自托管）

iFlyGo 通过独立服务集成 CrossDesk：

- **CrossDesk Server**：WSS 信令、设备身份/密码、在线状态、TURN 凭据；
- **Coturn**：P2P 失败时的媒体/数据中继；
- **CrossDesk Web 客户端**：浏览器远控；
- **官方桌面 / 移动 / CLI / MCP 客户端**：提供桌面墙、文件传输、音频、可信设备等全部功能。

> CrossDesk 客户端为 GPL-3.0，CrossDesk Server 为 LGPL-3.0，均以独立进程/镜像运行，
> 不链接进 iFlyGo 的 MIT 代码。镜像与源码：
> <https://github.com/kunkundi/crossdesk>、
> <https://github.com/kunkundi/crossdesk-server>。

## 1. 部署服务端

```bash
cd deploy/docker/crossdesk
cp .env.example .env
openssl rand -hex 32   # COTURN_AUTH_SECRET
openssl rand -hex 32   # COTURN_STATELESS_NONCE_SECRET
# 编辑 .env：EXTERNAL_IP、INTERNAL_IP、两个密钥
docker compose up -d
docker compose ps
```

放通防火墙/安全组：

| 端口 | 协议 | 用途 |
|---|---|---|
| `9099` | TCP | WSS 信令、HTTPS `/stats`、`/admin` |
| `3478` | TCP/UDP | STUN / TURN |
| `50000-60000` | UDP | TURN 中继 |

首次启动会在 `CROSSDESK_DATA_DIR/certs` 生成自签根证书
`api.crossdesk.cn_root.crt`，需要在控制端与被控端信任。

## 2. 配置 iFlyGo

进入 **设置 → 远程桌面（CrossDesk）**：

| 配置 | 示例 |
|---|---|
| 启用 | 开 |
| Server 地址 | `https://203.0.113.10:9099` |
| Web 客户端 | `https://web.crossdesk.cn` 或自托管 Web 地址 |
| 客户端下载 | `https://github.com/kunkundi/crossdesk/releases/latest` |
| 跳过 TLS 校验 | 仅自签证书测试时开启 |

保存后打开 **远程管理 → 远程桌面（CrossDesk）**：

- 查看 `/stats` 连通性与延迟；
- 在页面内打开 Web 客户端；
- 跳转服务端管理后台 `/admin`；
- 查看客户端下载入口与自托管配置。

> **Web 客户端**：官方 `https://web.crossdesk.cn` 默认连接官方信令。自托管场景需要用
> [crossdesk-web-client](https://github.com/kunkundi/crossdesk-web-client) 部署静态站点，
> 并在 `index.html` 中按文档设置 `window.CROSSDESK_CONFIG.signalingUrl` 指向本服务，
> 再把该 HTTPS 地址填入 iFlyGo 的「Web 客户端地址」。

## 3. 配置客户端

桌面/移动客户端 **设置 → 自建服务配置**：

- Server 地址：`203.0.113.10` 或域名；
- 信令端口：`9099`；
- 两端必须使用同一服务；
- 自签证书先导入根证书。

Web 客户端需要浏览器信任 WSS 证书；可在浏览器直接访问
`https://203.0.113.10:9099/stats` 验证。

## 4. 功能范围

CrossDesk 客户端提供：跨平台互控、IP 直连、桌面墙、文件传输、音频、
多显示器、剪贴板、CLI、MCP、可信设备、登录前远控、Linux 无头运行。
iFlyGo 提供统一入口、服务状态、Web 嵌入、下载与部署管理。

## 5. 管理后台

在 `.env` 同时设置 `ADMIN_USERNAME` / `ADMIN_PASSWORD` 后重启，
访问 `https://<server>:9099/admin`。

## 6. 常见问题

- **证书错误**：自签证书需导入系统/浏览器信任库；证书只覆盖 `EXTERNAL_IP`，
  用域名需替换证书。
- **能打开网页但无法连接**：检查 WSS/TURN/媒体端口和安全组。
- **页面状态不可达**：iFlyGo 容器/主机需能访问 CrossDesk Server；自签证书可临时开启
  「跳过 TLS 校验」测试。

## 7. 原生 Go 信令（Phase 1）

如不部署外部 CrossDesk Server，可开启 iFlyGo 内置信令：

1. 设置 → 远程桌面（CrossDesk）：
   - 启用；
   - 使用 iFlyGo 原生 Go 信令 = 开；
   - 信令路径 `/crossdesk/signal`；
   - TURN 地址/端口/密钥（与 Coturn `static-auth-secret` 一致）。
2. 反向代理把 `wss://<iflygo-host>/crossdesk/signal` 转发到 iFlyGo，并支持 WebSocket 升级。
3. CrossDesk 客户端自托管地址填 `wss://<iflygo-host>/crossdesk/signal`（或域名 + 路径）。
4. 原生 Web 客户端：`/crossdesk/web/`（已内置，自动注入原生 WSS/STUN 配置）。
5. 原生统计：`GET /crossdesk/stats`。

当前 Phase 1 覆盖设备注册/登录、Join、Offer/Answer/ICE、TURN 临时凭据；
设备密钥登录、重连令牌、修改密码与完整管理后台在后续阶段补齐。
