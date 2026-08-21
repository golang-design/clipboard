// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard

import (
	"net/url"
	"runtime"
	"strings"
)

// The portable byte encoding of FmtFiles is text/uri-list (RFC 2483): file
// URIs, one per line, separated by CRLF. Lines beginning with '#' are comments.
// It is the native form on X11 and Wayland, and a published standard rather
// than an invention, so it is what Read(FmtFiles) hands back everywhere and
// what backends convert to and from.

// uriListFromPaths encodes file paths as a text/uri-list body. Paths that
// cannot be expressed as a file URI are dropped rather than guessed at.
func uriListFromPaths(paths []string) []byte {
	var b strings.Builder
	for _, p := range paths {
		uri, ok := fileURIFromPath(p)
		if !ok {
			continue
		}
		b.WriteString(uri)
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// pathsFromURIList decodes a text/uri-list body into file paths, skipping blank
// lines, comments, and any URI that does not name a local file.
func pathsFromURIList(buf []byte) []string {
	var out []string
	for _, line := range strings.Split(string(buf), "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if p, ok := pathFromFileURI(line); ok {
			out = append(out, p)
		}
	}
	return out
}

// fileURIFromPath encodes an absolute path as a file URI. On Windows the
// separators are flipped and the drive letter is preceded by the empty root, so
// C:\dir\name becomes file:///C:/dir/name.
func fileURIFromPath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	if runtime.GOOS == "windows" {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	if !strings.HasPrefix(p, "/") {
		// A Windows path starts at its drive letter; anything else absolute
		// starts at the root. A relative path has no unambiguous URI.
		if !hasDriveLetter(p) {
			return "", false
		}
		p = "/" + p
	}
	// url.URL escapes the path correctly for the file scheme, including spaces
	// and non-ASCII characters, and leaves the separators alone.
	u := url.URL{Scheme: "file", Path: p}
	return u.String(), true
}

// pathFromFileURI decodes a file URI into a local path. Only the file scheme
// with an empty or localhost authority names something this package can hand
// back as a path; a remote URI does not.
func pathFromFileURI(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return "", false
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "", false
	}
	p := u.Path // already percent-decoded by url.Parse
	if p == "" {
		return "", false
	}
	if runtime.GOOS == "windows" {
		p = strings.TrimPrefix(p, "/")
		if !hasDriveLetter(p) {
			return "", false
		}
		p = strings.ReplaceAll(p, "/", `\`)
	}
	return p, true
}

// hasDriveLetter reports whether p starts with a Windows drive specification
// such as "C:".
func hasDriveLetter(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}
