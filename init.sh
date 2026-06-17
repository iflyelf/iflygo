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
# 2. 渲染配置文件
#    基于 /etc/iflygo/templates/ 下的完整模板生成, 仅当目标文件不存在时生成
#    这样可完整保留模板中的高级配置(unsafe_routes / static_map / cipher /
#    read_buffer / handshakes 等), 再用环境变量替换关键标量字段
# ------------------------------------------------------------------------------
TARGET_CONF="${CONF_DIR}/config.yml"

# 根据 RUNMODE 选择模板文件
case "${RUNMODE}" in
    server|lighthouse) SRC_TEMPLATE="${TEMPLATE_DIR}/server.yml" ;;
    client)            SRC_TEMPLATE="${TEMPLATE_DIR}/client.yml" ;;
    *)
        echo "[iflygo-init][ERROR] 未知 RUNMODE=${RUNMODE} (期望 server 或 client)"
        exit 1
        ;;
esac

# 用环境变量替换模板中的关键字段(锚定行首 + 缩进, 避免误伤其他出现位置)
apply_env_overrides() {
    local f="$1"
    # 证书路径(模板默认 /etc/iflygo, 若 CONF_DIR 不同则同步)
    sed -i "s|/etc/iflygo|${CONF_DIR}|g" "$f"
    # lighthouse 连接信息: 模板示例值替换为环境变量
    sed -i "s|lighthouse1.example.com:6688|${LIGHTHOUSE_PUBLIC}|g" "$f"
    sed -i "s|192.168.100.1|${LIGHTHOUSE_IP}|g" "$f"
    # TUN 设备名与 MTU
    sed -i "s|^  dev: iflygo$|  dev: ${TUN_DEV}|" "$f"
    sed -i "s|^  mtu: 1300$|  mtu: ${TUN_MTU}|" "$f"
    # 监听地址与端口(server 模板默认 6688, client 模板默认 0)
    sed -i "s|^  host: \"\\[::\\]\"$|  host: \"${LISTEN_HOST}\"|" "$f"
    sed -i "s|^  port: 6688$|  port: ${LISTEN_PORT}|" "$f"
    sed -i "s|^  port: 0$|  port: ${LISTEN_PORT}|" "$f"
    # 日志级别与格式
    sed -i "s|^  level: info$|  level: ${LOG_LEVEL}|" "$f"
    sed -i "s|^  format: text$|  format: ${LOG_FORMAT}|" "$f"
}

if [ ! -f "${TARGET_CONF}" ]; then
    if [ ! -f "${SRC_TEMPLATE}" ]; then
        echo "[iflygo-init][ERROR] 模板缺失: ${SRC_TEMPLATE}"
        echo "[iflygo-init] 请确认镜像内已包含 conf/ 模板, 或手动放置配置: ${TARGET_CONF}"
        exit 1
    fi
    echo "[iflygo-init] 基于模板生成 ${RUNMODE} 配置: ${SRC_TEMPLATE} -> ${TARGET_CONF}"
    cp "${SRC_TEMPLATE}" "${TARGET_CONF}"
    apply_env_overrides "${TARGET_CONF}"
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
