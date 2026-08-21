// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build ios

package clipboard

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework UIKit -framework MobileCoreServices

#import <stdlib.h>
void clipboard_write_string(char *s);
char *clipboard_read_string();
*/
import "C"
import (
	"bytes"
	"context"
	"time"
	"unsafe"
)

func initialize() error { return nil }

// enumerateFormats reports the formats on the clipboard. The iOS bridge exposes
// only text and no enumeration API, so Formats() returns empty.
func enumerateFormats(sel selection) []Format { return nil }

func read(sel selection, t Format) (buf []byte, err error) {
	if sel == selPrimary {
		// No primary selection on this platform (see FromPrimary).
		return nil, errUnsupported
	}
	switch t {
	case FmtText:
		return []byte(C.GoString(C.clipboard_read_string())), nil
	case FmtImage:
		return nil, errUnsupported
	default:
		// The iOS bridge handles only text; images, file lists and custom MIME
		// formats registered via Register degrade to nil here.
		return nil, errUnsupported
	}
}

// SetContent sets the clipboard content for iOS
func write(sel selection, t Format, buf []byte) (<-chan struct{}, error) {
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

		C.clipboard_write_string(cs)
		return done, nil
	case FmtImage:
		return nil, errUnsupported
	default:
		// The iOS bridge handles only text; images, file lists and custom MIME
		// formats registered via Register degrade to a no-op here.
		return nil, errUnsupported
	}
}

// writeAll publishes the most preferred item only. This platform has no
// multi-representation clipboard, and writing each item in turn would be worse
// than useless: every write replaces the last, so the *least* preferred
// representation would win — the reverse of what the caller asked for (#151).
func writeAll(sel selection, items []Item) (<-chan struct{}, error) {
	return write(sel, items[0].Format, items[0].Bytes)
}

func watch(ctx context.Context, sel selection, t Format) <-chan []byte {
	recv := make(chan []byte, 1)
	ti := time.NewTicker(time.Second)
	last := Read(t, withSelection(sel))
	go func() {
		defer ti.Stop()
		for {
			select {
			case <-ctx.Done():
				close(recv)
				return
			case <-ti.C:
				b := Read(t, withSelection(sel))
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
