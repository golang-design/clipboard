// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

// NOTE: FreeBSD and OpenBSD are verified to build in CI. NetBSD shares
// the same X11 implementation and is included on a best-effort basis, but
// it is not covered by CI and has not been fully tested — the X11 header
// prefix and runtime behavior there are unverified.

//go:build (openbsd || freebsd || netbsd) && !android

package clipboard

/*
// X11 headers live in different prefixes across the BSDs: OpenBSD ships
// them in base under /usr/X11R6, FreeBSD installs the libX11 port under
// /usr/local, and NetBSD uses /usr/X11R7 (base) or /usr/pkg (pkgsrc).
// List them all; the compiler ignores include paths that do not exist.
#cgo CFLAGS: -I/usr/X11R6/include -I/usr/local/include -I/usr/X11R7/include -I/usr/pkg/include
#include <stdlib.h>
#include <stdio.h>
#include <stdint.h>
#include <string.h>

int clipboard_test();
int clipboard_write(
	char*          typ,
	unsigned char* buf,
	size_t         n,
	uintptr_t      handle
);
unsigned long clipboard_read(char* typ, char **out);
*/
import "C"

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/cgo"
	"time"
	"unsafe"
)

var helpmsg = `%w: Failed to initialize the X11 display, and the clipboard package
will not work properly. Install the following dependency may help:

	apt install -y libx11-dev

If the clipboard package is in an environment without a frame buffer,
such as a cloud server, it may also be necessary to install xvfb:

	apt install -y xvfb

and initialize a virtual frame buffer:

	Xvfb :99 -screen 0 1024x768x24 > /dev/null 2>&1 &
	export DISPLAY=:99.0

Then this package should be ready to use.
`

func initialize() error {
	ok := C.clipboard_test()
	if ok != 0 {
		return fmt.Errorf(helpmsg, errUnavailable)
	}
	return nil
}

func read(t Format) (buf []byte, err error) {
	switch t {
	case FmtText:
		return readc("UTF8_STRING")
	case FmtImage:
		return readc("image/png")
	}
	return nil, errUnsupported
}

func readc(t string) ([]byte, error) {
	ct := C.CString(t)
	defer C.free(unsafe.Pointer(ct))

	var data *C.char
	n := C.clipboard_read(ct, &data)
	switch C.long(n) {
	case -1:
		return nil, errUnavailable
	case -2:
		return nil, errUnsupported
	}
	if data == nil {
		return nil, errUnavailable
	}
	defer C.free(unsafe.Pointer(data))
	switch {
	case n == 0:
		return nil, nil
	default:
		return C.GoBytes(unsafe.Pointer(data), C.int(n)), nil
	}
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
func write(t Format, buf []byte) (<-chan struct{}, error) {
	var s string
	switch t {
	case FmtText:
		s = "UTF8_STRING"
	case FmtImage:
		s = "image/png"
	}

	start := make(chan int)
	done := make(chan struct{}, 1)

	go func() { // serve as a daemon until the ownership is terminated.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		cs := C.CString(s)
		defer C.free(unsafe.Pointer(cs))

		h := cgo.NewHandle(start)
		var ok C.int
		if len(buf) == 0 {
			ok = C.clipboard_write(cs, nil, 0, C.uintptr_t(h))
		} else {
			ok = C.clipboard_write(cs, (*C.uchar)(unsafe.Pointer(&(buf[0]))), C.size_t(len(buf)), C.uintptr_t(h))
		}
		if ok != C.int(0) {
			fmt.Fprintf(os.Stderr, "write failed with status: %d\n", int(ok))
		}
		done <- struct{}{}
		close(done)
	}()

	status := <-start
	if status < 0 {
		return nil, errUnavailable
	}
	// wait until enter event loop
	return done, nil
}

func watch(ctx context.Context, t Format) <-chan []byte {
	recv := make(chan []byte, 1)
	ti := time.NewTicker(time.Second)
	last := Read(t)
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(recv)
				return
			case <-ti.C:
				b := Read(t)
				if b == nil {
					continue
				}
				if !bytes.Equal(last, b) {
					recv <- b
					last = b
				}
			}
		}
	}()
	return recv
}

//export syncStatus
func syncStatus(h uintptr, val int) {
	v := cgo.Handle(h).Value().(chan int)
	v <- val
	cgo.Handle(h).Delete()
}
