#!/bin/bash
# ==============================================================================
# iFlyGo 配置初始化脚本
# 根据 RUNMODE (server/client) 自动生成配置文件
# 仅当 /etc/iflygo/config.yml 不存在时才生成 (避免覆盖用户已有配置)
# ==============================================================================
set -euo pipefail

# 默认变量(可通过 docker -e 覆盖)
RUNMODE="${RUNMODE:-client}"            # 运行模式: server | client
NODE="${NODE:-iflygo-node}"             # 节点名称(用于证书 -name)
IFLYGO_IP="${IFLYGO_IP:-192.168.100.1}" # 此节点的 iflygo 内网 IP
IFLYGO_NETMASK="${IFLYGO_NETMASK:-24}"  # 内网子网掩码
LIGHTHOUSE_IP="${LIGHTHOUSE_IP:-192.168.100.1}"               # lighthouse 的内网 IP
LIGHTHOUSE_PUBLIC="${LIGHTHOUSE_PUBLIC:-127.0.0.1:6688}"      # lighthouse 的公网地址 host:port
LISTEN_HOST="${LISTEN_HOST:-::}"        # 监听地址
LISTEN_PORT="${LISTEN_PORT:-6688}"      # 监听端口(client 推荐 0)
TUN_DEV="${TUN_DEV:-iflygo}"            # tun 设备名
TUN_MTU="${TUN_MTU:-1300}"              # tun MTU
LOG_LEVEL="${LOG_LEVEL:-info}"          # 日志级别 trace/debug/info/warn/error
LOG_FORMAT="${LOG_FORMAT:-text}"        # 日志格式 text/json
GROUPS="${GROUPS:-}"                    # 证书签发时的分组(逗号分隔)
CERT_DURATION="${CERT_DURATION:-26280h}" # 证书有效期(默认 3 年, ca 默认 1 年, 这里用 -duration)
AUTO_GEN_CA="${AUTO_GEN_CA:-true}"      # 当 ca 不存在时是否自动生成

CONF_DIR="${IFLYGO_CONF_DIR:-/etc/iflygo}"
LOG_DIR="${IFLYGO_LOG_DIR:-/var/log/iflygo}"
TEMPLATE_DIR="${CONF_DIR}/templates"

mkdir -p "${CONF_DIR}/hosts" "${LOG_DIR}"

# ------------------------------------------------------------------------------
# 1. 自动生成 CA 与节点证书 (只在缺失且开启 AUTO_GEN_CA 时执行)
# ------------------------------------------------------------------------------
if [ ! -f "${CONF_DIR}/ca.crt" ] || [ ! -f "${CONF_DIR}/ca.key" ]; then
    if [ "${AUTO_GEN_CA}" = "true" ]; then
        echo "[iflygo-init] 未发现 CA, 自动生成: ${CONF_DIR}/ca.{crt,key}"
        ( cd "${CONF_DIR}" && \
          iflygo-cert ca -name "iFlyGo CA (${NODE})" -duration "${CERT_DURATION}" )
    else
        echo "[iflygo-init][WARN] 未发现 CA 且 AUTO_GEN_CA=false, 请手动放置 ca.crt/ca.key"
    fi
fi

if [ ! -f "${CONF_DIR}/host.crt" ] || [ ! -f "${CONF_DIR}/host.key" ]; then
    if [ -f "${CONF_DIR}/ca.crt" ] && [ -f "${CONF_DIR}/ca.key" ]; then
        echo "[iflygo-init] 自动签发节点证书: name=${NODE} ip=${IFLYGO_IP}/${IFLYGO_NETMASK}"
        SIGN_ARGS=(-name "${NODE}" -ip "${IFLYGO_IP}/${IFLYGO_NETMASK}" -duration "${CERT_DURATION}")
        if [ -n "${GROUPS}" ]; then
            SIGN_ARGS+=(-groups "${GROUPS}")
        fi
        ( cd "${CONF_DIR}" && \
          iflygo-cert sign "${SIGN_ARGS[@]}" -out-crt host.crt -out-key host.key )
    fi
fi

# ------------------------------------------------------------------------------
# 2. 渲染配置文件 (优先使用模板, 仅当目标文件不存在时生成)
# ------------------------------------------------------------------------------
TARGET_CONF="${CONF_DIR}/config.yml"

render_server_conf() {
    cat >"${TARGET_CONF}" <<EOF
# iFlyGo Server (Lighthouse) 配置 - 由 init.sh 自动生成
# 节点名称: ${NODE}
# 内网 IP : ${IFLYGO_IP}/${IFLYGO_NETMASK}
# 公网入口: ${LIGHTHOUSE_PUBLIC}

# PKI 证书配置
pki:
  ca: ${CONF_DIR}/ca.crt
  cert: ${CONF_DIR}/host.crt
  key: ${CONF_DIR}/host.key

# 静态主机映射 - lighthouse 自身公网地址
static_host_map:
  "${LIGHTHOUSE_IP}": ["${LIGHTHOUSE_PUBLIC}"]

# Lighthouse 配置
lighthouse:
  # SERVER 节点必须为 true
  am_lighthouse: true
  interval: 60
  hosts: []

# 监听端口
listen:
  host: "${LISTEN_HOST}"
  port: ${LISTEN_PORT}

# NAT 打洞
punchy:
  punch: true

# 中继(默认关闭)
relay:
  am_relay: false
  use_relays: true

# TUN 配置
tun:
  disabled: false
  dev: ${TUN_DEV}
  drop_local_broadcast: false
  drop_multicast: false
  tx_queue: 500
  mtu: ${TUN_MTU}

# 日志配置
logging:
  level: ${LOG_LEVEL}
  format: ${LOG_FORMAT}

# 防火墙规则 (lighthouse 通常允许任意, 由各节点用证书 group 控制)
firewall:
  outbound_action: drop
  inbound_action: drop
  conntrack:
    tcp_timeout: 12m
    udp_timeout: 3m
    default_timeout: 10m
  outbound:
    - port: any
      proto: any
      host: any
  inbound:
    - port: any
      proto: icmp
      host: any
    - port: any
      proto: any
      host: any
EOF
}

render_client_conf() {
    cat >"${TARGET_CONF}" <<EOF
# iFlyGo Client 配置 - 由 init.sh 自动生成
# 节点名称: ${NODE}
# 内网 IP : ${IFLYGO_IP}/${IFLYGO_NETMASK}
# 连接到 lighthouse: ${LIGHTHOUSE_IP} (${LIGHTHOUSE_PUBLIC})

# PKI 证书配置
pki:
  ca: ${CONF_DIR}/ca.crt
  cert: ${CONF_DIR}/host.crt
  key: ${CONF_DIR}/host.key

# 静态主机映射 - 必须指向 lighthouse 公网入口
static_host_map:
  "${LIGHTHOUSE_IP}": ["${LIGHTHOUSE_PUBLIC}"]

# Lighthouse 配置
lighthouse:
  # CLIENT 节点必须为 false
  am_lighthouse: false
  interval: 60
  hosts:
    - "${LIGHTHOUSE_IP}"

# 监听端口 (客户端推荐 0, 系统动态分配)
listen:
  host: "${LISTEN_HOST}"
  port: ${LISTEN_PORT}

# NAT 打洞 (client 建议开启 respond)
punchy:
  punch: true
  respond: true

# 中继(默认关闭)
relay:
  am_relay: false
  use_relays: true

# TUN 配置
tun:
  disabled: false
  dev: ${TUN_DEV}
  drop_local_broadcast: false
  drop_multicast: false
  tx_queue: 500
  mtu: ${TUN_MTU}

# 日志配置
logging:
  level: ${LOG_LEVEL}
  format: ${LOG_FORMAT}

# 防火墙规则 (默认允许任意出站, 入站允许 icmp; 按需添加更多规则)
firewall:
  outbound_action: drop
  inbound_action: drop
  conntrack:
    tcp_timeout: 12m
    udp_timeout: 3m
    default_timeout: 10m
  outbound:
    - port: any
      proto: any
      host: any
  inbound:
    - port: any
      proto: icmp
      host: any
EOF
}

if [ ! -f "${TARGET_CONF}" ]; then
    case "${RUNMODE}" in
        server|lighthouse)
            echo "[iflygo-init] 生成 SERVER (lighthouse) 配置: ${TARGET_CONF}"
            render_server_conf
            ;;
        client)
            echo "[iflygo-init] 生成 CLIENT 配置: ${TARGET_CONF}"
            render_client_conf
            ;;
        *)
            echo "[iflygo-init][ERROR] 未知 RUNMODE=${RUNMODE} (期望 server 或 client)"
            exit 1
            ;;
    esac
else
    echo "[iflygo-init] 已存在配置, 跳过生成: ${TARGET_CONF}"
fi

# 设置严格的文件权限(私钥 600)
chmod 600 "${CONF_DIR}"/*.key 2>/dev/null || true
chmod 644 "${CONF_DIR}"/*.crt 2>/dev/null || true
chmod 644 "${TARGET_CONF}" 2>/dev/null || true

# 确保 /dev/net/tun 存在(容器需要 --device /dev/net/tun)
if [ ! -e /dev/net/tun ]; then
    echo "[iflygo-init][WARN] /dev/net/tun 不存在, 请使用 --device /dev/net/tun 启动容器"
fi

echo "[iflygo-init] 初始化完成: RUNMODE=${RUNMODE} NODE=${NODE} IFLYGO_IP=${IFLYGO_IP}"
