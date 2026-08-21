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
	class_NSArray           = objc.GetClass("NSArray")
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
	sel_propertyListForType  = objc.RegisterName("propertyListForType:")
	sel_setPropertyListType  = objc.RegisterName("setPropertyList:forType:")
	sel_arrayWithObjects     = objc.RegisterName("arrayWithObjects:count:")
)

// pbTypeFilenames is NSFilenamesPboardType, the pasteboard type holding a file
// list as a property-list array of paths. It is spelled out rather than looked
// up with dlsym because the constant's value is exactly this string, and the
// symbol is deprecated.
//
// It is a legacy type, but it is the one that reads and writes a *whole* list in
// a single call: NSPasteboardTypeFileURL holds one URL per pasteboard item, and
// dataForType: only ever sees the first. macOS bridges the two — writing this
// property list makes the system synthesize public.file-url items, and after
// another application writes NSURLs this type returns their paths — so one call
// interoperates in both directions (see specs/file-clipboard.md §3).
const pbTypeFilenames = "NSFilenamesPboardType"

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

// darwinNativeTypes aliases a portable MIME type to the pasteboard type other
// macOS applications publish that data under. Pasteboard types are UTIs — their
// own namespace, not MIME types — so using the MIME string verbatim only ever
// round-trips with this library itself, never with another app (#160).
//
// Only aliases whose data is the MIME type's bytes verbatim belong here, since
// custom formats are raw passthrough.
var darwinNativeTypes = map[string]string{
	"text/html":       "public.html",
	"application/pdf": "com.adobe.pdf",
	"text/rtf":        "public.rtf",
	"image/png":       "public.png",
	"image/tiff":      "public.tiff",
	"image/jpeg":      "public.jpeg",
}

// darwinPasteboardTypes returns the pasteboard types a MIME type may appear
// under, most preferred first: its native UTI (when it has one), then the MIME
// string itself. Reads try each in turn, so data published under either is
// reachable; writes use the first.
func darwinPasteboardTypes(mime string) []string {
	if uti, ok := darwinNativeTypes[mime]; ok {
		return []string{uti, mime}
	}
	return []string{mime}
}

// darwinMIMEForType is the inverse of darwinNativeTypes: it maps a pasteboard
// UTI back to the MIME type it stands for.
func darwinMIMEForType(t string) (string, bool) {
	for mime, uti := range darwinNativeTypes {
		if t == uti {
			return mime, true
		}
	}
	return "", false
}

// darwinFormatFor maps a pasteboard type (a UTI, or for a MIME type without a
// native alias the MIME string used verbatim) to a Format: the built-in
// text/image UTIs to FmtText/FmtImage, an aliased UTI to its MIME type's custom
// token, and any other MIME-shaped type to a custom format registered on demand.
// The built-in image UTIs are matched first, so public.png/public.tiff report
// FmtImage even though they also alias image/png and image/tiff; both tokens
// read the same bytes.
func darwinFormatFor(t string) (Format, bool) {
	switch t {
	case "public.utf8-plain-text", "public.plain-text", "NSStringPboardType":
		return FmtText, true
	case "public.png", "public.tiff":
		return FmtImage, true
	case pbTypeFilenames, "public.file-url":
		return FmtFiles, true
	}
	if mime, ok := darwinMIMEForType(t); ok {
		return Register(mime), true
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
	case FmtFiles:
		paths := clipboard_read_filenames()
		if len(paths) == 0 {
			return nil, errUnavailable
		}
		return uriListFromPaths(paths), nil
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
	return writeAll([]Item{{Format: t, Bytes: buf}})
}

// darwinItem is an Item resolved to the pasteboard type it is written under:
// either one of the built-in type objects, or a name for a custom format, which
// becomes an NSString inside the write's autorelease pool. uti names the type
// either way, and is what two items are compared on.
type darwinItem struct {
	typ  uintptr
	uti  string
	name string
	buf  []byte
	// paths is set instead of buf for FmtFiles, which is stored as a property
	// list of path strings rather than as data.
	paths []string
}

// The UTIs behind the built-in pasteboard type constants, needed as plain
// strings to notice that a custom format resolves to the same type — FmtImage
// and Register("image/png") are both public.png.
const (
	utiPlainText = "public.utf8-plain-text"
	utiPNG       = "public.png"
)

// writeAll publishes every item on one clearContents generation, so the whole
// set replaces the pasteboard together (#151). NSPasteboard is built for this:
// a generation holds as many types as it is given, and a consumer picks the
// first it understands.
func writeAll(items []Item) (<-chan struct{}, error) {
	out := make([]darwinItem, 0, len(items))
	// Two different tokens can resolve to the same pasteboard type, and a second
	// store under a type would overwrite the first — inverting the rule that an
	// earlier item wins. Drop the later one instead.
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		var d darwinItem
		switch it.Format {
		case FmtText:
			d = darwinItem{typ: _NSPasteboardTypeString, uti: utiPlainText, buf: it.Bytes}
		case FmtImage:
			d = darwinItem{typ: _NSPasteboardTypePNG, uti: utiPNG, buf: it.Bytes}
		case FmtFiles:
			d = darwinItem{uti: pbTypeFilenames, name: pbTypeFilenames,
				paths: pathsFromURIList(it.Bytes)}
		default:
			mime, found := formatMIME(it.Format)
			if !found {
				return nil, errUnsupported
			}
			name := darwinPasteboardTypes(mime)[0]
			d = darwinItem{uti: name, name: name, buf: it.Bytes}
		}
		if seen[d.uti] {
			continue
		}
		seen[d.uti] = true
		out = append(out, d)
	}
	if !clipboard_write_all(out) {
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

// clipboard_write_all clears the pasteboard once and stores every item on that
// one generation, in order — NSPasteboard treats the order types are set in as
// the order a consumer should prefer them. It reports whether every item was
// stored.
//
// The types and NSData objects are built before clearContents, so nothing
// between the clear and the last store can fail for a reason other than the
// store itself: the pasteboard is not left holding a partial set (#151).
//
// Callers pass items already deduplicated by pasteboard type (see writeAll).
func clipboard_write_all(items []darwinItem) bool {
	defer newAutoreleasePool()()

	type entry struct{ typ, data, plist objc.ID }
	entries := make([]entry, 0, len(items))
	for _, it := range items {
		typ := objc.ID(it.typ)
		if typ == 0 {
			typ = nsString(it.name)
		}
		e := entry{typ: typ}
		if it.uti == pbTypeFilenames {
			e.plist = nsStringArray(it.paths)
		} else {
			e.data = nsData(it.buf)
		}
		entries = append(entries, e)
	}

	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	pasteboard.Send(sel_clearContents)
	for _, e := range entries {
		if e.plist != 0 {
			if pasteboard.Send(sel_setPropertyListType, e.plist, e.typ) == 0 {
				return false
			}
			continue
		}
		if pasteboard.Send(sel_setDataForType, e.data, e.typ) == 0 {
			return false
		}
	}
	return true
}

// nsStringArray builds an autoreleased NSArray of NSStrings.
func nsStringArray(ss []string) objc.ID {
	ids := make([]objc.ID, len(ss))
	for i, s := range ss {
		ids[i] = nsString(s)
	}
	arr := objc.ID(class_NSArray).Send(sel_arrayWithObjects, unsafe.SliceData(ids), len(ids))
	runtime.KeepAlive(ids)
	return arr
}

// clipboard_read_filenames returns the file paths on the pasteboard, from the
// property list under NSFilenamesPboardType. macOS populates that type from the
// file URLs a modern application writes, so this reads either representation.
func clipboard_read_filenames() []string {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	list := pasteboard.Send(sel_propertyListForType, nsString(pbTypeFilenames))
	if list == 0 {
		return nil
	}
	n := int(list.Send(sel_count))
	out := make([]string, 0, n)
	for i := range n {
		if p := nsStringGo(list.Send(sel_objectAtIndex, i)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// nsData wraps a byte slice in an autoreleased NSData. An empty slice is passed
// as a NULL pointer, which dataWithBytes:length: accepts for a zero length.
func nsData(buf []byte) objc.ID {
	var p *byte
	if len(buf) > 0 {
		p = unsafe.SliceData(buf)
	}
	data := objc.ID(class_NSData).Send(sel_dataWithBytesLength, p, len(buf))
	runtime.KeepAlive(buf)
	return data
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

// clipboard_read_custom returns the raw bytes stored under the given MIME type,
// resolved to a pasteboard type (see darwinPasteboardTypes), or nil if no such
// data is present.
func clipboard_read_custom(mime string) []byte {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	for _, t := range darwinPasteboardTypes(mime) {
		if out := nsdataBytes(pasteboard.Send(sel_dataForType, nsString(t))); out != nil {
			return out
		}
	}
	return nil
}

// clipboard_write_custom stores buf verbatim under the given MIME type's
// pasteboard type with no conversion (raw passthrough), replacing the current
// clipboard contents.
func clipboard_write_custom(mime string, buf []byte) bool {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	pasteboard.Send(sel_clearContents)
	return pasteboard.Send(sel_setDataForType, nsData(buf), nsString(darwinPasteboardTypes(mime)[0])) != 0
}

func clipboard_change_count() int {
	defer newAutoreleasePool()()
	return int(objc.ID(class_NSPasteboard).Send(sel_generalPasteboard).Send(sel_changeCount))
}
