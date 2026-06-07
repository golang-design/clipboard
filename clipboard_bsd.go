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
func enumerateFormats() []Format { return x11EnumerateFormats() }

func read(t Format) (buf []byte, err error) {
	switch t {
	case FmtText:
		return x11Read("UTF8_STRING")
	case FmtImage:
		return x11Read("image/png")
	default:
		mime, ok := formatMIME(t)
		if !ok {
			return nil, errUnsupported
		}
		// On X11 a MIME type is used directly as the target atom.
		return x11Read(mime)
	}
}

func write(t Format, buf []byte) (<-chan struct{}, error) {
	switch t {
	case FmtText:
		return x11Write("UTF8_STRING", buf)
	case FmtImage:
		return x11Write("image/png", buf)
	default:
		mime, ok := formatMIME(t)
		if !ok {
			return nil, errUnsupported
		}
		return x11Write(mime, buf)
	}
}

func watch(ctx context.Context, t Format) <-chan []byte {
	recv := make(chan []byte, 1)
	ti := time.NewTicker(time.Second)
	last := Read(t)
	go func() {
		defer ti.Stop()
		for {
			select {
			case <-ctx.Done():
				close(recv)
				return
			case <-ti.C:
				b := Read(t)
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
