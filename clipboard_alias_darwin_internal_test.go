// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build darwin && !ios

package clipboard

import (
	"bytes"
	"testing"

	"github.com/ebitengine/purego/objc"
)

// TestDarwinPasteboardTypes asserts the MIME → UTI table is applied on the
// outbound path, and that every alias round-trips back to its MIME type.
func TestDarwinPasteboardTypes(t *testing.T) {
	if got := darwinPasteboardTypes("text/html"); len(got) != 2 || got[0] != "public.html" || got[1] != "text/html" {
		t.Fatalf(`darwinPasteboardTypes("text/html") = %v, want ["public.html" "text/html"]`, got)
	}

	// A MIME type with no alias is used verbatim, as before.
	const raw = "application/x.golang-design.clipboard-test"
	if got := darwinPasteboardTypes(raw); len(got) != 1 || got[0] != raw {
		t.Fatalf("darwinPasteboardTypes(%q) = %v, want [%q]", raw, got, raw)
	}

	for mime, uti := range darwinNativeTypes {
		if got := darwinPasteboardTypes(mime)[0]; got != uti {
			t.Fatalf("darwinPasteboardTypes(%q)[0] = %q, want %q", mime, got, uti)
		}
		if got, ok := darwinMIMEForType(uti); !ok || got != mime {
			t.Fatalf("darwinMIMEForType(%q) = (%q, %v), want (%q, true)", uti, got, ok, mime)
		}
	}
	if _, ok := darwinMIMEForType("NSStringPboardType"); ok {
		t.Fatal(`darwinMIMEForType("NSStringPboardType") reported an alias, want none`)
	}
}

// TestDarwinFormatFor asserts the inbound path is the inverse of the outbound
// one: an advertised pasteboard type resolves to the token whose Read looks at
// the same pasteboard data.
func TestDarwinFormatFor(t *testing.T) {
	html := Register("text/html")
	for _, typ := range []string{"public.html", "text/html"} {
		f, ok := darwinFormatFor(typ)
		if !ok || f != html {
			t.Fatalf("darwinFormatFor(%q) = (%v, %v), want (%v, true)", typ, f, ok, html)
		}
	}
	if f, ok := darwinFormatFor("com.adobe.pdf"); !ok || f != Register("application/pdf") {
		t.Fatalf(`darwinFormatFor("com.adobe.pdf") = (%v, %v), want the application/pdf token`, f, ok)
	}
	// The built-in image UTIs keep reporting FmtImage even though they also
	// alias image/png and image/tiff.
	if f, ok := darwinFormatFor("public.png"); !ok || f != FmtImage {
		t.Fatalf(`darwinFormatFor("public.png") = (%v, %v), want (FmtImage, true)`, f, ok)
	}
	if f, ok := darwinFormatFor("NeXT smart paste pasteboard type"); ok {
		t.Fatalf("darwinFormatFor of a non-MIME type = (%v, true), want it to be skipped", f)
	}
}

// TestDarwinForeignTypeIsReadable is the #160 repro without a second
// application: data published under the UTI another app writes (public.html)
// must be reachable through Register("text/html") and reported by Formats, and
// this package's own write must land under that same UTI.
func TestDarwinForeignTypeIsReadable(t *testing.T) {
	want := []byte("<b>foreign</b>")
	// A pasteboard type with no MIME alias is used verbatim, so this publishes
	// public.html exactly as another application would.
	if !clipboard_write_custom("public.html", want) {
		t.Fatal("failed to publish public.html on the pasteboard")
	}

	html := Register("text/html")
	if got := Read(html); !bytes.Equal(got, want) {
		t.Fatalf(`Read(Register("text/html")) = %q, want %q`, got, want)
	}
	found := false
	for _, f := range Formats() {
		if f == html {
			found = true
		}
	}
	if !found {
		t.Fatalf(`Formats() = %v, want it to include the "text/html" token %v`, Formats(), html)
	}

	// Writing through the public API advertises the native UTI, which is what
	// makes the data visible to other applications.
	Write(html, want)
	types := pasteboardTypes()
	found = false
	for _, typ := range types {
		if typ == "public.html" {
			found = true
		}
	}
	if !found {
		t.Fatalf(`pasteboard types after writing text/html = %v, want them to include "public.html"`, types)
	}
}

// pasteboardTypes returns the types the general pasteboard currently advertises.
func pasteboardTypes() []string {
	defer newAutoreleasePool()()
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	types := pasteboard.Send(sel_types)
	if types == 0 {
		return nil
	}
	n := int(objc.ID(types).Send(sel_count))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, nsStringGo(objc.ID(objc.ID(types).Send(sel_objectAtIndex, uintptr(i)))))
	}
	return out
}
