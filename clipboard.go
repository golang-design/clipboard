// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard // import "golang.design/x/clipboard"

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
)

var (
	debug               = true
	errUnavailable      = errors.New("clipboard unavailable")
	errUnsupported      = errors.New("unsupported format")
	errInvalidOperation = errors.New("invalid operation")
)

// Format represents the MIME type of clipboard data.
type Format int

// All sorts of supported clipboard data
const (
	// FmtText indicates plain text MIME format
	FmtText Format = iota
	// FmtImage indicates image/png MIME format
	FmtImage
)

// Due to the limitation on operating systems (such as darwin),
// concurrent read can even cause panic, use a global lock to
// guarantee one read at a time.
var lock = sync.Mutex{}

// Read reads and returns the clipboard data.
func Read(t Format) []byte {
	lock.Lock()
	defer lock.Unlock()

	buf, err := read(t)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "read clipboard err: %v\n", err)
		}
		return nil
	}
	return buf
}

// Write writes a given buffer to the clipboard.
// The returned channel can receive an empty struct as signal that
// indicates the clipboard has been overwritten from this write.
//
// If the MIME type indicates an image, then the given buf assumes
// the image data is PNG encoded.
func Write(t Format, buf []byte) <-chan struct{} {
	lock.Lock()
	defer lock.Unlock()

	changed, err := write(t, buf)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "write to clipboard err: %v\n", err)
		}
		return nil
	}
	return changed
}

// Watch returns a receive-only channel that received the clipboard data
// if any changes of clipboard data in the desired format happends.
//
// The returned channel will be closed if the given context is canceled.
func Watch(ctx context.Context, t Format) <-chan []byte {
	return watch(ctx, t)
}
