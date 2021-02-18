// Copyright 2021 The golang.design Initiative authors.
// All rights reserved. Use of this source code is governed
// by a GNU GPL-3 license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build darwin
// +build darwin

package clipboard

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework Cocoa
#import <Foundation/Foundation.h>
#import <Cocoa/Cocoa.h>

unsigned int clipboard_read_string(void **out);
unsigned int clipboard_read_image(void **out);
int clipboard_write_string(const void *bytes, NSInteger n);
int clipboard_write_image(const void *bytes, NSInteger n);
NSInteger clipboard_change_count();
*/
import "C"
import (
	"time"
	"unsafe"
)

func read(t Format) (buf []byte) {
	var (
		data unsafe.Pointer
		n    C.uint
	)
	switch t {
	case FmtText:
		n = C.clipboard_read_string(&data)
	case FmtImage:
		n = C.clipboard_read_image(&data)
	}
	if data == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(data))
	if n == 0 {
		return nil
	}
	return C.GoBytes(data, C.int(n))
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
func write(t Format, buf []byte) (bool, <-chan struct{}) {
	var ok C.int
	switch t {
	case FmtText:
		ok = C.clipboard_write_string(unsafe.Pointer(&buf[0]),
			C.NSInteger(len(buf)))
	case FmtImage:
		ok = C.clipboard_write_image(unsafe.Pointer(&buf[0]),
			C.NSInteger(len(buf)))
	}
	if ok != 0 {
		return false, nil
	}

	// use unbuffered data to prevent goroutine leak
	changed := make(chan struct{}, 1)
	cnt := C.long(C.clipboard_change_count())
	go func() {
		for {
			time.Sleep(time.Second)
			cur := C.long(C.clipboard_change_count())
			if cnt != cur {
				changed <- struct{}{}
				close(changed)
				return
			}
		}
	}()
	return true, changed
}
