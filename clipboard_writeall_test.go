// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg" // also registers the JPEG decoder WriteAll normalizes through
	"os"
	"testing"

	"golang.design/x/clipboard"
)

// writeAllReady skips when this platform or build cannot exercise a
// same-process multi-format round-trip.
func writeAllReady(t *testing.T) {
	t.Helper()

	if degradesWithoutCgo() {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}
	// Under data-control a client does not observe its own just-set selection,
	// so Wayland is covered cross-process instead (clipboard_writeall_wayland_test.go).
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland multi-format writes are covered by the cross-process interop test")
	}
	if err := clipboard.Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}
}

// TestWriteAll is the point of #151: two representations of the same content
// reach the clipboard together, so a consumer can take the richer one.
//
// TestWriteReplacesPreviousWrite below pins why this needs its own call — doing
// it with two Write calls loses the first.
func TestWriteAll(t *testing.T) {
	writeAllReady(t)

	html := clipboard.Register("text/html")
	markup := []byte("<b>hi</b>")
	plain := []byte("hi")

	if ch := clipboard.WriteAll(
		clipboard.Item{Format: html, Bytes: markup},
		clipboard.Item{Format: clipboard.FmtText, Bytes: plain},
	); ch == nil {
		t.Fatal("WriteAll reported failure")
	}

	if got := clipboard.Read(html); !bytes.Equal(got, markup) {
		t.Fatalf(`Read(Register("text/html")) = %q, want %q`, got, markup)
	}
	if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, plain) {
		t.Fatalf("Read(FmtText) = %q, want %q", got, plain)
	}
}

// TestWriteReplacesPreviousWrite states the behavior WriteAll exists to work
// around, and keeps it true: a Write replaces the whole clipboard, so writing
// two formats in turn leaves only the last. If this ever stops holding, the
// assertion in TestWriteAll no longer proves anything about atomicity.
func TestWriteReplacesPreviousWrite(t *testing.T) {
	writeAllReady(t)

	html := clipboard.Register("text/html")
	clipboard.Write(html, []byte("<b>dropped</b>"))
	clipboard.Write(clipboard.FmtText, []byte("survives"))

	if got := clipboard.Read(html); got != nil {
		t.Fatalf(`Read(Register("text/html")) = %q after a later Write, want nil: `+
			`a single Write is supposed to replace the whole clipboard`, got)
	}
	if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, []byte("survives")) {
		t.Fatalf("Read(FmtText) = %q, want %q", got, "survives")
	}
}

// TestWriteAllOrderIsPreference checks the duplicate rule: order is preference,
// so the first occurrence of a format wins and a later one does not quietly
// overwrite it.
func TestWriteAllOrderIsPreference(t *testing.T) {
	writeAllReady(t)

	clipboard.WriteAll(
		clipboard.Item{Format: clipboard.FmtText, Bytes: []byte("preferred")},
		clipboard.Item{Format: clipboard.FmtText, Bytes: []byte("ignored")},
	)

	if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, []byte("preferred")) {
		t.Fatalf("Read(FmtText) = %q, want %q (the earlier item wins)", got, "preferred")
	}
}

// TestWriteAllNormalizesImages checks that an image item goes through the same
// PNG normalization Write applies, per item and alongside other formats.
func TestWriteAllNormalizesImages(t *testing.T) {
	writeAllReady(t)

	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			src.Set(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 64, A: 255})
		}
	}
	var jbuf bytes.Buffer
	if err := jpeg.Encode(&jbuf, src, nil); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}

	plain := []byte("a picture")
	clipboard.WriteAll(
		clipboard.Item{Format: clipboard.FmtImage, Bytes: jbuf.Bytes()},
		clipboard.Item{Format: clipboard.FmtText, Bytes: plain},
	)

	got := clipboard.Read(clipboard.FmtImage)
	if len(got) < 8 || !bytes.Equal(got[:8], []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("Read(FmtImage) did not return PNG-encoded bytes (got %d bytes, prefix %v)",
			len(got), got[:min(8, len(got))])
	}
	if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, plain) {
		t.Fatalf("Read(FmtText) = %q, want %q", got, plain)
	}
}

// TestWriteAllNoItems checks the empty call is a no-op rather than a clipboard
// wipe: there is nothing to publish, so nothing is published.
func TestWriteAllNoItems(t *testing.T) {
	writeAllReady(t)

	kept := []byte("keep me")
	clipboard.Write(clipboard.FmtText, kept)

	if ch := clipboard.WriteAll(); ch != nil {
		t.Fatal("WriteAll() with no items returned a channel, want nil")
	}
	if got := clipboard.Read(clipboard.FmtText); !bytes.Equal(got, kept) {
		t.Fatalf("Read(FmtText) = %q after WriteAll(), want the clipboard untouched (%q)", got, kept)
	}
}
