// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

package clipboard_test

import (
	"os"
	"testing"
)

// TestFormatsEnumerate exercises same-process enumeration on the X11 backend.
// The Wayland data-control backend is covered cross-process by
// clipboard_enumerate_wayland_test.go: a process does not observe its own
// just-set custom selection there (same caveat as the custom-format tests).
func TestFormatsEnumerate(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland enumeration is covered cross-process")
	}
	enumerateRoundTrip(t)
}
