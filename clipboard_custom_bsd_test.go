// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build (freebsd || openbsd || netbsd) && !android

package clipboard_test

import "testing"

// The BSDs share the X11 backend with Linux. CI only builds (no X server), so
// this exercises compilation; it runs the round-trip locally on a BSD with X11.
func TestCustomFormatRoundTrip(t *testing.T) { customRoundTrip(t) }
