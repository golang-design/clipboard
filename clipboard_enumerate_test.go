// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"os"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// enumerateRoundTrip writes known formats and asserts Formats() reports them
// with the right MIME identity. Platform backends that implement enumeration
// wire this into a build-tagged TestFormatsEnumerate; the helper itself is
// platform-agnostic.
func enumerateRoundTrip(t *testing.T) {
	t.Helper()

	if degradesWithoutCgo() {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}

	// A built-in format is reported after it is written.
	clipboard.Write(clipboard.FmtText, []byte("enumerate-text"))
	if fs := waitForFormat(clipboard.FmtText); !containsFormat(fs, clipboard.FmtText) {
		t.Fatalf("Formats() after writing FmtText = %v, want it to include FmtText", fs)
	}

	// A custom MIME type is discovered and reported, and the returned token
	// carries its MIME identity.
	html := clipboard.Register("text/html")
	clipboard.Write(html, []byte("<b>enumerate</b>"))
	fs := waitForFormat(html)
	if !containsFormat(fs, html) {
		t.Fatalf("Formats() after writing text/html = %v, want it to include the html token", fs)
	}
	if html.MIME() != "text/html" {
		t.Fatalf("html token MIME() = %q, want text/html", html.MIME())
	}
}

func containsFormat(fs []clipboard.Format, want clipboard.Format) bool {
	for _, f := range fs {
		if f == want {
			return true
		}
	}
	return false
}

// waitForFormat polls Formats() until it includes want (or a timeout), absorbing
// any brief delay between a write taking effect and a fresh enumeration seeing
// it.
func waitForFormat(want clipboard.Format) []clipboard.Format {
	var fs []clipboard.Format
	for i := 0; i < 50; i++ {
		if fs = clipboard.Formats(); containsFormat(fs, want) {
			return fs
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fs
}
