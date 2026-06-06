// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

package clipboard

// This file implements the wire-protocol core of a native Wayland clipboard
// backend (see specs/wayland-support.md). It speaks the Wayland protocol
// directly over the unix socket in pure Go — no Cgo, no libwayland — and is
// the foundation for the data-control read/write/watch paths added in later
// phases. Phase 2 covers connecting and discovering the advertised globals
// (the seat and the data-control manager).

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
)

// wlDisplayID is the well-known object id of the wl_display singleton; every
// Wayland connection starts with it.
const wlDisplayID = 1

// wlGlobal is an entry advertised by wl_registry.global.
type wlGlobal struct {
	name    uint32
	version uint32
}

// dataControlManagers lists the data-control manager interfaces we understand,
// in order of preference. ext-data-control-v1 is the standardized successor to
// the wlroots-specific zwlr_data_control_manager_v1; we accept either.
var dataControlManagers = []string{
	"ext_data_control_manager_v1",
	"zwlr_data_control_manager_v1",
}

// waylandSocketPath returns the absolute path to the Wayland display socket,
// or "" if the process is not in a Wayland session.
func waylandSocketPath() string {
	disp := os.Getenv("WAYLAND_DISPLAY")
	if disp == "" {
		return ""
	}
	if filepath.IsAbs(disp) {
		return disp
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, disp)
}

// wlConn is a minimal Wayland protocol connection.
type wlConn struct {
	c      *net.UnixConn
	nextID uint32
}

// wlConnect dials the Wayland display socket.
func wlConnect() (*wlConn, error) {
	p := waylandSocketPath()
	if p == "" {
		return nil, errUnavailable
	}
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: p, Net: "unix"})
	if err != nil {
		return nil, err
	}
	// Object ids 1.. are allocated by the client; 1 is wl_display.
	return &wlConn{c: c, nextID: wlDisplayID + 1}, nil
}

func (w *wlConn) Close() error { return w.c.Close() }

// newID allocates a fresh client-side object id.
func (w *wlConn) newID() uint32 {
	id := w.nextID
	w.nextID++
	return id
}

// request sends a Wayland request for objID with the given opcode and an
// already-encoded argument payload.
func (w *wlConn) request(objID uint32, opcode uint16, payload []byte) error {
	size := 8 + len(payload)
	if size > 0xffff {
		return fmt.Errorf("wayland: message too large (%d bytes)", size)
	}
	msg := make([]byte, size)
	binary.LittleEndian.PutUint32(msg[0:], objID)
	binary.LittleEndian.PutUint32(msg[4:], uint32(size)<<16|uint32(opcode))
	copy(msg[8:], payload)
	_, err := w.c.Write(msg)
	return err
}

// readEvent reads a single event: the sender object id, the opcode, and the
// (header-stripped) body.
func (w *wlConn) readEvent() (objID uint32, opcode uint16, body []byte, err error) {
	var hdr [8]byte
	if _, err = io.ReadFull(w.c, hdr[:]); err != nil {
		return 0, 0, nil, err
	}
	objID = binary.LittleEndian.Uint32(hdr[0:])
	word := binary.LittleEndian.Uint32(hdr[4:])
	size := int(word >> 16)
	opcode = uint16(word & 0xffff)
	if size < 8 {
		return 0, 0, nil, fmt.Errorf("wayland: invalid message size %d", size)
	}
	body = make([]byte, size-8)
	if _, err = io.ReadFull(w.c, body); err != nil {
		return 0, 0, nil, err
	}
	return objID, opcode, body, nil
}

// wlString decodes a length-prefixed, NUL-terminated, 32-bit-padded Wayland
// string starting at off, returning the string and the offset just past it.
func wlString(body []byte, off int) (string, int, error) {
	if off+4 > len(body) {
		return "", 0, io.ErrUnexpectedEOF
	}
	n := int(binary.LittleEndian.Uint32(body[off:]))
	off += 4
	padded := (n + 3) &^ 3
	if n == 0 || off+padded > len(body) {
		return "", 0, io.ErrUnexpectedEOF
	}
	s := string(body[off : off+n-1]) // strip trailing NUL
	return s, off + padded, nil
}

// wlListGlobals connects, asks the registry for the advertised globals, and
// returns them keyed by interface name. It uses a wl_display.sync barrier to
// know when all globals have been delivered.
func wlListGlobals() (map[string]wlGlobal, error) {
	w, err := wlConnect()
	if err != nil {
		return nil, err
	}
	defer w.Close()

	// wl_display.get_registry(new_id) — opcode 1.
	registryID := w.newID()
	arg := make([]byte, 4)
	binary.LittleEndian.PutUint32(arg, registryID)
	if err := w.request(wlDisplayID, 1, arg); err != nil {
		return nil, err
	}

	// wl_display.sync(callback new_id) — opcode 0; the callback's done event
	// marks the end of the initial registry burst.
	callbackID := w.newID()
	binary.LittleEndian.PutUint32(arg, callbackID)
	if err := w.request(wlDisplayID, 0, arg); err != nil {
		return nil, err
	}

	globals := make(map[string]wlGlobal)
	for {
		obj, opcode, body, err := w.readEvent()
		if err != nil {
			return nil, err
		}
		switch {
		case obj == wlDisplayID && opcode == 0:
			// wl_display.error(object_id, code, message)
			return nil, wlDisplayError(body)
		case obj == registryID && opcode == 0:
			// wl_registry.global(name, interface, version)
			if len(body) < 4 {
				continue
			}
			name := binary.LittleEndian.Uint32(body[0:])
			iface, off, err := wlString(body, 4)
			if err != nil || off+4 > len(body) {
				continue
			}
			version := binary.LittleEndian.Uint32(body[off:])
			globals[iface] = wlGlobal{name: name, version: version}
		case obj == callbackID && opcode == 0:
			// wl_callback.done — registry enumeration complete.
			return globals, nil
		}
	}
}

// wlDisplayError formats a wl_display.error event body for diagnostics.
func wlDisplayError(body []byte) error {
	if len(body) < 8 {
		return fmt.Errorf("wayland: protocol error")
	}
	code := binary.LittleEndian.Uint32(body[4:])
	msg, _, err := wlString(body, 8)
	if err != nil {
		return fmt.Errorf("wayland: protocol error (code %d)", code)
	}
	return fmt.Errorf("wayland: protocol error (code %d): %s", code, msg)
}

// dataControlManager returns the preferred available data-control manager
// interface name from the advertised globals, or ok=false if none is present.
func dataControlManager(globals map[string]wlGlobal) (string, wlGlobal, bool) {
	for _, iface := range dataControlManagers {
		if g, ok := globals[iface]; ok {
			return iface, g, true
		}
	}
	return "", wlGlobal{}, false
}
