#############################################################################
#  iFlyGo 多阶段构建 (基于 slackhq/nebula, 品牌化为 iflygo)
#  - builder(编译阶段) = iflyelf/ubuntu:latest
#      已预装 Go / build-essential / git 等完整工具链, 无需再装 Go 与
#      编译依赖, 直接交叉编译 iflygo / iflygo-cert 静态二进制。
#  - runtime(运行阶段) = iflyelf/ubuntu:lite
#      iflygo 为静态二进制, 仅拷贝产物 + 按需装网络工具(iptables 等),
#      镜像更小。
#############################################################################

# ##############################################################################
# ***** iFlyGo 全局构建变量 *****

# 上游源码仓库(用于源码构建)
ARG IFLYGO_UPSTREAM_REPO=https://github.com/slackhq/nebula.git
# 上游版本(由 update-version 工作流自动更新; 也可 --build-arg 覆盖)
ARG IFLYGO_UPSTREAM_VERSION=v1.11.2
# 项目品牌(替换文本标识时使用)
ARG IFLYGO_BRAND=iflygo
# 工作目录(运行时)
ARG IFLYGO_DIR=/data/iflygo


####################################
#  阶段一: 构建 iFlyGo 二进制(Go)    #
####################################
FROM iflyelf/ubuntu:latest AS builder

LABEL org.opencontainers.image.authors="iflyelf" \
      org.opencontainers.image.vendor="iflyelf" \
      org.opencontainers.image.title="iflygo" \
      org.opencontainers.image.description="iFlyGo overlay 安全网络隧道"

# 时区/语言
ARG TZ=Asia/Shanghai
ENV TZ=$TZ
ARG LANG=zh_CN.UTF-8
ENV LANG=$LANG
ARG DEBIAN_FRONTEND=noninteractive
ENV DEBIAN_FRONTEND=$DEBIAN_FRONTEND

# 继承全局构建变量
ARG IFLYGO_UPSTREAM_REPO
ARG IFLYGO_UPSTREAM_VERSION
ARG IFLYGO_BRAND
ENV IFLYGO_UPSTREAM_REPO=$IFLYGO_UPSTREAM_REPO \
    IFLYGO_UPSTREAM_VERSION=$IFLYGO_UPSTREAM_VERSION \
    IFLYGO_BRAND=$IFLYGO_BRAND

# Go 交叉编译环境(Go 与工具链已由 iflyelf/ubuntu:latest 预装)
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY
# buildx 自动注入的目标平台, 用于交叉编译
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

# ##############################################################################
# ***** 拉取上游源码 *****
WORKDIR /src
RUN set -eux && \
    git clone --depth=1 -b "${IFLYGO_UPSTREAM_VERSION}" \
        "${IFLYGO_UPSTREAM_REPO}" /src/upstream && \
    cd /src/upstream && git rev-parse HEAD > /src/COMMIT

# ##############################################################################
# ***** 品牌化: 把源码中的用户可见标识替换为 iFlyGo *****
# 注意:
#   - 不动 Go import path 及其包名(否则编译失败)
#   - 仅替换字符串字面量、默认值、用户可见输出
WORKDIR /src/upstream
RUN set -eux && \
    BRAND="${IFLYGO_BRAND}" && \
    BRAND_TITLE="iFlyGo" && \
    # 1) 默认 tun 接口名 -> iflygo
    grep -rln --include="*.go" --exclude-dir=vendor 'nebula1' . | while read -r f; do \
        sed -i 's|nebula1|'"${BRAND}"'|g' "$f"; \
    done; \
    # 2) 默认配置/PKI/工作目录: /etc/nebula -> /etc/iflygo, /var/log/nebula -> /var/log/iflygo
    grep -rln --include="*.go" --exclude-dir=vendor -e '/etc/nebula' -e '/var/log/nebula' . | while read -r f; do \
        sed -i \
            -e 's|/etc/nebula|/etc/'"${BRAND}"'|g' \
            -e 's|/var/log/nebula|/var/log/'"${BRAND}"'|g' "$f"; \
    done; \
    # 3) CLI/banner/help 文本中的旧品牌词形替换为新品牌
    grep -rln --include="*.go" --exclude-dir=vendor \
        -e '"Nebula' -e '"nebula version' -e 'nebula -config' -e 'Usage of nebula' . | while read -r f; do \
        sed -i \
            -e 's|"Nebula|"'"${BRAND_TITLE}"'|g' \
            -e 's|"nebula version|"'"${BRAND}"' version|g' \
            -e 's|nebula -config|'"${BRAND}"' -config|g' \
            -e 's|Usage of nebula|Usage of '"${BRAND}"'|g' "$f"; \
    done; \
    # 4) 命令入口中固定的 program name
    if [ -f cmd/nebula/main.go ]; then \
        sed -i 's|"nebula"|"'"${BRAND}"'"|g' cmd/nebula/main.go; \
    fi; \
    if [ -f cmd/nebula-cert/main.go ]; then \
        sed -i 's|"nebula-cert"|"'"${BRAND}"'-cert"|g' cmd/nebula-cert/main.go; \
    fi; \
    # 5) 示例配置: 默认 dev/路径标识改为 iflygo
    if [ -f examples/config.yml ]; then \
        sed -i \
            -e 's|/etc/nebula|/etc/'"${BRAND}"'|g' \
            -e 's|dev: nebula1|dev: '"${BRAND}"'|g' \
            examples/config.yml; \
    fi; \
    # 6) 在二进制对外的提示前缀(如 sshd banner)中替换旧品牌字样
    grep -rln --include="*.go" --exclude-dir=vendor 'sshd.*nebula' . | while read -r f; do \
        sed -i 's|"nebula>|"'"${BRAND}"'>|g; s|"nebula"|"'"${BRAND}"'"|g' "$f"; \
    done; \
    true

# ##############################################################################
# ***** 编译 iflygo / iflygo-cert (纯 Go, 静态链接) *****
# 从 go.mod 读取 Go 版本并用 GOTOOLCHAIN 精确锁定, 避免基础镜像 Go 版本
# 过高导致的编译不兼容; major.minor(如 1.22) 需补 .0 才是有效工具链版本。
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/opt/golang/pkg/mod \
    set -eux && \
    mkdir -p /out && \
    cd /src/upstream && \
    GOVER=$(grep -oP '^go \K[0-9]+\.[0-9]+(\.[0-9]+)?' go.mod | head -1) && \
    case "$GOVER" in *.*.*) GOTOOLCHAIN=go${GOVER} ;; *.*) GOTOOLCHAIN=go${GOVER}.0 ;; esac && \
    export GOTOOLCHAIN && \
    echo "iflygo(nebula) 要求 Go ${GOVER}, 锁定 GOTOOLCHAIN=${GOTOOLCHAIN}" && \
    go version && \
    BUILD_NUMBER=$(cat /src/COMMIT) && \
    LDFLAGS="-w -s -X main.Build=${BUILD_NUMBER}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
        go build -trimpath -ldflags "${LDFLAGS}" -o /out/iflygo      ./cmd/nebula && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
        go build -trimpath -ldflags "${LDFLAGS}" -o /out/iflygo-cert ./cmd/nebula-cert && \
    # 校验产物存在(不加 || true, 编译失败必须让构建失败)
    test -x /out/iflygo && test -x /out/iflygo-cert && \
    ls -lh /out


##########################################
#         阶段二: 构建运行时镜像           #
##########################################
FROM iflyelf/ubuntu:lite

LABEL org.opencontainers.image.authors="iflyelf" \
      org.opencontainers.image.vendor="iflyelf" \
      org.opencontainers.image.title="iflygo" \
      org.opencontainers.image.description="iFlyGo overlay 安全网络隧道, runtime on ubuntu:lite"

ARG TARGETARCH
ARG TARGETVARIANT

# 时区设置
ARG TZ=Asia/Shanghai
ENV TZ=$TZ
# 语言设置
ARG LANG=zh_CN.UTF-8
ENV LANG=$LANG
ARG DEBIAN_FRONTEND=noninteractive
ENV DEBIAN_FRONTEND=$DEBIAN_FRONTEND

# 镜像变量
ARG DOCKER_IMAGE=iflyelf/iflygo
ENV DOCKER_IMAGE=$DOCKER_IMAGE

# iFlyGo 运行时变量
ARG IFLYGO_DIR=/data/iflygo
ENV IFLYGO_DIR=$IFLYGO_DIR
# 配置目录(server/client 都用此目录持久化证书与 config.yml)
ENV IFLYGO_CONF_DIR=/etc/iflygo
# 日志目录
ENV IFLYGO_LOG_DIR=/var/log/iflygo

# ***** 运行阶段按需依赖 *****
# iflygo 为静态二进制, 无动态库依赖。ubuntu:lite 已含 zsh/bash/iproute2/nftables/
#   ipset/iputils-ping/telnet/tcpdump/procps/psmisc/sysstat/lsof/htop/jq/git/vim/
#   curl/wget/axel/zip/unzip/tar/tini/tzdata/ca-certificates/locales/bind9-dnsutils,
#   此处仅补装 overlay 隧道/路由排障相关的网络工具:
#     iptables/conntrack -> NAT 与连接跟踪
#     net-tools          -> ifconfig/route 等传统工具
#     ncat               -> 端口连通性测试
ARG RUNTIME_DEPS="\
    iptables \
    conntrack \
    net-tools \
    ncat"
ENV RUNTIME_DEPS=$RUNTIME_DEPS

# ***** 安装运行时依赖 *****
RUN set -eux && \
   DEBIAN_FRONTEND=noninteractive apt-get update -qqy && apt-get upgrade -qqy && \
   DEBIAN_FRONTEND=noninteractive apt-get install -qqy --no-install-recommends $RUNTIME_DEPS \
       --option=Dpkg::Options::=--force-confdef && \
   # 验证依赖包是否真正安装成功(逐个检查 dpkg 状态, 缺失则构建失败)
   for pkg in $RUNTIME_DEPS; do \
       if ! dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q "install ok installed"; then \
           echo "ERROR: 依赖包未成功安装: $pkg" >&2 && exit 1; \
       fi; \
   done && \
   echo "运行依赖验证通过" && \
   DEBIAN_FRONTEND=noninteractive apt-get -qqy autoremove --purge && \
   DEBIAN_FRONTEND=noninteractive apt-get -qqy autoclean && \
   rm -rf /var/lib/apt/lists/* /var/cache/apt/* /tmp/* && \
   ln -sf /usr/share/zoneinfo/${TZ} /etc/localtime && echo ${TZ} > /etc/timezone

# ***** 拷贝构建产物(iflygo / iflygo-cert) *****
COPY --from=builder /out/iflygo      /usr/local/bin/iflygo
COPY --from=builder /out/iflygo-cert /usr/local/bin/iflygo-cert
RUN set -eux && \
    chmod +x /usr/local/bin/iflygo /usr/local/bin/iflygo-cert && \
    ln -sf /usr/local/bin/iflygo      /usr/bin/iflygo && \
    ln -sf /usr/local/bin/iflygo-cert /usr/bin/iflygo-cert && \
    /usr/local/bin/iflygo -version || true

# ***** 拷贝默认配置模板(server/client) 与启动脚本 *****
# 模板放在 /opt/iflygo/templates/ (不会被用户挂载 /etc/iflygo 覆盖)
COPY conf/server/config.yml /opt/iflygo/templates/server.yml
COPY conf/client/config.yml /opt/iflygo/templates/client.yml
COPY init.sh        /init.sh
COPY entrypoint.sh  /entrypoint.sh
COPY sign-client.sh /sign-client.sh
RUN set -eux && \
    chmod +x /init.sh /entrypoint.sh /sign-client.sh && \
    mkdir -p ${IFLYGO_CONF_DIR}/hosts ${IFLYGO_LOG_DIR} ${IFLYGO_DIR}

# ***** TUN 设备 (运行时由 docker 注入 /dev/net/tun) *****

# 默认监听端口(UDP 6688, 可由 LISTEN_PORT 覆盖)
EXPOSE 6688/udp

# ***** 工作目录 *****
WORKDIR /etc/iflygo

# ***** 容器信号处理 *****
STOPSIGNAL SIGTERM

# ***** 健康检查: 检查 iflygo 进程是否存活 *****
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD pgrep -x iflygo >/dev/null || exit 1

# ***** 入口 *****
ENTRYPOINT ["/usr/bin/tini", "--", "/entrypoint.sh"]
