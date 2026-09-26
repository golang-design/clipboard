// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build (linux || freebsd || openbsd || netbsd) && !android

package clipboard

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestWaylandSensitive checks the marker on the data-control backend. It has
// to be cross-process, since a data-control client does not see its own
// selection: a child process copies a marked password and keeps serving it,
// and this one asks whether the clipboard is sensitive.
func TestWaylandSensitive(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("not a Wayland session (WAYLAND_DISPLAY unset)")
	}
	if err := Init(); err != nil || !useWayland {
		t.Skipf("the Wayland backend is not in use: %v", err)
	}

	for _, tt := range []struct {
		name   string
		marked bool
	}{{"marked", true}, {"plain", false}} {
		t.Run(tt.name, func(t *testing.T) {
			child := exec.Command(os.Args[0], "-test.run=^TestWaylandSensitiveCopier$")
			child.Env = append(os.Environ(), "CLIPBOARD_TEST_COPIER=1")
			if tt.marked {
				child.Env = append(child.Env, "CLIPBOARD_TEST_MARKED=1")
			}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { child.Process.Kill(); child.Wait() })
			time.Sleep(500 * time.Millisecond) // let it take the selection

			got, err := sensitive(context.Background(), selClipboard)
			if err != nil || got != tt.marked {
				t.Fatalf("sensitive() = %v, %v; want %v", got, err, tt.marked)
			}
		})
	}
}

// TestWaylandSensitiveCopier is the child for TestWaylandSensitive: it copies,
// with the marker if asked, and serves the selection until it is killed.
func TestWaylandSensitiveCopier(t *testing.T) {
	if os.Getenv("CLIPBOARD_TEST_COPIER") != "1" {
		t.Skip("run by TestWaylandSensitive")
	}
	items := []Item{{Format: FmtText, Bytes: []byte("hunter2")}}
	if os.Getenv("CLIPBOARD_TEST_MARKED") == "1" {
		items = append(items, Item{Format: Register(x11SensitiveTarget), Bytes: []byte("secret")})
	}
	if _, err := wlWriteAll(selClipboard, items, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
}
