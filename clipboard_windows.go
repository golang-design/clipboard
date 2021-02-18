// Copyright 2021 The golang.design Initiative authors.
// All rights reserved. Use of this source code is governed
// by a GNU GPL-3 license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows
// +build windows

package clipboard

func read(t Format) (buf []byte) {
	panic("unsupported")
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
func write(t Format, buf []byte) (bool, <-chan struct{}) {
	panic("unsupported")
}
