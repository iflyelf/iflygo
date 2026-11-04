VERSION ?= $(shell cat VERSION 2>/dev/null || echo 0.1.0)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -s -w \
	-X github.com/iflyelf/iflygo/internal/version.Version=$(VERSION) \
	-X github.com/iflyelf/iflygo/internal/version.Build=$(COMMIT)

.PHONY: build build-server build-client cross test vet fmt tidy clean

build: build-server build-client

build-server:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/iflygo-server ./cmd/server

build-client:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/iflygo-client ./cmd/client

test:
	go test ./...

vet:
	go vet ./...

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
