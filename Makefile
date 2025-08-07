# Makefile for golang.design/x/clipboard with performance optimizations

# Build variables
PACKAGE = golang.design/x/clipboard
CMD_GCLIP = ./cmd/gclip
CMD_GCLIP_GUI = ./cmd/gclip-gui
VERSION ?= $(shell git describe --tags --always --dirty)
BUILD_TIME = $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GO_VERSION = $(shell go version | cut -d' ' -f3)

# Optimization flags
LDFLAGS_BASE = -s -w
LDFLAGS_SIZE = $(LDFLAGS_BASE) -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)
LDFLAGS_PERF = $(LDFLAGS_SIZE) -linkmode external -extldflags "-static"

# Build tags for different optimization levels
TAGS_FAST = fast
TAGS_SIZE = size
TAGS_OPTIMIZE = optimize

# Go build flags
GOFLAGS_BASE = -trimpath
GOFLAGS_SIZE = $(GOFLAGS_BASE) -a -installsuffix cgo
GOFLAGS_PERF = $(GOFLAGS_SIZE) -gcflags="-l=4" -asmflags="-trimpath=$(CURDIR)"

# Default target
.PHONY: all
all: build

# Standard build
.PHONY: build
build:
	go build $(GOFLAGS_BASE) -ldflags="$(LDFLAGS_BASE)" -o bin/gclip $(CMD_GCLIP)
	go build $(GOFLAGS_BASE) -ldflags="$(LDFLAGS_BASE)" -o bin/gclip-gui $(CMD_GCLIP_GUI)

# Size-optimized build (smallest binary)
.PHONY: build-size
build-size:
	CGO_ENABLED=0 go build $(GOFLAGS_SIZE) -tags="$(TAGS_SIZE)" \
		-ldflags="$(LDFLAGS_SIZE)" -o bin/gclip-size $(CMD_GCLIP)
	@echo "Size-optimized build complete:"
	@ls -lh bin/gclip-size

# Performance-optimized build
.PHONY: build-perf
build-perf:
	go build $(GOFLAGS_PERF) -tags="$(TAGS_OPTIMIZE)" \
		-ldflags="$(LDFLAGS_PERF)" -o bin/gclip-perf $(CMD_GCLIP)
	@echo "Performance-optimized build complete:"
	@ls -lh bin/gclip-perf

# Fast build for development
.PHONY: build-fast
build-fast:
	go build -tags="$(TAGS_FAST)" -o bin/gclip-fast $(CMD_GCLIP)

# Cross-compilation targets
.PHONY: build-cross
build-cross:
	@mkdir -p bin/cross
	# Linux
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(GOFLAGS_SIZE) \
		-ldflags="$(LDFLAGS_SIZE)" -o bin/cross/gclip-linux-amd64 $(CMD_GCLIP)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(GOFLAGS_SIZE) \
		-ldflags="$(LDFLAGS_SIZE)" -o bin/cross/gclip-linux-arm64 $(CMD_GCLIP)
	# Windows
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(GOFLAGS_SIZE) \
		-ldflags="$(LDFLAGS_SIZE)" -o bin/cross/gclip-windows-amd64.exe $(CMD_GCLIP)
	# macOS
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(GOFLAGS_SIZE) \
		-ldflags="$(LDFLAGS_SIZE)" -o bin/cross/gclip-darwin-amd64 $(CMD_GCLIP)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(GOFLAGS_SIZE) \
		-ldflags="$(LDFLAGS_SIZE)" -o bin/cross/gclip-darwin-arm64 $(CMD_GCLIP)
	@echo "Cross-compilation complete:"
	@ls -lh bin/cross/

# UPX compression (requires upx to be installed)
.PHONY: compress
compress: build-size
	@which upx > /dev/null || (echo "UPX not found. Install with: apt-get install upx-ucl"; exit 1)
	upx --best --lzma bin/gclip-size
	@echo "Compressed binary:"
	@ls -lh bin/gclip-size

# Profile-guided optimization build
.PHONY: build-pgo
build-pgo:
	@echo "Building with Profile-Guided Optimization..."
	go build -pgo=auto $(GOFLAGS_PERF) -tags="$(TAGS_OPTIMIZE)" \
		-ldflags="$(LDFLAGS_PERF)" -o bin/gclip-pgo $(CMD_GCLIP)
	@echo "PGO build complete:"
	@ls -lh bin/gclip-pgo

# Benchmarks
.PHONY: bench
bench:
	go test -bench=. -benchmem -benchtime=5s ./...

.PHONY: bench-compare
bench-compare:
	@echo "Running benchmarks with different optimizations..."
	@echo "=== Standard Build ==="
	go test -bench=BenchmarkClipboard -benchmem -count=3 ./...
	@echo "=== Optimized Build ==="
	go test -bench=BenchmarkClipboard -benchmem -count=3 -tags="$(TAGS_OPTIMIZE)" ./...

# Memory profiling
.PHONY: profile-mem
profile-mem:
	go test -bench=BenchmarkMemoryUsage -memprofile=mem.prof -benchmem ./...
	go tool pprof mem.prof

# CPU profiling
.PHONY: profile-cpu
profile-cpu:
	go test -bench=BenchmarkWrite -cpuprofile=cpu.prof ./...
	go tool pprof cpu.prof

# Performance analysis
.PHONY: analyze
analyze:
	@echo "Analyzing binary sizes..."
	@echo "Standard build:"
	@go build $(GOFLAGS_BASE) -ldflags="$(LDFLAGS_BASE)" -o /tmp/gclip-std $(CMD_GCLIP) && ls -lh /tmp/gclip-std
	@echo "Size-optimized build:"
	@CGO_ENABLED=0 go build $(GOFLAGS_SIZE) -ldflags="$(LDFLAGS_SIZE)" -o /tmp/gclip-size $(CMD_GCLIP) && ls -lh /tmp/gclip-size
	@echo "Performance-optimized build:"
	@go build $(GOFLAGS_PERF) -tags="$(TAGS_OPTIMIZE)" -ldflags="$(LDFLAGS_PERF)" -o /tmp/gclip-perf $(CMD_GCLIP) && ls -lh /tmp/gclip-perf
	@rm -f /tmp/gclip-*

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf bin/
	rm -f *.prof
	go clean -cache

# Install optimized version
.PHONY: install
install: build-perf
	go install $(GOFLAGS_PERF) -tags="$(TAGS_OPTIMIZE)" \
		-ldflags="$(LDFLAGS_PERF)" $(CMD_GCLIP)

# Development targets
.PHONY: test
test:
	go test -v ./...

.PHONY: test-race
test-race:
	go test -race -v ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	go fmt ./...

# Dependency analysis
.PHONY: deps
deps:
	@echo "Analyzing dependencies..."
	go list -m all
	@echo "Direct dependencies:"
	go list -f '{{.Path}} {{.Version}}' -m all | grep -v "$(PACKAGE)"
	@echo "Dependency tree:"
	go mod graph | head -20

# Module optimization
.PHONY: mod-tidy
mod-tidy:
	go mod tidy
	go mod verify

# Security check (requires gosec)
.PHONY: security
security:
	@which gosec > /dev/null || (echo "gosec not found. Install with: go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest"; exit 1)
	gosec ./...

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build         - Standard build"
	@echo "  build-size    - Size-optimized build (smallest binary)"
	@echo "  build-perf    - Performance-optimized build"
	@echo "  build-fast    - Fast build for development"
	@echo "  build-cross   - Cross-compile for multiple platforms"
	@echo "  build-pgo     - Profile-guided optimization build"
	@echo "  compress      - Compress binary with UPX"
	@echo "  bench         - Run benchmarks"
	@echo "  bench-compare - Compare benchmarks between builds"
	@echo "  profile-mem   - Memory profiling"
	@echo "  profile-cpu   - CPU profiling"
	@echo "  analyze       - Analyze binary sizes"
	@echo "  install       - Install optimized version"
	@echo "  test          - Run tests"
	@echo "  test-race     - Run tests with race detection"
	@echo "  clean         - Clean build artifacts"
	@echo "  deps          - Analyze dependencies"
	@echo "  security      - Run security checks"
	@echo "  help          - Show this help"