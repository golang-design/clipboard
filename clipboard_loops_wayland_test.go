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

// TestWaylandLoopsDropsAfterServing checks the serve limit on the data-control
// backend: one wl-paste gets the data, the next gets nothing because the source
// was destroyed and the selection cleared (#22).
//
// It has to be cross-process — the count is kept by the serving side, and a
// data-control client does not observe its own selection, so nothing in-process
// can consume a serve. Runs under headless sway.
func TestWaylandLoopsDropsAfterServing(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("not a Wayland session (WAYLAND_DISPLAY unset)")
	}
	if _, err := exec.LookPath("wl-paste"); err != nil {
		t.Skip("wl-paste not found")
	}

	secret := []byte("pasted once, then gone")
	if _, err := wlWriteAll(selClipboard, []Item{{Format: FmtText, Bytes: secret}}, 1); err != nil {
		t.Fatalf("wlWriteAll with a serve limit: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	out, err := exec.Command("wl-paste", "--no-newline").Output()
	if err != nil {
		t.Fatalf("first wl-paste: %v", err)
	}
	if !bytes.Equal(out, secret) {
		t.Fatalf("first wl-paste = %q, want %q: the one allowed serve must work", out, secret)
	}

	// The source is destroyed after that serve, so the selection is cleared and
	// a second paste finds nothing. wl-paste exits non-zero on an empty
	// clipboard, so either an error or empty output is the expected outcome.
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := exec.Command("wl-paste", "--no-newline").Output()
		if err != nil || !bytes.Equal(out, secret) {
			return // dropped, as asked
		}
		if time.Now().After(deadline) {
			t.Fatalf("wl-paste still returns %q after the single allowed serve: "+
				"the serve limit did not clear the selection", out)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
