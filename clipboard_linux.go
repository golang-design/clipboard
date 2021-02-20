// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

// +build linux
//go:build linux

package clipboard

/*
#cgo LDFLAGS: -lX11
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <stdatomic.h>

int clipboard_test();
int clipboard_write(
	char*          typ,
	unsigned char* buf,
	size_t         n,
	int*           start // FIXME: should use atomic
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
	"time"
	"unsafe"
)

func init() {
	ok := C.clipboard_test()
	if ok < 0 {
		panic(`cannot use this package, failed to initialize x11 display, maybe try install:

	apt install -y libx11-dev
`)
	}
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
	if data == nil {
		return nil, errUnavailable
	}
	defer C.free(unsafe.Pointer(data))
	switch {
	case n < 0:
		return nil, errUnavailable
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

	var start C.int
	done := make(chan struct{}, 1)

	go func() { // surve as a daemon until the ownership is terminated.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		cs := C.CString(s)
		defer C.free(unsafe.Pointer(cs))

		var ok C.int
		if len(buf) == 0 {
			ok = C.clipboard_write(cs, nil, 0, &start)
		} else {
			ok = C.clipboard_write(cs, (*C.uchar)(unsafe.Pointer(&(buf[0]))), C.size_t(len(buf)), &start)
		}
		if ok != C.int(0) {
			fmt.Fprintf(os.Stderr, "write failed with status: %d\n", int(ok))
		}
		done <- struct{}{}
		close(done)
	}()

	// FIXME: this should race with the code on the C side, start
	// should use an atomic version, and use atomic_load.
	for start == 0 {
	}

	if start < 0 {
		return nil, errInvalidOperation
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
				if bytes.Compare(last, b) != 0 {
					recv <- b
					last = b
				}
			}
		}
	}()
	return recv
}
