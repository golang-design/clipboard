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
func writeText(buf []byte) error {
	r, _, err := emptyClipboard.Call()
	if r == 0 {
		return fmt.Errorf("failed to clear clipboard: %w", err)
	}

	// empty text, we are done here.
	if len(buf) == 0 {
		return nil
	}

	s, err := syscall.UTF16FromString(string(buf))
	if err != nil {
		return fmt.Errorf("failed to convert given string: %w", err)
	}

	hMem, _, err := gAlloc.Call(gmemMoveable, uintptr(len(s)*int(unsafe.Sizeof(s[0]))))
	if hMem == 0 {
		return fmt.Errorf("failed to alloc global memory: %w", err)
	}

	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return fmt.Errorf("failed to lock global memory: %w", err)
	}
	defer gUnlock.Call(hMem)

	// no return value
	memMove.Call(p, uintptr(unsafe.Pointer(&s[0])),
		uintptr(len(s)*int(unsafe.Sizeof(s[0]))))

	v, _, err := setClipboardData.Call(cFmtUnicodeText, hMem)
	if v == 0 {
		gFree.Call(hMem)
		return fmt.Errorf("failed to set text to clipboard: %w", err)
	}

	return nil
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

func writeImage(buf []byte) error {
	r, _, err := emptyClipboard.Call()
	if r == 0 {
		return fmt.Errorf("failed to clear clipboard: %w", err)
	}

	// empty text, we are done here.
	if len(buf) == 0 {
		return nil
	}

	img, err := png.Decode(bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("input bytes is not PNG encoded: %w", err)
	}

	data := imageToDIB(img)

	hMem, _, err := gAlloc.Call(gmemMoveable,
		uintptr(len(data)*int(unsafe.Sizeof(data[0]))))
	if hMem == 0 {
		return fmt.Errorf("failed to alloc global memory: %w", err)
	}

	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return fmt.Errorf("failed to lock global memory: %w", err)
	}
	defer gUnlock.Call(hMem)

	memMove.Call(p, uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)*int(unsafe.Sizeof(data[0]))))

	v, _, err := setClipboardData.Call(cFmtDIBV5, hMem)
	if v == 0 {
		gFree.Call(hMem)
		return fmt.Errorf("failed to set text to clipboard: %w", err)
	}

	return nil
}

// registerCustomFormat maps a MIME string to a Windows clipboard format ID via
// RegisterClipboardFormat. Repeated registrations of the same name return the
// same ID, and the ID is unique per name across the window station, so this
// library and any other app naming the format identically interoperate.
func registerCustomFormat(mime string) (uintptr, error) {
	name, err := syscall.BytePtrFromString(mime)
	if err != nil {
		return 0, err
	}
	id, _, err := registerClipboardFormatA.Call(uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)
	if id == 0 {
		return 0, err
	}
	return id, nil
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
	r, _, err := emptyClipboard.Call()
	if r == 0 {
		return fmt.Errorf("failed to clear clipboard: %w", err)
	}
	if len(buf) == 0 {
		return nil
	}

	hMem, _, err := gAlloc.Call(gmemMoveable, uintptr(len(buf)))
	if hMem == 0 {
		return fmt.Errorf("failed to alloc global memory: %w", err)
	}
	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return fmt.Errorf("failed to lock global memory: %w", err)
	}
	defer gUnlock.Call(hMem)

	memMove.Call(p, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))

	v, _, err := setClipboardData.Call(format, hMem)
	if v == 0 {
		gFree.Call(hMem)
		return fmt.Errorf("failed to set custom data to clipboard: %w", err)
	}
	return nil
}

// enumerateFormats reports the formats currently on the clipboard by iterating
// the available clipboard formats with EnumClipboardFormats and mapping each to
// a Format.
func enumerateFormats() []Format {
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
// predefined text/image formats to FmtText/FmtImage, and a registered format
// whose name is a MIME type to a custom format (registered on demand).
// Predefined formats we do not model have no registered name and are skipped.
func windowsFormatFor(format uintptr) (Format, bool) {
	switch format {
	case cFmtUnicodeText:
		return FmtText, true
	case cFmtDIBV5, cFmtDIB, cFmtBitmap:
		return FmtImage, true
	}
	switch name := clipboardFormatName(format); name {
	case "":
		return 0, false
	case "UTF8_STRING", "text/plain", "text/plain;charset=utf-8":
		return FmtText, true
	case "image/png":
		return FmtImage, true
	default:
		if strings.Contains(name, "/") {
			return Register(name), true
		}
		return 0, false
	}
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

func read(t Format) (buf []byte, err error) {
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
	default:
		mime, ok := formatMIME(t)
		if !ok {
			return nil, errUnsupported
		}
		format, err = registerCustomFormat(mime)
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
	default:
		return readCustom(format)
	}
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
func write(t Format, buf []byte) (<-chan struct{}, error) {
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

		// var param uintptr
		switch t {
		case FmtImage:
			err := writeImage(buf)
			if err != nil {
				errch <- err
				closeClipboard.Call()
				return
			}
		case FmtText:
			err := writeText(buf)
			if err != nil {
				errch <- err
				closeClipboard.Call()
				return
			}
		default:
			mime, ok := formatMIME(t)
			if !ok {
				errch <- errUnsupported
				closeClipboard.Call()
				return
			}
			id, err := registerCustomFormat(mime)
			if err == nil {
				err = writeCustom(id, buf)
			}
			if err != nil {
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

func watch(ctx context.Context, t Format) <-chan []byte {
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
					b := Read(t)
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
	cFmtBitmap      = 2 // Win+PrintScreen
	cFmtDIB         = 8
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
)
