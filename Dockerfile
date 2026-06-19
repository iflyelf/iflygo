#############################
#     设置公共的变量          #
#############################
ARG BASE_IMAGE_TAG=resolute
FROM ubuntu:${BASE_IMAGE_TAG} AS base

# 作者描述信息
LABEL org.opencontainers.image.authors="iflyelf" \
      org.opencontainers.image.vendor="iflyelf" \
      org.opencontainers.image.title="iflygo" \
      org.opencontainers.image.description="iFlyGo overlay 安全网络隧道"

# 时区设置
ARG TZ=Asia/Shanghai
ENV TZ=$TZ
# 语言设置
ARG LANG=zh_CN.UTF-8
ENV LANG=$LANG

# 镜像变量
ARG DOCKER_IMAGE=iflyelf/iflygo
ENV DOCKER_IMAGE=$DOCKER_IMAGE
ARG DOCKER_IMAGE_OS=ubuntu
ENV DOCKER_IMAGE_OS=$DOCKER_IMAGE_OS
ARG DOCKER_IMAGE_TAG=resolute
ENV DOCKER_IMAGE_TAG=$DOCKER_IMAGE_TAG

# 环境设置
ARG DEBIAN_FRONTEND=noninteractive
ENV DEBIAN_FRONTEND=$DEBIAN_FRONTEND

# ##############################################################################
# ***** 设置 iFlyGo 构建变量 *****

# 工作目录(运行时)
ARG IFLYGO_DIR=/data/iflygo
ENV IFLYGO_DIR=$IFLYGO_DIR

# 上游源码仓库(用于源码构建)
ARG IFLYGO_UPSTREAM_REPO=https://github.com/slackhq/nebula.git
ENV IFLYGO_UPSTREAM_REPO=$IFLYGO_UPSTREAM_REPO
# 上游版本(可在构建时通过 --build-arg IFLYGO_UPSTREAM_VERSION=vX.Y.Z 覆盖)
ARG IFLYGO_UPSTREAM_VERSION=v1.10.3
ENV IFLYGO_UPSTREAM_VERSION=$IFLYGO_UPSTREAM_VERSION
# 项目品牌(替换文本标识时使用)
ARG IFLYGO_BRAND=iflygo
ENV IFLYGO_BRAND=$IFLYGO_BRAND

# GO 环境变量
ARG GO_VERSION=1.26.4
ENV GO_VERSION=$GO_VERSION
ARG GOROOT=/opt/go
ENV GOROOT=$GOROOT
ARG GOPATH=/opt/golang
ENV GOPATH=$GOPATH
# Go 模块代理(加速依赖下载, 国内构建必备; 海外可改为 https://proxy.golang.org,direct)
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY

# 构建依赖
ARG BUILD_DEPS="\
    build-essential \
    ca-certificates \
    curl \
    wget \
    git \
    pkg-config \
    xz-utils"
ENV BUILD_DEPS=$BUILD_DEPS


####################################
#  阶段一: 构建 iFlyGo 二进制(Go)    #
####################################
FROM base AS builder

# buildx 自动注入的目标平台 (amd64/arm64/arm/386 等), 用于按架构下载对应 Go 包并交叉编译
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

# ***** 安装基础依赖(只装编译需要的, 保持构建层精简) *****
RUN set -eux && \
   # 更新源地址(走阿里云加速)
   sed -i 's@URIs: http://[a-z.]*\.ubuntu\.com/ubuntu/@URIs: https://mirrors.aliyun.com/ubuntu/@g' /etc/apt/sources.list.d/ubuntu.sources && \
   sed -i 's@^Types: deb$@Types: deb deb-src@' /etc/apt/sources.list.d/ubuntu.sources && \
   # 解决证书认证失败问题
   touch /etc/apt/apt.conf.d/99verify-peer.conf && \
   echo "Acquire { https::Verify-Peer false }" >>/etc/apt/apt.conf.d/99verify-peer.conf && \
   # 更新系统软件
   DEBIAN_FRONTEND=noninteractive apt-get update -qqy && apt-get upgrade -qqy && \
   # 安装编译依赖
   DEBIAN_FRONTEND=noninteractive apt-get install -qqy --no-install-recommends $BUILD_DEPS \
       --option=Dpkg::Options::=--force-confdef && \
   DEBIAN_FRONTEND=noninteractive apt-get -qqy --no-install-recommends autoremove --purge && \
   DEBIAN_FRONTEND=noninteractive apt-get -qqy --no-install-recommends autoclean && \
   rm -rf /var/lib/apt/lists/* && \
   # 更新时区
   ln -sf /usr/share/zoneinfo/${TZ} /etc/localtime && \
   echo ${TZ} > /etc/timezone

# ***** 安装 Go 工具链(按 buildx 目标架构选择二进制包) *****
RUN set -eux && \
    case "${TARGETARCH}" in \
        amd64)   GO_ARCH=amd64   ;; \
        arm64)   GO_ARCH=arm64   ;; \
        arm)     GO_ARCH=armv6l  ;; \
        386)     GO_ARCH=386     ;; \
        ppc64le) GO_ARCH=ppc64le ;; \
        s390x)   GO_ARCH=s390x   ;; \
        riscv64) GO_ARCH=riscv64 ;; \
        *)       echo "不支持的架构: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    echo "目标架构: ${TARGETARCH} => Go 包: linux-${GO_ARCH}" && \
    wget --no-check-certificate \
         "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" \
         -O /tmp/go.tar.gz && \
    tar xzf /tmp/go.tar.gz -C /opt && \
    mkdir -pv ${GOPATH}/bin && \
    rm -f /tmp/go.tar.gz && \
    ln -sf /opt/go/bin/* /usr/bin/ && \
    go version

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
#   - 替换包括: 默认 tun 接口名、默认配置目录(/etc -> /etc/iflygo)、
#               日志/CLI/Banner 中的旧品牌字样、示例配置中的标识
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
    #    仅匹配明确字符串字面量, 避免误伤
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
# ***** 编译 iflygo / iflygo-cert (纯 Go, 静态链接, 跨平台 buildx 自动设置 GOOS/GOARCH) *****
# 主二进制对应 ./cmd/nebula
# 证书工具对应 ./cmd/nebula-cert
RUN set -eux && \
    mkdir -p /out && \
    cd /src/upstream && \
    BUILD_NUMBER=$(cat /src/COMMIT) && \
    LDFLAGS="-w -s -X main.Build=${BUILD_NUMBER}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
        go build -trimpath -ldflags "${LDFLAGS}" -o /out/iflygo      ./cmd/nebula && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
        go build -trimpath -ldflags "${LDFLAGS}" -o /out/iflygo-cert ./cmd/nebula-cert && \
    /out/iflygo -version || true && \
    /out/iflygo-cert -h    || true && \
    ls -lh /out


##########################################
#         阶段二: 构建运行时镜像           #
##########################################
FROM base

# 作者描述信息
LABEL org.opencontainers.image.authors="iflyelf" \
      org.opencontainers.image.vendor="iflyelf" \
      org.opencontainers.image.title="iflygo" \
      org.opencontainers.image.description="iFlyGo overlay 安全网络隧道"

ARG TARGETARCH
ARG TARGETVARIANT

# 时区设置
ARG TZ=Asia/Shanghai
ENV TZ=$TZ
# 语言设置
ARG LANG=zh_CN.UTF-8
ENV LANG=$LANG

# iFlyGo 运行时变量
ARG IFLYGO_DIR=/data/iflygo
ENV IFLYGO_DIR=$IFLYGO_DIR
# 配置目录(server/client 都用此目录持久化证书与 config.yml)
ENV IFLYGO_CONF_DIR=/etc/iflygo
# 日志目录
ENV IFLYGO_LOG_DIR=/var/log/iflygo

# 安装运行时依赖包
ARG PKG_DEPS="\
    zsh \
    bash \
    bash-completion \
    bind9-dnsutils \
    iproute2 \
    net-tools \
    iptables \
    iputils-ping \
    telnet \
    ncat \
    tcpdump \
    conntrack \
    ipset \
    procps \
    psmisc \
    sysstat \
    lsof \
    htop \
    jq \
    git \
    vim \
    curl \
    wget \
    axel \
    zip \
    unzip \
    tar \
    rsync \
    tini \
    tzdata \
    ca-certificates \
    gnupg2 \
    locales \
    language-pack-zh-hans \
    fonts-droid-fallback \
    fonts-wqy-zenhei \
    fonts-wqy-microhei \
    fonts-arphic-ukai \
    fonts-arphic-uming"
ENV PKG_DEPS=$PKG_DEPS

# ***** 安装运行时依赖 *****
RUN set -eux && \
   # 更新源地址(走阿里云加速)
   sed -i 's@URIs: http://[a-z.]*\.ubuntu\.com/ubuntu/@URIs: https://mirrors.aliyun.com/ubuntu/@g' /etc/apt/sources.list.d/ubuntu.sources && \
   sed -i 's@^Types: deb$@Types: deb deb-src@' /etc/apt/sources.list.d/ubuntu.sources && \
   # 解决证书认证失败问题
   touch /etc/apt/apt.conf.d/99verify-peer.conf && \
   echo "Acquire { https::Verify-Peer false }" >>/etc/apt/apt.conf.d/99verify-peer.conf && \
   # 更新系统软件
   DEBIAN_FRONTEND=noninteractive apt-get update -qqy && apt-get upgrade -qqy && \
   # 安装运行时依赖
   DEBIAN_FRONTEND=noninteractive apt-get install -qqy --no-install-recommends $PKG_DEPS \
       --option=Dpkg::Options::=--force-confdef && \
   DEBIAN_FRONTEND=noninteractive apt-get -qqy --no-install-recommends autoremove --purge && \
   DEBIAN_FRONTEND=noninteractive apt-get -qqy --no-install-recommends autoclean && \
   rm -rf /var/lib/apt/lists/* && \
   # 更新时区
   ln -sf /usr/share/zoneinfo/${TZ} /etc/localtime && \
   echo ${TZ} > /etc/timezone && \
   # 默认 shell 切换到 zsh(可选)
   sh -c "$(curl -fsSL https://raw.github.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" || true && \
   sed -i -e "s/bin\/ash/bin\/zsh/" /etc/passwd && \
   find /usr/share/vim -name defaults.vim -exec sed -i -e 's/mouse=/mouse-=/g' {} + || true && \
   locale-gen zh_CN.UTF-8 && localedef -f UTF-8 -i zh_CN zh_CN.UTF-8 && locale-gen

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
RUN set -eux && \
    chmod +x /init.sh /entrypoint.sh && \
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
