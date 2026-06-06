// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

package clipboard_test

import "testing"

// Custom formats are supported on both the X11 and Wayland (data-control)
// Linux backends, so this runs on the X11 ubuntu job and the headless-sway
// wayland job alike.
func TestCustomFormatRoundTrip(t *testing.T) { customRoundTrip(t) }
