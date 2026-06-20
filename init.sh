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
LIGHTHOUSE_IP="${LIGHTHOUSE_IP:-10.88.0.1}"                   # lighthouse 的内网 IPv4
LIGHTHOUSE_IP_V6="${LIGHTHOUSE_IP_V6:-fd88::ffff:a58:1}"      # lighthouse 的内网 IPv6(可选, 留空则不生成)
LIGHTHOUSE_PUBLIC="${LIGHTHOUSE_PUBLIC:-127.0.0.1:6688}"      # lighthouse 的公网地址 host:port

LISTEN_HOST="${LISTEN_HOST:-[::]}"      # 监听地址
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

# 网络偏好范围: 逗号分隔的 CIDR 列表, 用于加速发现网络相邻节点
# 默认: 10.88.0.0/16 (IPv4) + fd88::/64 (IPv6)
PREFERRED_RANGES="${PREFERRED_RANGES:-10.88.0.0/16,fd88::/64}"

# 不安全路由: 用分号分隔多条路由, 每条路由格式如下:
#   单网关: route=<CIDR>,via=<GW>[,mtu=<MTU>][,metric=<N>]
#   多网关: route=<CIDR>,via=<GW1>:<W1>|<GW2>:<W2>[,mtu=<MTU>]
# 示例:
#   单网关: UNSAFE_ROUTES="route=10.8.1.0/24,via=10.88.1.1,mtu=1300,metric=100"
#   多网关: UNSAFE_ROUTES="route=10.0.9.0/24,via=10.88.2.1:10|10.88.2.2:5"
#   多条路由: UNSAFE_ROUTES="route=10.8.1.0/24,via=10.88.1.1;route=10.0.9.0/24,via=10.88.2.1:10|10.88.2.2:5"
# 留空则使用模板默认值(server 模板保留示例, client 为空)
UNSAFE_ROUTES="${UNSAFE_ROUTES:-}"
CERT_DURATION="${CERT_DURATION:-}"
AUTO_GEN_CA="${AUTO_GEN_CA:-true}"      # 当 ca 不存在时是否自动生成

# ===== 网络核心配置 =====
CIPHER="${CIPHER:-chachapoly}"          # 加密算法: chachapoly(推荐) / aes (所有节点必须一致)

# 中继配置 (relay)
RELAYS="${RELAYS:-}"                    # 中继服务器列表(逗号分隔内网IP), 留空则用模板默认
AM_RELAY="${AM_RELAY:-}"                # 是否作为中继节点(true/false), 留空则用模板默认(server=true,client=false)
USE_RELAYS="${USE_RELAYS:-}"            # 是否使用中继连接(true/false), 留空则用模板默认(server=false,client=true)

# Lighthouse 配置
LIGHTHOUSE_INTERVAL="${LIGHTHOUSE_INTERVAL:-3}"  # 向 lighthouse 报告间隔(秒)

# TUN 设备高级配置
TUN_DISABLED="${TUN_DISABLED:-false}"           # 是否禁用 TUN 设备
DROP_LOCAL_BROADCAST="${DROP_LOCAL_BROADCAST:-true}"  # 是否转发本地广播
DROP_MULTICAST="${DROP_MULTICAST:-true}"              # 是否转发组播
TX_QUEUE="${TX_QUEUE:-1500}"                          # 传输队列长度

# Listen 高级配置
READ_BUFFER="${READ_BUFFER:-20000000}"          # UDP 读缓冲区大小(字节)
WRITE_BUFFER="${WRITE_BUFFER:-20000000}"        # UDP 写缓冲区大小(字节)
SEND_RECV_ERROR="${SEND_RECV_ERROR:-always}"    # recv_error 数据包: always/never/private

# Punchy (NAT 打洞) 配置
PUNCH="${PUNCH:-true}"                  # 是否持续打洞
PUNCH_RESPOND="${PUNCH_RESPOND:-true}"  # 响应模式(对称NAT穿透)
PUNCH_DELAY="${PUNCH_DELAY:-1s}"        # 打洞响应延迟

# Handshakes 配置
HANDSHAKE_TRY_INTERVAL="${HANDSHAKE_TRY_INTERVAL:-100ms}"  # 握手重试间隔
HANDSHAKE_RETRIES="${HANDSHAKE_RETRIES:-10}"              # 握手超时次数
HANDSHAKE_TRIGGER_BUFFER="${HANDSHAKE_TRIGGER_BUFFER:-64}" # 握手缓冲通道大小

# Static_map 配置
STATIC_MAP_CADENCE="${STATIC_MAP_CADENCE:-30s}"          # DNS 缓存时间
STATIC_MAP_NETWORK="${STATIC_MAP_NETWORK:-ip}"           # 网络地址类型: ip4/ip6/ip
STATIC_MAP_LOOKUP_TIMEOUT="${STATIC_MAP_LOOKUP_TIMEOUT:-250ms}"  # DNS 查询超时

# PKI 配置
PKI_INITIATING_VERSION="${PKI_INITIATING_VERSION:-2}"    # 证书版本: 1/2 (推荐2)

# 配置重生策略
# false (默认): 已存在 config.yml 时跳过生成, 保留用户手动修改
# true        : 强制根据当前环境变量重新生成 config.yml (会备份旧配置为 config.yml.bak)
FORCE_REGEN="${FORCE_REGEN:-false}"

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
    LIGHTHOUSE_IPS_V6=("$LIGHTHOUSE_IP_V6")
    echo "[iflygo-init] 使用单 lighthouse 模式: ${LIGHTHOUSE_IP} -> ${LIGHTHOUSE_PUBLIC}"
    [ -n "${LIGHTHOUSE_IP_V6}" ] && echo "  (IPv6: ${LIGHTHOUSE_IP_V6})"
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

# 替换 tun 块中的 unsafe_routes 子字段(保留 tun 块的其他字段)
replace_tun_unsafe_routes() {
    local f="$1" new_routes="$2"
    local tmp
    tmp=$(mktemp)
    awk -v new="$new_routes" '
        BEGIN { in_tun = 0; in_unsafe = 0; printed = 0 }
        /^tun:/ { in_tun = 1; print; next }
        # 匹配 tun 内的 unsafe_routes: 字段(2 空格缩进)
        in_tun == 1 && /^  unsafe_routes:/ {
            in_unsafe = 1
            if (!printed) {
                print new
                printed = 1
            }
            next
        }
        # 在 unsafe_routes 块内
        in_unsafe == 1 {
            # 遇到顶级 key (无缩进) -> unsafe 和 tun 都结束, 打印此行
            if (/^[a-zA-Z_]/) { in_unsafe = 0; in_tun = 0; print; next }
            # 遇到 tun 内的下一个 key (2 空格缩进 + 字母) -> unsafe 结束, 打印此行
            if (/^  [a-zA-Z_]/) { in_unsafe = 0; print; next }
            # 其余(列表项 / 注释 / 空行)跳过
            next
        }
        # tun 块结束(遇到下一个顶级 key, 且不在 unsafe 块内)
        in_tun == 1 && /^[a-zA-Z_]/ { in_tun = 0 }
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

# 替换 relay 块中的 relays/am_relay/use_relays 字段
replace_relay_block() {
    local f="$1"
    local new_relays="$2"    # 生成的 relays 列表 YAML
    local am_relay="$3"      # true/false/空
    local use_relays="$4"    # true/false/空
    
    local tmp
    tmp=$(mktemp)
    awk -v new_relays="$new_relays" -v am_relay="$am_relay" -v use_relays="$use_relays" '
        BEGIN { in_relay = 0; in_relays_list = 0; printed_relays = 0 }
        /^relay:/ { in_relay = 1; print; next }
        # 匹配 relay 内的 relays: 字段
        in_relay == 1 && /^  relays:/ {
            in_relays_list = 1
            if (!printed_relays && new_relays != "") {
                print new_relays
                printed_relays = 1
            } else {
                print  # 保留原 relays:
            }
            next
        }
        # 在 relays 列表内: 跳过列表项/注释/空行, 遇到 relay 内下一个 key 结束
        in_relays_list == 1 {
            if (/^  [a-zA-Z_]/) { in_relays_list = 0; }
            else { next }
        }
        # 替换 am_relay 字段(如果提供了值)
        in_relay == 1 && /^  am_relay:/ {
            if (am_relay != "") {
                print "  am_relay: " am_relay
            } else {
                print
            }
            next
        }
        # 替换 use_relays 字段(如果提供了值)
        in_relay == 1 && /^  use_relays:/ {
            if (use_relays != "") {
                print "  use_relays: " use_relays
            } else {
                print
            }
            next
        }
        # relay 块结束(遇到顶级 key)
        in_relay == 1 && /^[a-zA-Z_]/ { in_relay = 0 }
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
    # PKI 证书版本
    sed -i "s|^  initiating_version: 2$|  initiating_version: ${PKI_INITIATING_VERSION}|" "$f"
    # static_map 配置
    sed -i "s|^  cadence: 30s$|  cadence: ${STATIC_MAP_CADENCE}|" "$f"
    sed -i "s|^  network: ip$|  network: ${STATIC_MAP_NETWORK}|" "$f"
    sed -i "s|^  lookup_timeout: 250ms$|  lookup_timeout: ${STATIC_MAP_LOOKUP_TIMEOUT}|" "$f"
    # lighthouse 报告间隔
    sed -i "s|^  interval: 3$|  interval: ${LIGHTHOUSE_INTERVAL}|" "$f"
    # 监听地址与端口(server 模板默认 6688, client 模板默认 0)
    sed -i "s|^  host: \"\\[::\\]\"$|  host: \"${LISTEN_HOST}\"|" "$f"
    sed -i "s|^  port: 6688$|  port: ${LISTEN_PORT}|" "$f"
    sed -i "s|^  port: 0$|  port: ${LISTEN_PORT}|" "$f"
    # listen 缓冲区与 recv_error
    sed -i "s|^  read_buffer: 20000000$|  read_buffer: ${READ_BUFFER}|" "$f"
    sed -i "s|^  write_buffer: 20000000$|  write_buffer: ${WRITE_BUFFER}|" "$f"
    sed -i "s|^  send_recv_error: always$|  send_recv_error: ${SEND_RECV_ERROR}|" "$f"
    # punchy (NAT 打洞)
    sed -i "s|^  punch: true$|  punch: ${PUNCH}|" "$f"
    sed -i "s|^  respond: true$|  respond: ${PUNCH_RESPOND}|" "$f"
    sed -i "s|^  delay: 1s$|  delay: ${PUNCH_DELAY}|" "$f"
    # 加密算法
    sed -i "s|^cipher: chachapoly$|cipher: ${CIPHER}|" "$f"
    # TUN 设备配置
    sed -i "s|^  disabled: false$|  disabled: ${TUN_DISABLED}|" "$f"
    sed -i "s|^  dev: iflygo$|  dev: ${TUN_DEV}|" "$f"
    sed -i "s|^  drop_local_broadcast: true$|  drop_local_broadcast: ${DROP_LOCAL_BROADCAST}|" "$f"
    sed -i "s|^  drop_multicast: true$|  drop_multicast: ${DROP_MULTICAST}|" "$f"
    sed -i "s|^  tx_queue: 1500$|  tx_queue: ${TX_QUEUE}|" "$f"
    sed -i "s|^  mtu: 1300$|  mtu: ${TUN_MTU}|" "$f"
    # 日志级别与格式
    sed -i "s|^  level: info$|  level: ${LOG_LEVEL}|" "$f"
    sed -i "s|^  format: text$|  format: ${LOG_FORMAT}|" "$f"
    # handshakes 配置
    sed -i "s|^  try_interval: 100ms$|  try_interval: ${HANDSHAKE_TRY_INTERVAL}|" "$f"
    sed -i "s|^  retries: 10$|  retries: ${HANDSHAKE_RETRIES}|" "$f"
    sed -i "s|^  trigger_buffer: 64$|  trigger_buffer: ${HANDSHAKE_TRIGGER_BUFFER}|" "$f"
}

# 生成 preferred_ranges YAML (从环境变量 PREFERRED_RANGES)
generate_preferred_ranges() {
    # 输入: 逗号分隔 CIDR (如 10.88.0.0/16,fd88::/64)
    # 输出: preferred_ranges: ["10.88.0.0/16", "fd88::/64"]
    if [ -z "${PREFERRED_RANGES}" ]; then
        echo 'preferred_ranges: []'
        return
    fi
    # 将逗号替换为 ", " 并加上 ["..."]
    local ranges_json=$(echo "${PREFERRED_RANGES}" | sed 's/,/", "/g')
    echo "preferred_ranges: [\"${ranges_json}\"]"
}

# 生成 relay.relays 列表 YAML (从环境变量 RELAYS)
generate_relays() {
    # 输入: 逗号分隔内网 IP (如 10.88.0.1,10.88.0.2)
    # 输出:
    #   relays:
    #     - 10.88.0.1
    #     - 10.88.0.2
    if [ -z "${RELAYS}" ]; then
        echo "  relays: []"
        return 0
    fi
    echo "  relays:"
    IFS=',' read -ra relay_ips <<< "${RELAYS}"
    for ip in "${relay_ips[@]}"; do
        # 去除首尾空格
        ip="$(echo "$ip" | xargs)"
        [ -n "$ip" ] && echo "    - ${ip}"
    done
    return 0
}

# 生成 unsafe_routes YAML (从环境变量 UNSAFE_ROUTES)
generate_unsafe_routes() {
    # 输入格式: 分号分隔多条路由
    #   单网关: route=<CIDR>,via=<GW>[,mtu=<N>][,metric=<N>]
    #   多网关: route=<CIDR>,via=<GW1>:<W1>|<GW2>:<W2>[,mtu=<N>]
    # 输出: YAML unsafe_routes 列表 (缩进 2 空格)
    if [ -z "${UNSAFE_ROUTES}" ]; then
        echo "  unsafe_routes: []"
        return
    fi
    
    echo "  unsafe_routes:"
    # 用分号分隔路由条目
    IFS=';' read -ra routes <<< "${UNSAFE_ROUTES}"
    for route_str in "${routes[@]}"; do
        # 解析 route_str: 拆分 key=value 对
        local route="" via="" mtu="" metric=""
        IFS=',' read -ra kv_pairs <<< "${route_str}"
        for kv in "${kv_pairs[@]}"; do
            key="${kv%%=*}"
            val="${kv#*=}"
            case "$key" in
                route)  route="$val" ;;
                via)    via="$val" ;;
                mtu)    mtu="$val" ;;
                metric) metric="$val" ;;
            esac
        done
        
        # 输出 YAML (缩进 4 空格)
        echo "    - route: ${route}"
        
        # via 可能是单网关(10.88.1.1) 或多网关(10.88.2.1:10|10.88.2.2:5)
        if [[ "$via" =~ \| ]]; then
            # 多网关 ECMP
            echo "      via:"
            IFS='|' read -ra gateways <<< "${via}"
            for gw_entry in "${gateways[@]}"; do
                if [[ "$gw_entry" =~ : ]]; then
                    gw="${gw_entry%%:*}"
                    weight="${gw_entry#*:}"
                    echo "        - gateway: ${gw}"
                    echo "          weight: ${weight}"
                else
                    echo "        - gateway: ${gw_entry}"
                fi
            done
        else
            # 单网关
            echo "      via: ${via}"
        fi
        
        [ -n "$mtu" ] && echo "      mtu: ${mtu}" || true
        [ -n "$metric" ] && echo "      metric: ${metric}" || true
    done
    return 0
}

# ------------------------------------------------------------------------------
# 5. 配置生成主逻辑
# ------------------------------------------------------------------------------

# 强制重新生成配置(若 FORCE_REGEN=true)
if [ "${FORCE_REGEN}" = "true" ] && [ -f "${TARGET_CONF}" ]; then
    echo "[iflygo-init] FORCE_REGEN=true, 备份现有配置并强制重新生成"
    mv "${TARGET_CONF}" "${TARGET_CONF}.bak.$(date +%Y%m%d_%H%M%S)"
fi

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

    # 替换 preferred_ranges (顶级字段, 始终用环境变量值)
    new_preferred_ranges=$(generate_preferred_ranges)
    replace_yaml_block "${TARGET_CONF}" "preferred_ranges" "${new_preferred_ranges}"

    # 替换 tun.unsafe_routes (仅当 UNSAFE_ROUTES 环境变量非空时覆盖模板)
    if [ -n "${UNSAFE_ROUTES}" ]; then
        echo "[iflygo-init] 应用自定义 unsafe_routes"
        new_unsafe_routes=$(generate_unsafe_routes)
        replace_tun_unsafe_routes "${TARGET_CONF}" "${new_unsafe_routes}"
    fi

    # 替换 relay 块(relays 列表 / am_relay / use_relays)
    # 仅当至少一个 relay 相关变量非空时才覆盖模板
    if [ -n "${RELAYS}" ] || [ -n "${AM_RELAY}" ] || [ -n "${USE_RELAYS}" ]; then
        echo "[iflygo-init] 应用自定义 relay 配置"
        if [ -n "${RELAYS}" ]; then
            new_relays=$(generate_relays)
        else
            new_relays=""  # 不覆盖 relays 列表, 仅改 am_relay/use_relays
        fi
        replace_relay_block "${TARGET_CONF}" "${new_relays}" "${AM_RELAY}" "${USE_RELAYS}"
    fi

    # 替换简单标量字段
    apply_env_overrides "${TARGET_CONF}"
else
    echo "[iflygo-init] 已存在配置, 跳过生成: ${TARGET_CONF}"
    echo "[iflygo-init] 如需用新环境变量重新生成, 请设置 FORCE_REGEN=true"
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
