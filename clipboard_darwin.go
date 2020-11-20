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
*/
import "C"
import "unsafe"

func read(t MIMEType) (buf []byte) {
	var (
		data unsafe.Pointer
		n    C.uint
	)
	switch t {
	case MIMEText:
		n = C.clipboard_read_string(&data)
	case MIMEImage:
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
func write(t MIMEType, buf []byte) bool {
	var ok C.int

	switch t {
	case MIMEText:
		ok = C.clipboard_write_string(unsafe.Pointer(&buf[0]),
			C.NSInteger(len(buf)))
	case MIMEImage:
		ok = C.clipboard_write_image(unsafe.Pointer(&buf[0]),
			C.NSInteger(len(buf)))
	}
	if ok != 0 {
		return false
	}
	return true
}
