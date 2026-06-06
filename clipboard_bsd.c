// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build (openbsd || freebsd || netbsd) && !android

// The BSD X11 clipboard uses the shared cgo X11 implementation in
// clipboard_x11.c, included textually here. Linux dropped cgo for a pure-Go
// X11 backend; the BSDs still use this cgo path. The only per-platform
// differences are the cgo flags in clipboard_bsd.go (the BSDs ship dlopen in
// libc and need the X11 include path).
#include "clipboard_x11.c"
