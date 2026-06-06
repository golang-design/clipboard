// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build darwin && !ios

package clipboard

import (
	"bytes"
	"context"
	"image/png"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"golang.org/x/image/tiff"
)

var (
	appkit = must(purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW))

	_NSPasteboardTypeString = must2(purego.Dlsym(appkit, "NSPasteboardTypeString"))
	_NSPasteboardTypePNG    = must2(purego.Dlsym(appkit, "NSPasteboardTypePNG"))
	_NSPasteboardTypeTIFF   = must2(purego.Dlsym(appkit, "NSPasteboardTypeTIFF"))

	class_NSPasteboard = objc.GetClass("NSPasteboard")
	class_NSData       = objc.GetClass("NSData")

	sel_generalPasteboard   = objc.RegisterName("generalPasteboard")
	sel_length              = objc.RegisterName("length")
	sel_getBytesLength      = objc.RegisterName("getBytes:length:")
	sel_dataForType         = objc.RegisterName("dataForType:")
	sel_clearContents       = objc.RegisterName("clearContents")
	sel_setDataForType      = objc.RegisterName("setData:forType:")
	sel_dataWithBytesLength = objc.RegisterName("dataWithBytes:length:")
	sel_changeCount         = objc.RegisterName("changeCount")
)

func must(sym uintptr, err error) uintptr {
	if err != nil {
		panic(err)
	}
	return sym
}

func must2(sym uintptr, err error) uintptr {
	if err != nil {
		panic(err)
	}
	// dlsym returns a pointer to the object so dereference like this to avoid possible misuse of 'unsafe.Pointer' warning
	return **(**uintptr)(unsafe.Pointer(&sym))
}

func initialize() error { return nil }

func read(t Format) (buf []byte, err error) {
	switch t {
	case FmtText:
		return clipboard_read_string(), nil
	case FmtImage:
		return clipboard_read_image(), nil
	}
	return nil, errUnavailable
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
func write(t Format, buf []byte) (<-chan struct{}, error) {
	var ok bool
	switch t {
	case FmtText:
		if len(buf) == 0 {
			ok = clipboard_write_string(nil)
		} else {
			ok = clipboard_write_string(buf)
		}
	case FmtImage:
		if len(buf) == 0 {
			ok = clipboard_write_image(nil)
		} else {
			ok = clipboard_write_image(buf)
		}
	default:
		return nil, errUnsupported
	}
	if !ok {
		return nil, errUnavailable
	}

	// use unbuffered data to prevent goroutine leak
	changed := make(chan struct{}, 1)
	cnt := clipboard_change_count()
	go func() {
		for {
			// not sure if we are too slow or the user too fast :)
			time.Sleep(time.Second)
			cur := clipboard_change_count()
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
	lastCount := clipboard_change_count()
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(recv)
				return
			case <-ti.C:
				this := clipboard_change_count()
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

// nsdataBytes copies the contents of an NSData object into a new byte
// slice, returning nil if the object is null or empty.
func nsdataBytes(data objc.ID) []byte {
	if data == 0 {
		return nil
	}
	size := uint(data.Send(sel_length))
	if size == 0 {
		return nil
	}
	out := make([]byte, size)
	data.Send(sel_getBytesLength, unsafe.SliceData(out), size)
	return out
}

func clipboard_read_string() []byte {
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	return nsdataBytes(pasteboard.Send(sel_dataForType, _NSPasteboardTypeString))
}

func clipboard_read_image() []byte {
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	if out := nsdataBytes(pasteboard.Send(sel_dataForType, _NSPasteboardTypePNG)); out != nil {
		return out
	}

	// macOS stores copied images as TIFF by default (e.g. screenshots and
	// "Copy Image" in many apps). Fall back to TIFF and transcode to PNG so
	// callers always receive PNG data, consistent with the other platforms.
	raw := nsdataBytes(pasteboard.Send(sel_dataForType, _NSPasteboardTypeTIFF))
	if raw == nil {
		return nil
	}
	img, err := tiff.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

func clipboard_write_image(bytes []byte) bool {
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	data := objc.ID(class_NSData).Send(sel_dataWithBytesLength, unsafe.SliceData(bytes), len(bytes))
	pasteboard.Send(sel_clearContents)
	return pasteboard.Send(sel_setDataForType, data, _NSPasteboardTypePNG) != 0
}

func clipboard_write_string(bytes []byte) bool {
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	data := objc.ID(class_NSData).Send(sel_dataWithBytesLength, unsafe.SliceData(bytes), len(bytes))
	pasteboard.Send(sel_clearContents)
	return pasteboard.Send(sel_setDataForType, data, _NSPasteboardTypeString) != 0
}

func clipboard_change_count() int {
	return int(objc.ID(class_NSPasteboard).Send(sel_generalPasteboard).Send(sel_changeCount))
}
