// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build android

package clipboard

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
char *clipboard_read_string(uintptr_t java_vm, uintptr_t jni_env, uintptr_t ctx);
void clipboard_write_string(uintptr_t java_vm, uintptr_t jni_env, uintptr_t ctx, char *str);

*/
import "C"
import (
	"bytes"
	"context"
	"time"
	"unsafe"

	"golang.org/x/mobile/app"
)

func initialize() error { return nil }

// enumerateFormats reports the formats on the clipboard. The Android bridge
// exposes only text and no enumeration API, so Formats() returns empty.
func enumerateFormats(ctx context.Context, sel selection) []Format { return nil }

func read(ctx context.Context, sel selection, t Format) (buf []byte, err error) {
	if sel == selPrimary {
		// No primary selection on this platform (see FromPrimary).
		return nil, errUnsupported
	}
	switch t {
	case FmtText:
		s := ""
		if err := app.RunOnJVM(func(vm, env, ctx uintptr) error {
			cs := C.clipboard_read_string(C.uintptr_t(vm), C.uintptr_t(env), C.uintptr_t(ctx))
			if cs == nil {
				return nil
			}

			s = C.GoString(cs)
			C.free(unsafe.Pointer(cs))
			return nil
		}); err != nil {
			return nil, err
		}
		return []byte(s), nil
	case FmtImage:
		return nil, errUnsupported
	default:
		// The Android bridge handles only text; images, file lists and custom MIME
		// formats registered via Register degrade to nil here.
		return nil, errUnsupported
	}
}

// write writes the given data to clipboard and
// returns true if success or false if failed.
func write(ctx context.Context, sel selection, t Format, buf []byte) (<-chan struct{}, error) {
	if sel == selPrimary {
		// No primary selection here, and redirecting to the ordinary clipboard
		// would destroy what the user had copied (see FromPrimary).
		return nil, errUnsupported
	}
	done := make(chan struct{}, 1)
	switch t {
	case FmtText:
		cs := C.CString(string(buf))
		defer C.free(unsafe.Pointer(cs))

		if err := app.RunOnJVM(func(vm, env, ctx uintptr) error {
			C.clipboard_write_string(C.uintptr_t(vm), C.uintptr_t(env), C.uintptr_t(ctx), cs)
			done <- struct{}{}
			return nil
		}); err != nil {
			return nil, err
		}
		return done, nil
	case FmtImage:
		return nil, errUnsupported
	default:
		// The Android bridge handles only text; images, file lists and custom MIME
		// formats registered via Register degrade to a no-op here.
		return nil, errUnsupported
	}
}

// writeAll publishes the most preferred item only. This platform has no
// multi-representation clipboard, and writing each item in turn would be worse
// than useless: every write replaces the last, so the *least* preferred
// representation would win — the reverse of what the caller asked for (#151).
func writeAll(ctx context.Context, sel selection, items []Item, loops int) (<-chan struct{}, error) {
	// loops is ignored: this platform's clipboard is a store the OS serves, so
	// no paste request ever reaches this process to be counted (see Loops).
	_ = loops
	return write(ctx, sel, items[0].Format, items[0].Bytes)
}

func watch(ctx context.Context, sel selection, t Format) <-chan []byte {
	recv := make(chan []byte, 1)
	ti := time.NewTicker(time.Second)
	last, _ := Read(ctx, t, withSelection(sel))
	go func() {
		defer ti.Stop()
		for {
			select {
			case <-ctx.Done():
				close(recv)
				return
			case <-ti.C:
				b, _ := Read(ctx, t, withSelection(sel)) // a failed read is nothing new to report
				if b == nil {
					continue
				}
				if !bytes.Equal(last, b) {
					select {
					case recv <- b:
						last = b
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
