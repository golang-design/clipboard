// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard

import (
	"bytes"
	"image/png"
	"os"
	"runtime"
	"testing"
	"unsafe"
)

// TestRead24BitDIB puts a real 24-bit DIB on the clipboard (as CF_DIB, the way
// many Windows apps copy non-alpha bitmaps) and verifies Read(FmtImage) decodes
// it. This is the scenario PR #65 set out to support; the test answers, via CI,
// whether the current backend already handles it.
func TestRead24BitDIB(t *testing.T) {
	raw, err := os.ReadFile("tests/testdata/image24bit.bmp")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// A CF_DIB handle is a BMP without the 14-byte BITMAPFILEHEADER.
	dib := raw[14:]
	setClipboardDIB(t, dib)

	out := Read(FmtImage)
	if out == nil {
		t.Fatal("Read(FmtImage) returned nil for a 24-bit DIB on the clipboard")
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("Read(FmtImage) did not return PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 8 || b.Dy() != 8 {
		t.Fatalf("decoded image is %dx%d, want 8x8", b.Dx(), b.Dy())
	}
	// The fixture's pattern is R=x*32, G=y*32, B=128. Spot-check two corners
	// (8-bit channels, small tolerance for any conversion rounding).
	check := func(x, y int, wantR, wantG, wantB uint8) {
		r, g, b, _ := img.At(x, y).RGBA()
		gotR, gotG, gotB := uint8(r>>8), uint8(g>>8), uint8(b>>8)
		near := func(a, b uint8) bool { d := int(a) - int(b); return d >= -1 && d <= 1 }
		if !near(gotR, wantR) || !near(gotG, wantG) || !near(gotB, wantB) {
			t.Fatalf("pixel (%d,%d) = (%d,%d,%d), want ~(%d,%d,%d)", x, y, gotR, gotG, gotB, wantR, wantG, wantB)
		}
	}
	check(0, 0, 0, 0, 128)
	check(7, 0, 7*32, 0, 128)
}

// setClipboardDIB stores raw DIB bytes on the clipboard as CF_DIB.
func setClipboardDIB(t *testing.T, dib []byte) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for {
		r, _, _ := openClipboard.Call(0)
		if r != 0 {
			break
		}
	}
	defer closeClipboard.Call()
	if r, _, err := emptyClipboard.Call(); r == 0 {
		t.Fatalf("EmptyClipboard: %v", err)
	}
	hMem, _, err := gAlloc.Call(gmemMoveable, uintptr(len(dib)))
	if hMem == 0 {
		t.Fatalf("GlobalAlloc: %v", err)
	}
	p, _, err := gLock.Call(hMem)
	if p == 0 {
		gFree.Call(hMem)
		t.Fatalf("GlobalLock: %v", err)
	}
	memMove.Call(p, uintptr(unsafe.Pointer(&dib[0])), uintptr(len(dib)))
	gUnlock.Call(hMem)
	if v, _, err := setClipboardData.Call(cFmtDIB, hMem); v == 0 {
		gFree.Call(hMem)
		t.Fatalf("SetClipboardData(CF_DIB): %v", err)
	}
}
