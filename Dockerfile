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
        -X github.com/iflyelf/iflygo/internal/version.Build=${COMMIT}" && \
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

# 控制面 / edge / lighthouse
EXPOSE 8080/tcp
EXPOSE 443/tcp
EXPOSE 443/udp
EXPOSE 6688/udp

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/iflygo-server"]
