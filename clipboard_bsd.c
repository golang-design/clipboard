// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build (openbsd || freebsd || netbsd) && !android

// The BSD X11 clipboard implementation is identical to the Linux one.
// Include it directly so the two never drift apart: any fix to the X11
// logic in clipboard_linux.c applies to the BSDs automatically. The only
// per-platform differences are the cgo flags, which live in the
// respective clipboard_linux.go / clipboard_bsd.go files (Linux links
// -ldl for dlopen, the BSDs ship it in libc and need the X11 include
// path instead).
#include "clipboard_linux.c"
