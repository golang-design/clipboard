// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build (linux || freebsd || openbsd || netbsd) && !android

package clipboard

import (
	"os"
	"testing"
	"time"
)

// unownedSelection is a selection atom no client ever takes ownership of, which
// is what makes the assertions below deterministic: PRIMARY and CLIPBOARD are
// owned or not depending on what else has run.
const unownedSelection = "GOLANG_DESIGN_CLIPBOARD_UNOWNED_TEST"

// x11Ready skips when there is no X server to talk to.
func x11Ready(t *testing.T) {
	t.Helper()
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland session; this covers the X11 selection path")
	}
	if err := x11Test(); err != nil {
		t.Skipf("X11 unavailable: %v", err)
	}
}

// TestReadUnownedSelectionReturnsPromptly covers the ordinary state of PRIMARY:
// nobody owns it until the user selects something with the mouse.
//
// An unowned selection never answers a ConvertSelection — the server sends no
// SelectionNotify at all — so without asking for the owner first the read sits
// until x11ReadTimeout. That is a 5 s block, and Watch pays it synchronously
// before it returns, so Watch(ctx, FmtText, FromPrimary()) would hang for 5 s on
// any session where nothing had been selected yet, then again on every poll.
func TestReadUnownedSelectionReturnsPromptly(t *testing.T) {
	x11Ready(t)

	start := time.Now()
	got, err := x11ReadSelection(unownedSelection, "UTF8_STRING")
	took := time.Since(start)

	if err != nil {
		t.Fatalf("x11ReadSelection of an unowned selection = %v, want no error", err)
	}
	if got != nil {
		t.Fatalf("x11ReadSelection of an unowned selection = %q, want nil", got)
	}
	if took > x11ReadTimeout/2 {
		t.Fatalf("x11ReadSelection of an unowned selection took %v, want well under the "+
			"%v read deadline: it waited for a SelectionNotify that nobody was going to send",
			took, x11ReadTimeout)
	}
}

// TestTargetsOfUnownedSelectionReturnsPromptly is the same for enumeration,
// which Formats goes through and which asks the selection a separate question.
func TestTargetsOfUnownedSelectionReturnsPromptly(t *testing.T) {
	x11Ready(t)

	start := time.Now()
	got, err := x11TargetsOf(unownedSelection)
	took := time.Since(start)

	if err != nil {
		t.Fatalf("x11TargetsOf an unowned selection = %v, want no error", err)
	}
	if len(got) != 0 {
		t.Fatalf("x11TargetsOf an unowned selection = %v, want none", got)
	}
	if took > x11ReadTimeout/2 {
		t.Fatalf("x11TargetsOf an unowned selection took %v, want well under the %v read deadline",
			took, x11ReadTimeout)
	}
}
