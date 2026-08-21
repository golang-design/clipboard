// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

package clipboard

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestWaylandPrimarySelection verifies the primary selection on the data-control
// backend against an independent client: the two selections are set through
// different requests (set_selection and set_primary_selection) and announced by
// different events, so a backend that confused them would return the wrong
// clipboard's data — which reads as success to any same-process test.
//
// Runs under headless sway; skips off-Wayland, without wl-clipboard, or when the
// compositor's data-control manager predates the primary selection.
func TestWaylandPrimarySelection(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("not a Wayland session (WAYLAND_DISPLAY unset)")
	}
	if _, err := exec.LookPath("wl-paste"); err != nil {
		t.Skip("wl-paste not found")
	}

	onClipboard := []byte("copied with ctrl-c")
	onPrimary := []byte("selected with the mouse")

	if _, err := wlWriteAll(selClipboard, []Item{{Format: FmtText, Bytes: onClipboard}}); err != nil {
		t.Fatalf("wlWriteAll(clipboard): %v", err)
	}
	if _, err := wlWriteAll(selPrimary, []Item{{Format: FmtText, Bytes: onPrimary}}); err != nil {
		if errors.Is(err, errUnsupported) {
			t.Skip("compositor's data-control manager predates the primary selection")
		}
		t.Fatalf("wlWriteAll(primary): %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Each selection must serve its own contents. Setting one must not have
	// replaced the other, which is the failure a single-selection backend shows.
	for _, tc := range []struct {
		args []string
		want []byte
		name string
	}{
		{[]string{"--no-newline"}, onClipboard, "clipboard"},
		{[]string{"--primary", "--no-newline"}, onPrimary, "primary"},
	} {
		out, err := exec.Command("wl-paste", tc.args...).Output()
		if err != nil {
			t.Fatalf("wl-paste %v: %v", tc.args, err)
		}
		if !bytes.Equal(out, tc.want) {
			t.Fatalf("wl-paste %s = %q, want %q", tc.name, out, tc.want)
		}
	}

	// And reading through the package agrees with the independent client.
	if got, err := wlRead(selPrimary, FmtText); err != nil || !bytes.Equal(got, onPrimary) {
		t.Fatalf("wlRead(primary) = (%q, %v), want (%q, nil)", got, err, onPrimary)
	}
	if got, err := wlRead(selClipboard, FmtText); err != nil || !bytes.Equal(got, onClipboard) {
		t.Fatalf("wlRead(clipboard) = (%q, %v), want (%q, nil)", got, err, onClipboard)
	}
}
