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

// customRoundTrip writes and reads back a raw custom format, asserting the
// bytes survive verbatim (no encoding or conversion) and that ReadAs decodes
// them. Each platform backend that adopts the custom-format seam wires this
// helper into a build-tagged TestCustomFormatRoundTrip; the helper itself is
// platform-agnostic.
func customRoundTrip(t *testing.T) {
	t.Helper()

	if degradesWithoutCgo() {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}

	f := clipboard.Register("application/x.golang-design.clipboard-test")

	// Raw bytes including a NUL and non-UTF-8 bytes: a passthrough format must
	// preserve them exactly, unlike FmtText (UTF-8) or FmtImage (PNG).
	want := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 'h', 'i', 0x00, 0x80}
	clipboard.Write(f, want)

	// Poll Read briefly: on some backends (notably Wayland data-control) the
	// newly set selection takes a moment to become visible to a fresh reader
	// connection, so an immediate Read can miss it.
	var got []byte
	for i := 0; i < 50; i++ {
		if got = clipboard.Read(f); got != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("custom format round-trip mismatch:\n want %v\n  got %v", want, got)
	}

	// ReadAs decodes the same bytes through a caller-supplied function.
	n, err := clipboard.ReadAs(f, func(b []byte) (int, error) { return len(b), nil })
	if err != nil {
		t.Fatalf("ReadAs returned error: %v", err)
	}
	if n != len(want) {
		t.Fatalf("ReadAs decoded length = %d, want %d", n, len(want))
	}

	// A different, unwritten custom format reads back as nil / ErrNoData.
	other := clipboard.Register("application/x.golang-design.clipboard-test-absent")
	if got := clipboard.Read(other); got != nil {
		t.Fatalf("unwritten custom format should read nil, got %v", got)
	}
}
