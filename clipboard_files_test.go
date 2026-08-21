// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"golang.design/x/clipboard"
)

// filesReady skips when this platform or build cannot exercise a file-list
// round-trip.
func filesReady(t *testing.T) {
	t.Helper()

	skipWithoutClipboard(t)
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland file lists are covered by the cross-process interop test")
	}
	if err := clipboard.Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}
}

// testPaths returns absolute paths exercising the characters that break naive
// path handling: a space, a non-ASCII name, and a nested directory.
func testPaths(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		// t.TempDir may hand back a short (8.3) path; either way it is absolute
		// and drive-qualified, which is what the CF_HDROP round trip needs.
		var err error
		if dir, err = filepath.Abs(dir); err != nil {
			t.Fatalf("abs: %v", err)
		}
	}
	return []string{
		filepath.Join(dir, "a file.txt"),
		filepath.Join(dir, "ünïcode.txt"),
		filepath.Join(dir, "nested", "deep.bin"),
	}
}

// TestWriteReadFiles is the point of #152: a file list copied by this package
// comes back as the same paths, through each platform's own native
// representation (CF_HDROP, NSFilenamesPboardType, text/uri-list).
func TestWriteReadFiles(t *testing.T) {
	filesReady(t)

	want := testPaths(t)
	if ch, _ := clipboard.WriteFiles(context.TODO(), want); ch == nil {
		t.Fatal("WriteFiles reported failure")
	}

	if got, _ := clipboard.ReadFiles(context.TODO()); !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadFiles() = %q, want %q", got, want)
	}
}

// TestFilesFormatIsEnumerated checks the enumeration path, which is separate
// from the read path on every backend: a consumer discovers a file list is
// available through Formats, not by trying to read one.
func TestFilesFormatIsEnumerated(t *testing.T) {
	filesReady(t)

	clipboard.WriteFiles(context.TODO(), testPaths(t))

	formats, err := clipboard.Formats(context.TODO())
	if err != nil {
		t.Fatalf("Formats: %v", err)
	}
	for _, f := range formats {
		if f == clipboard.FmtFiles {
			if got := f.MIME(); got != "text/uri-list" {
				t.Fatalf("FmtFiles.MIME() = %q, want %q", got, "text/uri-list")
			}
			return
		}
	}
	t.Fatalf("Formats() = %v, want it to include FmtFiles after WriteFiles", formats)
}

// TestReadFilesRawBytes checks the portable byte encoding the format is
// documented to carry: Read(FmtFiles) is a text/uri-list body, whatever the
// platform stores underneath.
func TestReadFilesRawBytes(t *testing.T) {
	filesReady(t)

	paths := testPaths(t)
	clipboard.WriteFiles(context.TODO(), paths)

	buf, _ := clipboard.Read(context.TODO(), clipboard.FmtFiles)
	if buf == nil {
		t.Fatal("Read(FmtFiles) returned nil after WriteFiles")
	}
	got := string(buf)
	for _, p := range paths {
		// Every path must appear as a file URI, not as a bare path.
		if !containsFileURI(got, p) {
			t.Fatalf("Read(FmtFiles) = %q, want it to carry %q as a file: URI", got, p)
		}
	}
}

// TestWriteAllWithFiles checks a file list composes with another format in one
// clipboard transaction, which is the case macOS could not serve through the
// per-item file-URL type.
func TestWriteAllWithFiles(t *testing.T) {
	filesReady(t)

	paths := testPaths(t)
	label := []byte("three files")

	// WriteFiles is Write(FmtFiles, ...) over a uri-list body; take that body
	// back out so the same list can go through WriteAll beside another format.
	clipboard.WriteFiles(context.TODO(), paths)
	uris, _ := clipboard.Read(context.TODO(), clipboard.FmtFiles)

	clipboard.WriteAll(context.TODO(),
		clipboard.Item{Format: clipboard.FmtFiles, Bytes: uris},
		clipboard.Item{Format: clipboard.FmtText, Bytes: label},
	)

	if got, _ := clipboard.ReadFiles(context.TODO()); !reflect.DeepEqual(got, paths) {
		t.Fatalf("ReadFiles() = %q, want %q", got, paths)
	}
	if got, _ := clipboard.Read(context.TODO(), clipboard.FmtText); string(got) != string(label) {
		t.Fatalf("Read(FmtText) = %q, want %q", got, label)
	}
}

// containsFileURI reports whether a uri-list body names path. It checks for the
// "file:" scheme explicitly, so a backend handing back bare paths under the
// FmtFiles format — which would break every other platform's consumer — fails
// rather than passing on a substring match.
func containsFileURI(uriList, path string) bool {
	for _, line := range strings.Split(uriList, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "file:") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			continue
		}
		p := u.Path
		if runtime.GOOS == "windows" {
			p = strings.ReplaceAll(strings.TrimPrefix(p, "/"), "/", `\`)
		}
		if p == path {
			return true
		}
	}
	return false
}
