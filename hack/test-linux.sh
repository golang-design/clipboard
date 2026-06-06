#!/usr/bin/env bash
# Copyright 2026 The golang.design Initiative Authors.
# All rights reserved. Use of this source code is governed
# by a MIT license that can be found in the LICENSE file.
#
# Run the Linux/X11 test suite inside a container, so the cgo path can be
# exercised from any host (e.g. macOS) without a local X server. Mirrors
# what the GitHub Actions Linux job does: install libx11-dev, run the
# tests under a virtual framebuffer with cgo on and off.
#
# Usage:
#   hack/test-linux.sh            # auto-detect docker or podman
#   GO_VERSION=1.24 hack/test-linux.sh
#   CONTAINER=podman hack/test-linux.sh

set -euo pipefail

GO_VERSION="${GO_VERSION:-1.24}"

# Pick a container runtime: explicit $CONTAINER, else docker, else podman.
runtime="${CONTAINER:-}"
if [ -z "${runtime}" ]; then
	if command -v docker >/dev/null 2>&1; then
		runtime="docker"
	elif command -v podman >/dev/null 2>&1; then
		runtime="podman"
	else
		echo "error: neither docker nor podman found on PATH" >&2
		exit 1
	fi
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "Running Linux/X11 tests in ${runtime} (golang:${GO_VERSION})..."

exec "${runtime}" run --rm -v "${repo_root}":/src -w /src "golang:${GO_VERSION}" bash -c '
	set -e
	apt-get update -qq >/dev/null
	apt-get install -y -qq libx11-dev xvfb xclip >/dev/null 2>&1
	echo "--- go test (CGO_ENABLED=1, X11 path) ---"
	CGO_ENABLED=1 xvfb-run -a go test -count=1 -covermode=atomic .
	echo "--- go test (CGO_ENABLED=0, nocgo path) ---"
	CGO_ENABLED=0 go test -count=1 -covermode=atomic .
'
