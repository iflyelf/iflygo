# syntax=docker/dockerfile:1
# ==============================================================================
#  iFlyGo 多阶段构建
#    builder = iflyelf/ubuntu:latest（已预装 Go / Node / Python 等完整工具链）
#    runtime = iflyelf/ubuntu:lite（仅拷贝编译产物，镜像更小）
#  产物：iflygo-server / iflygo-client（CGO_ENABLED=0 静态二进制）
# ==============================================================================

FROM iflyelf/ubuntu:latest AS builder

ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY . .

ARG VERSION=0.1.0
ARG COMMIT=dev

RUN set -eux && \
    GOVER=$(grep -oP '^go \K[0-9]+\.[0-9]+(\.[0-9]+)?' go.mod | head -1) && \
    case "$GOVER" in *.*.*) GT=go${GOVER} ;; *.*) GT=go${GOVER}.0 ;; esac && \
    export GOTOOLCHAIN=$GT && \
    go version && \
    python3 scripts/patch_grdp.py third_party/grdp && \
    python3 scripts/inject_tunnel_frontend.py && \
    node --check web/dist/assets/app.js && \
    mkdir -p /out && \
    LDFLAGS="-s -w \
        -X github.com/iflyelf/iflygo/internal/version.Version=${VERSION} \
        -X github.com/iflyelf/iflygo/internal/version.Build=${COMMIT} \
        -X github.com/iflyelf/iflygo/internal/version.Commit=${COMMIT}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
        go build -trimpath -ldflags "${LDFLAGS}" -o /out/iflygo-server ./cmd/server && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
        go build -trimpath -ldflags "${LDFLAGS}" -o /out/iflygo-client ./cmd/client && \
    ls -lh /out

# ==============================================================================
#  运行阶段
# ==============================================================================
FROM iflyelf/ubuntu:lite

ARG TZ=Asia/Shanghai
ENV TZ=$TZ

# ubuntu:lite 已含 tini/tzdata/ca-certificates/iproute2/nftables/curl 等，
# 这里只补装 overlay 隧道 / 路由 / NAT 排障所需的网络工具。
RUN set -eux && \
    DEBIAN_FRONTEND=noninteractive apt-get update -qqy && \
    DEBIAN_FRONTEND=noninteractive apt-get install -qqy --no-install-recommends \
        iptables conntrack net-tools ncat && \
    rm -rf /var/lib/apt/lists/* && \
    ln -sf /usr/share/zoneinfo/${TZ} /etc/localtime && echo ${TZ} > /etc/timezone

COPY --from=builder /out/iflygo-server /usr/local/bin/iflygo-server
COPY --from=builder /out/iflygo-client /usr/local/bin/iflygo-client

# 控制面 / TCP 中继 / edge / lighthouse
# 8090 控制面 Web/API（默认 server.port）
# 8091 TCP 中继（默认控制面端口 +1）
# 443  edge HTTPS / Nebula（edge 与 443/UDP 按角色使用）
# 6688 UDP lighthouse/relay
# 80   可选：ACME HTTP-01
EXPOSE 8090/tcp
EXPOSE 8091/tcp
EXPOSE 80/tcp
EXPOSE 443/tcp
EXPOSE 443/udp
EXPOSE 6688/udp

# 健康检查由部署方按角色设置：
#   控制面：--health-cmd 'curl -fsS http://127.0.0.1:8090/health'
#   数据面：不启动 8090，建议使用进程/端口检查或直接依赖容器重启策略。

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/iflygo-server"]
