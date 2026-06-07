// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build darwin && !ios

package clipboard_test

import "testing"

func TestFormatsEnumerate(t *testing.T) { enumerateRoundTrip(t) }
