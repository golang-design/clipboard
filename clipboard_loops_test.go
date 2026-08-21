// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"bytes"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// ownerServed reports whether this platform serves pastes from the writing
// process, which is what makes a serve countable. X11 and Wayland do; Windows
// and macOS copy into an OS-owned store instead.
func ownerServed() bool {
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		return true
	}
	return false
}

func loopsReady(t *testing.T) {
	t.Helper()

	if degradesWithoutCgo() {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland loops are covered by the cross-process interop test")
	}
	if err := clipboard.Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}
}

// TestLoopsDropsAfterServing is the point of #22: data written with Loops(1) is
// pastable once and then gone, so a secret does not sit on the clipboard until
// something else replaces it.
//
// Without the limit the owner keeps answering requests forever and the second
// read returns the data again, so this fails on exactly the behavior being added.
func TestLoopsDropsAfterServing(t *testing.T) {
	loopsReady(t)
	if !ownerServed() {
		t.Skipf("%s serves the clipboard from an OS-owned store; Loops cannot apply", runtime.GOOS)
	}

	secret := []byte("pasted once, then gone")
	if ch := clipboard.Write(clipboard.FmtText, secret, clipboard.Loops(1)); ch == nil {
		t.Fatal("Write with Loops reported failure")
	}

	if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, secret) {
		t.Fatalf("first Read(FmtText) = %q, want %q: the one allowed serve must work", got, secret)
	}

	// Ownership is dropped by the serving goroutine once it has served enough,
	// which the reader observes as the selection going away.
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := clipboard.Read(clipboard.FmtText)
		if !bytes.Equal(got, secret) {
			return // dropped, as asked
		}
		if time.Now().After(deadline) {
			t.Fatalf("Read(FmtText) still returns %q after the single allowed serve: "+
				"Loops(1) did not drop the data", got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestLoopsUnlimitedByDefault checks the option is opt-in: without it, and with
// a non-positive count, the data stays pastable as it always has.
func TestLoopsUnlimitedByDefault(t *testing.T) {
	loopsReady(t)
	if !ownerServed() {
		t.Skipf("%s serves the clipboard from an OS-owned store; Loops cannot apply", runtime.GOOS)
	}

	want := []byte("stays put")
	clipboard.Write(clipboard.FmtText, want, clipboard.Loops(0))

	for i := range 3 {
		if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, want) {
			t.Fatalf("read %d = %q, want %q: Loops(0) means unlimited", i, got, want)
		}
	}
}

// TestLoopsIgnoredWhereUnsupported puts the documented no-op on the record. On a
// store-backed clipboard nothing can withdraw the data, and Loops must not
// pretend otherwise by failing the write — but it must also not be mistaken for
// a way to clear a secret there, which is why the godoc leads with this.
func TestLoopsIgnoredWhereUnsupported(t *testing.T) {
	loopsReady(t)
	if ownerServed() {
		t.Skipf("%s serves pastes from the writing process; Loops applies", runtime.GOOS)
	}

	want := []byte("stays until replaced")
	if ch := clipboard.Write(clipboard.FmtText, want, clipboard.Loops(1)); ch == nil {
		t.Fatal("Write with Loops failed; it should succeed and ignore the limit")
	}
	for i := range 3 {
		if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, want) {
			t.Fatalf("read %d = %q, want %q: Loops is a no-op here, not a delete", i, got, want)
		}
	}
}
