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
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// Interacting with Clipboard on Windows:
// https://docs.microsoft.com/zh-cn/windows/win32/dataxchg/using-the-clipboard

const (
	cfUnicodeText = 13
	cfBitmap      = 2
	gmemMoveable  = 0x0002
)

var (
	user32                     = syscall.MustLoadDLL("user32")
	openClipboard              = user32.MustFindProc("OpenClipboard")
	closeClipboard             = user32.MustFindProc("CloseClipboard")
	emptyClipboard             = user32.MustFindProc("EmptyClipboard")
	getClipboardData           = user32.MustFindProc("GetClipboardData")
	setClipboardData           = user32.MustFindProc("SetClipboardData")
	getClipboardSequenceNumber = user32.MustFindProc("GetClipboardSequenceNumber")

	kernel32 = syscall.NewLazyDLL("kernel32")
	gLock    = kernel32.NewProc("GlobalLock")
	gUnlock  = kernel32.NewProc("GlobalUnlock")
	gAlloc   = kernel32.NewProc("GlobalAlloc")
	gFree    = kernel32.NewProc("GlobalFree")
	lstrcpy  = kernel32.NewProc("lstrcpyW")
)

func read(t MIMEType) (buf []byte) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r, _, err := openClipboard.Call(0)
	if r == 0 {
		fmt.Fprintf(os.Stderr, "failed to open clipboard: %v\n", err)
		return nil
	}
	defer closeClipboard.Call()

	var param uintptr
	switch t {
	case MIMEImage:
		param = cfBitmap
	case MIMEText:
		fallthrough
	default:
		param = cfUnicodeText
	}

	h, _, err := getClipboardData.Call(param)
	if h == 0 {
		return nil
	}

	l, _, err := gLock.Call(h)
	if l == 0 {
		fmt.Fprintf(os.Stderr, "failed to lock clipboard: %v\n", err)
		return nil
	}

	s := syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(l))[:])
	r, _, err = gUnlock.Call(h)
	if r == 0 {
		fmt.Fprintf(os.Stderr, "failed to unlock clipboard: %v\n", err)
		return nil
	}

	return bytes.NewBufferString(s).Bytes()
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
func write(t MIMEType, buf []byte) (bool, <-chan struct{}) {
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
	case MIMEImage:
		param = cfBitmap
	case MIMEText:
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

	done := make(chan struct{}, 1)
	go func() {
		done <- struct{}{}
	}()

	return true, done
}
