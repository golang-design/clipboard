// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android && cgo

package clipboard

import "testing"

// TestX11ProtocolErrorDoesNotCrash is a regression test for #61: an X11
// protocol error must not terminate the host process.
//
// triggerProtocolError issues a deliberately invalid X request (a BadWindow on
// X_ChangeProperty, the same opcode that crashed in #61). Without the
// XSetErrorHandler installed in initX11, Xlib's default handler would print the
// error and call exit(1) — which would kill this test binary and fail the run.
// With the handler installed, the error is swallowed and the call returns,
// proving the process survives.
func TestX11ProtocolErrorDoesNotCrash(t *testing.T) {
	if err := Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}
	got := triggerProtocolError()
	if got == -1 {
		// No X display (e.g. a pure Wayland session, where Init uses the
		// Wayland backend); there is no X connection to provoke an error on.
		t.Skip("no X display available")
	}
	if got != 0 {
		t.Fatalf("triggerProtocolError() = %d, want 0", got)
	}
	// Reaching here means the process survived the protocol error.
}
