// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard

import (
	"errors"
	"sync"
)

// ErrNoData is returned by ReadAs when the clipboard holds no data in the
// requested format.
var ErrNoData = errors.New("clipboard: no data available for the given format")

// The custom-format registry maps a portable MIME-type string to an opaque
// Format token (and back). The built-in FmtText and FmtImage tokens are not
// part of the registry; custom tokens are allocated above them. Keeping the
// MIME identity here, behind the Format token, means no platform- or
// Cgo-specific type ever leaks into user code: each backend resolves the MIME
// string to its own native clipboard type (see formatMIME).
var (
	registryMu   sync.RWMutex
	mimeToFormat = map[string]Format{}
	formatToMIME = map[Format]string{}
	// nextFormat is the next custom token to hand out. Custom formats start
	// immediately above the built-in tokens so they never collide with
	// FmtText/FmtImage.
	nextFormat = FmtImage + 1
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
// The boolean result is false for the built-in FmtText/FmtImage tokens and for
// any token that was never returned by Register. Backends call this in their
// read/write default case to resolve a custom token to a native clipboard type.
func formatMIME(f Format) (string, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	mime, ok := formatToMIME[f]
	return mime, ok
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
func ReadAs[T any](f Format, decode func([]byte) (T, error)) (T, error) {
	var zero T
	buf := Read(f)
	if buf == nil {
		return zero, ErrNoData
	}
	return decode(buf)
}
