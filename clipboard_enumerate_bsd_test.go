// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build (freebsd || openbsd || netbsd) && !android

package clipboard_test

import "testing"

// The BSDs share the X11 enumeration with Linux. CI only builds (no X server),
// so this exercises compilation; it runs the round-trip locally on a BSD/X11.
func TestFormatsEnumerate(t *testing.T) { enumerateRoundTrip(t) }
