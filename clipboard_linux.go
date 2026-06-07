// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

package clipboard

// Linux clipboard dispatch. Both backends are pure Go: the native Wayland
// backend (clipboard_wayland_linux.go) when a data-control manager is present,
// otherwise the X11 backend (clipboard_x11_linux.go). Neither needs Cgo, so the
// package builds and runs on Linux with CGO_ENABLED=0 and no C toolchain.

import (
	"bytes"
	"context"
	"fmt"
	"time"
)

var helpmsg = `%w: Failed to connect to the X11 display, so the clipboard
package will not work properly. Make sure an X server is running and the
DISPLAY environment variable is set.

If the clipboard package runs in an environment without a frame buffer,
such as a cloud server, it may be necessary to install xvfb:

	apt install -y xvfb

and initialize a virtual frame buffer:

	Xvfb :99 -screen 0 1024x768x24 > /dev/null 2>&1 &
	export DISPLAY=:99.0

Then this package should be ready to use.
`

func initialize() error {
	// Prefer the native Wayland backend when running under a Wayland session
	// that exposes a data-control manager; this avoids the XWayland bridge and
	// works without an X server. Fall back to X11 otherwise (including Wayland
	// sessions whose compositor lacks data-control, via XWayland).
	if wlAvailable() {
		useWayland = true
		return nil
	}
	if err := x11Test(); err != nil {
		return fmt.Errorf(helpmsg, errUnavailable)
	}
	return nil
}

// enumerateFormats reports the formats currently on the clipboard, via the
// Wayland data-control offer or the X11 TARGETS list.
func enumerateFormats() []Format {
	if useWayland {
		return wlEnumerateFormats()
	}
	return x11EnumerateFormats()
}

func read(t Format) (buf []byte, err error) {
	if useWayland {
		return wlRead(t)
	}
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
	if useWayland {
		return wlWrite(t, buf)
	}
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
	if useWayland {
		return wlWatch(ctx, t)
	}
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
