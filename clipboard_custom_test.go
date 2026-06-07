// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// customRoundTrip writes and reads back several custom formats, asserting the
// bytes survive verbatim (no encoding or conversion) and that ReadAs decodes
// them. It exercises a synthetic edge-case blob as well as real-world fixtures
// (a text/html document and a binary PNG). Each platform backend that adopts
// the custom-format seam wires this helper into a build-tagged
// TestCustomFormatRoundTrip; the helper itself is platform-agnostic.
func customRoundTrip(t *testing.T) {
	t.Helper()

	if degradesWithoutCgo() {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}

	// Raw bytes including a NUL and non-UTF-8 bytes: a passthrough format must
	// preserve them exactly, unlike FmtText (UTF-8) or FmtImage (PNG).
	customExact(t, "application/x.golang-design.clipboard-test",
		[]byte{0x00, 0x01, 0x02, 0xff, 0xfe, 'h', 'i', 0x00, 0x80})

	// A real text/html document (vendored from example.com) round-trips
	// verbatim — realistic, non-trivial content for the canonical rich-text
	// custom format.
	html, err := os.ReadFile("tests/testdata/sample.html")
	if err != nil {
		t.Fatalf("read sample.html: %v", err)
	}
	customExact(t, "text/html", html)

	// A real binary payload (the test PNG bytes) under a non-image MIME proves
	// custom formats are raw passthrough — distinct from FmtImage, which
	// transcodes. This is also a larger (~21 KB) transfer than the blob above.
	bin, err := os.ReadFile("tests/testdata/clipboard.png")
	if err != nil {
		t.Fatalf("read clipboard.png: %v", err)
	}
	customExact(t, "application/octet-stream", bin)

	// ReadAs decodes the format last written above through a caller function.
	f := clipboard.Register("application/octet-stream")
	n, err := clipboard.ReadAs(f, func(b []byte) (int, error) { return len(b), nil })
	if err != nil {
		t.Fatalf("ReadAs returned error: %v", err)
	}
	if n != len(bin) {
		t.Fatalf("ReadAs decoded length = %d, want %d", n, len(bin))
	}

	// A different, unwritten custom format reads back as nil / ErrNoData.
	other := clipboard.Register("application/x.golang-design.clipboard-test-absent")
	if got := clipboard.Read(other); got != nil {
		t.Fatalf("unwritten custom format should read nil, got %v", got)
	}
}

// customExact registers mime, writes want, and asserts the bytes read back are
// identical. It polls Read briefly because on some backends (notably Wayland
// data-control) a freshly set selection takes a moment to become visible to a
// new reader connection, so an immediate Read can miss it.
func customExact(t *testing.T, mime string, want []byte) {
	t.Helper()

	f := clipboard.Register(mime)
	clipboard.Write(f, want)

	var got []byte
	for i := 0; i < 50; i++ {
		if got = clipboard.Read(f); got != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("custom %q round-trip mismatch: want %d bytes, got %d bytes", mime, len(want), len(got))
	}
}
