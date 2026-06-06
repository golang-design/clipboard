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

// TestCustomFormatRoundTrip exercises the same-process write→read round-trip on
// the X11 backend. The Wayland data-control backend is covered separately by
// the cross-process interop tests (clipboard_custom_wayland_test.go): under
// data-control a client does not observe its own just-set custom selection from
// a fresh reader connection, so the real use case (exchanging custom data with
// other apps) is what we verify there.
func TestCustomFormatRoundTrip(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland custom formats are covered by the cross-process interop tests")
	}
	customRoundTrip(t)
}
