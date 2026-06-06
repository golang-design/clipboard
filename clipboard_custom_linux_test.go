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

func TestCustomFormatRoundTrip(t *testing.T) {
	// This PR wires custom formats into the X11 backend only. Under Wayland
	// (e.g. the headless-sway CI job) the data-control backend is used, where
	// custom-format support lands in a separate PR; skip there for now.
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland custom-format support lands in a separate PR")
	}
	customRoundTrip(t)
}
