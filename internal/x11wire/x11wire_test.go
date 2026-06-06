// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package x11wire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseDisplay(t *testing.T) {
	for _, tt := range []struct {
		in      string
		net     string
		addr    string
		num     int
		wantErr bool
	}{
		{in: ":0", net: "unix", addr: "/tmp/.X11-unix/X0", num: 0},
		{in: ":1", net: "unix", addr: "/tmp/.X11-unix/X1", num: 1},
		{in: ":0.0", net: "unix", addr: "/tmp/.X11-unix/X0", num: 0},
		{in: "unix:2", net: "unix", addr: "/tmp/.X11-unix/X2", num: 2},
		{in: "localhost:0", net: "tcp", addr: "localhost:6000", num: 0},
		{in: "192.168.0.5:3.1", net: "tcp", addr: "192.168.0.5:6003", num: 3},
		{in: "", wantErr: true},
		{in: "nope", wantErr: true},
		{in: ":x", wantErr: true},
	} {
		got, err := ParseDisplay(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseDisplay(%q) = %+v, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDisplay(%q) error: %v", tt.in, err)
			continue
		}
		if got.Net != tt.net || got.Addr != tt.addr || got.Num != tt.num {
			t.Errorf("ParseDisplay(%q) = %+v, want {%s %s %d}", tt.in, got, tt.net, tt.addr, tt.num)
		}
	}
}

// authRecord builds one .Xauthority record.
func authRecord(family uint16, addr, num, name string, data []byte) []byte {
	var b bytes.Buffer
	put := func(v uint16) { binary.Write(&b, binary.BigEndian, v) }
	putStr := func(s []byte) { put(uint16(len(s))); b.Write(s) }
	put(family)
	putStr([]byte(addr))
	putStr([]byte(num))
	putStr([]byte(name))
	putStr(data)
	return b.Bytes()
}

func TestParseXauthorityAndChooseCookie(t *testing.T) {
	local := bytes.Repeat([]byte{0xAA}, 16)
	wild := bytes.Repeat([]byte{0xBB}, 16)
	file := append(authRecord(FamilyLocal, "myhost", "0", MITCookie, local),
		authRecord(FamilyWild, "", "0", MITCookie, wild)...)

	entries, err := ParseXauthority(file)
	if err != nil {
		t.Fatalf("ParseXauthority: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	// Local match for this host wins.
	name, data := ChooseCookie(entries, 0, "myhost")
	if name != MITCookie || !bytes.Equal(data, local) {
		t.Errorf("ChooseCookie(myhost) = %q %x, want local cookie", name, data)
	}

	// Different host falls back to the wild entry.
	name, data = ChooseCookie(entries, 0, "otherhost")
	if name != MITCookie || !bytes.Equal(data, wild) {
		t.Errorf("ChooseCookie(otherhost) = %q %x, want wild cookie", name, data)
	}

	// Wrong display number → no cookie (caller connects without auth).
	if name, _ := ChooseCookie(entries, 9, "myhost"); name != "" {
		t.Errorf("ChooseCookie(display 9) = %q, want no cookie", name)
	}

	// No entries at all → no cookie.
	if name, _ := ChooseCookie(nil, 0, "myhost"); name != "" {
		t.Errorf("ChooseCookie(nil) = %q, want no cookie", name)
	}
}

func TestParseXauthorityTruncated(t *testing.T) {
	if _, err := ParseXauthority([]byte{0x01}); err == nil {
		t.Error("expected error for truncated authority")
	}
}

func TestSetupRequest(t *testing.T) {
	cookie := bytes.Repeat([]byte{0x11}, 16)
	b := SetupRequest(MITCookie, cookie)
	if b[0] != 'l' {
		t.Errorf("byte order = %q, want 'l'", b[0])
	}
	if got := binary.LittleEndian.Uint16(b[2:]); got != 11 {
		t.Errorf("major = %d, want 11", got)
	}
	if got := binary.LittleEndian.Uint16(b[6:]); got != uint16(len(MITCookie)) {
		t.Errorf("name length = %d, want %d", got, len(MITCookie))
	}
	if got := binary.LittleEndian.Uint16(b[8:]); got != 16 {
		t.Errorf("data length = %d, want 16", got)
	}
	if len(b)%4 != 0 {
		t.Errorf("setup request length %d not 4-aligned", len(b))
	}
	// MIT-MAGIC-COOKIE-1 is 18 bytes → padded to 20; cookie at 12+20.
	if !bytes.Equal(b[32:48], cookie) {
		t.Errorf("cookie not placed at expected offset")
	}
	// No auth: just the 12-byte header.
	if got := SetupRequest("", nil); len(got) != 12 {
		t.Errorf("no-auth setup len = %d, want 12", len(got))
	}
}

// setupAdditional builds a minimal successful setup "additional data" block
// with the given id base/mask and root window, one screen and no formats.
func setupAdditional(idBase, idMask, root uint32, vendor string) []byte {
	v := pad4(len(vendor))
	add := make([]byte, 32+v+4) // +4 for the single screen's root field
	binary.LittleEndian.PutUint32(add[4:], idBase)
	binary.LittleEndian.PutUint32(add[8:], idMask)
	binary.LittleEndian.PutUint16(add[16:], uint16(len(vendor)))
	add[20] = 1 // one screen
	add[21] = 0 // no pixmap formats
	copy(add[32:], vendor)
	binary.LittleEndian.PutUint32(add[32+v:], root)
	return add
}

func TestParseSetupAdditional(t *testing.T) {
	add := setupAdditional(0x04600000, 0x001fffff, 0x000005a3, "The X.Org Foundation")
	s, err := parseSetupAdditional(add)
	if err != nil {
		t.Fatalf("parseSetupAdditional: %v", err)
	}
	if s.IDBase != 0x04600000 || s.IDMask != 0x001fffff || s.Root != 0x000005a3 {
		t.Errorf("got %+v", s)
	}
}

func TestReadSetupSuccess(t *testing.T) {
	add := setupAdditional(0x100, 0x1ff, 0x42, "vendor")
	hdr := make([]byte, 8)
	hdr[0] = 1
	binary.LittleEndian.PutUint16(hdr[6:], uint16(len(add)/4))
	s, err := ReadSetup(bytes.NewReader(append(hdr, add...)))
	if err != nil {
		t.Fatalf("ReadSetup: %v", err)
	}
	if s.Root != 0x42 || s.IDBase != 0x100 {
		t.Errorf("got %+v", s)
	}
}

func TestReadSetupFailed(t *testing.T) {
	reason := "no protocol specified"
	add := make([]byte, pad4(len(reason)))
	copy(add, reason)
	hdr := make([]byte, 8)
	hdr[0] = 0 // failed
	hdr[1] = byte(len(reason))
	binary.LittleEndian.PutUint16(hdr[6:], uint16(len(add)/4))
	_, err := ReadSetup(bytes.NewReader(append(hdr, add...)))
	if err == nil {
		t.Fatal("expected error on failed setup")
	}
}

func TestIDGen(t *testing.T) {
	g := NewIDGen(Setup{IDBase: 0x0a00000, IDMask: 0x1fffff})
	a, b := g.Next(), g.Next()
	if a == b {
		t.Errorf("IDGen returned duplicate id %#x", a)
	}
	if a&^0x1fffff != 0x0a00000 {
		t.Errorf("id %#x not within base", a)
	}
}

func TestRequestEncoders(t *testing.T) {
	checkHdr := func(name string, b []byte, op byte) {
		if b[0] != op {
			t.Errorf("%s opcode = %d, want %d", name, b[0], op)
		}
		if got := int(binary.LittleEndian.Uint16(b[2:])) * 4; got != len(b) {
			t.Errorf("%s length field = %d, want %d", name, got, len(b))
		}
		if len(b)%4 != 0 {
			t.Errorf("%s length %d not 4-aligned", name, len(b))
		}
	}
	checkHdr("InternAtom", InternAtom("CLIPBOARD", false), opInternAtom)
	checkHdr("InternAtom(image/png)", InternAtom("image/png", true), opInternAtom)
	checkHdr("CreateWindow", CreateWindow(2, 3), opCreateWindow)
	checkHdr("SetSelectionOwner", SetSelectionOwner(1, 2, 0), opSetSelectionOwner)
	checkHdr("GetSelectionOwner", GetSelectionOwner(2), opGetSelectionOwner)
	checkHdr("ConvertSelection", ConvertSelection(1, 2, 3, 4, 0), opConvertSelection)
	checkHdr("ChangeProperty8", ChangeProperty(1, 2, 3, 8, []byte("hello")), opChangeProperty)
	checkHdr("ChangePropertyAtoms", ChangeProperty(1, 2, AtomATOM, 32, AtomList(4, 5)), opChangeProperty)
	checkHdr("GetProperty", GetProperty(true, 1, 2, 0, 0, 0xffffffff), opGetProperty)
	checkHdr("DeleteProperty", DeleteProperty(1, 2), opDeleteProperty)
	checkHdr("GetInputFocus", GetInputFocus(), opGetInputFocus)
	checkHdr("SendSelectionNotify", SendSelectionNotify(SelectionNotify{Requestor: 9}), opSendEvent)

	// ChangeProperty format-8 length field is the byte count.
	cp := ChangeProperty(1, 2, 3, 8, []byte("hello"))
	if got := binary.LittleEndian.Uint32(cp[20:]); got != 5 {
		t.Errorf("ChangeProperty8 nItems = %d, want 5", got)
	}
	// Format-32 atom list length field is the atom count.
	cpa := ChangeProperty(1, 2, AtomATOM, 32, AtomList(4, 5, 6))
	if got := binary.LittleEndian.Uint32(cpa[20:]); got != 3 {
		t.Errorf("ChangePropertyAtoms nItems = %d, want 3", got)
	}
}

// makeReply builds a 32-byte reply with optional trailing value bytes.
func makeReply(value []byte) []byte {
	b := make([]byte, 32+pad4(len(value)))
	b[0] = 1
	binary.LittleEndian.PutUint32(b[4:], uint32(pad4(len(value))/4))
	copy(b[32:], value)
	return b
}

func TestReadPacketReplyWithValue(t *testing.T) {
	val := []byte("clipboard data")
	r := bytes.NewReader(makeReply(val))
	p, err := ReadPacket(r)
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if !p.IsReply() {
		t.Fatal("expected reply")
	}
	if len(p.Raw) != 32+pad4(len(val)) {
		t.Errorf("reply length = %d", len(p.Raw))
	}
}

// errorPacket builds a 32-byte X11 error with the given error code.
func errorPacket(code byte) []byte {
	b := make([]byte, 32)
	b[0] = 0
	b[1] = code
	return b
}

// eventPacket builds a 32-byte event with the given code.
func eventPacket(code byte) []byte {
	b := make([]byte, 32)
	b[0] = code
	return b
}

// TestNextEventIgnoresErrors is the pure-Go regression for #61: an X11 protocol
// error must never abort the process. NextEvent decodes and drops the error and
// returns the following event, proving the crash class is impossible.
func TestNextEventIgnoresErrors(t *testing.T) {
	stream := append(errorPacket(9 /*BadDrawable*/), eventPacket(EventSelectionRequest)...)
	p, err := NextEvent(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("NextEvent: %v", err)
	}
	if p.EventCode() != EventSelectionRequest {
		t.Errorf("event code = %d, want %d", p.EventCode(), EventSelectionRequest)
	}
}

func TestNextEventMasksSendEventBit(t *testing.T) {
	p, err := NextEvent(bytes.NewReader(eventPacket(EventSelectionNotify | 0x80)))
	if err != nil {
		t.Fatalf("NextEvent: %v", err)
	}
	if p.EventCode() != EventSelectionNotify {
		t.Errorf("event code = %d, want %d", p.EventCode(), EventSelectionNotify)
	}
}

func TestSequenceAndErrorCode(t *testing.T) {
	b := errorPacket(11)
	binary.LittleEndian.PutUint16(b[2:], 1234)
	p := Packet{Raw: b}
	if p.Sequence() != 1234 {
		t.Errorf("Sequence = %d, want 1234", p.Sequence())
	}
	if p.ErrorCode() != 11 {
		t.Errorf("ErrorCode = %d, want 11", p.ErrorCode())
	}
}

func TestNextReplyFailsOnError(t *testing.T) {
	if _, err := NextReply(bytes.NewReader(errorPacket(2))); err == nil {
		t.Error("NextReply should fail on an error packet")
	}
}

func TestPropertyValueDecode(t *testing.T) {
	// format 8, 5 items -> "hello"
	b := makeReply([]byte("hello\x00\x00\x00"))
	b[1] = 8
	binary.LittleEndian.PutUint32(b[16:], 5)
	p := Packet{Raw: b}
	if got := string(p.PropertyValue()); got != "hello" {
		t.Errorf("PropertyValue = %q, want hello", got)
	}
	// format 0 -> no data
	z := makeReply(nil)
	z[1] = 0
	if got := (Packet{Raw: z}).PropertyValue(); got != nil {
		t.Errorf("PropertyValue(format 0) = %v, want nil", got)
	}
}

func TestAtomAndOwnerDecode(t *testing.T) {
	b := make([]byte, 32)
	b[0] = 1
	binary.LittleEndian.PutUint32(b[8:], 0x123)
	if got := (Packet{Raw: b}).Atom(); got != 0x123 {
		t.Errorf("Atom = %#x", got)
	}
	if got := (Packet{Raw: b}).SelectionOwner(); got != 0x123 {
		t.Errorf("SelectionOwner = %#x", got)
	}
}

func TestSelectionRequestDecode(t *testing.T) {
	ev := SendSelectionNotify(SelectionNotify{
		Time: 7, Requestor: 0x10, Selection: 0x20, Target: 0x30, Property: 0x40,
	})
	// The event body sits at offset 12 of a SendEvent request; feed just the
	// 32-byte event back through the decoder.
	p := Packet{Raw: ev[12:44]}
	if p.EventCode() != EventSelectionNotify {
		t.Fatalf("event code = %d", p.EventCode())
	}
	sn := p.SelectionNotify()
	if sn.Requestor != 0x10 || sn.Selection != 0x20 || sn.Target != 0x30 || sn.Property != 0x40 {
		t.Errorf("SelectionNotify decode = %+v", sn)
	}
}
