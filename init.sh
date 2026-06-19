#!/bin/bash
# ==============================================================================
# iFlyGo 配置初始化脚本
# 根据 RUNMODE (server/client) 自动生成配置文件
# 仅当 /etc/iflygo/config.yml 不存在时才生成 (避免覆盖用户已有配置)
#
# 支持单 lighthouse 和多 lighthouse 两种模式:
#   - 单 lighthouse:  使用 LIGHTHOUSE_IP / LIGHTHOUSE_PUBLIC (向后兼容)
#   - 多 lighthouse:  使用 LIGHTHOUSE_IP_1..N / LIGHTHOUSE_PUBLIC_1..N
#                     可选 IPv6: LIGHTHOUSE_IP_1_V6..N_V6
# ==============================================================================
set -euo pipefail

# 默认变量(可通过 docker -e 覆盖)
RUNMODE="${RUNMODE:-client}"            # 运行模式: server | client
NODE="${NODE:-iflygo-node}"             # 节点名称(用于证书 -name)
# 证书文件主机名(用于命名 <CERT_HOSTNAME>.crt / .key, 默认取 NODE)
# 例如 CERT_HOSTNAME=uola-servers-lead-01 -> uola-servers-lead-01.crt / .key
CERT_HOSTNAME="${CERT_HOSTNAME:-${NODE}}"
IFLYGO_IP="${IFLYGO_IP:-10.88.0.1}"     # 此节点的 iflygo 内网 IPv4 地址
IFLYGO_IP_V6="${IFLYGO_IP_V6:-fd88::ffff:a58:1}"  # 此节点的 iflygo 内网 IPv6 地址(可选, 留空则不签发 IPv6)
IFLYGO_NETMASK="${IFLYGO_NETMASK:-16}"  # 内网子网掩码(IPv4 CIDR 位数)
IFLYGO_NETMASK_V6="${IFLYGO_NETMASK_V6:-64}"  # IPv6 子网掩码(CIDR 位数, 默认 64)

# 单 lighthouse 模式变量(向后兼容)
LIGHTHOUSE_IP="${LIGHTHOUSE_IP:-10.88.0.1}"                   # lighthouse 的内网 IP
LIGHTHOUSE_PUBLIC="${LIGHTHOUSE_PUBLIC:-127.0.0.1:6688}"      # lighthouse 的公网地址 host:port

LISTEN_HOST="${LISTEN_HOST:-::}"        # 监听地址
LISTEN_PORT="${LISTEN_PORT:-6688}"      # 监听端口(client 推荐 0)
TUN_DEV="${TUN_DEV:-iflygo}"            # tun 设备名
TUN_MTU="${TUN_MTU:-1300}"              # tun MTU
LOG_LEVEL="${LOG_LEVEL:-info}"          # 日志级别 trace/debug/info/warn/error
LOG_FORMAT="${LOG_FORMAT:-text}"        # 日志格式 text/json
CERT_GROUPS="${CERT_GROUPS:-}"          # 证书签发时的分组(逗号分隔, 注意: 不要用 GROUPS, 它是 bash 内置变量)
SUBNETS="${SUBNETS:-}"                  # 网关证书签发时的子网路由(逗号分隔, 用于 unsafe_routes)
CA_DURATION="${CA_DURATION:-876000h}"   # CA 证书有效期(默认 100 年)
# 节点证书有效期: 留空则自动取 CA 剩余有效期(防止超过 CA 有效期导致签发失败)
# 显式设置示例: CERT_DURATION=26280h (3年) / CERT_DURATION=876000h (100年)
CERT_DURATION="${CERT_DURATION:-}"
AUTO_GEN_CA="${AUTO_GEN_CA:-true}"      # 当 ca 不存在时是否自动生成

CONF_DIR="${IFLYGO_CONF_DIR:-/etc/iflygo}"
LOG_DIR="${IFLYGO_LOG_DIR:-/var/log/iflygo}"
# 模板目录: 放在 /opt/iflygo/templates/ 不会被用户挂载 /etc/iflygo 覆盖
TEMPLATE_DIR="${IFLYGO_TEMPLATE_DIR:-/opt/iflygo/templates}"

mkdir -p "${CONF_DIR}/hosts" "${LOG_DIR}"

# ------------------------------------------------------------------------------
# 1. 自动生成 CA 与节点证书 (只在缺失且开启 AUTO_GEN_CA 时执行)
# ------------------------------------------------------------------------------
if [ ! -f "${CONF_DIR}/ca.crt" ] || [ ! -f "${CONF_DIR}/ca.key" ]; then
    if [ "${AUTO_GEN_CA}" = "true" ]; then
        echo "[iflygo-init] 未发现 CA, 自动生成: ${CONF_DIR}/ca.{crt,key} (有效期: ${CA_DURATION})"
        ( cd "${CONF_DIR}" && \
          iflygo-cert ca -name "iFlyGo CA (${NODE})" -duration "${CA_DURATION}" )
    else
        echo "[iflygo-init][WARN] 未发现 CA 且 AUTO_GEN_CA=false, 请手动放置 ca.crt/ca.key"
    fi
fi

# 节点证书文件名(基于自定义主机名, 默认取 NODE)
HOST_CRT="${CERT_HOSTNAME}.crt"
HOST_KEY="${CERT_HOSTNAME}.key"

if [ ! -f "${CONF_DIR}/${HOST_CRT}" ] || [ ! -f "${CONF_DIR}/${HOST_KEY}" ]; then
    if [ -f "${CONF_DIR}/ca.crt" ] && [ -f "${CONF_DIR}/ca.key" ]; then
        # 构建 -networks 参数: IPv4 必需, IPv6 可选
        NETWORKS="${IFLYGO_IP}/${IFLYGO_NETMASK}"
        if [ -n "${IFLYGO_IP_V6}" ]; then
            NETWORKS="${NETWORKS},${IFLYGO_IP_V6}/${IFLYGO_NETMASK_V6}"
        fi
        
        # 构建签发参数(SIGN_ARGS 数组), 支持 -duration 可选
        SIGN_ARGS=(-name "${NODE}" -networks "${NETWORKS}")
        
        if [ -n "${CERT_DURATION}" ]; then
            SIGN_ARGS+=(-duration "${CERT_DURATION}")
            echo "[iflygo-init] 自动签发节点证书: name=${NODE} networks=${NETWORKS} duration=${CERT_DURATION} -> ${HOST_CRT}/${HOST_KEY}"
        else
            # 不指定 -duration: nebula 默认会取 CA 剩余有效期(确保不会超过 CA)
            echo "[iflygo-init] 自动签发节点证书: name=${NODE} networks=${NETWORKS} duration=<跟随 CA 剩余有效期> -> ${HOST_CRT}/${HOST_KEY}"
        fi
        
        if [ -n "${CERT_GROUPS}" ]; then
            SIGN_ARGS+=(-groups "${CERT_GROUPS}")
        fi
        
        if [ -n "${SUBNETS}" ]; then
            SIGN_ARGS+=(-subnets "${SUBNETS}")
            echo "[iflygo-init]   网关子网: ${SUBNETS}"
        fi
        
        ( cd "${CONF_DIR}" && \
          iflygo-cert sign "${SIGN_ARGS[@]}" -out-crt "${HOST_CRT}" -out-key "${HOST_KEY}" )
    fi
fi

# ------------------------------------------------------------------------------
# 2. 收集 lighthouse 列表
#    优先级: 多 lighthouse 环境变量(LIGHTHOUSE_IP_1..N) > 单 lighthouse 变量
#    格式: LIGHTHOUSE_IPS / LIGHTHOUSE_PUBLICS 为对齐数组
#          LIGHTHOUSE_IPS_V6 为可选的 IPv6 地址数组(对应 IPv4 索引)
# ------------------------------------------------------------------------------
LIGHTHOUSE_IPS=()
LIGHTHOUSE_PUBLICS=()
LIGHTHOUSE_IPS_V6=()

# 扫描 LIGHTHOUSE_IP_1, LIGHTHOUSE_IP_2 ... 直到连续两个未定义则停止
i=1
gap=0
while [ "$gap" -lt 2 ]; do
    ip_var="LIGHTHOUSE_IP_${i}"
    pub_var="LIGHTHOUSE_PUBLIC_${i}"
    v6_var="LIGHTHOUSE_IP_${i}_V6"

    ip_val="${!ip_var:-}"
    pub_val="${!pub_var:-}"
    v6_val="${!v6_var:-}"

    if [ -n "$ip_val" ] && [ -n "$pub_val" ]; then
        LIGHTHOUSE_IPS+=("$ip_val")
        LIGHTHOUSE_PUBLICS+=("$pub_val")
        LIGHTHOUSE_IPS_V6+=("$v6_val")
        gap=0
    else
        gap=$((gap + 1))
    fi
    i=$((i + 1))

    # 安全上限, 避免误配置导致死循环
    [ "$i" -gt 64 ] && break
done

# 如果没有多 lighthouse 变量, 退化为单 lighthouse 模式(向后兼容)
if [ "${#LIGHTHOUSE_IPS[@]}" -eq 0 ]; then
    LIGHTHOUSE_IPS=("$LIGHTHOUSE_IP")
    LIGHTHOUSE_PUBLICS=("$LIGHTHOUSE_PUBLIC")
    LIGHTHOUSE_IPS_V6=("")
    echo "[iflygo-init] 使用单 lighthouse 模式: ${LIGHTHOUSE_IP} -> ${LIGHTHOUSE_PUBLIC}"
else
    echo "[iflygo-init] 使用多 lighthouse 模式, 检测到 ${#LIGHTHOUSE_IPS[@]} 个 lighthouse:"
    for j in "${!LIGHTHOUSE_IPS[@]}"; do
        echo "  - ${LIGHTHOUSE_IPS[$j]} -> ${LIGHTHOUSE_PUBLICS[$j]}"
        [ -n "${LIGHTHOUSE_IPS_V6[$j]}" ] && echo "    (IPv6: ${LIGHTHOUSE_IPS_V6[$j]})"
    done
fi

# ------------------------------------------------------------------------------
# 3. 生成 lighthouse 相关 YAML 片段
# ------------------------------------------------------------------------------

# 生成 static_host_map 片段
generate_static_host_map() {
    echo "static_host_map:"
    for j in "${!LIGHTHOUSE_IPS[@]}"; do
        echo "  \"${LIGHTHOUSE_IPS[$j]}\": [\"${LIGHTHOUSE_PUBLICS[$j]}\"]"
        if [ -n "${LIGHTHOUSE_IPS_V6[$j]}" ]; then
            echo "  \"${LIGHTHOUSE_IPS_V6[$j]}\": [\"${LIGHTHOUSE_PUBLICS[$j]}\"]"
        fi
    done
}

# 生成 lighthouse.hosts 片段(client 用, server 应为空)
generate_lighthouse_hosts() {
    if [ ${#LIGHTHOUSE_IPS[@]} -eq 0 ]; then
        echo "  hosts: []"
        return
    fi
    echo "  hosts:"
    for j in "${!LIGHTHOUSE_IPS[@]}"; do
        echo "    - \"${LIGHTHOUSE_IPS[$j]}\""
        if [ -n "${LIGHTHOUSE_IPS_V6[$j]}" ]; then
            echo "    - \"${LIGHTHOUSE_IPS_V6[$j]}\""
        fi
    done
}

# ------------------------------------------------------------------------------
# 4. 渲染配置文件
#    基于 templates/ 下的完整模板生成, 替换关键段落
#    保留模板中 unsafe_routes / static_map / cipher / handshakes / firewall 等高级配置
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

# 替换 YAML 中以指定 key 开始的整段(到下一个顶级 key 之前)
# 用法: replace_yaml_block <file> <key> <new_content_string>
replace_yaml_block() {
    local f="$1" key="$2" new_content="$3"
    local tmp
    tmp=$(mktemp)
    awk -v key="$key" -v new="$new_content" '
        BEGIN { in_block = 0; printed = 0 }
        # 匹配以 key: 开头的顶级行(无前导空格)
        $0 ~ "^" key ":" {
            in_block = 1
            if (!printed) {
                print new
                printed = 1
            }
            next
        }
        # 在块内: 遇到下一个顶级 key (无前导空格 + 以非空字符开头) 则结束
        in_block == 1 {
            if ($0 ~ /^[a-zA-Z_]/) {
                in_block = 0
                print
            }
            # 块内的行(缩进的 / 注释 / 空行)直接跳过
            next
        }
        { print }
    ' "$f" > "$tmp"
    mv "$tmp" "$f"
}

# 替换 lighthouse 块中的 hosts 字段(保留 lighthouse 块的其他字段)
replace_lighthouse_hosts() {
    local f="$1" new_hosts="$2"
    local tmp
    tmp=$(mktemp)
    awk -v new="$new_hosts" '
        BEGIN { in_lighthouse = 0; in_hosts = 0; printed = 0 }
        /^lighthouse:/ { in_lighthouse = 1; print; next }
        # lighthouse 块结束(遇到下一个顶级 key)
        in_lighthouse == 1 && /^[a-zA-Z_]/ { in_lighthouse = 0; in_hosts = 0 }
        # 匹配 lighthouse 内的 hosts: 字段(2 空格缩进)
        in_lighthouse == 1 && /^  hosts:/ {
            in_hosts = 1
            if (!printed) {
                print new
                printed = 1
            }
            next
        }
        # 在 hosts 块内: 跳过列表项(- 开头)、注释行、空行
        # 遇到 lighthouse 内的下一个 key (^  非空格 开头) 则结束 hosts 块
        in_hosts == 1 {
            # 列表项(4+ 空格 + -)
            if ($0 ~ /^[[:space:]]+-/) next
            # 注释行(任意空格 + #)
            if ($0 ~ /^[[:space:]]*#/) next
            # 空行
            if ($0 ~ /^[[:space:]]*$/) next
            # 否则: 这是 lighthouse 内的下一个 key, 结束 hosts 块
            in_hosts = 0
        }
        { print }
    ' "$f" > "$tmp"
    mv "$tmp" "$f"
}

# 用环境变量替换模板中的简单标量字段
apply_env_overrides() {
    local f="$1"
    # 证书路径(模板默认 /etc/iflygo, 若 CONF_DIR 不同则同步)
    sed -i "s|/etc/iflygo|${CONF_DIR}|g" "$f"
    # 节点证书文件名(模板默认 host.crt/host.key, 替换为自定义主机名)
    sed -i "s|host\\.crt|${HOST_CRT}|g" "$f"
    sed -i "s|host\\.key|${HOST_KEY}|g" "$f"
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

    # 替换 static_host_map 段落(server 和 client 都需要)
    new_static_host_map=$(generate_static_host_map)
    replace_yaml_block "${TARGET_CONF}" "static_host_map" "${new_static_host_map}"

    # client 模式: 替换 lighthouse.hosts
    # server 模式: lighthouse.hosts 应为空, 模板已是注释状态, 不需修改
    if [ "${RUNMODE}" = "client" ]; then
        new_hosts=$(generate_lighthouse_hosts)
        replace_lighthouse_hosts "${TARGET_CONF}" "${new_hosts}"
    fi

    # 替换简单标量字段
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

echo "[iflygo-init] 初始化完成: RUNMODE=${RUNMODE} NODE=${NODE} CERT=${HOST_CRT} IFLYGO_IP=${IFLYGO_IP}"
