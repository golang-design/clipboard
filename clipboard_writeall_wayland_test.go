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

// TestWaylandWriteAll verifies the multi-format write on the data-control
// backend: one data source offers every item's MIME types and serves the bytes
// belonging to whichever type the requestor names (#151).
//
// It has to be cross-process — under data-control a client does not observe its
// own just-set selection — so an independent wl-paste asks for each type in
// turn, which is also the real use case. Runs under headless sway; skips
// off-Wayland or without wl-clipboard.
func TestWaylandWriteAll(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("not a Wayland session (WAYLAND_DISPLAY unset)")
	}
	if _, err := exec.LookPath("wl-paste"); err != nil {
		t.Skip("wl-paste not found")
	}

	const mime = "application/x.golang-design.clipboard-wl-writeall"
	plain := []byte("plain text")

	if _, err := wlWriteAll(selClipboard, []Item{
		{Format: Register(mime), Bytes: rawCustom},
		{Format: FmtText, Bytes: plain},
	}, 0); err != nil {
		t.Fatalf("wlWriteAll: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Each type must serve its own bytes: before #151 a source served the same
	// buffer for every type it offered, so asking for the custom type would
	// hand back the text (or the reverse), not the payload it was written with.
	for _, tc := range []struct {
		typ  string
		want []byte
	}{
		{mime, rawCustom},
		{"text/plain", plain},
	} {
		out, err := exec.Command("wl-paste", "--type", tc.typ, "--no-newline").Output()
		if err != nil {
			t.Fatalf("wl-paste --type %s: %v", tc.typ, err)
		}
		if !bytes.Equal(out, tc.want) {
			t.Fatalf("wl-paste --type %s = %v, want %v", tc.typ, out, tc.want)
		}
	}
}
