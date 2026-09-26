// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

// NOTE: FreeBSD and OpenBSD are verified to build in CI, and the Wayland
// backend is exercised on FreeBSD under a headless sway compositor. NetBSD
// shares the same pure-Go backends and is included on a best-effort basis, but
// it is not covered by CI and has not been runtime-tested.

//go:build (openbsd || freebsd || netbsd) && !android

package clipboard

// BSD clipboard dispatch. It uses the pure-Go backends shared with Linux: the
// native Wayland backend (clipboard_wayland.go) when a data-control manager is
// present, otherwise the X11 backend (clipboard_x11.go). Neither needs Cgo,
// libX11 or libwayland (#173).

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
	// Prefer the native Wayland backend when running under a Wayland session
	// that exposes a data-control manager, as on Linux. Fall back to X11
	// otherwise (including Wayland sessions whose compositor lacks
	// data-control, via XWayland).
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
func enumerateFormats(ctx context.Context, sel selection) []Format {
	if useWayland {
		return wlEnumerateFormats(sel)
	}
	return x11EnumerateFormats(ctx, sel)
}

func read(ctx context.Context, sel selection, t Format) (buf []byte, err error) {
	if useWayland {
		return wlRead(sel, t)
	}
	target, ok := x11TargetFor(t)
	if !ok {
		return nil, errUnsupported
	}
	// On X11 a MIME type is used directly as the target atom.
	return x11Read(ctx, sel, target)
}

func writeAll(ctx context.Context, sel selection, items []Item, loops int) (<-chan struct{}, error) {
	if useWayland {
		return wlWriteAll(sel, items, loops)
	}
	return x11WritePayloads(sel, items, loops)
}

func watch(ctx context.Context, sel selection, t Format) <-chan []byte {
	if useWayland {
		return wlWatch(ctx, sel, t)
	}
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

// sensitive reports whether the content was marked sensitive (see Sensitive):
// by the Wayland offer's MIME types, or the X11 selection's targets.
func sensitive(ctx context.Context, sel selection) (bool, error) {
	if useWayland {
		mimes, err := wlSelectionMIMEs(sel)
		if err != nil {
			return false, err
		}
		return containsAny(mimes, x11SensitiveTarget), nil
	}
	return x11Sensitive(ctx, sel)
}
