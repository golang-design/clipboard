#!/usr/bin/env bash
# Copyright 2026 The golang.design Initiative Authors.
# All rights reserved. Use of this source code is governed
# by a MIT license that can be found in the LICENSE file.
#
# Run the Wayland tests inside a container under a headless sway compositor, so
# the data-control backend can be exercised from any host (e.g. macOS) without
# a real Wayland session. Mirrors the wayland_test CI job.
#
# sway is used because it implements a data-control manager
# (zwlr_data_control_manager_v1); the reference compositor weston and the
# minimal kiosk cage do not.
#
# Usage:
#   hack/test-wayland.sh
#   GO_VERSION=1.24 CONTAINER=podman hack/test-wayland.sh

set -euo pipefail

GO_VERSION="${GO_VERSION:-1.24}"

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

echo "Running Wayland tests in ${runtime} (golang:${GO_VERSION}) under headless sway..."

exec "${runtime}" run --rm -v "${repo_root}":/src -w /src "golang:${GO_VERSION}" bash -c '
	set -e
	apt-get update -qq >/dev/null
	apt-get install -y -qq sway libx11-dev wl-clipboard >/dev/null 2>&1
	export XDG_RUNTIME_DIR=/tmp/xdg
	mkdir -p "$XDG_RUNTIME_DIR" && chmod 700 "$XDG_RUNTIME_DIR"
	export WLR_BACKENDS=headless WLR_LIBINPUT_NO_DEVICES=1 WLR_RENDERER=pixman
	# Run the whole suite as a sway client, so the public API (Init/Read/Write/
	# Watch) is exercised through the Wayland dispatch, then quit the compositor.
	# sway sets WAYLAND_DISPLAY in the environment of processes it execs.
	# xwayland is disabled: we do not need it and it just logs a missing-binary
	# error in CI.
	printf "xwayland disable\nexec \"go test -count=1 -covermode=atomic -v . > /tmp/res.txt 2>&1; echo EXIT=\$? >> /tmp/res.txt; swaymsg exit 2>/dev/null\"\n" > /tmp/sway.conf
	timeout 180 sway -c /tmp/sway.conf >/tmp/sway.log 2>&1 || true
	echo "----- go test output -----"
	cat /tmp/res.txt 2>/dev/null || { echo "no test output; sway log:"; tail -20 /tmp/sway.log; exit 1; }
	grep -q "^EXIT=0$" /tmp/res.txt
'
