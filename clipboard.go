// Copyright 2021 The golang.design Initiative authors.
// All rights reserved. Use of this source code is governed
// by a GNU GPL-3 license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard // import "golang.design/x/clipboard"

import (
	"sync"
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

	return read(t)
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

	ok, changed := write(t, buf)
	if !ok {
		return nil
	}
	return changed
}
