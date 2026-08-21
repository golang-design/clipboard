// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard

import (
	"context"
	"errors"
	"slices"
	"sync"
)

// ErrNoData is returned by ReadAs when the clipboard holds no data in the
// requested format.
var ErrNoData = errors.New("clipboard: no data available for the given format")

// The custom-format registry maps a portable MIME-type string to an opaque
// Format token (and back). The built-in tokens are not part of the registry;
// custom tokens are allocated above them. Keeping the
// MIME identity here, behind the Format token, means no platform- or
// Cgo-specific type ever leaks into user code: each backend resolves the MIME
// string to its own native clipboard type (see formatMIME).
var (
	registryMu   sync.RWMutex
	mimeToFormat = map[string]Format{}
	formatToMIME = map[Format]string{}
	// nextFormat is the next custom token to hand out. Custom formats start
	// immediately above the built-in tokens so they never collide with them.
	nextFormat = FmtFiles + 1
)

// Register maps a MIME type to a Format token usable with Read, Write, and
// Watch. It is idempotent: Register returns the same token for the same MIME
// string, so it is safe to call repeatedly. Register is safe to call before
// Init and concurrently from multiple goroutines.
//
// The returned token denotes a raw, passthrough format: unlike FmtImage (which
// transcodes between PNG and each platform's native image type), Read and Write
// move the exact bytes to and from the clipboard under the MIME type's native
// representation, with no encoding or conversion. Registering "image/png" is
// therefore distinct from the built-in FmtImage.
func Register(mime string) Format {
	registryMu.Lock()
	defer registryMu.Unlock()
	if f, ok := mimeToFormat[mime]; ok {
		return f
	}
	f := nextFormat
	nextFormat++
	mimeToFormat[mime] = f
	formatToMIME[f] = mime
	return f
}

// formatMIME returns the MIME string a custom Format token was registered with.
// The boolean result is false for the built-in tokens and for any token that was
// never returned by Register. Backends call this in their
// read/write default case to resolve a custom token to a native clipboard type.
func formatMIME(f Format) (string, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	mime, ok := formatToMIME[f]
	return mime, ok
}

// MIME returns the MIME type that the Format f denotes. For the built-in
// formats it returns a canonical type ("text/plain;charset=utf-8" for FmtText,
// "image/png" for FmtImage, "text/uri-list" for FmtFiles); for a custom format
// it returns the string passed to Register; for a token that was never
// registered it returns "".
func (f Format) MIME() string {
	switch f {
	case FmtText:
		return "text/plain;charset=utf-8"
	case FmtImage:
		return "image/png"
	case FmtFiles:
		return "text/uri-list"
	}
	if mime, ok := formatMIME(f); ok {
		return mime
	}
	return ""
}

// Formats reports the formats currently available on the clipboard, in a stable
// order: the built-ins first (FmtText, FmtImage, FmtFiles, when present),
// followed by custom formats in registration order. Custom MIME types
// discovered on the clipboard are registered on demand, so every returned token
// can be passed straight to Read (and its identity inspected with Format.MIME).
//
// Pass FromPrimary to enumerate the primary selection instead.
//
// It returns an empty slice when the clipboard is empty or unavailable — for
// example on iOS/Android or in a CGO-disabled build, where it degrades like the
// rest of the API rather than panicking.
func Formats(ctx context.Context, opts ...Option) ([]Format, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock.Lock()
	defer lock.Unlock()
	return normalizeFormats(enumerateFormats(ctx, newConfig(opts).sel)), nil
}

// normalizeFormats de-duplicates tokens and returns them in a stable order: the
// built-ins first, in declaration order, then custom tokens in ascending token
// value, which is their registration order.
func normalizeFormats(in []Format) []Format {
	seen := make(map[Format]bool, len(in))
	for _, f := range in {
		seen[f] = true
	}

	var out []Format
	for _, b := range []Format{FmtText, FmtImage, FmtFiles} {
		if seen[b] {
			out = append(out, b)
			delete(seen, b)
		}
	}

	custom := make([]Format, 0, len(seen))
	for f := range seen {
		custom = append(custom, f)
	}
	slices.Sort(custom)
	return append(out, custom...)
}

// ReadAs reads the clipboard contents for the format f and decodes them into a
// typed value with decode. It returns the zero value of T and ErrNoData when
// the clipboard holds nothing in that format, or the zero value and decode's
// error when decoding fails.
//
// ReadAs relocates the typed-decode idea to where Go generics actually compose
// — a free helper over Read — instead of a heterogeneous registry that would
// have to erase every decode function to any. It is also the seam where an
// error-returning, capability-aware read path can grow without changing the
// byte-oriented Read.
func ReadAs[T any](ctx context.Context, f Format, decode func([]byte) (T, error), opts ...Option) (T, error) {
	var zero T
	buf, err := Read(ctx, f, opts...)
	if err != nil {
		return zero, err
	}
	return decode(buf)
}
