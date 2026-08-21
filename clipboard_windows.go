// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard

// Interacting with Clipboard on Windows:
// https://docs.microsoft.com/zh-cn/windows/win32/dataxchg/using-the-clipboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/image/bmp"
)

func initialize() error { return nil }

// readText reads the clipboard and returns the text data if presents.
// The caller is responsible for opening/closing the clipboard before
// calling this function.
func readText() (buf []byte, err error) {
	hMem, _, err := getClipboardData.Call(cFmtUnicodeText)
	if hMem == 0 {
		return nil, err
	}
	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return nil, err
	}
	defer gUnlock.Call(hMem)

	// Find NUL terminator
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; n++ {
		ptr = unsafe.Pointer(uintptr(ptr) +
			unsafe.Sizeof(*((*uint16)(unsafe.Pointer(p)))))
	}

	var s []uint16
	h := (*reflect.SliceHeader)(unsafe.Pointer(&s))
	h.Data = p
	h.Len = n
	h.Cap = n
	return []byte(string(utf16.Decode(s))), nil
}

// writeText writes given data to the clipboard. It is the caller's
// responsibility for opening/closing the clipboard before calling
// this function.
// setClipboardBytes copies buf into a moveable global block and hands ownership
// of it to the clipboard under the given format. The caller must have opened and
// emptied the clipboard.
//
// This is the only part of a write that can fail once the transaction is open,
// and only on allocation: everything decodable — the UTF-16 conversion, the PNG
// decode, the custom-format registration — is resolved by resolveItem before the
// clipboard is touched, so a multi-format write cannot leave a partial set
// behind (#151).
func setClipboardBytes(format uintptr, buf []byte) error {
	hMem, _, err := gAlloc.Call(gmemMoveable, uintptr(len(buf)))
	if hMem == 0 {
		return fmt.Errorf("failed to alloc global memory: %w", err)
	}

	p, _, err := gLock.Call(hMem)
	if p == 0 {
		gFree.Call(hMem)
		return fmt.Errorf("failed to lock global memory: %w", err)
	}
	defer gUnlock.Call(hMem)

	// no return value
	memMove.Call(p, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))

	v, _, err := setClipboardData.Call(format, hMem)
	if v == 0 {
		gFree.Call(hMem)
		return fmt.Errorf("failed to set data to clipboard: %w", err)
	}
	return nil
}

// utf16Bytes lays UTF-16 code units out as the little-endian bytes CF_UNICODETEXT
// expects. Every Windows architecture Go targets is little-endian.
func utf16Bytes(s []uint16) []byte {
	out := make([]byte, len(s)*2)
	for i, v := range s {
		binary.LittleEndian.PutUint16(out[i*2:], v)
	}
	return out
}

// readImage reads the clipboard and returns PNG encoded image data
// if presents. The caller is responsible for opening/closing the
// clipboard before calling this function.
func readImage() ([]byte, error) {
	hMem, _, err := getClipboardData.Call(cFmtDIBV5)
	if hMem == 0 {
		// second chance to try FmtDIB
		return readImageDib()
	}
	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return nil, err
	}
	defer gUnlock.Call(hMem)

	// inspect header information
	info := (*bitmapV5Header)(unsafe.Pointer(p))

	// The 32-bit path below reads straight BGRA. Other bit depths (e.g. a
	// 24-bit image, which Windows commonly exposes as CF_DIB and synthesizes
	// into a 24-bit CF_DIBV5) are decoded via the CF_DIB path, which rebuilds a
	// BMP and decodes it with x/image/bmp — covering 24/16/8-bit DIBs (#65).
	if info.BitCount != 32 {
		return readImageDib()
	}

	var data []byte
	sh := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	sh.Data = uintptr(p)
	sh.Cap = int(info.Size + 4*uint32(info.Width)*uint32(info.Height))
	sh.Len = int(info.Size + 4*uint32(info.Width)*uint32(info.Height))
	// The DIBV5 stores straight (non-premultiplied) BGRA (see imageToDIB), so
	// decode into NRGBA, whose channels are also straight. Using color.RGBA
	// here would treat the bytes as premultiplied and round-trip transparent
	// images incorrectly (#105).
	img := image.NewNRGBA(image.Rect(0, 0, int(info.Width), int(info.Height)))
	offset := int(info.Size)
	stride := int(info.Width)
	for y := 0; y < int(info.Height); y++ {
		for x := 0; x < int(info.Width); x++ {
			idx := offset + 4*(y*stride+x)
			xhat := (x + int(info.Width)) % int(info.Width)
			yhat := int(info.Height) - 1 - y
			r := data[idx+2]
			g := data[idx+1]
			b := data[idx+0]
			a := data[idx+3]
			img.SetNRGBA(xhat, yhat, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	// always use PNG encoding.
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes(), nil
}

func readImageDib() ([]byte, error) {
	const (
		fileHeaderLen = 14
		infoHeaderLen = 40
		cFmtDIB       = 8
	)

	// Check the returned handle, not the syscall's lastErr: GetClipboardData
	// does not clear GetLastError on success, so err can hold a stale non-zero
	// value even when the format is present.
	hClipDat, _, _ := getClipboardData.Call(cFmtDIB)
	if hClipDat == 0 {
		return nil, errUnavailable
	}
	pMemBlk, _, err := gLock.Call(hClipDat)
	if pMemBlk == 0 {
		return nil, errors.New("failed to call global lock: " + err.Error())
	}
	defer gUnlock.Call(hClipDat)

	bmpHeader := (*bitmapHeader)(unsafe.Pointer(pMemBlk))
	dataSize := bmpHeader.SizeImage + fileHeaderLen + infoHeaderLen

	if bmpHeader.SizeImage == 0 && bmpHeader.Compression == 0 {
		iSizeImage := bmpHeader.Height * ((bmpHeader.Width*uint32(bmpHeader.BitCount)/8 + 3) &^ 3)
		dataSize += iSizeImage
	}
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16('B')|(uint16('M')<<8))
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	const sizeof_colorbar = 0
	binary.Write(buf, binary.LittleEndian, uint32(fileHeaderLen+infoHeaderLen+sizeof_colorbar))
	j := 0
	for i := fileHeaderLen; i < int(dataSize); i++ {
		binary.Write(buf, binary.BigEndian, *(*byte)(unsafe.Pointer(pMemBlk + uintptr(j))))
		j++
	}
	return bmpToPng(buf)
}

func bmpToPng(bmpBuf *bytes.Buffer) (buf []byte, err error) {
	var f bytes.Buffer
	original_image, err := bmp.Decode(bmpBuf)
	if err != nil {
		return nil, err
	}
	err = png.Encode(&f, original_image)
	if err != nil {
		return nil, err
	}
	return f.Bytes(), nil
}

// windowsItem is an Item resolved to what the clipboard transaction will place:
// the format id, and the exact bytes to store under it. An empty buf means the
// format contributes nothing but the emptying, matching the previous behavior
// for an empty payload.
type windowsItem struct {
	format uintptr
	buf    []byte
}

// resolveItem does every part of a write that can fail for a reason other than
// running out of memory — the UTF-16 conversion, the PNG decode and DIB
// conversion, the custom-format lookup and registration — so that it happens
// before the clipboard is opened. See setClipboardBytes.
func resolveItem(it Item) (windowsItem, error) {
	switch it.Format {
	case FmtText:
		if len(it.Bytes) == 0 {
			return windowsItem{format: cFmtUnicodeText}, nil
		}
		s, err := syscall.UTF16FromString(string(it.Bytes))
		if err != nil {
			return windowsItem{}, fmt.Errorf("failed to convert given string: %w", err)
		}
		return windowsItem{format: cFmtUnicodeText, buf: utf16Bytes(s)}, nil
	case FmtFiles:
		paths := pathsFromURIList(it.Bytes)
		if len(paths) == 0 {
			return windowsItem{format: cFmtHDrop}, nil
		}
		return windowsItem{format: cFmtHDrop, buf: dropFilesFromPaths(paths)}, nil
	case FmtImage:
		if len(it.Bytes) == 0 {
			return windowsItem{format: cFmtDIBV5}, nil
		}
		img, err := png.Decode(bytes.NewReader(it.Bytes))
		if err != nil {
			return windowsItem{}, fmt.Errorf("input bytes is not PNG encoded: %w", err)
		}
		return windowsItem{format: cFmtDIBV5, buf: imageToDIB(img)}, nil
	default:
		mime, ok := formatMIME(it.Format)
		if !ok {
			return windowsItem{}, errUnsupported
		}
		id, err := registerCustomFormat(mime)
		if err != nil {
			return windowsItem{}, err
		}
		return windowsItem{format: id, buf: it.Bytes}, nil
	}
}

// dropFilesHeader is the DROPFILES structure that prefixes a CF_HDROP payload.
// The paths follow it, at the byte offset PFiles names.
type dropFilesHeader struct {
	PFiles uint32 // offset of the path list from the start of this struct
	X, Y   int32  // POINT pt: the drop point, unused for a clipboard copy
	FNC    uint32 // BOOL fNC: pt is in non-client coordinates
	FWide  uint32 // BOOL fWide: the path list is UTF-16 rather than ANSI
}

// dropFilesHeaderSize is sizeof(DROPFILES): 4 + 8 + 4 + 4. It is spelled out
// rather than taken from unsafe.Sizeof so that no padding rule can change the
// on-clipboard layout, which other applications parse by offset.
const dropFilesHeaderSize = 20

// dropFilesFromPaths encodes file paths as a CF_HDROP payload: the DROPFILES
// header, then the paths as UTF-16, each NUL-terminated, the list closed by a
// second NUL.
func dropFilesFromPaths(paths []string) []byte {
	var units []uint16
	for _, p := range paths {
		u, err := syscall.UTF16FromString(p) // already NUL-terminated
		if err != nil {
			continue // a path with an interior NUL names no file
		}
		units = append(units, u...)
	}
	units = append(units, 0) // the extra NUL that ends the list

	out := make([]byte, dropFilesHeaderSize+len(units)*2)
	binary.LittleEndian.PutUint32(out[0:], dropFilesHeaderSize) // pFiles
	binary.LittleEndian.PutUint32(out[16:], 1)                  // fWide
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[dropFilesHeaderSize+i*2:], u)
	}
	return out
}

// pathsFromDropFiles decodes a CF_HDROP payload into file paths. It honors the
// header's own pFiles offset and fWide flag rather than assuming the layout this
// package writes, since the payload usually comes from another application.
func pathsFromDropFiles(buf []byte) []string {
	if len(buf) < dropFilesHeaderSize {
		return nil
	}
	off := int(binary.LittleEndian.Uint32(buf[0:]))
	wide := binary.LittleEndian.Uint32(buf[16:]) != 0
	if off < dropFilesHeaderSize || off > len(buf) {
		return nil
	}
	list := buf[off:]

	if !wide {
		// An ANSI list: NUL-separated bytes, closed by a second NUL. Only the
		// ASCII range is unambiguous without the source's code page, which the
		// payload does not carry.
		var out []string
		for _, part := range bytes.Split(list, []byte{0}) {
			if len(part) == 0 {
				break
			}
			out = append(out, string(part))
		}
		return out
	}

	units := make([]uint16, 0, len(list)/2)
	for i := 0; i+1 < len(list); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(list[i:]))
	}
	var out []string
	for start := 0; start < len(units); {
		end := start
		for end < len(units) && units[end] != 0 {
			end++
		}
		if end == start {
			break // the empty string that terminates the list
		}
		out = append(out, string(utf16.Decode(units[start:end])))
		start = end + 1
	}
	return out
}

// windowsNativeNames aliases a portable MIME type to the registered clipboard
// format name Windows applications actually publish that data under. Windows
// registered-format names are their own namespace — they are not MIME types —
// so without this table Register("image/png") would name a format literally
// "image/png", which no other application writes or reads (#160).
//
// Only aliases whose data is the MIME type's bytes verbatim belong here, since
// custom formats are raw passthrough. That deliberately excludes CF_HTML
// ("HTML Format"), whose payload is a header-wrapped fragment rather than
// text/html itself, and CF_HDROP, which is a struct rather than a text/uri-list.
var windowsNativeNames = map[string]string{
	// The de-facto PNG format: Chromium, Firefox, Microsoft Office, Snip &
	// Sketch and Paint all publish original PNG bytes under this name. It is
	// the lossless alternative to CF_DIBV5, which carries raw pixels only.
	"image/png": "PNG",
	// CF_RTF, registered by name; the data is RTF text as-is.
	"text/rtf": "Rich Text Format",
}

// windowsFormatNames returns the registered format names a MIME type may appear
// under, most preferred first: its native alias (when it has one), then the MIME
// string itself. Reads try each in turn, so data published under either name is
// reachable; writes use the first.
func windowsFormatNames(mime string) []string {
	if native, ok := windowsNativeNames[mime]; ok {
		return []string{native, mime}
	}
	return []string{mime}
}

// windowsMIMEForName is the inverse of windowsNativeNames: it maps a registered
// format name back to the MIME type it stands for. Format names are compared
// case-insensitively, matching RegisterClipboardFormat.
func windowsMIMEForName(name string) (string, bool) {
	for mime, native := range windowsNativeNames {
		if strings.EqualFold(name, native) {
			return mime, true
		}
	}
	return "", false
}

// registerCustomFormat maps a MIME string to the Windows clipboard format ID it
// is written under, resolving it to a native format name first (see
// windowsNativeNames).
func registerCustomFormat(mime string) (uintptr, error) {
	return registerFormatName(windowsFormatNames(mime)[0])
}

// registerFormatName maps a clipboard format name to a Windows clipboard format
// ID via RegisterClipboardFormat. Repeated registrations of the same name return
// the same ID, and the ID is unique per name across the window station, so this
// library and any other app naming the format identically interoperate.
func registerFormatName(name string) (uintptr, error) {
	p, err := syscall.BytePtrFromString(name)
	if err != nil {
		return 0, err
	}
	id, _, err := registerClipboardFormatA.Call(uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
	if id == 0 {
		return 0, err
	}
	return id, nil
}

// availableCustomFormat resolves a MIME type to the clipboard format ID to read
// it from: the first of its candidate names (windowsFormatNames) currently on
// the clipboard. When none is present it returns the preferred name's ID, so the
// caller's availability check reports the format as missing as usual.
func availableCustomFormat(mime string) (uintptr, error) {
	var first uintptr
	for _, name := range windowsFormatNames(mime) {
		id, err := registerFormatName(name)
		if err != nil {
			return 0, err
		}
		if first == 0 {
			first = id
		}
		if r, _, _ := isClipboardFormatAvailable.Call(id); r != 0 {
			return id, nil
		}
	}
	return first, nil
}

// clearClipboard empties the clipboard and makes this process its owner, which
// SetClipboardData requires. It runs once per write transaction, before any
// format is set: emptying between two formats of the same write would discard
// the one already set. The caller must have opened the clipboard.
func clearClipboard() error {
	r, _, err := emptyClipboard.Call()
	if r == 0 {
		return fmt.Errorf("failed to clear clipboard: %w", err)
	}
	return nil
}

// readFiles reads the CF_HDROP file list and returns it in the format's portable
// encoding, a text/uri-list body. The caller must have opened the clipboard.
func readFiles() ([]byte, error) {
	buf, err := readCustom(cFmtHDrop)
	if err != nil {
		return nil, err
	}
	paths := pathsFromDropFiles(buf)
	if len(paths) == 0 {
		return nil, errUnavailable
	}
	return uriListFromPaths(paths), nil
}

// readCustom returns the raw bytes stored under the given clipboard format ID,
// or nil if the handle is empty. The caller must have opened the clipboard.
func readCustom(format uintptr) ([]byte, error) {
	hMem, _, err := getClipboardData.Call(format)
	if hMem == 0 {
		return nil, err
	}
	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return nil, err
	}
	defer gUnlock.Call(hMem)

	size, _, _ := gSize.Call(hMem)
	if size == 0 {
		return nil, nil
	}
	out := make([]byte, int(size))
	memMove.Call(uintptr(unsafe.Pointer(&out[0])), p, size)
	return out, nil
}

// writeCustom stores buf verbatim under the given clipboard format ID with no
// conversion (raw passthrough). The caller must have opened the clipboard.
func writeCustom(format uintptr, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	return setClipboardBytes(format, buf)
}

// enumerateFormats reports the formats currently on the clipboard by iterating
// the available clipboard formats with EnumClipboardFormats and mapping each to
// a Format.
func enumerateFormats(ctx context.Context, sel selection) []Format {
	if sel == selPrimary {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if openClipboardRetry() != nil {
		return nil
	}
	defer closeClipboard.Call()

	var out []Format
	var format uintptr
	for {
		format, _, _ = enumClipboardFormats.Call(format)
		if format == 0 {
			break
		}
		if f, ok := windowsFormatFor(format); ok {
			out = append(out, f)
		}
	}
	return out
}

// windowsFormatFor maps a Windows clipboard format id to a Format: the
// predefined text/image formats to FmtText/FmtImage, and a registered format to
// a custom format (registered on demand) when its name denotes one.
// Predefined formats we do not model have no registered name and are skipped.
func windowsFormatFor(format uintptr) (Format, bool) {
	switch format {
	case cFmtUnicodeText:
		return FmtText, true
	case cFmtDIBV5, cFmtDIB, cFmtBitmap:
		return FmtImage, true
	case cFmtHDrop:
		return FmtFiles, true
	}
	return windowsFormatForName(clipboardFormatName(format))
}

// windowsFormatForName maps a registered clipboard format name to a Format. A
// name that aliases a MIME type (windowsNativeNames) and a name that already is
// a MIME type both resolve to that MIME type's custom token, so every token
// reported by Formats resolves back to the same clipboard data on Read.
func windowsFormatForName(name string) (Format, bool) {
	switch name {
	case "":
		return 0, false
	case "UTF8_STRING", "text/plain", "text/plain;charset=utf-8":
		return FmtText, true
	}
	if mime, ok := windowsMIMEForName(name); ok {
		return Register(mime), true
	}
	if strings.Contains(name, "/") {
		return Register(name), true
	}
	return 0, false
}

// clipboardFormatName returns the registered name of a clipboard format id, or
// "" for a predefined format (which has no registered name).
func clipboardFormatName(format uintptr) string {
	var buf [256]byte
	n, _, _ := getClipboardFormatNameA.Call(format, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return string(buf[:n])
}

// clipboardOpenTimeout bounds openClipboardRetry. It is a var (not a const) so
// tests can shorten it.
var clipboardOpenTimeout = 5 * time.Second

// openClipboardOnce attempts to open the clipboard once, returning whether it
// succeeded. It is a var so tests can simulate contention (a real second holder
// can only be another process). Pass a NULL (0) window handle explicitly:
// omitting it leaves a garbage value on the stack under the 386 stdcall ABI and
// the call spins (see #45).
var openClipboardOnce = func() bool {
	r, _, _ := openClipboard.Call(0)
	return r != 0
}

// openClipboardRetry opens the clipboard, retrying with a short backoff because
// another application may briefly hold it open. It returns errUnavailable once
// clipboardOpenTimeout elapses instead of busy-waiting forever at 100% CPU
// (#144). Call it on an OS-locked thread — OpenClipboard and CloseClipboard must
// run on the same thread — and CloseClipboard on success.
func openClipboardRetry() error {
	deadline := time.Now().Add(clipboardOpenTimeout)
	for {
		if openClipboardOnce() {
			return nil
		}
		if time.Now().After(deadline) {
			return errUnavailable
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func read(ctx context.Context, sel selection, t Format) (buf []byte, err error) {
	if sel == selPrimary {
		// This platform has no primary selection (see FromPrimary).
		return nil, errUnsupported
	}
	// On Windows, OpenClipboard and CloseClipboard must be executed on
	// the same thread. Thus, lock the OS thread for further execution.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var format uintptr
	switch t {
	case FmtImage:
		format = cFmtDIBV5
	case FmtText:
		format = cFmtUnicodeText
	case FmtFiles:
		format = cFmtHDrop
	default:
		mime, ok := formatMIME(t)
		if !ok {
			return nil, errUnsupported
		}
		format, err = availableCustomFormat(mime)
		if err != nil {
			return nil, err
		}
	}

	// check if clipboard is avaliable for the requested format
	r, _, err := isClipboardFormatAvailable.Call(format)
	if r == 0 {
		return nil, errUnavailable
	}

	if err := openClipboardRetry(); err != nil {
		return nil, err
	}
	defer closeClipboard.Call()

	switch format {
	case cFmtDIBV5:
		return readImage()
	case cFmtUnicodeText:
		return readText()
	case cFmtHDrop:
		return readFiles()
	default:
		return readCustom(format)
	}
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
// writeAll publishes every item inside one OpenClipboard/EmptyClipboard/
// CloseClipboard transaction, which is what makes the set atomic: the clipboard
// is emptied once, then each format is set on it, so no other application ever
// observes a partial set (#151). Order is preserved, and Windows reports formats
// in the order they were set, which is how a consumer knows what to prefer.
//
// Every item is resolved before the clipboard is opened, so a bad payload — an
// undecodable image, a string with an interior NUL, an unregistered format —
// fails with the clipboard untouched rather than emptied and half filled.
func writeAll(ctx context.Context, sel selection, items []Item, loops int) (<-chan struct{}, error) {
	// loops is ignored: this platform's clipboard is a store the OS serves, so
	// no paste request ever reaches this process to be counted (see Loops).
	_ = loops
	if sel == selPrimary {
		// This platform has no primary selection. Refusing is deliberate:
		// writing to the ordinary clipboard instead would destroy whatever the
		// user had copied (see FromPrimary).
		return nil, errUnsupported
	}
	resolved := make([]windowsItem, 0, len(items))
	for _, it := range items {
		r, err := resolveItem(it)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, r)
	}

	errch := make(chan error)
	changed := make(chan struct{}, 1)
	go func() {
		// make sure GetClipboardSequenceNumber happens with
		// OpenClipboard on the same thread.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := openClipboardRetry(); err != nil {
			errch <- err
			return
		}

		if err := clearClipboard(); err != nil {
			errch <- err
			closeClipboard.Call()
			return
		}
		for _, r := range resolved {
			if len(r.buf) == 0 {
				continue // an empty payload contributes only the emptying
			}
			if err := setClipboardBytes(r.format, r.buf); err != nil {
				errch <- err
				closeClipboard.Call()
				return
			}
		}
		// Close the clipboard otherwise other applications cannot
		// paste the data.
		closeClipboard.Call()

		cnt, _, _ := getClipboardSequenceNumber.Call()
		errch <- nil
		for {
			time.Sleep(time.Second)
			cur, _, _ := getClipboardSequenceNumber.Call()
			if cur != cnt {
				changed <- struct{}{}
				close(changed)
				return
			}
		}
	}()
	err := <-errch
	if err != nil {
		return nil, err
	}
	return changed, nil
}

// watch observes the clipboard for changes in format t until ctx is canceled.
//
// Windows will report changes rather than being asked for them: a window
// registered with AddClipboardFormatListener receives WM_CLIPBOARDUPDATE on
// every clipboard change (#153). watchEvent takes that path; watchPoll is the
// fallback for the environments where a window cannot be created at all, such
// as a service running in Session 0 (#145).
func watch(ctx context.Context, sel selection, t Format) <-chan []byte {
	if sel == selPrimary {
		// No primary selection on Windows: nothing will ever be delivered, so
		// say so by closing rather than leaving the caller waiting.
		recv := make(chan []byte)
		close(recv)
		return recv
	}
	if recv, ok := watchEvent(ctx, sel, t); ok {
		return recv
	}
	return watchPoll(ctx, sel, t)
}

// watchEvent watches through a message-only window registered as a clipboard
// format listener, so a change is delivered when it happens instead of at the
// next tick. It reports false — having cleaned up whatever it did create — when
// the window cannot be set up, leaving the caller to fall back to polling.
func watchEvent(ctx context.Context, sel selection, t Format) (<-chan []byte, bool) {
	if !clipboardListenerAvailable() {
		return nil, false
	}

	recv := make(chan []byte, 1)
	// started reports whether the window is up and listening. watch blocks on
	// it, so the fallback decision is made before Watch returns to the user.
	started := make(chan bool, 1)

	go func() {
		// A window belongs to the thread that created it and its messages are
		// only retrievable there, so this goroutine keeps the thread for as
		// long as it holds the window. It never unlocks: letting the goroutine
		// exit while locked retires the thread along with its message queue.
		runtime.LockOSThread()

		hwnd, err := newMessageWindow()
		if err != nil {
			started <- false
			return
		}
		defer destroyWindow.Call(hwnd)

		if r, _, _ := addClipboardFormatListener.Call(hwnd); r == 0 {
			started <- false
			return
		}
		defer removeClipboardFormatListener.Call(hwnd)

		// Baseline the sequence number before announcing readiness: a change
		// made the instant Watch returns must not slip through, and a
		// WM_CLIPBOARDUPDATE that merely follows registration must not be
		// reported as one.
		cnt, _, _ := getClipboardSequenceNumber.Call()
		started <- true

		// PostMessageW is the only way to reach a thread parked in GetMessageW,
		// and it is safe to call from another goroutine. This goroutine ends
		// with ctx, so it outlives nothing.
		go func() {
			<-ctx.Done()
			postMessageW.Call(hwnd, wmWatchStop, 0, 0)
		}()

		var msg wndMsg
		for {
			// Filtered to hwnd: only this window's messages are retrieved.
			r, _, _ := getMessageW.Call(uintptr(unsafe.Pointer(&msg)), hwnd, 0, 0)
			if int32(r) <= 0 { // 0 is WM_QUIT, -1 an error; both end the watch.
				close(recv)
				return
			}
			if msg.Message == wmWatchStop {
				close(recv)
				return
			}
			if msg.Message != wmClipboardUpdate {
				continue
			}
			cur, _, _ := getClipboardSequenceNumber.Call()
			if cur == cnt {
				continue
			}
			// The listener fires for every change, whatever its format; a nil
			// read means this change was not in the watched one.
			b, _ := Read(ctx, t, withSelection(sel)) // a failed read is nothing new to report
			if b == nil {
				cnt = cur
				continue
			}
			select {
			case recv <- b:
				cnt = cur
			case <-ctx.Done():
				close(recv)
				return
			}
		}
	}()

	if !<-started {
		return nil, false
	}
	return recv, true
}

// watchPoll observes the clipboard by comparing the sequence number on a fixed
// tick. It is the fallback for backends and environments without a usable
// clipboard listener; changes closer together than the interval coalesce.
func watchPoll(ctx context.Context, sel selection, t Format) <-chan []byte {
	recv := make(chan []byte, 1)
	ready := make(chan struct{})
	go func() {
		// not sure if we are too slow or the user too fast :)
		ti := time.NewTicker(time.Second)
		defer ti.Stop()
		cnt, _, _ := getClipboardSequenceNumber.Call()
		ready <- struct{}{}
		for {
			select {
			case <-ctx.Done():
				close(recv)
				return
			case <-ti.C:
				cur, _, _ := getClipboardSequenceNumber.Call()
				if cnt != cur {
					b, _ := Read(ctx, t, withSelection(sel)) // a failed read is nothing new to report
					if b == nil {
						continue
					}
					select {
					case recv <- b:
						cnt = cur
					case <-ctx.Done():
						close(recv)
						return
					}
				}
			}
		}
	}()
	<-ready
	return recv
}

const (
	// WM_CLIPBOARDUPDATE, posted to every registered clipboard format listener
	// when the clipboard changes.
	wmClipboardUpdate = 0x031D
	// wmWatchStop is a private message (the WM_APP range is reserved for
	// application use) posted to unpark a watcher from GetMessageW on cancel.
	wmWatchStop = 0x8000 + 1 // WM_APP+1
	// HWND_MESSAGE, i.e. (HWND)-3. A window parented to it is message-only: no
	// screen presence, no z-order, no input — it exists to receive messages.
	hwndMessage = ^uintptr(2)
)

// wndMsg is the Win32 MSG structure. LPrivate is undocumented but present in
// the layout, so it is included: GetMessage writes the whole struct.
type wndMsg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       struct{ X, Y int32 }
	LPrivate uint32
}

// wndClassEx is the Win32 WNDCLASSEXW structure.
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

var (
	// The window class is registered once per process. syscall.NewCallback
	// allocates a callback that is never released and the process has a hard
	// cap on how many it may hold, so registering per watch() would eventually
	// panic a program that starts and cancels watchers in a loop.
	watchClassOnce sync.Once
	watchClassName *uint16
	watchClassAtom uintptr
)

// registerWatchClass registers the window class the watcher's message-only
// window is created from. Its window procedure only defers to DefWindowProcW:
// WM_CLIPBOARDUPDATE is a posted message, so the message loop reads it straight
// off the queue and nothing needs handling here.
func registerWatchClass() {
	name, err := syscall.UTF16PtrFromString("golang.design.clipboard.watch")
	if err != nil {
		return
	}
	instance, _, _ := getModuleHandleW.Call(0)
	wc := wndClassEx{
		Style: 0,
		WndProc: syscall.NewCallback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
			r, _, _ := defWindowProcW.Call(hwnd, msg, wparam, lparam)
			return r
		}),
		Instance:  instance,
		ClassName: name,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	atom, _, _ := registerClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return
	}
	watchClassName, watchClassAtom = name, atom
}

// newMessageWindow creates a message-only window on the calling thread. The
// caller must have locked the OS thread and must destroy the window from it.
func newMessageWindow() (uintptr, error) {
	watchClassOnce.Do(registerWatchClass)
	if watchClassAtom == 0 {
		return 0, errUnavailable
	}
	instance, _, _ := getModuleHandleW.Call(0)
	hwnd, _, err := createWindowExW.Call(
		0,                                       // dwExStyle
		uintptr(unsafe.Pointer(watchClassName)), // lpClassName
		0,                                       // lpWindowName
		0,                                       // dwStyle
		0, 0, 0, 0,                              // x, y, nWidth, nHeight
		hwndMessage, // hWndParent: message-only
		0,           // hMenu
		instance,    // hInstance
		0,           // lpParam
	)
	if hwnd == 0 {
		return 0, err
	}
	return hwnd, nil
}

// clipboardListenerAvailable reports whether the user32 exports the event-driven
// watch needs are all present. They have shipped since Vista, so this only ever
// fails on an unexpectedly stripped system — in which case watch polls instead
// of panicking at package initialization the way MustFindProc would.
func clipboardListenerAvailable() bool {
	for _, p := range []*syscall.Proc{
		addClipboardFormatListener, removeClipboardFormatListener,
		registerClassExW, createWindowExW, destroyWindow, defWindowProcW,
		getMessageW, postMessageW,
	} {
		if p == nil {
			return false
		}
	}
	return getModuleHandleW.Find() == nil
}

const (
	cFmtBitmap      = 2 // Win+PrintScreen
	cFmtDIB         = 8
	cFmtHDrop       = 15 // a DROPFILES struct: what Explorer copies files as
	cFmtUnicodeText = 13
	cFmtDIBV5       = 17
	// Screenshot taken from special shortcut is in different format (why??), see:
	// https://jpsoft.com/forums/threads/detecting-clipboard-format.5225/
	cFmtDataObject = 49161 // Shift+Win+s, returned from enumClipboardFormats
	gmemMoveable   = 0x0002
)

type bitmapHeader struct {
	Size          uint32
	Width         uint32
	Height        uint32
	PLanes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter uint32
	YPelsPerMeter uint32
	ClrUsed       uint32
	ClrImportant  uint32
}

// Calling a Windows DLL, see:
// https://github.com/golang/go/wiki/WindowsDLLs
var (
	user32 = syscall.MustLoadDLL("user32")
	// Opens the clipboard for examination and prevents other
	// applications from modifying the clipboard content.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-openclipboard
	openClipboard = user32.MustFindProc("OpenClipboard")
	// Closes the clipboard.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-closeclipboard
	closeClipboard = user32.MustFindProc("CloseClipboard")
	// Empties the clipboard and frees handles to data in the clipboard.
	// The function then assigns ownership of the clipboard to the
	// window that currently has the clipboard open.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-emptyclipboard
	emptyClipboard = user32.MustFindProc("EmptyClipboard")
	// Retrieves data from the clipboard in a specified format.
	// The clipboard must have been opened previously.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getclipboarddata
	getClipboardData = user32.MustFindProc("GetClipboardData")
	// Places data on the clipboard in a specified clipboard format.
	// The window must be the current clipboard owner, and the
	// application must have called the OpenClipboard function. (When
	// responding to the WM_RENDERFORMAT message, the clipboard owner
	// must not call OpenClipboard before calling SetClipboardData.)
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setclipboarddata
	setClipboardData = user32.MustFindProc("SetClipboardData")
	// Determines whether the clipboard contains data in the specified format.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-isclipboardformatavailable
	isClipboardFormatAvailable = user32.MustFindProc("IsClipboardFormatAvailable")
	// Clipboard data formats are stored in an ordered list. To perform
	// an enumeration of clipboard data formats, you make a series of
	// calls to the EnumClipboardFormats function. For each call, the
	// format parameter specifies an available clipboard format, and the
	// function returns the next available clipboard format.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-isclipboardformatavailable
	enumClipboardFormats = user32.MustFindProc("EnumClipboardFormats")
	// Retrieves the clipboard sequence number for the current window station.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getclipboardsequencenumber
	getClipboardSequenceNumber = user32.MustFindProc("GetClipboardSequenceNumber")
	// Registers a new clipboard format. This format can then be used as
	// a valid clipboard format.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-registerclipboardformata
	registerClipboardFormatA = user32.MustFindProc("RegisterClipboardFormatA")
	// Retrieves from the clipboard the name of the specified registered format.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getclipboardformatnamea
	getClipboardFormatNameA = user32.MustFindProc("GetClipboardFormatNameA")

	// The event-driven watch (#153) needs a window to receive
	// WM_CLIPBOARDUPDATE on. These are looked up leniently rather than with
	// MustFindProc: they have shipped since Vista, but a missing one should
	// cost the latency win and fall back to polling, not panic every importer
	// of this package at initialization.

	// Places the given window in the system-maintained clipboard format
	// listener list, which receives WM_CLIPBOARDUPDATE on every change.
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-addclipboardformatlistener
	addClipboardFormatListener = findProc(user32, "AddClipboardFormatListener")
	// Removes the given window from the clipboard format listener list.
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-removeclipboardformatlistener
	removeClipboardFormatListener = findProc(user32, "RemoveClipboardFormatListener")
	// Registers a window class for subsequent use in calls to CreateWindowEx.
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-registerclassexw
	registerClassExW = findProc(user32, "RegisterClassExW")
	// Creates a window. With HWND_MESSAGE as the parent the window is
	// message-only: invisible, and used purely to receive messages.
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-createwindowexw
	createWindowExW = findProc(user32, "CreateWindowExW")
	// Destroys the given window.
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-destroywindow
	destroyWindow = findProc(user32, "DestroyWindow")
	// Provides default processing for any window message a window procedure
	// does not handle.
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-defwindowprocw
	defWindowProcW = findProc(user32, "DefWindowProcW")
	// Retrieves a message from the calling thread's message queue, blocking
	// until one arrives.
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getmessagew
	getMessageW = findProc(user32, "GetMessageW")
	// Places a message in a window's message queue and returns without waiting.
	// It is safe to call from another thread, which is how a watcher parked in
	// GetMessageW is unparked on cancel.
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-postmessagew
	postMessageW = findProc(user32, "PostMessageW")

	kernel32 = syscall.NewLazyDLL("kernel32")

	// Locks a global memory object and returns a pointer to the first
	// byte of the object's memory block.
	// https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globallock
	gLock = kernel32.NewProc("GlobalLock")
	// Decrements the lock count associated with a memory object that was
	// allocated with GMEM_MOVEABLE. This function has no effect on memory
	// objects allocated with GMEM_FIXED.
	// https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globalunlock
	gUnlock = kernel32.NewProc("GlobalUnlock")
	// Allocates the specified number of bytes from the heap.
	// https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globalalloc
	gAlloc = kernel32.NewProc("GlobalAlloc")
	// Frees the specified global memory object and invalidates its handle.
	// https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globalfree
	gFree   = kernel32.NewProc("GlobalFree")
	memMove = kernel32.NewProc("RtlMoveMemory")
	// Retrieves the current size of the specified global memory object, in
	// bytes. Used to size reads of raw custom-format data.
	// https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globalsize
	gSize = kernel32.NewProc("GlobalSize")
	// Retrieves a module handle for the calling process; the window class the
	// watcher registers is owned by it.
	// https://learn.microsoft.com/en-us/windows/win32/api/libloaderapi/nf-libloaderapi-getmodulehandlew
	getModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

// findProc resolves an exported symbol, returning nil when it or its DLL is
// absent. Unlike MustFindProc it lets an optional API degrade at the call site
// rather than panicking during package initialization.
func findProc(dll *syscall.DLL, name string) *syscall.Proc {
	if dll == nil {
		return nil
	}
	p, err := dll.FindProc(name)
	if err != nil {
		return nil
	}
	return p
}
