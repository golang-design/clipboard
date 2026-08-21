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
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// customRoundTrip writes and reads back several custom formats, asserting the
// bytes survive verbatim (no encoding or conversion) and that ReadAs decodes
// them. It exercises a synthetic edge-case blob as well as real-world fixtures
// (HTML, PDF, DOCX, XLSX, and a PNG used as opaque binary). Each platform
// backend that adopts the custom-format seam wires this helper into a
// build-tagged TestCustomFormatRoundTrip; the helper itself is
// platform-agnostic.
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

	// Real-world fixtures (see tests/testdata/README.md) round-trip verbatim
	// under their canonical MIME types, covering text and several binary office
	// document formats. The PNG bytes are reused under a generic binary MIME to
	// prove custom formats are raw passthrough — distinct from FmtImage, which
	// transcodes.
	fixtures := []struct{ mime, file string }{
		{"text/html", "sample.html"},
		{"application/pdf", "sample.pdf"},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "sample.docx"},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "sample.xlsx"},
		{"application/octet-stream", "clipboard.png"},
	}
	for _, f := range fixtures {
		data, err := os.ReadFile("tests/testdata/" + f.file)
		if err != nil {
			t.Fatalf("read %s: %v", f.file, err)
		}
		customExact(t, f.mime, data)

		// ReadAs decodes the same bytes through a caller-supplied function.
		token := clipboard.Register(f.mime)
		n, err := clipboard.ReadAs(context.TODO(), token, func(b []byte) (int, error) { return len(b), nil })
		if err != nil {
			t.Fatalf("ReadAs(%s): %v", f.mime, err)
		}
		if n != len(data) {
			t.Fatalf("ReadAs(%s) decoded length = %d, want %d", f.mime, n, len(data))
		}
	}

	// A different, unwritten custom format reads back as nil / ErrNoData.
	other := clipboard.Register("application/x.golang-design.clipboard-test-absent")
	if got, _ := clipboard.Read(context.TODO(), other); got != nil {
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
	clipboard.Write(context.TODO(), f, want)

	var got []byte
	for i := 0; i < 50; i++ {
		if got, _ = clipboard.Read(context.TODO(), f); got != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("custom %q round-trip mismatch: want %d bytes, got %d bytes", mime, len(want), len(got))
	}
}
