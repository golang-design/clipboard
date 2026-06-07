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
	"image/jpeg" // also registers the JPEG decoder used by Write's normalization
	"image/png"
	"os"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// TestWriteImageAcceptsJPEG verifies Write(FmtImage, ...) accepts a non-PNG
// image (JPEG here) when its decoder is registered, normalizes it to PNG, and
// serves PNG back (#155). Without normalization the raw JPEG bytes would be
// stored under the image format and Read would return non-PNG data.
func TestWriteImageAcceptsJPEG(t *testing.T) {
	if degradesWithoutCgo() {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}
	// Ensure the backend is initialized (selects Wayland vs X11); this test may
	// run before TestClipboardInit, and Write/Read need the backend resolved.
	if err := clipboard.Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}

	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 64, A: 255})
		}
	}
	var jbuf bytes.Buffer
	if err := jpeg.Encode(&jbuf, src, nil); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}

	clipboard.Write(clipboard.FmtImage, jbuf.Bytes())

	// Poll Read: on some backends (Wayland data-control) a freshly set
	// selection takes a moment to be visible to a new reader connection.
	var got []byte
	for i := 0; i < 50; i++ {
		if got = clipboard.Read(clipboard.FmtImage); got != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got == nil {
		t.Fatal("Read(FmtImage) returned nil after writing a JPEG")
	}
	if !bytes.HasPrefix(got, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("clipboard image is not PNG-encoded (Write should normalize JPEG to PNG)")
	}
	img, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decode clipboard PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 8 || b.Dy() != 8 {
		t.Fatalf("decoded image is %dx%d, want 8x8", b.Dx(), b.Dy())
	}
}
