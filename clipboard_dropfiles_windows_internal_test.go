// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard

import (
	"encoding/binary"
	"reflect"
	"testing"
)

// TestDropFilesLayout pins the exact bytes of a CF_HDROP payload. This needs a
// test of its own because a wrong header field does not fail: another
// application parses the struct by offset and quietly reads nothing, or the
// wrong thing, and a round-trip test through this package's own decoder would
// agree with itself either way.
func TestDropFilesLayout(t *testing.T) {
	got := dropFilesFromPaths([]string{`C:\a.txt`, `D:\b.txt`})

	if n := binary.LittleEndian.Uint32(got[0:]); n != dropFilesHeaderSize {
		t.Fatalf("pFiles = %d, want %d: the path list must start right after the header", n, dropFilesHeaderSize)
	}
	if x, y := binary.LittleEndian.Uint32(got[4:]), binary.LittleEndian.Uint32(got[8:]); x != 0 || y != 0 {
		t.Fatalf("pt = (%d, %d), want (0, 0) for a clipboard copy", x, y)
	}
	if n := binary.LittleEndian.Uint32(got[12:]); n != 0 {
		t.Fatalf("fNC = %d, want 0", n)
	}
	if n := binary.LittleEndian.Uint32(got[16:]); n != 1 {
		t.Fatalf("fWide = %d, want 1: the paths are UTF-16", n)
	}

	// Each path is NUL-terminated and the list is closed by a second NUL, so a
	// two-path list ends in two zero units.
	want := []uint16{'C', ':', '\\', 'a', '.', 't', 'x', 't', 0,
		'D', ':', '\\', 'b', '.', 't', 'x', 't', 0, 0}
	list := got[dropFilesHeaderSize:]
	units := make([]uint16, 0, len(list)/2)
	for i := 0; i+1 < len(list); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(list[i:]))
	}
	if !reflect.DeepEqual(units, want) {
		t.Fatalf("path list = %v, want %v", units, want)
	}
}

// TestDropFilesDecodeForeign checks the decoder reads a payload laid out the way
// another application may write it rather than the way this package does: a
// header with trailing padding it must skip via pFiles, and the ANSI variant.
func TestDropFilesDecodeForeign(t *testing.T) {
	// A wide list whose paths start past the standard header.
	const pad = 8
	units := []uint16{'C', ':', '\\', 'x', 0, 0}
	buf := make([]byte, dropFilesHeaderSize+pad+len(units)*2)
	binary.LittleEndian.PutUint32(buf[0:], dropFilesHeaderSize+pad)
	binary.LittleEndian.PutUint32(buf[16:], 1)
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[dropFilesHeaderSize+pad+i*2:], u)
	}
	if got := pathsFromDropFiles(buf); !reflect.DeepEqual(got, []string{`C:\x`}) {
		t.Fatalf("pathsFromDropFiles with a padded header = %q, want [%q]", got, `C:\x`)
	}

	// The ANSI variant: fWide is 0 and the paths are bytes.
	ansi := append(make([]byte, dropFilesHeaderSize), []byte("C:\\y\x00\x00")...)
	binary.LittleEndian.PutUint32(ansi[0:], dropFilesHeaderSize)
	if got := pathsFromDropFiles(ansi); !reflect.DeepEqual(got, []string{`C:\y`}) {
		t.Fatalf("pathsFromDropFiles of an ANSI list = %q, want [%q]", got, `C:\y`)
	}
}

// TestDropFilesRejectsMalformed checks a truncated or nonsensical payload is
// refused rather than read out of bounds.
func TestDropFilesRejectsMalformed(t *testing.T) {
	for name, buf := range map[string][]byte{
		"empty":           nil,
		"short header":    make([]byte, dropFilesHeaderSize-1),
		"offset past end": dropFilesOffset(t, 1<<20),
		"offset inside":   dropFilesOffset(t, 4),
	} {
		if got := pathsFromDropFiles(buf); got != nil {
			t.Fatalf("pathsFromDropFiles(%s) = %q, want nil", name, got)
		}
	}
}

func dropFilesOffset(t *testing.T, off uint32) []byte {
	t.Helper()
	buf := make([]byte, dropFilesHeaderSize+4)
	binary.LittleEndian.PutUint32(buf[0:], off)
	binary.LittleEndian.PutUint32(buf[16:], 1)
	return buf
}
