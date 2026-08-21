// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build js && wasm

package clipboard

import (
	"context"
	"errors"
	"strings"
	"syscall/js"
	"testing"
)

// These run under Go's wasm test runner on Node, which has a navigator global
// but no navigator.clipboard — the same shape as a browser serving the page over
// plain HTTP, where the Clipboard API is withheld for want of a secure context.
//
// So this covers the unavailable path, which is the one that used to be a silent
// nil. Behavior against a real navigator.clipboard needs a browser and a
// permission grant, and is not covered here; see specs/web-support.md.

func hasClipboardAPI() bool { return clipboardAPI().Truthy() }

// TestInitReportsMissingClipboardAPI checks Init explains itself. "clipboard
// unavailable" alone would leave a developer guessing; the usual cause is a
// page served over http, so the message has to name it.
func TestInitReportsMissingClipboardAPI(t *testing.T) {
	if hasClipboardAPI() {
		t.Skip("navigator.clipboard exists here; this covers the missing-API path")
	}

	err := initialize()
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("initialize() = %v, want an ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "secure context") {
		t.Fatalf("initialize() = %q, want it to name the secure-context requirement", err)
	}
}

// TestDegradesWithoutClipboardAPI checks reads and writes report the missing API
// instead of panicking on an undefined JS value or returning a bare nil — the
// failure mode this backend waited for the error-returning API to avoid.
func TestDegradesWithoutClipboardAPI(t *testing.T) {
	if hasClipboardAPI() {
		t.Skip("navigator.clipboard exists here; this covers the missing-API path")
	}

	if _, err := read(context.Background(), selClipboard, FmtText); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("read without the Clipboard API = %v, want an ErrUnavailable", err)
	}
	_, err := writeAll(context.Background(), selClipboard,
		[]Item{{Format: FmtText, Bytes: []byte("hi")}}, 0)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("writeAll without the Clipboard API = %v, want an ErrUnavailable", err)
	}
}

// TestUnsupportedFormatsAreNamed checks the backend says what it cannot do
// rather than returning empty. A caller who asked for an image and got nil would
// reasonably conclude the clipboard held no image.
func TestUnsupportedFormatsAreNamed(t *testing.T) {
	for _, f := range []Format{FmtImage, FmtFiles, Register("text/html")} {
		if _, err := read(context.Background(), selClipboard, f); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("read(%v) = %v, want an ErrUnsupported", f, err)
		}
		_, err := writeAll(context.Background(), selClipboard,
			[]Item{{Format: f, Bytes: []byte("x")}}, 0)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("writeAll(%v) = %v, want an ErrUnsupported", f, err)
		}
	}
}

// TestPrimarySelectionUnsupported checks a browser write asking for the primary
// selection is refused rather than redirected to the only clipboard there is,
// which would destroy whatever the user had copied.
func TestPrimarySelectionUnsupported(t *testing.T) {
	if _, err := read(context.Background(), selPrimary, FmtText); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("read(FromPrimary) = %v, want an ErrUnsupported", err)
	}
	_, err := writeAll(context.Background(), selPrimary,
		[]Item{{Format: FmtText, Bytes: []byte("x")}}, 0)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("writeAll(FromPrimary) = %v, want an ErrUnsupported", err)
	}
}

// TestWatchClosesImmediately checks Watch does not leave a caller waiting on a
// channel that can never deliver: browsers have no dependable clipboard-change
// event, so the honest answer is a closed channel.
func TestWatchClosesImmediately(t *testing.T) {
	ch := watch(context.Background(), selClipboard, FmtText)
	if _, ok := <-ch; ok {
		t.Fatal("watch delivered data on a backend with no change event")
	}
}

// TestAwaitHonorsContext checks a cancelled context ends the wait rather than
// hanging on a Promise that never settles — the case a page gets when the
// permission prompt is left unanswered.
func TestAwaitHonorsContext(t *testing.T) {
	// A Promise that is never resolved.
	pending := js.Global().Get("Promise").New(js.FuncOf(func(js.Value, []js.Value) any { return nil }))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := await(ctx, pending); !errors.Is(err, context.Canceled) {
		t.Fatalf("await with a cancelled context = %v, want context.Canceled", err)
	}
}
