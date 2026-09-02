# MyShare build tooling.
#
#   make build      -> ./bin/myshare for this OS/arch (embeds web/dist)
#   make web        -> build the frontend into web/dist
#   make dev        -> run backend + Vite with live reload
#   make test       -> go test -race ./...
#   make dist       -> cross-compiled binaries for linux/darwin/windows (amd64+arm64)
#   make install    -> build + copy to ~/.local/bin (scripts/install.sh)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath
CGO_ENABLED ?= 0
export CGO_ENABLED

BIN := bin/myshare
DISTDIR := dist-bin

.PHONY: build web dev test race vet fmt lint dist install uninstall clean tidy

build: web
	@mkdir -p bin
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/myshare
	@echo "built $(BIN) ($(VERSION))"

# Build the frontend. Falls back to the committed stub if npm is unavailable so
# `make build` still works in a Go-only environment.
web:
	@if [ -d web/node_modules ]; then \
		cd web && npm run build; \
	elif command -v npm >/dev/null 2>&1; then \
		cd web && npm ci && npm run build; \
	else \
		echo "npm not found — using the committed web/dist stub"; \
	fi

dev:
	@echo "Starting Vite (5173) and MyShare (8787 with --dev-proxy)…"
	@(cd web && npm run dev) & \
	 go run ./cmd/myshare --dev-proxy http://localhost:5173 --log-level debug; \
	 kill %1 2>/dev/null || true

test:
	go test -race ./...

# The 3 GiB streaming test writes to $$TMPDIR — point it at a disk with room
# (the default /tmp is often a small tmpfs).
bigtest:
	MYSHARE_BIGTEST=1 TMPDIR=$(HOME)/.myshare-bigtmp go test -run 'Large|BigTest' -timeout 30m ./... ; \
	rm -rf $(HOME)/.myshare-bigtmp

vet:
	go vet ./...

fmt:
	gofmt -w .
	@cd web && npx --yes prettier -w "src/**/*.{ts,tsx,css}" 2>/dev/null || true

tidy:
	go mod tidy

dist: web
	@mkdir -p $(DISTDIR)
	@set -e; for target in \
		linux/amd64 linux/arm64 \
		darwin/amd64 darwin/arm64 \
		windows/amd64 windows/arm64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		out=$(DISTDIR)/myshare-$${os}-$${arch}$$ext; \
		echo "  $$out"; \
		GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $$out ./cmd/myshare; \
	done
	@echo "cross-compiled binaries in $(DISTDIR)/"

install: build
	sh scripts/install.sh

uninstall:
	sh scripts/uninstall.sh

clean:
	rm -rf bin $(DISTDIR)
