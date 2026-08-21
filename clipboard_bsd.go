// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

// NOTE: FreeBSD and OpenBSD are verified to build in CI. NetBSD shares the
// same pure-Go X11 backend and is included on a best-effort basis, but it is
// not covered by CI and has not been runtime-tested.

//go:build (openbsd || freebsd || netbsd) && !android

package clipboard

// BSD clipboard dispatch. It uses the shared pure-Go X11 backend
// (clipboard_x11.go), so it needs no Cgo and no libX11. The BSDs have no native
// Wayland backend here, so this dispatches only to X11.

import (
	"bytes"
	"context"
	"fmt"
	"time"
)

var helpmsg = `%w: Failed to connect to the X11 display, so the clipboard
package will not work properly. Make sure an X server is running and the
DISPLAY environment variable is set.

If the clipboard package runs in an environment without a frame buffer, it may
be necessary to start a virtual frame buffer (e.g. Xvfb) and point DISPLAY at
it. Then this package should be ready to use.
`

func initialize() error {
	if err := x11Test(); err != nil {
		return fmt.Errorf(helpmsg, errUnavailable)
	}
	return nil
}

// enumerateFormats reports the formats currently on the clipboard via the shared
// X11 TARGETS enumeration.
func enumerateFormats(sel selection) []Format { return x11EnumerateFormats(sel) }

func read(sel selection, t Format) (buf []byte, err error) {
	target, ok := x11TargetFor(t)
	if !ok {
		return nil, errUnsupported
	}
	// On X11 a MIME type is used directly as the target atom.
	return x11Read(sel, target)
}

func write(sel selection, t Format, buf []byte) (<-chan struct{}, error) {
	return writeAll(sel, []Item{{Format: t, Bytes: buf}})
}

func writeAll(sel selection, items []Item) (<-chan struct{}, error) {
	return x11WritePayloads(sel, items)
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
