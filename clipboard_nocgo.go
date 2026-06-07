//go:build !darwin && !windows && !linux && !freebsd && !openbsd && !netbsd && !cgo

package clipboard

import "context"

func initialize() error {
	return errNoCgo
}

// enumerateFormats reports the formats on the clipboard. In a CGO-disabled
// build the clipboard is unavailable, so Formats() returns empty.
func enumerateFormats() []Format { return nil }

// read returns errNoCgo for every format, including custom ones registered via
// Register: in a CGO-disabled build the clipboard is unavailable, so the public
// API degrades gracefully (Read returns nil, Write returns nil) rather than
// panicking.
func read(t Format) (buf []byte, err error) {
	return nil, errNoCgo
}

func readc(t string) ([]byte, error) {
	return nil, errNoCgo
}

func write(t Format, buf []byte) (<-chan struct{}, error) {
	return nil, errNoCgo
}

func watch(ctx context.Context, t Format) <-chan []byte {
	// The clipboard is unavailable in a CGO-disabled build. Return a
	// closed channel so that receivers observe completion immediately
	// instead of blocking forever, consistent with the documented
	// behavior when the given context is canceled.
	ch := make(chan []byte)
	close(ch)
	return ch
}
