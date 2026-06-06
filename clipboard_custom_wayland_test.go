// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

package clipboard

import (
	"bytes"
	"os"
	"os/exec"
	"testing"
	"time"
)

// rawCustom is a payload with a NUL and non-UTF-8 bytes: a custom (passthrough)
// format must carry it verbatim, unlike FmtText (UTF-8) or FmtImage (PNG).
var rawCustom = []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 'h', 'i', 0x00, 0x80}

// TestWaylandCustomWrite verifies the data-control write path for a custom
// format: the backend owns the selection and serves the registered MIME type
// verbatim, which an independent client (wl-paste --type) reads back. Runs under
// headless sway; skips off-Wayland or without wl-clipboard.
func TestWaylandCustomWrite(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("not a Wayland session (WAYLAND_DISPLAY unset)")
	}
	if _, err := exec.LookPath("wl-paste"); err != nil {
		t.Skip("wl-paste not found")
	}

	const mime = "application/x.golang-design.clipboard-wl-write"
	f := Register(mime)
	if _, err := wlWrite(f, rawCustom); err != nil {
		t.Fatalf("wlWrite: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	out, err := exec.Command("wl-paste", "--type", mime, "--no-newline").Output()
	if err != nil {
		t.Fatalf("wl-paste: %v", err)
	}
	if !bytes.Equal(out, rawCustom) {
		t.Fatalf("wl-paste --type %s = %v, want %v", mime, out, rawCustom)
	}
}

// TestWaylandCustomRead verifies the data-control read path for a custom format:
// an independent client (wl-copy --type) sets a registered MIME type from binary
// stdin, and the backend reads the bytes back verbatim. Runs under headless
// sway; skips off-Wayland or without wl-clipboard.
func TestWaylandCustomRead(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("not a Wayland session (WAYLAND_DISPLAY unset)")
	}
	if _, err := exec.LookPath("wl-copy"); err != nil {
		t.Skip("wl-copy not found")
	}

	const mime = "application/x.golang-design.clipboard-wl-read"
	cmd := exec.Command("wl-copy", "--type", mime)
	cmd.Stdin = bytes.NewReader(rawCustom) // stdin is copied verbatim (no newline)
	if err := cmd.Run(); err != nil {
		t.Fatalf("wl-copy: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // let wl-copy acquire selection ownership

	f := Register(mime)
	got, err := wlRead(f)
	if err != nil {
		t.Fatalf("wlRead: %v", err)
	}
	if !bytes.Equal(got, rawCustom) {
		t.Fatalf("wlRead = %v, want %v", got, rawCustom)
	}
}
