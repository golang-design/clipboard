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

// TestWaylandFormatsEnumerate verifies data-control enumeration: an independent
// client (wl-copy --type) sets a custom MIME type, and the backend reports it
// (mapped to a registered Format) from the current selection's offer. Runs under
// headless sway; skips off-Wayland or without wl-clipboard.
func TestWaylandFormatsEnumerate(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("not a Wayland session (WAYLAND_DISPLAY unset)")
	}
	if _, err := exec.LookPath("wl-copy"); err != nil {
		t.Skip("wl-copy not found")
	}

	const mime = "application/x.golang-design.enumerate"
	cmd := exec.Command("wl-copy", "--type", mime)
	cmd.Stdin = bytes.NewReader([]byte("enumerate"))
	if err := cmd.Run(); err != nil {
		t.Fatalf("wl-copy: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // let wl-copy acquire selection ownership

	want := Register(mime)
	// Call the Wayland enumeration directly (like the other wl* tests): the
	// public enumerateFormats() routes on useWayland, which is only set by
	// Init() and unset in this unit test.
	fs := wlEnumerateFormats(selClipboard)
	found := false
	for _, f := range fs {
		if f == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("enumerateFormats() = %v, want it to include the %s token %v", fs, mime, want)
	}
}
