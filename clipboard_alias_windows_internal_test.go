// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard

import (
	"bytes"
	"context"
	"runtime"
	"testing"
)

// TestWindowsFormatNames asserts the MIME → native name table is applied on the
// outbound path, and that every alias round-trips back to its MIME type.
func TestWindowsFormatNames(t *testing.T) {
	if got := windowsFormatNames("image/png"); len(got) != 2 || got[0] != "PNG" || got[1] != "image/png" {
		t.Fatalf(`windowsFormatNames("image/png") = %v, want ["PNG" "image/png"]`, got)
	}

	// A MIME type with no alias is used verbatim, as before.
	const raw = "application/x.golang-design.clipboard-test"
	if got := windowsFormatNames(raw); len(got) != 1 || got[0] != raw {
		t.Fatalf("windowsFormatNames(%q) = %v, want [%q]", raw, got, raw)
	}

	for mime, native := range windowsNativeNames {
		if got := windowsFormatNames(mime)[0]; got != native {
			t.Fatalf("windowsFormatNames(%q)[0] = %q, want %q", mime, got, native)
		}
		if got, ok := windowsMIMEForName(native); !ok || got != mime {
			t.Fatalf("windowsMIMEForName(%q) = (%q, %v), want (%q, true)", native, got, ok, mime)
		}
	}

	// RegisterClipboardFormat compares names case-insensitively, so the reverse
	// lookup must too: the name another app registered first wins.
	if got, ok := windowsMIMEForName("png"); !ok || got != "image/png" {
		t.Fatalf(`windowsMIMEForName("png") = (%q, %v), want ("image/png", true)`, got, ok)
	}
	if _, ok := windowsMIMEForName("TARGETS"); ok {
		t.Fatal(`windowsMIMEForName("TARGETS") reported an alias, want none`)
	}
}

// TestWindowsFormatForName asserts the inbound path is the inverse of the
// outbound one: an enumerated format resolves to the token whose Read looks at
// the same clipboard data.
func TestWindowsFormatForName(t *testing.T) {
	png := Register("image/png")
	for _, name := range []string{"PNG", "image/png"} {
		f, ok := windowsFormatForName(name)
		if !ok || f != png {
			t.Fatalf("windowsFormatForName(%q) = (%v, %v), want (%v, true)", name, f, ok, png)
		}
	}
	if f, ok := windowsFormatForName("UTF8_STRING"); !ok || f != FmtText {
		t.Fatalf(`windowsFormatForName("UTF8_STRING") = (%v, %v), want (FmtText, true)`, f, ok)
	}
	// A registered name that is neither an alias nor MIME-shaped is not a
	// format this package can serve, so it is skipped rather than reported.
	for _, name := range []string{"", "FileNameW", "HTML Format"} {
		if f, ok := windowsFormatForName(name); ok {
			t.Fatalf("windowsFormatForName(%q) = (%v, true), want it to be skipped", name, f)
		}
	}
}

// TestWindowsForeignFormatIsReadable is the #160 repro without a second
// application: PNG bytes published under the registered format name "PNG" (what
// Chromium, Firefox, Office and Snip & Sketch write) must be reachable through
// Register("image/png") and reported by Formats.
func TestWindowsForeignFormatIsReadable(t *testing.T) {
	want := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01, 0xff}
	writeFormatName(t, "PNG", want)

	png := Register("image/png")
	if got, _ := Read(context.TODO(), png); !bytes.Equal(got, want) {
		t.Fatalf(`Read(Register("image/png")) = %v, want %v`, got, want)
	}
	found := false
	formats, _ := Formats(context.TODO())
	for _, f := range formats {
		if f == png {
			found = true
		}
	}
	if !found {
		t.Fatalf(`Formats() = %v, want it to include the "image/png" token %v`, formats, png)
	}
}

// writeFormatName publishes buf under a registered clipboard format name,
// standing in for another application that publishes that name.
func writeFormatName(t *testing.T, name string, buf []byte) {
	t.Helper()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	id, err := registerFormatName(name)
	if err != nil {
		t.Fatalf("failed to register format %q: %v", name, err)
	}
	if err := openClipboardRetry(); err != nil {
		t.Fatalf("failed to open clipboard: %v", err)
	}
	defer closeClipboard.Call()
	if err := clearClipboard(); err != nil {
		t.Fatalf("failed to clear clipboard: %v", err)
	}
	if err := writeCustom(id, buf); err != nil {
		t.Fatalf("failed to write format %q: %v", name, err)
	}
}
