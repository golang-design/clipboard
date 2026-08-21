// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestRegister(t *testing.T) {
	// Idempotent: the same MIME always maps to the same token.
	html := Register("text/html")
	if got := Register("text/html"); got != html {
		t.Fatalf("Register is not idempotent: first %v, second %v", html, got)
	}

	// Distinct MIME types map to distinct tokens.
	pdf := Register("application/pdf")
	if pdf == html {
		t.Fatalf("distinct MIME types share a token: html=%v pdf=%v", html, pdf)
	}

	// Custom tokens never collide with the built-in formats.
	for _, builtin := range []Format{FmtText, FmtImage} {
		if html == builtin || pdf == builtin {
			t.Fatalf("custom token collides with a built-in: html=%v pdf=%v builtin=%v", html, pdf, builtin)
		}
	}

	// formatMIME round-trips a custom token back to its MIME string.
	if mime, ok := formatMIME(html); !ok || mime != "text/html" {
		t.Fatalf("formatMIME(html) = (%q, %v), want (\"text/html\", true)", mime, ok)
	}

	// formatMIME reports false for the built-ins and for unregistered tokens.
	if _, ok := formatMIME(FmtText); ok {
		t.Fatalf("formatMIME(FmtText) reported ok; built-ins are not in the registry")
	}
	if _, ok := formatMIME(FmtImage); ok {
		t.Fatalf("formatMIME(FmtImage) reported ok; built-ins are not in the registry")
	}
	if _, ok := formatMIME(Format(1 << 20)); ok {
		t.Fatalf("formatMIME reported ok for an unregistered token")
	}
}

func TestRegisterConcurrent(t *testing.T) {
	const mime = "application/x.concurrent-test"
	const n = 64

	var wg sync.WaitGroup
	tokens := make([]Format, n)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tokens[i] = Register(mime)
		}(i)
	}
	wg.Wait()

	for i, got := range tokens {
		if got != tokens[0] {
			t.Fatalf("concurrent Register returned different tokens: tokens[%d]=%v tokens[0]=%v", i, got, tokens[0])
		}
	}
}

func TestReadAsNoData(t *testing.T) {
	// A freshly registered format that was never written holds no data, so
	// ReadAs must report ErrNoData and must not invoke decode.
	f := Register("application/x.readas-nodata-test")
	called := false
	v, err := ReadAs(context.TODO(), f, func(b []byte) (int, error) {
		called = true
		return len(b), nil
	})
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("ReadAs error = %v, want ErrNoData", err)
	}
	if v != 0 {
		t.Fatalf("ReadAs value = %v, want zero value", v)
	}
	if called {
		t.Fatalf("decode was called despite no data being available")
	}
}

func TestFormatMIME(t *testing.T) {
	if got := FmtText.MIME(); got != "text/plain;charset=utf-8" {
		t.Fatalf("FmtText.MIME() = %q", got)
	}
	if got := FmtImage.MIME(); got != "image/png" {
		t.Fatalf("FmtImage.MIME() = %q", got)
	}
	if got := Register("application/x.mime-test").MIME(); got != "application/x.mime-test" {
		t.Fatalf("custom MIME() = %q, want application/x.mime-test", got)
	}
	if got := Format(1 << 20).MIME(); got != "" {
		t.Fatalf("unregistered MIME() = %q, want empty", got)
	}
}

func TestNormalizeFormats(t *testing.T) {
	a := Register("application/x.normalize-a")
	b := Register("application/x.normalize-b") // registered after a, so a < b

	// Out of order, with duplicates and both built-ins.
	got := normalizeFormats([]Format{b, FmtImage, a, FmtText, a, FmtImage, b})
	want := []Format{FmtText, FmtImage, a, b}
	if len(got) != len(want) {
		t.Fatalf("normalizeFormats len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeFormats = %v, want %v", got, want)
		}
	}

	// Empty input yields no formats.
	if got := normalizeFormats(nil); len(got) != 0 {
		t.Fatalf("normalizeFormats(nil) = %v, want empty", got)
	}
}
