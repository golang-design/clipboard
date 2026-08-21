// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard

import (
	"reflect"
	"runtime"
	"testing"
)

// TestURIListRoundTrip covers the conversion layer directly, on every platform:
// it is pure Go, and it is where a file list gets mangled if percent-encoding or
// separators are handled sloppily.
func TestURIListRoundTrip(t *testing.T) {
	var paths []string
	if runtime.GOOS == "windows" {
		paths = []string{
			`C:\dir\a file.txt`,
			`C:\dir\ünïcode.txt`,
			`D:\x\y#z.bin`,
			`C:\dir\100% done.txt`,
		}
	} else {
		paths = []string{
			"/dir/a file.txt",
			"/dir/ünïcode.txt",
			"/x/y#z.bin",
			"/dir/100% done.txt",
		}
	}

	got := pathsFromURIList(uriListFromPaths(paths))
	if !reflect.DeepEqual(got, paths) {
		t.Fatalf("round trip = %q, want %q", got, paths)
	}
}

// TestURIListEncoding pins the wire form: RFC 2483 says CRLF-separated lines,
// and the characters that must be escaped must actually be escaped — a raw
// space or '#' would truncate the list for another application's parser.
func TestURIListEncoding(t *testing.T) {
	p := "/dir/a file#1.txt"
	if runtime.GOOS == "windows" {
		p = `C:\dir\a file#1.txt`
	}
	got := string(uriListFromPaths([]string{p}))

	want := "file:///dir/a%20file%231.txt\r\n"
	if runtime.GOOS == "windows" {
		want = "file:///C:/dir/a%20file%231.txt\r\n"
	}
	if got != want {
		t.Fatalf("uriListFromPaths(%q) = %q, want %q", p, got, want)
	}
}

// TestURIListParsing covers what RFC 2483 requires a reader to tolerate, and
// what this package must refuse: a remote URI is not a local path, so handing
// one back as if it were would be a lie.
func TestURIListParsing(t *testing.T) {
	local := "/a/b.txt"
	wantLocal := []string{"/a/b.txt", "/c.txt"}
	if runtime.GOOS == "windows" {
		local = "/C:/a/b.txt"
		wantLocal = []string{`C:\a\b.txt`, `C:\c.txt`}
	}
	second := "/c.txt"
	if runtime.GOOS == "windows" {
		second = "/C:/c.txt"
	}

	in := "# a comment, per RFC 2483\r\n" +
		"file://" + local + "\r\n" +
		"\r\n" + // blank lines are skipped
		"https://example.com/not-a-file\r\n" +
		"file://localhost" + second + "\r\n" + // localhost authority is local
		"file://remote.example.com/x.txt\r\n" // a remote host is not

	if got := pathsFromURIList([]byte(in)); !reflect.DeepEqual(got, wantLocal) {
		t.Fatalf("pathsFromURIList = %q, want %q", got, wantLocal)
	}
}

// TestURIListRejectsUnusablePaths checks that a path with no unambiguous URI is
// dropped rather than turned into a wrong one.
func TestURIListRejectsUnusablePaths(t *testing.T) {
	for _, p := range []string{"", "relative/path.txt", "./x.txt"} {
		if got := uriListFromPaths([]string{p}); len(got) != 0 {
			t.Fatalf("uriListFromPaths(%q) = %q, want it dropped", p, got)
		}
	}
	if runtime.GOOS == "windows" {
		// A POSIX-rooted path names no Windows file.
		if got := pathsFromURIList([]byte("file:///no-drive/x.txt\r\n")); len(got) != 0 {
			t.Fatalf("pathsFromURIList of a driveless path = %q, want it dropped", got)
		}
	}
}
