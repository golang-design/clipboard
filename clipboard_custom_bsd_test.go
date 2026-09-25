// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build (freebsd || openbsd || netbsd) && !android

package clipboard_test

import (
	"os"
	"testing"
)

// The BSDs share the X11 and Wayland backends with Linux. CI has no X server on
// a BSD, so the X11 round-trip runs locally on a BSD with X11; under Wayland it
// is covered cross-process instead, as on Linux.
func TestCustomFormatRoundTrip(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland custom formats are covered by the cross-process interop tests")
	}
	customRoundTrip(t)
}
