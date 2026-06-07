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

func TestFormatsEnumerate(t *testing.T) {
	// This PR implements enumeration on the X11 backend. Wayland enumeration
	// lands in a follow-up PR; skip there for now.
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("Wayland enumeration lands in a separate PR")
	}
	enumerateRoundTrip(t)
}
