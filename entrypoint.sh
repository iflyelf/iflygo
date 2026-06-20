#!/bin/bash
# ==============================================================================
# iFlyGo 容器启动入口脚本
# 默认: 调用 init.sh 初始化配置, 然后启动 iflygo 主进程
# 子命令:
#   sign  - 在服务端为客户端签发证书 (调用 sign-client.sh)
#           用法: docker run --rm -v $(pwd)/data/server/config:/etc/iflygo \
#                   iflyelf/iflygo:latest sign -name c2 -ip 10.88.0.101 ...
# ==============================================================================
set -euo pipefail

# 子命令分发: 第一个参数为 sign 时, 走证书签发流程(不启动主进程)
if [ "${1:-}" = "sign" ]; then
    shift
    echo "[entrypoint] 进入证书签发模式..."
    exec /sign-client.sh "$@"
fi

# 执行初始化脚本
echo "[entrypoint] 开始初始化 iFlyGo 配置..."
/init.sh

# 配置文件路径
CONF_DIR="${IFLYGO_CONF_DIR:-/etc/iflygo}"
CONFIG_FILE="${CONF_DIR}/config.yml"

if [ ! -f "${CONFIG_FILE}" ]; then
    echo "[entrypoint][ERROR] 配置文件未生成: ${CONFIG_FILE}"
    echo "[entrypoint] 请检查 init.sh 是否正常执行或手动放置配置文件"
    exit 1
fi

# 启动参数(可通过 CMD 传入额外参数, 如 -v 提高日志等级)
IFLYGO_ARGS=(-config "${CONFIG_FILE}" "$@")

echo "[entrypoint] 启动 iFlyGo 主进程..."
echo "[entrypoint] 命令: iflygo ${IFLYGO_ARGS[*]}"
echo "[entrypoint] 配置: ${CONFIG_FILE}"
echo "[entrypoint] ========================================="

# 启动 iflygo 主进程 (exec 替换 sh, 确保信号正确传递给 iflygo, 由 tini 管理)
exec /usr/local/bin/iflygo "${IFLYGO_ARGS[@]}"
