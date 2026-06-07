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
	"runtime"
	"strings"
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

	class_NSPasteboard      = objc.GetClass("NSPasteboard")
	class_NSData            = objc.GetClass("NSData")
	class_NSString          = objc.GetClass("NSString")
	class_NSAutoreleasePool = objc.GetClass("NSAutoreleasePool")

	sel_alloc                = objc.RegisterName("alloc")
	sel_init                 = objc.RegisterName("init")
	sel_drain                = objc.RegisterName("drain")
	sel_generalPasteboard    = objc.RegisterName("generalPasteboard")
	sel_length               = objc.RegisterName("length")
	sel_getBytesLength       = objc.RegisterName("getBytes:length:")
	sel_dataForType          = objc.RegisterName("dataForType:")
	sel_clearContents        = objc.RegisterName("clearContents")
	sel_setDataForType       = objc.RegisterName("setData:forType:")
	sel_dataWithBytesLength  = objc.RegisterName("dataWithBytes:length:")
	sel_stringWithUTF8String = objc.RegisterName("stringWithUTF8String:")
	sel_changeCount          = objc.RegisterName("changeCount")
	sel_types                = objc.RegisterName("types")
	sel_count                = objc.RegisterName("count")
	sel_objectAtIndex        = objc.RegisterName("objectAtIndex:")
	sel_UTF8String           = objc.RegisterName("UTF8String")
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

// newAutoreleasePool creates an NSAutoreleasePool and returns a function that
// drains it. Every pasteboard operation runs inside one: accessors such as
// -dataForType: and +[NSData dataWithBytes:length:] return autoreleased
// objects, but these goroutines run on arbitrary OS threads with no pool of
// their own, so without draining the objects leak — notably in the per-second
// poll loops driven by write and watch. Use as: defer newAutoreleasePool()().
//
// An autorelease pool is thread-local and must be drained on the same OS
// thread it was created on. A goroutine can otherwise migrate threads between
// creation and drain (e.g. across the allocations in the TIFF transcode),
// which crashes when the pool is popped on the wrong thread. Pin the OS thread
// for the pool's lifetime to keep creation and drain together.
func newAutoreleasePool() (drain func()) {
	runtime.LockOSThread()
	pool := objc.ID(class_NSAutoreleasePool).Send(sel_alloc).Send(sel_init)
	return func() {
		pool.Send(sel_drain)
		runtime.UnlockOSThread()
	}
}

// enumerateFormats reports the formats currently on the clipboard by reading the
// general pasteboard's advertised types and mapping each to a Format.
func enumerateFormats() []Format {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	types := pasteboard.Send(sel_types)
	if types == 0 {
		return nil
	}
	n := int(objc.ID(types).Send(sel_count))
	out := make([]Format, 0, n)
	for i := 0; i < n; i++ {
		t := objc.ID(types).Send(sel_objectAtIndex, uintptr(i))
		if f, ok := darwinFormatFor(nsStringGo(objc.ID(t))); ok {
			out = append(out, f)
		}
	}
	return out
}

// darwinFormatFor maps a pasteboard type (a UTI, or for this package's custom
// formats the MIME string used verbatim) to a Format: the built-in text/image
// UTIs to FmtText/FmtImage, a few common UTIs to their MIME via a best-effort
// alias, and any MIME-shaped type to a custom format registered on demand.
func darwinFormatFor(t string) (Format, bool) {
	switch t {
	case "public.utf8-plain-text", "public.plain-text", "NSStringPboardType":
		return FmtText, true
	case "public.png", "public.tiff":
		return FmtImage, true
	case "public.html":
		return Register("text/html"), true
	case "com.adobe.pdf":
		return Register("application/pdf"), true
	case "public.rtf":
		return Register("text/rtf"), true
	}
	if strings.Contains(t, "/") {
		return Register(t), true
	}
	return 0, false
}

// nsStringGo converts an NSString to a Go string via its UTF8String pointer.
func nsStringGo(s objc.ID) string {
	if s == 0 {
		return ""
	}
	p := uintptr(s.Send(sel_UTF8String))
	if p == 0 {
		return ""
	}
	var b []byte
	for {
		c := *(*byte)(unsafe.Pointer(p))
		if c == 0 {
			break
		}
		b = append(b, c)
		p++
	}
	return string(b)
}

func read(t Format) (buf []byte, err error) {
	switch t {
	case FmtText:
		return clipboard_read_string(), nil
	case FmtImage:
		return clipboard_read_image(), nil
	default:
		mime, ok := formatMIME(t)
		if !ok {
			return nil, errUnsupported
		}
		return clipboard_read_custom(mime), nil
	}
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
		mime, found := formatMIME(t)
		if !found {
			return nil, errUnsupported
		}
		ok = clipboard_write_custom(mime, buf)
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
		defer ti.Stop()
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
					select {
					case recv <- b:
						lastCount = this
					case <-ctx.Done():
						close(recv)
						return
					}
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
	runtime.KeepAlive(out)
	return out
}

func clipboard_read_string() []byte {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	return nsdataBytes(pasteboard.Send(sel_dataForType, _NSPasteboardTypeString))
}

func clipboard_read_image() []byte {
	defer newAutoreleasePool()()
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

func clipboard_write_image(buf []byte) bool {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	data := objc.ID(class_NSData).Send(sel_dataWithBytesLength, unsafe.SliceData(buf), len(buf))
	runtime.KeepAlive(buf)
	pasteboard.Send(sel_clearContents)
	return pasteboard.Send(sel_setDataForType, data, _NSPasteboardTypePNG) != 0
}

func clipboard_write_string(buf []byte) bool {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	data := objc.ID(class_NSData).Send(sel_dataWithBytesLength, unsafe.SliceData(buf), len(buf))
	runtime.KeepAlive(buf)
	pasteboard.Send(sel_clearContents)
	return pasteboard.Send(sel_setDataForType, data, _NSPasteboardTypeString) != 0
}

// nsString builds an autoreleased NSString from a Go string, used as a custom
// pasteboard type. It must be called inside an autorelease pool (read/write
// custom both install one) so the temporary string is reclaimed.
func nsString(s string) objc.ID {
	b := append([]byte(s), 0) // NUL-terminate for stringWithUTF8String:
	str := objc.ID(class_NSString).Send(sel_stringWithUTF8String, unsafe.SliceData(b))
	runtime.KeepAlive(b)
	return str
}

// clipboard_read_custom returns the raw bytes stored under the given MIME type
// (used as the pasteboard type verbatim), or nil if no such data is present.
func clipboard_read_custom(mime string) []byte {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	return nsdataBytes(pasteboard.Send(sel_dataForType, nsString(mime)))
}

// clipboard_write_custom stores buf verbatim under the given MIME type with no
// conversion (raw passthrough), replacing the current clipboard contents.
func clipboard_write_custom(mime string, buf []byte) bool {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	data := objc.ID(class_NSData).Send(sel_dataWithBytesLength, unsafe.SliceData(buf), len(buf))
	runtime.KeepAlive(buf)
	pasteboard.Send(sel_clearContents)
	return pasteboard.Send(sel_setDataForType, data, nsString(mime)) != 0
}

func clipboard_change_count() int {
	defer newAutoreleasePool()()
	return int(objc.ID(class_NSPasteboard).Send(sel_generalPasteboard).Send(sel_changeCount))
}
