// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows
// +build windows

package clipboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image/png"
	"os"
	"reflect"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/image/bmp"
)

// Interacting with Clipboard on Windows:
// https://docs.microsoft.com/zh-cn/windows/win32/dataxchg/using-the-clipboard

const (
	cfUnicodeText = 13
	cfHdrop       = 15 // Files
	cfDibv5       = 17 // ?
	cfBitmap      = 2  // Win+PrintScreen
	// Screenshot taken from special shortcut is in different format (why??), see:
	// https://jpsoft.com/forums/threads/detecting-clipboard-format.5225/
	cfDataObject = 49161 // Shift+Win+s
	gmemMoveable = 0x0002
)

var (
	user32                     = syscall.MustLoadDLL("user32")
	openClipboard              = user32.MustFindProc("OpenClipboard")
	closeClipboard             = user32.MustFindProc("CloseClipboard")
	emptyClipboard             = user32.MustFindProc("EmptyClipboard")
	getClipboardData           = user32.MustFindProc("GetClipboardData")
	setClipboardData           = user32.MustFindProc("SetClipboardData")
	isClipboardFormatAvailable = user32.MustFindProc("IsClipboardFormatAvailable")
	enumClipboardFormats       = user32.MustFindProc("EnumClipboardFormats")
	getClipboardSequenceNumber = user32.MustFindProc("GetClipboardSequenceNumber")

	kernel32 = syscall.NewLazyDLL("kernel32")
	gLock    = kernel32.NewProc("GlobalLock")
	gUnlock  = kernel32.NewProc("GlobalUnlock")
	gAlloc   = kernel32.NewProc("GlobalAlloc")
	gFree    = kernel32.NewProc("GlobalFree")
	lstrcpy  = kernel32.NewProc("lstrcpyW")
)

type bitmapV5HEADER struct {
	BiSize          uint32 //
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
	BV4RedMask      uint32
	BV4GreenMask    uint32
	BV4BlueMask     uint32
	BV4AlphaMask    uint32
	BV4CSType       uint32
	BV4Endpoints    struct {
		CiexyzRed, CiexyzGreen, CiexyzBlue struct {
			CiexyzX, CiexyzY, CiexyzZ int32 // FXPT2DOT30
		}
	}
	BV4GammaRed    uint32
	BV4GammaGreen  uint32
	BV4GammaBlue   uint32
	BV5Intent      uint32
	BV5ProfileData uint32
	BV5ProfileSize uint32
	BV5Reserved    uint32
}

// FIXME: return detailed error would be useful.
func read(t Format) (buf []byte) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var param uintptr
	switch t {
	case FmtImage:
		param = cfDibv5
	case FmtText:
		fallthrough
	default:
		param = cfUnicodeText
	}

	r, _, err := isClipboardFormatAvailable.Call(param)
	if r == 0 {
		return nil
	}

	r, _, err = openClipboard.Call()
	if r == 0 {
		return nil
	}
	defer closeClipboard.Call()

	f, _, err := enumClipboardFormats.Call(0)
	if f == 0 {
		return nil
	}

	h, _, err := getClipboardData.Call(param)
	if h == 0 {
		return nil
	}

	l, _, err := gLock.Call(h)
	if l == 0 {
		return nil
	}

	switch param {
	case cfDibv5:
		b := readImage(unsafe.Pointer(l))
		img, err := bmp.Decode(bytes.NewReader(b))
		if err != nil {
			return nil
		}
		var buf bytes.Buffer
		err = png.Encode(&buf, img)
		if err != nil {
			return nil
		}
		return buf.Bytes()
		// return readImage(unsafe.Pointer(l))
	case cfUnicodeText:
		fallthrough
	default:
		s := syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(l))[:])
		r, _, err = gUnlock.Call(h)
		if r == 0 {
			fmt.Fprintf(os.Stderr, "failed to unlock clipboard: %v\n", err)
			return nil
		}
		return bytes.NewBufferString(s).Bytes()
	}
}

// readImage reads image from given handle
//
// ref: https://github.com/YanxinTang/clipboard-online/blob/ab60d1b00dc8e50b7aaa20bc40dd97b6ef1fce3e/utils/clipboard.go#L116
func readImage(p unsafe.Pointer) []byte {
	header := (*bitmapV5HEADER)(p)
	if header.BiSizeImage == 0 { // just for safety
		header.BiSizeImage = 4 * uint32(header.BiWidth) * uint32(header.BiHeight)
	}
	siz := 14 + header.BiSize + header.BiSizeImage
	ret := make([]byte, siz)
	binary.LittleEndian.PutUint16(ret[0:], 0x4d42) // BM
	binary.LittleEndian.PutUint32(ret[2:], siz)
	binary.LittleEndian.PutUint16(ret[6:], 0)
	binary.LittleEndian.PutUint16(ret[8:], 0)
	binary.LittleEndian.PutUint32(ret[10:], 14+header.BiSize)

	var data []byte
	sh := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	sh.Data = uintptr(p)
	sh.Cap = int(header.BiSize + header.BiSizeImage)
	sh.Len = int(header.BiSize + header.BiSizeImage)

	// If compression is set to BITFIELDS, but the bitmask is set to the
	// default bitmask that would be used if compression was set to 0,
	// we can continue as if compression was 0. See:
	// https://github.com/golang/image/blob/4410531fe0302c24ddced794ec5760f25dd22066/bmp/reader.go#L177
	if header.BiCompression == 3 && header.BV4RedMask == 0xff0000 &&
		header.BV4GreenMask == 0xff00 && header.BV4BlueMask == 0xff {
		header.BiCompression = 0 // BI_RGB
	}
	copy(ret[14:], data[:])
	return ret
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
func write(t Format, buf []byte) (bool, <-chan struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r, _, err := openClipboard.Call(0)
	if r == 0 {
		fmt.Fprintf(os.Stderr, "failed to open clipboard: %v\n", err)
		return false, nil
	}
	defer closeClipboard.Call()

	r, _, err = emptyClipboard.Call(0)
	if r == 0 {
		fmt.Fprintf(os.Stderr, "failed to clear clipboard: %v\n", err)
		return false, nil
	}

	// FIXME: encode buf to bitmap format depends on the format
	data := syscall.StringToUTF16(string(buf))

	// Doc: If the hMem parameter identifies a memory object, the object must have
	// been allocated using the function with the GMEM_MOVEABLE flag.
	h, _, err := gAlloc.Call(gmemMoveable, uintptr(len(data)*int(unsafe.Sizeof(data[0]))))
	if h == 0 {
		fmt.Fprintf(os.Stderr, "failed to alloc clipboard data buffer: %v\n", err)
		return false, nil
	}
	defer func() {
		if h != 0 {
			gFree.Call(h)
		}
	}()

	l, _, err := gLock.Call(h)
	if l == 0 {
		fmt.Fprintf(os.Stderr, "failed to lock alloc handle: %v\n", err)
		return false, nil
	}

	r, _, err = lstrcpy.Call(l, uintptr(unsafe.Pointer(&data[0])))
	if r == 0 {
		fmt.Fprintf(os.Stderr, "failed to convert data: %v\n", err)
		return false, nil
	}

	r, _, err = gUnlock.Call(h)
	if r == 0 {
		if err.(syscall.Errno) != 0 {
			fmt.Fprintf(os.Stderr, "failed to unlock clipboard lock: %v\n", err)
			return false, nil
		}
	}

	var param uintptr
	switch t {
	case FmtImage:
		param = cfBitmap
	case FmtText:
		fallthrough
	default:
		param = cfUnicodeText
	}

	r, _, err = setClipboardData.Call(param, h)
	if r == 0 {
		fmt.Fprintf(os.Stderr, "failed to set data to clipboard: %v\n", err)
		return false, nil
	}
	h = 0 // don't do global free if setclipboarddata works

	// TODO: listener
	done := make(chan struct{}, 1)
	go func() {
		done <- struct{}{}
	}()

	return true, done
}

func watch(ctx context.Context, t Format) <-chan []byte {
	// TODO:
	panic("unimplemented")
}
