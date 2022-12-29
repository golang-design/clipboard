// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build darwin && !ios
// +build darwin,!ios

package clipboard

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework Cocoa
#import <Foundation/Foundation.h>
#import <Cocoa/Cocoa.h>

unsigned int clipboard_read(void **out, void* t);
int clipboard_write(const void *bytes, NSInteger n, void* t);
NSInteger clipboard_change_count();
*/
import "C"
import (
	"context"
	"sync"
	"time"
	"unsafe"
)

func initialize() error { return nil }

func read(t Format) (buf []byte, err error) {

	var format unsafe.Pointer
	switch tt := t.(type) {
	case internalFormat:
		switch tt {
		case FmtText:
			format = unsafe.Pointer(C.NSPasteboardTypeString)
		case FmtImage:
			format = unsafe.Pointer(C.NSPasteboardTypePNG)
		}
	default:
		found := false
		registeredFormats.Range(func(key, value interface{}) bool {
			if t == key {
				found = true
				return false
			}
			return true
		})
		if !found {
			return nil, errUnsupported
		}
		actualFormat, ok := t.(unsafe.Pointer)
		if !ok {
			return nil, errUnsupported
		}
		format = actualFormat
	}

	var data unsafe.Pointer
	n := C.clipboard_read(&data, unsafe.Pointer(&format))
	if data == nil {
		return nil, errUnavailable
	}
	defer C.free(unsafe.Pointer(data))
	if n == 0 {
		return nil, nil
	}
	return C.GoBytes(data, C.int(n)), nil
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
func write(t Format, buf []byte) (<-chan struct{}, error) {
	var format unsafe.Pointer
	switch tt := t.(type) {
	case internalFormat:
		switch tt {
		case FmtText:
			format = unsafe.Pointer(C.NSPasteboardTypeString)
		case FmtImage:
			format = unsafe.Pointer(C.NSPasteboardTypePNG)
		default:
			return nil, errUnsupported
		}
	default:
		found := false
		registeredFormats.Range(func(key, value interface{}) bool {
			if t == key {
				found = true
				return false
			}
			return true
		})
		if !found {
			return nil, errUnsupported
		}
		actualFormat, ok := t.(unsafe.Pointer)
		if !ok {
			return nil, errUnsupported
		}
		format = actualFormat
	}
	var ok C.int
	if len(buf) == 0 {
		ok = C.clipboard_write(unsafe.Pointer(nil), 0, unsafe.Pointer(&format))
	} else {
		ok = C.clipboard_write(unsafe.Pointer(&buf[0]), C.NSInteger(len(buf)), unsafe.Pointer(&format))
	}
	if ok != 0 {
		return nil, errUnavailable
	}

	// use unbuffered data to prevent goroutine leak
	changed := make(chan struct{}, 1)
	cnt := C.long(C.clipboard_change_count())
	go func() {
		for {
			// not sure if we are too slow or the user too fast :)
			time.Sleep(time.Second)
			cur := C.long(C.clipboard_change_count())
			if cnt != cur {
				changed <- struct{}{}
				close(changed)
				return
			}
		}
	}()
	return changed, nil
}

func watch(ctx context.Context, t Format) <-chan []byte {
	recv := make(chan []byte, 1)
	// not sure if we are too slow or the user too fast :)
	ti := time.NewTicker(time.Second)
	lastCount := C.long(C.clipboard_change_count())
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(recv)
				return
			case <-ti.C:
				this := C.long(C.clipboard_change_count())
				if lastCount != this {
					b := Read(t)
					if b == nil {
						continue
					}
					recv <- b
					lastCount = this
				}
			}
		}
	}()
	return recv
}

var registeredFormats sync.Map // map[any]any

func register(h Handler) error {
	t := h.Format()
	registeredFormats.Store(t, struct{}{})
	return nil
}
