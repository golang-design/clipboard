// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

// Package x11wire implements the subset of the X11 wire protocol needed to
// access the CLIPBOARD selection, in pure Go. It contains only the
// encoding/decoding and parsing logic (no sockets), so it builds and is unit
// tested on any platform; the Linux backend layers socket I/O on top.
//
// References: the X Window System Protocol, version 11
// (https://www.x.org/releases/current/doc/xproto/x11protocol.html).
package x11wire

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// X11 request opcodes (subset).
const (
	opCreateWindow      = 1
	opChangeProperty    = 18
	opDeleteProperty    = 19
	opGetProperty       = 20
	opSetSelectionOwner = 22
	opGetSelectionOwner = 23
	opConvertSelection  = 24
	opSendEvent         = 25
	opInternAtom        = 16
	opGetInputFocus     = 43
)

// X11 event codes (subset).
const (
	EventSelectionClear   = 29
	EventSelectionRequest = 30
	EventSelectionNotify  = 31
)

// Predefined atoms and constants from the core protocol.
const (
	None        uint32 = 0 // no atom / no window / no property
	CurrentTime uint32 = 0
	AtomATOM    uint32 = 4 // XA_ATOM
	classInOut         = 1 // InputOutput window class
	modeReplace        = 0 // PropModeReplace
)

// le is the little-endian byte order used on supported targets; the byte order
// is declared in the connection setup request (see SetupRequest).
var le = binary.LittleEndian

// pad4 rounds n up to a multiple of four (X11 pads all variable fields).
func pad4(n int) int { return (n + 3) &^ 3 }

// ---------------------------------------------------------------------------
// $DISPLAY parsing
// ---------------------------------------------------------------------------

// Display describes where to reach the X server.
type Display struct {
	Net  string // "unix" or "tcp"
	Addr string // socket path or host:port
	Num  int    // display number
}

// ParseDisplay parses a $DISPLAY value of the form
// "[host]:display[.screen]". An empty or "unix" host selects the unix-domain
// socket /tmp/.X11-unix/X<display>; any other host selects TCP at
// host:(6000+display).
func ParseDisplay(display string) (Display, error) {
	if display == "" {
		return Display{}, fmt.Errorf("x11wire: empty DISPLAY")
	}
	colon := strings.LastIndex(display, ":")
	if colon < 0 {
		return Display{}, fmt.Errorf("x11wire: invalid DISPLAY %q", display)
	}
	host := display[:colon]
	rest := display[colon+1:]
	if dot := strings.IndexByte(rest, '.'); dot >= 0 {
		rest = rest[:dot] // drop the optional screen number
	}
	num, err := strconv.Atoi(rest)
	if err != nil {
		return Display{}, fmt.Errorf("x11wire: invalid display number in %q", display)
	}
	if host == "" || host == "unix" {
		return Display{Net: "unix", Addr: "/tmp/.X11-unix/X" + strconv.Itoa(num), Num: num}, nil
	}
	return Display{Net: "tcp", Addr: host + ":" + strconv.Itoa(6000+num), Num: num}, nil
}

// ---------------------------------------------------------------------------
// .Xauthority parsing and cookie selection
// ---------------------------------------------------------------------------

// Authority family values (.Xauthority stores these big-endian).
const (
	FamilyInternet uint16 = 0
	FamilyLocal    uint16 = 256
	FamilyWild     uint16 = 65535
)

// AuthEntry is one record from an .Xauthority file.
type AuthEntry struct {
	Family  uint16
	Address string
	Number  string
	Name    string
	Data    []byte
}

// ParseXauthority parses the binary .Xauthority format: a sequence of records,
// each a big-endian uint16 family followed by four (uint16 length, bytes)
// fields: address, number, name, data.
func ParseXauthority(b []byte) ([]AuthEntry, error) {
	var entries []AuthEntry
	for len(b) > 0 {
		if len(b) < 2 {
			return nil, fmt.Errorf("x11wire: truncated .Xauthority")
		}
		var e AuthEntry
		e.Family = binary.BigEndian.Uint16(b)
		b = b[2:]
		fields := make([][]byte, 4)
		for i := range fields {
			if len(b) < 2 {
				return nil, fmt.Errorf("x11wire: truncated .Xauthority field")
			}
			n := int(binary.BigEndian.Uint16(b))
			b = b[2:]
			if len(b) < n {
				return nil, fmt.Errorf("x11wire: truncated .Xauthority value")
			}
			fields[i] = b[:n]
			b = b[n:]
		}
		e.Address = string(fields[0])
		e.Number = string(fields[1])
		e.Name = string(fields[2])
		e.Data = fields[3]
		entries = append(entries, e)
	}
	return entries, nil
}

// MITCookie is the authorization protocol name X uses for cookies.
const MITCookie = "MIT-MAGIC-COOKIE-1"

// ChooseCookie selects the MIT-MAGIC-COOKIE-1 entry matching the given display
// number and hostname, preferring a Local entry for this host, then a Wild
// entry, then any cookie for the display. It returns ("", nil) when no cookie
// applies, in which case the caller connects without authorization (common and
// often authorized for local unix sockets).
func ChooseCookie(entries []AuthEntry, displayNum int, hostname string) (name string, data []byte) {
	want := strconv.Itoa(displayNum)
	var wild, any []byte
	for _, e := range entries {
		if e.Name != MITCookie {
			continue
		}
		if e.Number != "" && e.Number != want {
			continue
		}
		switch {
		case e.Family == FamilyLocal && e.Address == hostname:
			return MITCookie, e.Data // best match
		case e.Family == FamilyWild && wild == nil:
			wild = e.Data
		case any == nil:
			any = e.Data
		}
	}
	if wild != nil {
		return MITCookie, wild
	}
	if any != nil {
		return MITCookie, any
	}
	return "", nil
}

// ---------------------------------------------------------------------------
// Connection setup
// ---------------------------------------------------------------------------

// SetupRequest builds the connection setup request declaring little-endian byte
// order, protocol 11.0, and the given authorization protocol name and data.
func SetupRequest(authName string, authData []byte) []byte {
	n, d := len(authName), len(authData)
	b := make([]byte, 12+pad4(n)+pad4(d))
	b[0] = 'l' // little-endian
	le.PutUint16(b[2:], 11)
	le.PutUint16(b[4:], 0)
	le.PutUint16(b[6:], uint16(n))
	le.PutUint16(b[8:], uint16(d))
	copy(b[12:], authName)
	copy(b[12+pad4(n):], authData)
	return b
}

// Setup holds the fields parsed from a successful connection setup reply.
type Setup struct {
	IDBase    uint32
	IDMask    uint32
	Root      uint32
	MaxReqLen uint16
}

// ReadSetup reads and parses the connection setup reply from r.
func ReadSetup(r io.Reader) (Setup, error) {
	h := make([]byte, 8)
	if _, err := io.ReadFull(r, h); err != nil {
		return Setup{}, err
	}
	add := make([]byte, int(le.Uint16(h[6:]))*4)
	if _, err := io.ReadFull(r, add); err != nil {
		return Setup{}, err
	}
	switch h[0] {
	case 1: // success
		return parseSetupAdditional(add)
	case 0: // failed
		return Setup{}, fmt.Errorf("x11wire: server refused connection: %s", string(trimReason(add, int(h[1]))))
	case 2: // authenticate
		return Setup{}, fmt.Errorf("x11wire: server requires authentication: %s", string(add))
	default:
		return Setup{}, fmt.Errorf("x11wire: unexpected setup reply code %d", h[0])
	}
}

func trimReason(add []byte, n int) []byte {
	if n > len(add) {
		n = len(add)
	}
	return add[:n]
}

// parseSetupAdditional parses the "additional data" of a successful setup
// reply. The fixed header is 32 bytes; the first screen's root window follows
// the padded vendor string and the pixmap formats.
func parseSetupAdditional(add []byte) (Setup, error) {
	if len(add) < 32 {
		return Setup{}, fmt.Errorf("x11wire: short setup reply")
	}
	var s Setup
	s.IDBase = le.Uint32(add[4:])
	s.IDMask = le.Uint32(add[8:])
	vendorLen := int(le.Uint16(add[16:]))
	s.MaxReqLen = le.Uint16(add[18:])
	numFormats := int(add[21])
	screenOff := 32 + pad4(vendorLen) + 8*numFormats
	if screenOff+4 > len(add) {
		return Setup{}, fmt.Errorf("x11wire: setup reply missing screen")
	}
	s.Root = le.Uint32(add[screenOff:])
	return s, nil
}

// ---------------------------------------------------------------------------
// Resource ID allocator
// ---------------------------------------------------------------------------

// IDGen allocates client resource IDs from the base/mask in the setup reply.
type IDGen struct {
	base, mask, last uint32
}

// NewIDGen returns an allocator for the given setup.
func NewIDGen(s Setup) *IDGen { return &IDGen{base: s.IDBase, mask: s.IDMask} }

// Next returns the next free resource ID.
func (g *IDGen) Next() uint32 {
	g.last++
	return g.base | (g.last & g.mask)
}

// ---------------------------------------------------------------------------
// Request encoders
// ---------------------------------------------------------------------------

// reqHeader writes the standard request header (opcode, one data byte, and the
// length in 4-byte units) into b, whose length must be the full request size.
func reqHeader(b []byte, opcode, data byte) {
	b[0] = opcode
	b[1] = data
	le.PutUint16(b[2:], uint16(len(b)/4))
}

// InternAtom builds an InternAtom request for the given name.
func InternAtom(name string, onlyIfExists bool) []byte {
	b := make([]byte, 8+pad4(len(name)))
	var oie byte
	if onlyIfExists {
		oie = 1
	}
	reqHeader(b, opInternAtom, oie)
	le.PutUint16(b[4:], uint16(len(name)))
	copy(b[8:], name)
	return b
}

// CreateWindow builds a CreateWindow request for a 1x1 InputOutput window with
// no value list, inheriting depth and visual from the parent.
func CreateWindow(wid, parent uint32) []byte {
	b := make([]byte, 32)
	reqHeader(b, opCreateWindow, 0) // depth = CopyFromParent
	le.PutUint32(b[4:], wid)
	le.PutUint32(b[8:], parent)
	// x, y, border-width = 0 (already zero)
	le.PutUint16(b[16:], 1) // width
	le.PutUint16(b[18:], 1) // height
	le.PutUint16(b[22:], classInOut)
	// visual = CopyFromParent (0), value-mask = 0 (already zero)
	return b
}

// SetSelectionOwner builds a SetSelectionOwner request.
func SetSelectionOwner(owner, selection, time uint32) []byte {
	b := make([]byte, 16)
	reqHeader(b, opSetSelectionOwner, 0)
	le.PutUint32(b[4:], owner)
	le.PutUint32(b[8:], selection)
	le.PutUint32(b[12:], time)
	return b
}

// GetSelectionOwner builds a GetSelectionOwner request.
func GetSelectionOwner(selection uint32) []byte {
	b := make([]byte, 8)
	reqHeader(b, opGetSelectionOwner, 0)
	le.PutUint32(b[4:], selection)
	return b
}

// ConvertSelection builds a ConvertSelection request.
func ConvertSelection(requestor, selection, target, property, time uint32) []byte {
	b := make([]byte, 24)
	reqHeader(b, opConvertSelection, 0)
	le.PutUint32(b[4:], requestor)
	le.PutUint32(b[8:], selection)
	le.PutUint32(b[12:], target)
	le.PutUint32(b[16:], property)
	le.PutUint32(b[20:], time)
	return b
}

// ChangeProperty builds a ChangeProperty (mode Replace) request. format is 8 or
// 32; data is the raw bytes, and the protocol length field is derived as the
// number of format units in data.
func ChangeProperty(window, property, typ uint32, format byte, data []byte) []byte {
	b := make([]byte, 24+pad4(len(data)))
	reqHeader(b, opChangeProperty, modeReplace)
	le.PutUint32(b[4:], window)
	le.PutUint32(b[8:], property)
	le.PutUint32(b[12:], typ)
	b[16] = format
	le.PutUint32(b[20:], uint32(len(data)/(int(format)/8)))
	copy(b[24:], data)
	return b
}

// GetProperty builds a GetProperty request retrieving up to longLength 32-bit
// units starting at longOffset, optionally deleting the property afterwards.
func GetProperty(del bool, window, property, typ, longOffset, longLength uint32) []byte {
	b := make([]byte, 24)
	var d byte
	if del {
		d = 1
	}
	reqHeader(b, opGetProperty, d)
	le.PutUint32(b[4:], window)
	le.PutUint32(b[8:], property)
	le.PutUint32(b[12:], typ)
	le.PutUint32(b[16:], longOffset)
	le.PutUint32(b[20:], longLength)
	return b
}

// GetInputFocus builds a GetInputFocus request. It takes no arguments and
// always produces a reply, so it serves as a cheap sync barrier to flush the
// results (including any errors) of preceding fire-and-forget requests.
func GetInputFocus() []byte {
	b := make([]byte, 4)
	reqHeader(b, opGetInputFocus, 0)
	return b
}

// DeleteProperty builds a DeleteProperty request.
func DeleteProperty(window, property uint32) []byte {
	b := make([]byte, 12)
	reqHeader(b, opDeleteProperty, 0)
	le.PutUint32(b[4:], window)
	le.PutUint32(b[8:], property)
	return b
}

// SelectionNotify is the body of a SelectionNotify event.
type SelectionNotify struct {
	Time      uint32
	Requestor uint32
	Selection uint32
	Target    uint32
	Property  uint32 // None to refuse
}

// SendSelectionNotify builds a SendEvent request delivering a SelectionNotify
// (non-propagating, empty event mask) to the requestor window.
func SendSelectionNotify(ev SelectionNotify) []byte {
	b := make([]byte, 44)
	reqHeader(b, opSendEvent, 0) // propagate = false
	le.PutUint32(b[4:], ev.Requestor)
	// event-mask = 0 at b[8:12]
	e := b[12:44] // the 32-byte event
	e[0] = EventSelectionNotify
	le.PutUint32(e[4:], ev.Time)
	le.PutUint32(e[8:], ev.Requestor)
	le.PutUint32(e[12:], ev.Selection)
	le.PutUint32(e[16:], ev.Target)
	le.PutUint32(e[20:], ev.Property)
	return b
}

// ---------------------------------------------------------------------------
// Packet reading and decoding
// ---------------------------------------------------------------------------

// Packet is one server-to-client message: an error, a reply, or an event.
type Packet struct {
	Raw []byte // 32 bytes for errors/events; 32+ for replies
}

// IsError reports whether the packet is an X11 protocol error.
func (p Packet) IsError() bool { return p.Raw[0] == 0 }

// IsReply reports whether the packet is a request reply.
func (p Packet) IsReply() bool { return p.Raw[0] == 1 }

// IsEvent reports whether the packet is an event.
func (p Packet) IsEvent() bool { return p.Raw[0] >= 2 }

// EventCode returns the event type, masking off the SendEvent-generated bit.
func (p Packet) EventCode() byte { return p.Raw[0] & 0x7f }

// Sequence returns the low 16 bits of the sequence number of the request a
// reply or error corresponds to. Meaningless for events.
func (p Packet) Sequence() uint16 { return le.Uint16(p.Raw[2:]) }

// ErrorCode returns the X11 error code of an error packet.
func (p Packet) ErrorCode() byte { return p.Raw[1] }

// ReadPacket reads one packet from r. Replies carry an extra reply-length*4
// bytes beyond the 32-byte header.
func ReadPacket(r io.Reader) (Packet, error) {
	hdr := make([]byte, 32)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return Packet{}, err
	}
	if hdr[0] == 1 { // reply
		if extra := int(le.Uint32(hdr[4:])) * 4; extra > 0 {
			buf := make([]byte, 32+extra)
			copy(buf, hdr)
			if _, err := io.ReadFull(r, buf[32:]); err != nil {
				return Packet{}, err
			}
			return Packet{Raw: buf}, nil
		}
	}
	return Packet{Raw: hdr}, nil
}

// NextEvent reads packets from r until it returns an event, discarding X11
// errors and stray replies. Discarding errors here is precisely what makes an
// asynchronous X11 protocol error unable to terminate the process (#61): there
// is no library default handler, so an Error is just decoded and dropped.
func NextEvent(r io.Reader) (Packet, error) {
	for {
		p, err := ReadPacket(r)
		if err != nil {
			return Packet{}, err
		}
		if p.IsEvent() {
			return p, nil
		}
	}
}

// NextReply reads packets from r until it returns a reply. It returns an error
// if an X11 Error packet arrives first (the request failed).
func NextReply(r io.Reader) (Packet, error) {
	for {
		p, err := ReadPacket(r)
		if err != nil {
			return Packet{}, err
		}
		if p.IsReply() {
			return p, nil
		}
		if p.IsError() {
			return Packet{}, fmt.Errorf("x11wire: request failed (error code %d)", p.Raw[1])
		}
	}
}

// Atom decodes the atom from an InternAtom reply.
func (p Packet) Atom() uint32 { return le.Uint32(p.Raw[8:]) }

// SelectionOwner decodes the window from a GetSelectionOwner reply.
func (p Packet) SelectionOwner() uint32 { return le.Uint32(p.Raw[8:]) }

// PropertyValue decodes the value bytes from a GetProperty reply, using the
// format and item count to compute the exact (unpadded) length.
func (p Packet) PropertyValue() []byte {
	format := p.Raw[1]
	if format == 0 {
		return nil // property does not exist / no data
	}
	nItems := le.Uint32(p.Raw[16:])
	n := int(nItems) * (int(format) / 8)
	if 32+n > len(p.Raw) {
		n = len(p.Raw) - 32
	}
	return p.Raw[32 : 32+n]
}

// SelectionRequestEvent decodes a SelectionRequest event body.
type SelectionRequestEvent struct {
	Time      uint32
	Owner     uint32
	Requestor uint32
	Selection uint32
	Target    uint32
	Property  uint32
}

// SelectionRequest decodes a SelectionRequest event.
func (p Packet) SelectionRequest() SelectionRequestEvent {
	return SelectionRequestEvent{
		Time:      le.Uint32(p.Raw[4:]),
		Owner:     le.Uint32(p.Raw[8:]),
		Requestor: le.Uint32(p.Raw[12:]),
		Selection: le.Uint32(p.Raw[16:]),
		Target:    le.Uint32(p.Raw[20:]),
		Property:  le.Uint32(p.Raw[24:]),
	}
}

// SelectionNotifyEvent decodes a SelectionNotify event (read path).
type SelectionNotifyEvent struct {
	Requestor uint32
	Selection uint32
	Target    uint32
	Property  uint32
}

// SelectionNotify decodes a SelectionNotify event.
func (p Packet) SelectionNotify() SelectionNotifyEvent {
	return SelectionNotifyEvent{
		Requestor: le.Uint32(p.Raw[8:]),
		Selection: le.Uint32(p.Raw[12:]),
		Target:    le.Uint32(p.Raw[16:]),
		Property:  le.Uint32(p.Raw[20:]),
	}
}

// AtomList encodes a list of atoms as ChangeProperty data (format 32).
func AtomList(atoms ...uint32) []byte {
	b := make([]byte, 4*len(atoms))
	for i, a := range atoms {
		le.PutUint32(b[4*i:], a)
	}
	return b
}
