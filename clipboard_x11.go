// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build (linux || freebsd || openbsd || netbsd) && !android

package clipboard

// Pure-Go X11 CLIPBOARD selection backend, shared by Linux and the BSDs. It
// speaks the X11 wire protocol (encoded/decoded by internal/x11wire) directly
// over the display socket, so it needs no Cgo, no libX11 headers at build time,
// and no libX11.so at runtime.

import (
	"bufio"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	x11wire "golang.design/x/x11"
)

// x11ReadTimeout bounds a single Read so a missing SelectionNotify (e.g. an
// owner that never answers) surfaces as an error instead of hanging.
const x11ReadTimeout = 5 * time.Second

// x11conn is a live connection to the X server.
type x11conn struct {
	c     net.Conn
	r     *bufio.Reader
	ids   *x11wire.IDGen
	root  uint32
	win   uint32
	atoms map[string]uint32
	seq   uint16 // last request sequence number sent (server counts from 1)
}

// send writes a request and returns the sequence number the server assigns it,
// so a later reply can be matched by sequence.
func (x *x11conn) send(req []byte) (uint16, error) {
	if _, err := x.c.Write(req); err != nil {
		return 0, err
	}
	x.seq++
	return x.seq, nil
}

// reply reads packets until it returns the reply matching seq. Events and
// packets for other sequence numbers are discarded; in particular, asynchronous
// X11 errors caused by earlier fire-and-forget requests (e.g. a requestor
// window that has gone away) are dropped rather than mistaken for this reply —
// the same reason a protocol error cannot crash the process (#61). An error
// whose sequence matches seq means this request itself failed.
func (x *x11conn) reply(seq uint16) (x11wire.Packet, error) {
	for {
		p, err := x11wire.ReadPacket(x.r)
		if err != nil {
			return x11wire.Packet{}, err
		}
		if p.IsEvent() || p.Sequence() != seq {
			continue
		}
		if p.IsError() {
			return x11wire.Packet{}, errUnavailable
		}
		return p, nil
	}
}

// x11Dial connects to the display socket, trying the filesystem unix socket
// first, then the Linux abstract socket, then TCP.
func x11Dial(d x11wire.Display) (net.Conn, error) {
	if d.Net == "unix" {
		c, err := net.Dial("unix", d.Addr)
		if err == nil || runtime.GOOS != "linux" {
			// The abstract-socket fallback below is Linux-only; the BSDs have
			// no abstract unix namespace, so there is nothing more to try.
			return c, err
		}
		return net.Dial("unix", "@/tmp/.X11-unix/X"+strconv.Itoa(d.Num))
	}
	return net.Dial(d.Net, d.Addr)
}

// loadCookie reads the MIT-MAGIC-COOKIE-1 for the given display from
// $XAUTHORITY (or ~/.Xauthority). It returns ("", nil) when none applies, in
// which case we connect without authorization.
func loadCookie(displayNum int) (string, []byte) {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil
		}
		path = filepath.Join(home, ".Xauthority")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	entries, err := x11wire.ParseXauthority(b)
	if err != nil {
		return "", nil
	}
	host, _ := os.Hostname()
	return x11wire.ChooseCookie(entries, displayNum, host)
}

// x11Handshake performs the connection setup over an already-dialed socket.
func x11Handshake(c net.Conn, name string, data []byte) (*bufio.Reader, x11wire.Setup, error) {
	if _, err := c.Write(x11wire.SetupRequest(name, data)); err != nil {
		return nil, x11wire.Setup{}, err
	}
	r := bufio.NewReader(c)
	s, err := x11wire.ReadSetup(r)
	return r, s, err
}

// x11DialDisplay dials the display and authenticates, returning a connection
// with no window yet. Splitting this from window creation lets x11Test check
// reachability without churning a window resource on the server.
//
// The connection setup is retried a few times: under rapid connect/disconnect
// churn the X server can reset a connection mid-handshake ("connection reset by
// peer"). The original cgo backend tolerated the same transient by retrying
// XOpenDisplay many times.
func x11DialDisplay() (*x11conn, error) {
	d, err := x11wire.ParseDisplay(os.Getenv("DISPLAY"))
	if err != nil {
		return nil, errUnavailable
	}
	name, data := loadCookie(d.Num)

	for range 10 {
		x, err := x11dialOnce(d, name, data)
		if err == nil {
			return x, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil, errUnavailable
}

// x11dialOnce makes one connection-setup attempt, falling back to no
// authorization (common for local unix sockets) if the cookie is rejected.
func x11dialOnce(d x11wire.Display, name string, data []byte) (*x11conn, error) {
	c, err := x11Dial(d)
	if err != nil {
		return nil, err
	}
	r, setup, err := x11Handshake(c, name, data)
	if err != nil {
		c.Close()
		if name == "" {
			return nil, err
		}
		if c, err = x11Dial(d); err != nil {
			return nil, err
		}
		if r, setup, err = x11Handshake(c, "", nil); err != nil {
			c.Close()
			return nil, err
		}
	}
	return &x11conn{c: c, r: r, ids: x11wire.NewIDGen(setup), root: setup.Root, atoms: map[string]uint32{}}, nil
}

// x11Connect dials, authenticates, and creates the 1x1 window used to own or
// request the selection.
func x11Connect() (*x11conn, error) {
	x, err := x11DialDisplay()
	if err != nil {
		return nil, err
	}
	if err := x.createWindow(); err != nil {
		x.Close()
		return nil, err
	}
	return x, nil
}

// createWindow creates x.win and confirms it succeeded, retrying with a fresh
// resource id if the server rejects the id. The X server may hand a new
// connection the same resource-id base a just-closed connection used; creating
// a window with an id the server has not yet reaped yields BadIDChoice. A
// GetInputFocus sync surfaces that asynchronous error so we can retry.
func (x *x11conn) createWindow() error {
	for range 8 {
		x.win = x.ids.Next()
		cwSeq, err := x.send(x11wire.CreateWindow(x.win, x.root))
		if err != nil {
			return errUnavailable
		}
		syncSeq, err := x.send(x11wire.GetInputFocus())
		if err != nil {
			return errUnavailable
		}
		created := true
		for {
			p, err := x11wire.ReadPacket(x.r)
			if err != nil {
				return errUnavailable
			}
			if p.IsEvent() {
				continue
			}
			if p.IsError() && p.Sequence() == cwSeq {
				created = false // the id was rejected; try the next one
				continue
			}
			if p.Sequence() == syncSeq {
				break // CreateWindow result has been flushed
			}
		}
		if created {
			return nil
		}
	}
	return errUnavailable
}

func (x *x11conn) Close() error { return x.c.Close() }

// intern resolves an atom name to its id, caching the result.
func (x *x11conn) intern(name string) (uint32, error) {
	if a, ok := x.atoms[name]; ok {
		return a, nil
	}
	seq, err := x.send(x11wire.InternAtom(name, false))
	if err != nil {
		return 0, err
	}
	p, err := x.reply(seq)
	if err != nil {
		return 0, err
	}
	a := p.Atom()
	x.atoms[name] = a
	return a, nil
}

// x11Test verifies that the X server can be reached. Used by initialize. It
// only dials and authenticates; it does not create a window, to avoid churning
// a server resource on every Init.
func x11Test() error {
	c, err := x11DialDisplay()
	if err != nil {
		return err
	}
	c.Close()
	return nil
}

// x11Read reads the CLIPBOARD selection in the given target format.
func x11Read(target string) ([]byte, error) {
	x, err := x11Connect()
	if err != nil {
		return nil, errUnavailable
	}
	defer x.Close()
	x.c.SetReadDeadline(time.Now().Add(x11ReadTimeout))

	sel, e1 := x.intern("CLIPBOARD")
	prop, e2 := x.intern("GOLANG_DESIGN_DATA")
	tgt, e3 := x.intern(target)
	if e1 != nil || e2 != nil || e3 != nil {
		return nil, errUnavailable
	}

	if _, err := x.send(x11wire.ConvertSelection(x.win, sel, tgt, prop, x11wire.CurrentTime)); err != nil {
		return nil, errUnavailable
	}
	for {
		p, err := x11wire.NextEvent(x.r)
		if err != nil {
			return nil, errUnavailable
		}
		if p.EventCode() != x11wire.EventSelectionNotify {
			continue
		}
		if p.SelectionNotify().Property == x11wire.None {
			return nil, nil // nothing available in this format
		}
		break
	}

	gseq, err := x.send(x11wire.GetProperty(true, x.win, prop, 0, 0, 0xffffffff))
	if err != nil {
		return nil, errUnavailable
	}
	rp, err := x.reply(gseq)
	if err != nil {
		return nil, errUnavailable
	}
	v := rp.PropertyValue()
	if len(v) == 0 {
		// An empty property reads back as nil, matching the historical cgo
		// behavior (and Read's documented "returns nil when absent").
		return nil, nil
	}
	return v, nil
}

// atomName resolves an atom id to its string name via GetAtomName.
func (x *x11conn) atomName(atom uint32) (string, error) {
	seq, err := x.send(x11wire.GetAtomName(atom))
	if err != nil {
		return "", err
	}
	p, err := x.reply(seq)
	if err != nil {
		return "", err
	}
	return p.AtomName(), nil
}

// x11Targets returns the target names the current CLIPBOARD selection advertises
// (via the TARGETS target), or nil if the clipboard is empty. The TARGETS
// property is a list of 4-byte atom ids, each resolved back to its name.
func x11Targets() ([]string, error) {
	x, err := x11Connect()
	if err != nil {
		return nil, errUnavailable
	}
	defer x.Close()
	x.c.SetReadDeadline(time.Now().Add(x11ReadTimeout))

	sel, e1 := x.intern("CLIPBOARD")
	prop, e2 := x.intern("GOLANG_DESIGN_DATA")
	tgts, e3 := x.intern("TARGETS")
	if e1 != nil || e2 != nil || e3 != nil {
		return nil, errUnavailable
	}

	if _, err := x.send(x11wire.ConvertSelection(x.win, sel, tgts, prop, x11wire.CurrentTime)); err != nil {
		return nil, errUnavailable
	}
	for {
		p, err := x11wire.NextEvent(x.r)
		if err != nil {
			return nil, errUnavailable
		}
		if p.EventCode() != x11wire.EventSelectionNotify {
			continue
		}
		if p.SelectionNotify().Property == x11wire.None {
			return nil, nil // empty clipboard
		}
		break
	}

	gseq, err := x.send(x11wire.GetProperty(true, x.win, prop, 0, 0, 0xffffffff))
	if err != nil {
		return nil, errUnavailable
	}
	rp, err := x.reply(gseq)
	if err != nil {
		return nil, errUnavailable
	}

	atoms := rp.PropertyValue() // ATOM list, 4 bytes each
	names := make([]string, 0, len(atoms)/4)
	for i := 0; i+4 <= len(atoms); i += 4 {
		name, err := x.atomName(binary.LittleEndian.Uint32(atoms[i:]))
		if err != nil || name == "" {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// x11EnumerateFormats maps the advertised TARGETS to Format tokens, registering
// custom MIME types on demand. Shared by the Linux and BSD backends.
func x11EnumerateFormats() []Format {
	names, err := x11Targets()
	if err != nil {
		return nil
	}
	out := make([]Format, 0, len(names))
	for _, n := range names {
		if f, ok := x11FormatForTarget(n); ok {
			out = append(out, f)
		}
	}
	return out
}

// x11FormatForTarget maps an X11 target/atom name to a Format: the common text
// atoms to FmtText, image/png to FmtImage, any other MIME-shaped name (one that
// contains '/') to a registered custom format. X11 meta-targets such as TARGETS,
// MULTIPLE and TIMESTAMP have no '/', so they are ignored.
func x11FormatForTarget(name string) (Format, bool) {
	switch name {
	case "UTF8_STRING", "STRING", "TEXT", "text/plain", "text/plain;charset=utf-8":
		return FmtText, true
	case "image/png":
		return FmtImage, true
	}
	if strings.Contains(name, "/") {
		return Register(name), true
	}
	return 0, false
}

// x11Write takes ownership of the CLIPBOARD selection and serves its content to
// requestors until ownership is lost (another writer overwrites it). The
// returned channel is closed on that loss, matching the documented contract.
func x11Write(target string, buf []byte) (<-chan struct{}, error) {
	x, err := x11Connect()
	if err != nil {
		return nil, errUnavailable
	}

	sel, e1 := x.intern("CLIPBOARD")
	targets, e2 := x.intern("TARGETS")
	tgt, e3 := x.intern(target)
	if e1 != nil || e2 != nil || e3 != nil {
		x.Close()
		return nil, errUnavailable
	}

	if _, err := x.send(x11wire.SetSelectionOwner(x.win, sel, x11wire.CurrentTime)); err != nil {
		x.Close()
		return nil, errUnavailable
	}
	gseq, err := x.send(x11wire.GetSelectionOwner(sel))
	if err != nil {
		x.Close()
		return nil, errUnavailable
	}
	p, err := x.reply(gseq)
	if err != nil || p.SelectionOwner() != x.win {
		x.Close()
		return nil, errUnavailable
	}

	done := make(chan struct{}, 1)
	go func() {
		defer x.Close()
		x.serveSelection(sel, targets, tgt, buf)
		done <- struct{}{}
		close(done)
	}()
	return done, nil
}

// serveSelection runs the owner event loop, answering selection requests until
// a SelectionClear (ownership lost) or a connection error.
func (x *x11conn) serveSelection(sel, targets, tgt uint32, buf []byte) {
	for {
		p, err := x11wire.NextEvent(x.r)
		if err != nil {
			return
		}
		switch p.EventCode() {
		case x11wire.EventSelectionClear:
			return
		case x11wire.EventSelectionRequest:
			req := p.SelectionRequest()
			if req.Selection == sel {
				x.answerSelectionRequest(req, targets, tgt, buf)
			}
		}
	}
}

// answerSelectionRequest replies to a single SelectionRequest: it serves the
// data for our target, the supported list for TARGETS, or refuses otherwise,
// then notifies the requestor.
func (x *x11conn) answerSelectionRequest(req x11wire.SelectionRequestEvent, targets, tgt uint32, buf []byte) {
	notify := x11wire.SelectionNotify{
		Time:      req.Time,
		Requestor: req.Requestor,
		Selection: req.Selection,
		Target:    req.Target,
		Property:  req.Property,
	}
	switch req.Target {
	case tgt:
		x.send(x11wire.ChangeProperty(req.Requestor, req.Property, tgt, 8, buf))
	case targets:
		// Advertise the supported targets so correct clients re-request the
		// data in a format we serve (#60).
		x.send(x11wire.ChangeProperty(req.Requestor, req.Property,
			x11wire.AtomATOM, 32, x11wire.AtomList(targets, tgt)))
	default:
		notify.Property = x11wire.None // refuse unsupported target
	}
	x.send(x11wire.SendSelectionNotify(notify))
}
