VERSION ?= $(shell cat VERSION 2>/dev/null || echo 0.1.0)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -s -w \
	-X github.com/iflyelf/iflygo/internal/version.Version=$(VERSION) \
	-X github.com/iflyelf/iflygo/internal/version.Build=$(COMMIT) \
	-X github.com/iflyelf/iflygo/internal/version.Commit=$(COMMIT)

.PHONY: build build-server build-client cross test vet fmt tidy clean frontend patch-grdp prepare

# patch-grdp 对 grdp 子模块应用 iflygo 本地补丁（移除 CGO / 删除 broken VNC）。
# 子模块更新后（git submodule update）需重新运行。
patch-grdp:
	python3 scripts/patch_grdp.py third_party/grdp

frontend:
	python3 scripts/inject_tunnel_frontend.py
	node --check web/dist/assets/app.js

# prepare 用于干净检出（CI/Docker）：应用 grdp 补丁 + 前端合并注入。
prepare: patch-grdp frontend

build: build-server build-client

build-server:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/iflygo-server ./cmd/server

build-client:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/iflygo-client ./cmd/client

test:
	go test ./...

vet:
	go vet ./...

check-frontend:
	node --check web/dist/assets/app.js

fmt:
	gofmt -w .

tidy:
	go mod tidy

cross:
	@mkdir -p dist
	@for osarch in "linux amd64" "linux arm64" "linux arm" "windows amd64" "windows arm64" "darwin amd64" "darwin arm64"; do \
		set -- $$osarch; os=$$1; arch=$$2; ext=""; [ "$$os" = windows ] && ext=".exe"; \
		echo "==> server $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/iflygo-server-$$os-$$arch$$ext ./cmd/server; \
		echo "==> client $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/iflygo-client-$$os-$$arch$$ext ./cmd/client; \
	done

clean:
	rm -rf bin dist
