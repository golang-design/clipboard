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
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
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

// wlConn is a minimal Wayland protocol connection. It buffers incoming bytes
// and any file descriptors received as ancillary data (SCM_RIGHTS), so that an
// event carrying an fd (e.g. data_source.send) can be paired with it.
type wlConn struct {
	c      *net.UnixConn
	nextID uint32
	rbuf   []byte
	fds    []int
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

// fill reads one batch from the socket, accumulating message bytes and any
// file descriptors delivered as ancillary data.
func (w *wlConn) fill() error {
	var p [4096]byte
	var oob [256]byte
	n, oobn, _, _, err := w.c.ReadMsgUnix(p[:], oob[:])
	if err != nil {
		return err
	}
	if n > 0 {
		w.rbuf = append(w.rbuf, p[:n]...)
	}
	if oobn > 0 {
		if scms, err := syscall.ParseSocketControlMessage(oob[:oobn]); err == nil {
			for i := range scms {
				if fds, err := syscall.ParseUnixRights(&scms[i]); err == nil {
					w.fds = append(w.fds, fds...)
				}
			}
		}
	}
	return nil
}

// readEvent reads a single event: the sender object id, the opcode, and the
// (header-stripped) body. File descriptors carried alongside are queued and
// retrieved with nextFd.
func (w *wlConn) readEvent() (objID uint32, opcode uint16, body []byte, err error) {
	for len(w.rbuf) < 8 {
		if err := w.fill(); err != nil {
			return 0, 0, nil, err
		}
	}
	objID = binary.LittleEndian.Uint32(w.rbuf[0:])
	word := binary.LittleEndian.Uint32(w.rbuf[4:])
	size := int(word >> 16)
	opcode = uint16(word & 0xffff)
	if size < 8 {
		return 0, 0, nil, fmt.Errorf("wayland: invalid message size %d", size)
	}
	for len(w.rbuf) < size {
		if err := w.fill(); err != nil {
			return 0, 0, nil, err
		}
	}
	body = make([]byte, size-8)
	copy(body, w.rbuf[8:size])
	rest := make([]byte, len(w.rbuf)-size)
	copy(rest, w.rbuf[size:])
	w.rbuf = rest
	return objID, opcode, body, nil
}

// nextFd dequeues the next received file descriptor, if any.
func (w *wlConn) nextFd() (int, bool) {
	if len(w.fds) == 0 {
		return -1, false
	}
	fd := w.fds[0]
	w.fds = w.fds[1:]
	return fd, true
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

// Data-control protocol opcodes. ext_data_control_* and zwlr_data_control_*
// are structurally identical (ext was modeled on the wlroots protocol), so the
// same opcodes apply to whichever manager the compositor advertises.
const (
	// wl_registry
	regOpcodeBind = 0
	// wl_display
	dispOpcodeSync        = 0
	dispOpcodeGetRegistry = 1
	// manager requests
	mgrOpcodeCreateDataSource = 0
	mgrOpcodeGetDataDevice    = 1
	// device requests
	devOpcodeSetSelection = 0
	// device events
	devEvtDataOffer = 0
	devEvtSelection = 1
	// offer
	offerOpcodeReceive = 0
	offerOpcodeDestroy = 1
	offerEvtOffer      = 0
	// source requests
	srcOpcodeOffer = 0
	// source events
	srcEvtSend      = 0
	srcEvtCancelled = 1
)

// MIME types we map each Format to, in order of preference when reading.
var (
	textMIMEs  = []string{"text/plain;charset=utf-8", "UTF8_STRING", "text/plain", "STRING", "TEXT"}
	imageMIMEs = []string{"image/png"}
)

// wlEncodeString encodes a Wayland string argument: a 32-bit length including
// the trailing NUL, the bytes, the NUL, and padding to a 32-bit boundary.
func wlEncodeString(s string) []byte {
	n := len(s) + 1
	padded := (n + 3) &^ 3
	out := make([]byte, 4+padded)
	binary.LittleEndian.PutUint32(out, uint32(n))
	copy(out[4:], s)
	return out
}

// bind issues wl_registry.bind for a global and returns the new object id.
func (w *wlConn) bind(registryID, name uint32, iface string, version uint32) (uint32, error) {
	id := w.newID()
	var num [4]byte
	p := make([]byte, 0, 12+len(iface))
	binary.LittleEndian.PutUint32(num[:], name)
	p = append(p, num[:]...)
	p = append(p, wlEncodeString(iface)...)
	binary.LittleEndian.PutUint32(num[:], version)
	p = append(p, num[:]...)
	binary.LittleEndian.PutUint32(num[:], id)
	p = append(p, num[:]...)
	return id, w.request(registryID, regOpcodeBind, p)
}

// sync issues wl_display.sync and returns the callback id; the callback's done
// event marks that the server has processed all prior requests.
func (w *wlConn) sync() (uint32, error) {
	id := w.newID()
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], id)
	return id, w.request(wlDisplayID, dispOpcodeSync, b[:])
}

// requestFd sends a request whose trailing fd argument is passed as ancillary
// data (SCM_RIGHTS); fd arguments occupy no space in the message body.
func (w *wlConn) requestFd(objID uint32, opcode uint16, payload []byte, fd int) error {
	size := 8 + len(payload)
	msg := make([]byte, size)
	binary.LittleEndian.PutUint32(msg[0:], objID)
	binary.LittleEndian.PutUint32(msg[4:], uint32(size)<<16|uint32(opcode))
	copy(msg[8:], payload)
	_, _, err := w.c.WriteMsgUnix(msg, syscall.UnixRights(fd), nil)
	return err
}

// wlRead reads the current clipboard selection for the given format.
func wlRead(t Format) ([]byte, error) {
	switch t {
	case FmtText:
		return wlReadSelection(textMIMEs)
	case FmtImage:
		return wlReadSelection(imageMIMEs)
	default:
		mime, ok := formatMIME(t)
		if !ok {
			return nil, errUnsupported
		}
		// Wayland advertises selections by MIME type, so a custom format's
		// MIME string is offered/requested verbatim.
		return wlReadSelection([]string{mime})
	}
}

// wlReadSelection connects to the compositor and reads the regular clipboard
// selection, returning the bytes for the first of mimes the current offer
// provides. It returns (nil, nil) if the clipboard is empty or holds none of
// the requested types.
func wlReadSelection(mimes []string) ([]byte, error) {
	w, _, deviceID, err := wlConnectDevice()
	if err != nil {
		return nil, err
	}
	defer w.Close()

	sync2, err := w.sync()
	if err != nil {
		return nil, err
	}

	// Collect the offers the device announces and the current selection.
	offers := make(map[uint32][]string)
	var selection uint32
	for {
		obj, op, body, err := w.readEvent()
		if err != nil {
			return nil, err
		}
		switch {
		case obj == wlDisplayID && op == 0:
			return nil, wlDisplayError(body)
		case obj == deviceID && op == devEvtDataOffer && len(body) >= 4:
			offers[binary.LittleEndian.Uint32(body[0:])] = nil
		case obj == deviceID && op == devEvtSelection && len(body) >= 4:
			selection = binary.LittleEndian.Uint32(body[0:])
		case op == offerEvtOffer:
			if _, isOffer := offers[obj]; isOffer {
				if mime, _, err := wlString(body, 0); err == nil {
					offers[obj] = append(offers[obj], mime)
				}
			}
		}
		if obj == sync2 && op == 0 {
			break
		}
	}
	if selection == 0 {
		return nil, nil // empty clipboard
	}

	chosen := pickMIME(mimes, offers[selection])
	if chosen == "" {
		return nil, nil // none of the requested formats are available
	}
	data, err := wlReceiveOffer(w, selection, chosen)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil // normalize empty to nil, matching the X11 backend
	}
	return data, nil
}

// wlSelectionMIMEs returns the MIME types advertised by the current clipboard
// selection, or nil if the clipboard is empty. It mirrors wlReadSelection's
// offer-collection but returns the type list instead of receiving data.
func wlSelectionMIMEs() ([]string, error) {
	w, _, deviceID, err := wlConnectDevice()
	if err != nil {
		return nil, err
	}
	defer w.Close()

	sync2, err := w.sync()
	if err != nil {
		return nil, err
	}

	offers := make(map[uint32][]string)
	var selection uint32
	for {
		obj, op, body, err := w.readEvent()
		if err != nil {
			return nil, err
		}
		switch {
		case obj == wlDisplayID && op == 0:
			return nil, wlDisplayError(body)
		case obj == deviceID && op == devEvtDataOffer && len(body) >= 4:
			offers[binary.LittleEndian.Uint32(body[0:])] = nil
		case obj == deviceID && op == devEvtSelection && len(body) >= 4:
			selection = binary.LittleEndian.Uint32(body[0:])
		case op == offerEvtOffer:
			if _, isOffer := offers[obj]; isOffer {
				if mime, _, err := wlString(body, 0); err == nil {
					offers[obj] = append(offers[obj], mime)
				}
			}
		}
		if obj == sync2 && op == 0 {
			break
		}
	}
	if selection == 0 {
		return nil, nil
	}
	return offers[selection], nil
}

// wlEnumerateFormats maps the MIME types of the current selection to Format
// tokens, registering custom types on demand.
func wlEnumerateFormats() []Format {
	mimes, err := wlSelectionMIMEs()
	if err != nil {
		return nil
	}
	out := make([]Format, 0, len(mimes))
	for _, m := range mimes {
		out = append(out, wlFormatForMIME(m))
	}
	return out
}

// wlFormatForMIME maps a Wayland MIME type to a Format: any of the known text
// types to FmtText, image/png to FmtImage, and anything else to a custom format
// registered on demand.
func wlFormatForMIME(m string) Format {
	for _, tm := range textMIMEs {
		if m == tm {
			return FmtText
		}
	}
	for _, im := range imageMIMEs {
		if m == im {
			return FmtImage
		}
	}
	return Register(m)
}

// wlReceiveOffer requests data for mime from the given offer and reads it from
// a pipe whose write end is handed to the compositor (SCM_RIGHTS).
func wlReceiveOffer(w *wlConn, offerID uint32, mime string) ([]byte, error) {
	r, wp, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	if err := w.requestFd(offerID, offerOpcodeReceive, wlEncodeString(mime), int(wp.Fd())); err != nil {
		r.Close()
		wp.Close()
		return nil, err
	}
	wp.Close() // close our copy so the reader observes EOF once the source is done
	data, err := io.ReadAll(r)
	r.Close()
	return data, err
}

// pickMIME returns the first of want that appears in have, or "".
func pickMIME(want, have []string) string {
	for _, m := range want {
		for _, a := range have {
			if a == m {
				return m
			}
		}
	}
	return ""
}

// wlConnectDevice opens a Wayland connection, discovers the globals, binds the
// data-control manager and the seat, and creates the data-control device. It
// returns the open connection (the caller must Close it), the manager id, and
// the device id. Server events from get_data_device are left unread for the
// caller to process.
func wlConnectDevice() (w *wlConn, managerID, deviceID uint32, err error) {
	w, err = wlConnect()
	if err != nil {
		return nil, 0, 0, err
	}

	registryID := w.newID()
	arg := make([]byte, 4)
	binary.LittleEndian.PutUint32(arg, registryID)
	if err = w.request(wlDisplayID, dispOpcodeGetRegistry, arg); err != nil {
		w.Close()
		return nil, 0, 0, err
	}
	sync1, err := w.sync()
	if err != nil {
		w.Close()
		return nil, 0, 0, err
	}

	globals := make(map[string]wlGlobal)
	for {
		obj, op, body, e := w.readEvent()
		if e != nil {
			w.Close()
			return nil, 0, 0, e
		}
		if obj == wlDisplayID && op == 0 {
			w.Close()
			return nil, 0, 0, wlDisplayError(body)
		}
		if obj == registryID && op == 0 && len(body) >= 4 {
			name := binary.LittleEndian.Uint32(body[0:])
			iface, off, e := wlString(body, 4)
			if e == nil && off+4 <= len(body) {
				globals[iface] = wlGlobal{name: name, version: binary.LittleEndian.Uint32(body[off:])}
			}
		}
		if obj == sync1 && op == 0 {
			break
		}
	}

	mgrIface, mgr, ok := dataControlManager(globals)
	if !ok {
		w.Close()
		return nil, 0, 0, errUnavailable
	}
	seat, ok := globals["wl_seat"]
	if !ok {
		w.Close()
		return nil, 0, 0, errUnavailable
	}
	if managerID, err = w.bind(registryID, mgr.name, mgrIface, 1); err != nil {
		w.Close()
		return nil, 0, 0, err
	}
	seatID, err := w.bind(registryID, seat.name, "wl_seat", 1)
	if err != nil {
		w.Close()
		return nil, 0, 0, err
	}

	// manager.get_data_device(new_id device, seat)
	deviceID = w.newID()
	dd := make([]byte, 8)
	binary.LittleEndian.PutUint32(dd[0:], deviceID)
	binary.LittleEndian.PutUint32(dd[4:], seatID)
	if err = w.request(managerID, mgrOpcodeGetDataDevice, dd); err != nil {
		w.Close()
		return nil, 0, 0, err
	}
	return w, managerID, deviceID, nil
}

// wlWrite sets the clipboard selection to data for the given format and serves
// paste requests until ownership is lost. It returns a channel that is closed
// when the selection is replaced (the source's cancelled event) or the
// connection ends, matching the package Write contract.
func wlWrite(t Format, data []byte) (<-chan struct{}, error) {
	var mimes []string
	switch t {
	case FmtText:
		mimes = textMIMEs
	case FmtImage:
		mimes = imageMIMEs
	default:
		mime, ok := formatMIME(t)
		if !ok {
			return nil, errUnsupported
		}
		mimes = []string{mime}
	}

	w, managerID, deviceID, err := wlConnectDevice()
	if err != nil {
		return nil, err
	}

	// manager.create_data_source(new_id)
	sourceID := w.newID()
	arg := make([]byte, 4)
	binary.LittleEndian.PutUint32(arg, sourceID)
	if err := w.request(managerID, mgrOpcodeCreateDataSource, arg); err != nil {
		w.Close()
		return nil, err
	}
	// source.offer(mime) for each advertised type
	for _, m := range mimes {
		if err := w.request(sourceID, srcOpcodeOffer, wlEncodeString(m)); err != nil {
			w.Close()
			return nil, err
		}
	}
	// device.set_selection(source)
	binary.LittleEndian.PutUint32(arg, sourceID)
	if err := w.request(deviceID, devOpcodeSetSelection, arg); err != nil {
		w.Close()
		return nil, err
	}

	// Confirm the requests were processed before returning, so callers can
	// rely on the selection being set.
	confirm, err := w.sync()
	if err != nil {
		w.Close()
		return nil, err
	}
	for {
		obj, op, body, err := w.readEvent()
		if err != nil {
			w.Close()
			return nil, err
		}
		if obj == wlDisplayID && op == 0 {
			w.Close()
			return nil, wlDisplayError(body)
		}
		// A send may already arrive before the sync barrier; serve it.
		if obj == sourceID && op == srcEvtSend {
			wlServeSend(w, body, data)
		}
		if obj == sourceID && op == srcEvtCancelled {
			// Replaced immediately; deliver the overwrite signal and close.
			w.Close()
			done := make(chan struct{}, 1)
			done <- struct{}{}
			close(done)
			return done, nil
		}
		if obj == confirm && op == 0 {
			break
		}
	}

	// The channel receives one signal then closes when ownership is lost,
	// matching the package Write contract.
	done := make(chan struct{}, 1)
	go func() {
		defer func() {
			done <- struct{}{}
			close(done)
		}()
		defer w.Close()
		for {
			obj, op, body, err := w.readEvent()
			if err != nil {
				return
			}
			switch {
			case obj == wlDisplayID && op == 0:
				return
			case obj == sourceID && op == srcEvtSend:
				wlServeSend(w, body, data)
			case obj == sourceID && op == srcEvtCancelled:
				return
			}
		}
	}()
	return done, nil
}

// wlServeSend answers a data_source.send event by writing data to the fd the
// requestor provided (received as ancillary data) and closing it.
func wlServeSend(w *wlConn, body []byte, data []byte) {
	_, _, _ = wlString(body, 0) // mime; we serve the same data for any type offered
	fd, ok := w.nextFd()
	if !ok {
		return
	}
	f := os.NewFile(uintptr(fd), "wl-clipboard-send")
	if f == nil {
		syscall.Close(fd)
		return
	}
	_, _ = f.Write(data)
	f.Close()
}

// wlWatch watches the clipboard for changes of the given format and delivers
// each new value on the returned channel until ctx is cancelled (then the
// channel is closed). The data-control device reports a selection event on
// every change, so this is event-driven rather than polled.
func wlWatch(ctx context.Context, t Format) <-chan []byte {
	recv := make(chan []byte, 1)
	var mimes []string
	switch t {
	case FmtText:
		mimes = textMIMEs
	case FmtImage:
		mimes = imageMIMEs
	default:
		mime, ok := formatMIME(t)
		if !ok {
			close(recv)
			return recv
		}
		mimes = []string{mime}
	}

	w, _, deviceID, err := wlConnectDevice()
	if err != nil {
		close(recv)
		return recv
	}

	offers := make(map[uint32][]string)
	// fetch resolves the data for a selection event: it forgets stale offers,
	// then reads the current selection's bytes (nil if empty or unsupported).
	fetch := func(sel uint32) []byte {
		for id := range offers {
			if id != sel {
				w.request(id, offerOpcodeDestroy, nil)
				delete(offers, id)
			}
		}
		if sel == 0 {
			return nil
		}
		chosen := pickMIME(mimes, offers[sel])
		if chosen == "" {
			return nil
		}
		d, err := wlReceiveOffer(w, sel, chosen)
		if err != nil || len(d) == 0 {
			return nil
		}
		return d
	}

	// Capture the current selection synchronously as the baseline (mirrors the
	// X11 watch reading the current value before returning), so a write that
	// happens right after Watch returns is reported rather than swallowed.
	var last []byte
	sync1, err := w.sync()
	if err != nil {
		w.Close()
		close(recv)
		return recv
	}
baseline:
	for {
		obj, op, body, err := w.readEvent()
		if err != nil {
			w.Close()
			close(recv)
			return recv
		}
		switch {
		case obj == wlDisplayID && op == 0:
			w.Close()
			close(recv)
			return recv
		case obj == sync1 && op == 0:
			break baseline
		case obj == deviceID && op == devEvtDataOffer && len(body) >= 4:
			offers[binary.LittleEndian.Uint32(body[0:])] = nil
		case obj == deviceID && op == devEvtSelection && len(body) >= 4:
			last = fetch(binary.LittleEndian.Uint32(body[0:]))
		case op == offerEvtOffer:
			if _, isOffer := offers[obj]; isOffer {
				if mime, _, err := wlString(body, 0); err == nil {
					offers[obj] = append(offers[obj], mime)
				}
			}
		}
	}

	go func() {
		defer close(recv)
		defer w.Close()
		// Closing the connection when ctx is done unblocks readEvent.
		go func() {
			<-ctx.Done()
			w.Close()
		}()
		for {
			obj, op, body, err := w.readEvent()
			if err != nil {
				return
			}
			switch {
			case obj == wlDisplayID && op == 0:
				return
			case obj == deviceID && op == devEvtDataOffer && len(body) >= 4:
				offers[binary.LittleEndian.Uint32(body[0:])] = nil
			case obj == deviceID && op == devEvtSelection && len(body) >= 4:
				data := fetch(binary.LittleEndian.Uint32(body[0:]))
				if bytes.Equal(data, last) {
					continue
				}
				last = data
				if data == nil {
					continue // cleared or unsupported format; nothing to deliver
				}
				select {
				case recv <- data:
				case <-ctx.Done():
					return
				}
			case op == offerEvtOffer:
				if _, isOffer := offers[obj]; isOffer {
					if mime, _, err := wlString(body, 0); err == nil {
						offers[obj] = append(offers[obj], mime)
					}
				}
			}
		}
	}()
	return recv
}

// useWayland is set by initialize() when the native Wayland backend is in use,
// so read/write/watch dispatch to it instead of the X11 path.
var useWayland bool

// wlAvailable reports whether the process is in a Wayland session whose
// compositor exposes a data-control manager (ext or wlroots). It performs a
// real probe by connecting and listing globals.
func wlAvailable() bool {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	globals, err := wlListGlobals()
	if err != nil {
		return false
	}
	_, _, ok := dataControlManager(globals)
	return ok
}
