// Copyright 2021 The golang.design Initiative authors.
// All rights reserved. Use of this source code is governed
// by a GNU GPL-3 license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

// +build !linux,darwin

package clipboard

func readAll() (buf []byte) {
	panic("unsupported")
}

func writeAll(buf []byte) {
	panic("unsupported")
}
