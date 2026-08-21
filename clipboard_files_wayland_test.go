// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

package clipboard

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestWaylandFiles verifies the file list on the data-control backend against an
// independent client: wl-paste asks for text/uri-list and must get the RFC 2483
// body other applications expect, since that is how a file manager on this
// platform learns what was copied.
//
// It has to be cross-process — a data-control client does not observe its own
// just-set selection. Runs under headless sway; skips off-Wayland or without
// wl-clipboard.
func TestWaylandFiles(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("not a Wayland session (WAYLAND_DISPLAY unset)")
	}
	if _, err := exec.LookPath("wl-paste"); err != nil {
		t.Skip("wl-paste not found")
	}

	paths := []string{"/tmp/a file.txt", "/tmp/ünïcode.txt"}
	if _, err := wlWriteAll([]Item{
		{Format: FmtFiles, Bytes: uriListFromPaths(paths)},
	}); err != nil {
		t.Fatalf("wlWriteAll: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	out, err := exec.Command("wl-paste", "--type", "text/uri-list", "--no-newline").Output()
	if err != nil {
		t.Fatalf("wl-paste --type text/uri-list: %v", err)
	}

	got := pathsFromURIList(out)
	if len(got) != len(paths) {
		t.Fatalf("wl-paste returned %d paths (%q) from %q, want %d", len(got), got, out, len(paths))
	}
	for i := range paths {
		if got[i] != paths[i] {
			t.Fatalf("path %d = %q, want %q (raw uri-list %q)", i, got[i], paths[i], out)
		}
	}
	// The body must be URIs, not bare paths, or no other application parses it.
	if !strings.Contains(string(out), "file:///tmp/a%20file.txt") {
		t.Fatalf("uri-list = %q, want it to percent-encode the space in a file URI", out)
	}
}
