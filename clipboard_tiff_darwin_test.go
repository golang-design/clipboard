// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build darwin && !ios

package clipboard_test

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.design/x/clipboard"
)

// TestReadImageTIFF verifies that reading an image works when the clipboard
// only holds a TIFF representation, which is the default format macOS uses
// for screenshots and the "Copy Image" command in many apps. Reading such a
// clipboard previously returned nil because only PNG data was requested.
func TestReadImageTIFF(t *testing.T) {

	gold, err := os.ReadFile("tests/testdata/clipboard.png")
	if err != nil {
		t.Fatalf("failed to read gold file: %v", err)
	}
	want, err := png.Decode(bytes.NewReader(gold))
	if err != nil {
		t.Fatalf("gold file is not png encoded: %v", err)
	}

	// Convert the gold PNG to a TIFF file using the system sips tool.
	tiff := filepath.Join(t.TempDir(), "clipboard.tiff")
	if out, err := exec.Command("sips", "-s", "format", "tiff",
		"tests/testdata/clipboard.png", "--out", tiff).CombinedOutput(); err != nil {
		t.Skipf("sips unavailable: %v: %s", err, out)
	}

	// Place the TIFF, and only the TIFF, on the pasteboard via AppleScript.
	script := `set the clipboard to (read (POSIX file "` + tiff + `") as TIFF picture)`
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		t.Skipf("osascript unavailable: %v: %s", err, out)
	}

	b, _ := clipboard.Read(context.TODO(), clipboard.FmtImage)
	if b == nil {
		t.Fatal("reading a TIFF-only clipboard returned nil, want PNG data")
	}
	got, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("clipboard image is not png encoded: %v", err)
	}
	if got.Bounds() != want.Bounds() {
		t.Fatalf("decoded image bounds = %v, want %v", got.Bounds(), want.Bounds())
	}
}
