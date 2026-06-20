#!/bin/bash
# ==============================================================================
# iFlyGo 客户端证书签发脚本
# 在服务端(已有 CA)上为客户端节点签发证书
#
# 用法:
#   docker run --rm -v $(pwd)/data/server/config:/etc/iflygo \
#     iflyelf/iflygo:latest sign \
#     -name client2 \
#     -ip 10.88.0.101 \
#     [-ip6 fd88::ffff:a58:101] \
#     [-groups laptop,home] \
#     [-subnets 10.8.1.0/24] \
#     [-duration 26280h]
#
# 也可用环境变量传参(与命令行等价, 命令行优先):
#   NODE / IFLYGO_IP / IFLYGO_IP_V6 / CERT_GROUPS / SUBNETS / CERT_DURATION
#
# 签发的证书输出到 ${IFLYGO_CONF_DIR}/<name>.crt / <name>.key
# ==============================================================================
set -euo pipefail

CONF_DIR="${IFLYGO_CONF_DIR:-/etc/iflygo}"

# 默认值(可被环境变量或命令行覆盖)
SIGN_NAME="${NODE:-}"
SIGN_IP="${IFLYGO_IP:-}"
SIGN_IP_V6="${IFLYGO_IP_V6:-}"
SIGN_NETMASK="${IFLYGO_NETMASK:-16}"
SIGN_NETMASK_V6="${IFLYGO_NETMASK_V6:-64}"
SIGN_GROUPS="${CERT_GROUPS:-}"
SIGN_SUBNETS="${SUBNETS:-}"
SIGN_DURATION="${CERT_DURATION:-}"
SIGN_OUT_DIR="${CONF_DIR}"

# 解析命令行参数(覆盖环境变量默认值)
while [ $# -gt 0 ]; do
    case "$1" in
        -name)       SIGN_NAME="$2"; shift 2 ;;
        -ip|-ipv4)   SIGN_IP="$2"; shift 2 ;;
        -ip6|-ipv6)  SIGN_IP_V6="$2"; shift 2 ;;
        -netmask)    SIGN_NETMASK="$2"; shift 2 ;;
        -netmask6)   SIGN_NETMASK_V6="$2"; shift 2 ;;
        -groups)     SIGN_GROUPS="$2"; shift 2 ;;
        -subnets)    SIGN_SUBNETS="$2"; shift 2 ;;
        -duration)   SIGN_DURATION="$2"; shift 2 ;;
        -out-dir)    SIGN_OUT_DIR="$2"; shift 2 ;;
        -h|--help)
            grep '^#' "$0" | sed 's/^# \?//'
            exit 0
            ;;
        *)
            echo "[sign][ERROR] 未知参数: $1" >&2
            echo "[sign] 使用 -h 查看帮助" >&2
            exit 1
            ;;
    esac
done

# 校验必填参数
if [ -z "${SIGN_NAME}" ]; then
    echo "[sign][ERROR] 缺少 -name (节点名称)" >&2
    exit 1
fi
if [ -z "${SIGN_IP}" ]; then
    echo "[sign][ERROR] 缺少 -ip (节点内网 IPv4 地址)" >&2
    exit 1
fi

# 校验 CA 是否存在
if [ ! -f "${CONF_DIR}/ca.crt" ] || [ ! -f "${CONF_DIR}/ca.key" ]; then
    echo "[sign][ERROR] 未找到 CA: ${CONF_DIR}/ca.crt 或 ca.key" >&2
    echo "[sign] 请先启动服务端生成 CA, 或挂载已有 CA 到 ${CONF_DIR}" >&2
    exit 1
fi

# 构建 -networks 参数 (IPv4 必需, IPv6 可选)
NETWORKS="${SIGN_IP}/${SIGN_NETMASK}"
if [ -n "${SIGN_IP_V6}" ]; then
    NETWORKS="${NETWORKS},${SIGN_IP_V6}/${SIGN_NETMASK_V6}"
fi

OUT_CRT="${SIGN_OUT_DIR}/${SIGN_NAME}.crt"
OUT_KEY="${SIGN_OUT_DIR}/${SIGN_NAME}.key"

# 防止误覆盖已存在的证书
if [ -f "${OUT_CRT}" ] || [ -f "${OUT_KEY}" ]; then
    echo "[sign][WARN] 证书已存在, 跳过签发: ${OUT_CRT}" >&2
    echo "[sign] 如需重签, 请先删除旧证书" >&2
    exit 0
fi

echo "[sign] 签发客户端证书:"
echo "[sign]   节点名称: ${SIGN_NAME}"
echo "[sign]   网络地址: ${NETWORKS}"
[ -n "${SIGN_GROUPS}" ]  && echo "[sign]   分组: ${SIGN_GROUPS}"
[ -n "${SIGN_SUBNETS}" ] && echo "[sign]   子网路由: ${SIGN_SUBNETS}"
[ -n "${SIGN_DURATION}" ] && echo "[sign]   有效期: ${SIGN_DURATION}"

# 构建签发参数数组
SIGN_ARGS=(-name "${SIGN_NAME}" -networks "${NETWORKS}")
[ -n "${SIGN_DURATION}" ] && SIGN_ARGS+=(-duration "${SIGN_DURATION}")
[ -n "${SIGN_GROUPS}" ]   && SIGN_ARGS+=(-groups "${SIGN_GROUPS}")
[ -n "${SIGN_SUBNETS}" ]  && SIGN_ARGS+=(-subnets "${SIGN_SUBNETS}")

# 执行签发(在 CONF_DIR 内, 使用本地 ca.crt/ca.key)
( cd "${CONF_DIR}" && \
  iflygo-cert sign "${SIGN_ARGS[@]}" -out-crt "${OUT_CRT}" -out-key "${OUT_KEY}" )

# 复制 CA 证书到输出目录, 方便一起分发给客户端(客户端需要 ca.crt)
if [ "${SIGN_OUT_DIR}" != "${CONF_DIR}" ]; then
    cp "${CONF_DIR}/ca.crt" "${SIGN_OUT_DIR}/ca.crt"
fi

chmod 600 "${OUT_KEY}" 2>/dev/null || true
chmod 644 "${OUT_CRT}" 2>/dev/null || true

echo "[sign] ✅ 签发完成:"
echo "[sign]   证书: ${OUT_CRT}"
echo "[sign]   私钥: ${OUT_KEY}"
echo "[sign]   CA  : ${CONF_DIR}/ca.crt (分发给客户端时需一并提供)"
echo "[sign]"
echo "[sign] 将以下 3 个文件复制到客户端的 /etc/iflygo/ 目录:"
echo "[sign]   - ca.crt"
echo "[sign]   - ${SIGN_NAME}.crt  (重命名为客户端的 CERT_HOSTNAME.crt)"
echo "[sign]   - ${SIGN_NAME}.key  (重命名为客户端的 CERT_HOSTNAME.key)"
