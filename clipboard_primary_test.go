// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// hasPrimarySelection reports whether this platform has a second clipboard at
// all. X11 and Wayland do; nothing else does.
func hasPrimarySelection() bool {
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		return true
	}
	return false
}

func primaryReady(t *testing.T) {
	t.Helper()

	if degradesWithoutCgo() {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland primary selection is covered by the cross-process interop test")
	}
	if err := clipboard.Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}
}

// TestPrimarySelectionIsIndependent is the point of #67: the primary selection
// and the clipboard are two different clipboards, and writing one must not
// disturb the other.
//
// Without this change the second write would land on the same clipboard and the
// first value would be gone, so the assertion fails on both halves at once.
func TestPrimarySelectionIsIndependent(t *testing.T) {
	primaryReady(t)
	if !hasPrimarySelection() {
		t.Skipf("%s has no primary selection", runtime.GOOS)
	}

	onClipboard := []byte("copied with ctrl-c")
	onPrimary := []byte("selected with the mouse")

	clipboard.Write(clipboard.FmtText, onClipboard)
	clipboard.Write(clipboard.FmtText, onPrimary, clipboard.FromPrimary())

	if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, onClipboard) {
		t.Fatalf("Read(FmtText) = %q, want %q: the primary write disturbed the clipboard",
			got, onClipboard)
	}
	if got := clipboard.Read(clipboard.FmtText, clipboard.FromPrimary()); !bytes.Equal(got, onPrimary) {
		t.Fatalf("Read(FmtText, FromPrimary()) = %q, want %q", got, onPrimary)
	}
}

// TestPrimarySelectionEnumerates checks Formats reports the selection's own
// contents rather than the clipboard's — enumeration is a separate code path
// from reading on every backend, and it takes the selection as its own argument.
func TestPrimarySelectionEnumerates(t *testing.T) {
	primaryReady(t)
	if !hasPrimarySelection() {
		t.Skipf("%s has no primary selection", runtime.GOOS)
	}

	html := clipboard.Register("text/html")
	clipboard.Write(html, []byte("<b>clipboard</b>"))
	clipboard.Write(clipboard.FmtText, []byte("primary"), clipboard.FromPrimary())

	var sawText bool
	primary := clipboard.Formats(clipboard.FromPrimary())
	for _, f := range primary {
		if f == html {
			t.Fatalf("Formats(FromPrimary()) = %v, want it not to report the clipboard's text/html", primary)
		}
		if f == clipboard.FmtText {
			sawText = true
		}
	}
	if !sawText {
		t.Fatalf("Formats(FromPrimary()) = %v, want it to report FmtText", primary)
	}
}

// TestPrimarySelectionWatch checks a watch bound to the selection reports the
// selection's changes, and only those.
func TestPrimarySelectionWatch(t *testing.T) {
	primaryReady(t)
	if !hasPrimarySelection() {
		t.Skipf("%s has no primary selection", runtime.GOOS)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	clipboard.Write(clipboard.FmtText, []byte(""), clipboard.FromPrimary())
	ch := clipboard.Watch(ctx, clipboard.FmtText, clipboard.FromPrimary())

	want := []byte("selection changed")
	done := make(chan struct{})
	go func() {
		defer close(done)
		tk := time.NewTicker(300 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				clipboard.Write(clipboard.FmtText, want, clipboard.FromPrimary())
			}
		}
	}()
	defer func() { cancel(); <-done }()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("watch on the primary selection never reported the change")
		case data, ok := <-ch:
			if !ok {
				t.Fatal("primary watch channel closed before reporting the change")
			}
			if bytes.Equal(data.Bytes, want) {
				return
			}
		}
	}
}

// TestPrimarySelectionDegradesSafely covers the platforms without a second
// clipboard. The read returning nil is the small half; the half that matters is
// that a primary write does not fall back to the ordinary clipboard, which would
// silently destroy whatever the user had copied.
func TestPrimarySelectionDegradesSafely(t *testing.T) {
	primaryReady(t)
	if hasPrimarySelection() {
		t.Skipf("%s has a primary selection", runtime.GOOS)
	}

	kept := []byte("what the user copied")
	clipboard.Write(clipboard.FmtText, kept)

	if ch := clipboard.Write(clipboard.FmtText, []byte("must not land"), clipboard.FromPrimary()); ch != nil {
		t.Fatal("Write(..., FromPrimary()) reported success on a platform with no primary selection")
	}
	if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, kept) {
		t.Fatalf("Read(FmtText) = %q, want %q: a primary write must not touch the clipboard", got, kept)
	}
	if got := clipboard.Read(clipboard.FmtText, clipboard.FromPrimary()); got != nil {
		t.Fatalf("Read(FmtText, FromPrimary()) = %q, want nil", got)
	}
	if got := clipboard.Formats(clipboard.FromPrimary()); len(got) != 0 {
		t.Fatalf("Formats(FromPrimary()) = %v, want empty", got)
	}

	// A watch that can never fire must close rather than hang.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch := clipboard.Watch(ctx, clipboard.FmtText, clipboard.FromPrimary())
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("primary watch delivered data on a platform with no primary selection")
		}
	case <-ctx.Done():
		t.Fatal("primary watch neither closed nor delivered; a caller would wait forever")
	}
}

// TestOptionsAreSourceCompatible is a compile-time check that the existing call
// shapes still work now that Format and Item are Options: everything below is
// how the API was called before FromPrimary existed.
func TestOptionsAreSourceCompatible(t *testing.T) {
	primaryReady(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clipboard.Write(clipboard.FmtText, []byte("x"))
	clipboard.WriteAll(clipboard.Item{Format: clipboard.FmtText, Bytes: []byte("x")})
	_ = clipboard.Read(clipboard.FmtText)
	_ = clipboard.Formats()
	_ = clipboard.Watch(ctx, clipboard.FmtText)
	_ = clipboard.Watch(ctx)
}
