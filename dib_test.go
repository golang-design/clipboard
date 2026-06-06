// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
	"unsafe"
)

// pixelAt returns the BGRA bytes imageToDIB stored for source pixel (x, y).
func pixelAt(dib []byte, width, height, x, y int) (b, g, r, a byte) {
	offset := int(unsafe.Sizeof(bitmapV5Header{}))
	// imageToDIB writes scanlines bottom-up, so source row y lands at
	// DIB row (height-1-y).
	idx := offset + 4*((height-1-y)*width+x)
	return dib[idx+0], dib[idx+1], dib[idx+2], dib[idx+3]
}

// TestImageToDIB16Bit guards against the 16-bit truncation bug in #48: a
// JPEG decoded then re-encoded to PNG becomes a 16-bit (RGBA64) image, whose
// channels must be down-sampled by taking the high byte. The old code used
// uint8(r), keeping the low byte, which corrupted every such image.
func TestImageToDIB16Bit(t *testing.T) {
	img := image.NewRGBA64(image.Rect(0, 0, 2, 1))
	// Opaque, 16-bit values whose low byte differs from the high byte, so
	// uint8(v) and uint8(v>>8) disagree.
	img.SetRGBA64(0, 0, color.RGBA64{R: 0x1234, G: 0x5678, B: 0x9abc, A: 0xffff})
	img.SetRGBA64(1, 0, color.RGBA64{R: 0xaabb, G: 0xccdd, B: 0xeeff, A: 0xffff})

	dib := imageToDIB(img)

	for _, tc := range []struct {
		x          int
		r, g, b, a byte
	}{
		{0, 0x12, 0x56, 0x9a, 0xff},
		{1, 0xaa, 0xcc, 0xee, 0xff},
	} {
		b, g, r, a := pixelAt(dib, 2, 1, tc.x, 0)
		if r != tc.r || g != tc.g || b != tc.b || a != tc.a {
			t.Errorf("pixel (%d,0): got BGRA=%#x,%#x,%#x,%#x, want R=%#x G=%#x B=%#x A=%#x",
				tc.x, b, g, r, a, tc.r, tc.g, tc.b, tc.a)
		}
	}
}

// TestImageToDIBJPEGPNGPipeline reproduces the exact pipeline from #48: an
// image is encoded to JPEG, decoded (yielding *image.YCbCr), re-encoded to
// PNG (which emits 16-bit truecolor for a non-RGBA color model), then decoded
// again before being written to the clipboard. Every DIB pixel must match the
// high byte of the corresponding decoded pixel; the pre-fix code did not.
func TestImageToDIBJPEGPNGPipeline(t *testing.T) {
	const w, h = 32, 24
	base := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base.Set(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 9), B: 128, A: 255})
		}
	}

	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, base, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	jimg, err := jpeg.Decode(&jb)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := jimg.(*image.YCbCr); !ok {
		t.Fatalf("expected JPEG to decode to *image.YCbCr, got %T", jimg)
	}

	var pb bytes.Buffer
	if err := png.Encode(&pb, jimg); err != nil {
		t.Fatal(err)
	}
	pimg, err := png.Decode(&pb)
	if err != nil {
		t.Fatal(err)
	}

	dib := imageToDIB(pimg)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			wr, wg, wb, wa := pimg.At(x, y).RGBA()
			b, g, r, a := pixelAt(dib, w, h, x, y)
			if r != uint8(wr>>8) || g != uint8(wg>>8) || b != uint8(wb>>8) || a != uint8(wa>>8) {
				t.Fatalf("pixel (%d,%d): got BGRA=%#x,%#x,%#x,%#x, want R=%#x G=%#x B=%#x A=%#x",
					x, y, b, g, r, a, uint8(wr>>8), uint8(wg>>8), uint8(wb>>8), uint8(wa>>8))
			}
		}
	}
}
