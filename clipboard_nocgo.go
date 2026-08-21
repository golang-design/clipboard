//go:build !darwin && !windows && !linux && !freebsd && !openbsd && !netbsd && !js && !cgo

package clipboard

import "context"

func initialize() error {
	return errNoCgo
}

// enumerateFormats reports the formats on the clipboard. In a CGO-disabled
// build the clipboard is unavailable, so Formats() returns empty.
func enumerateFormats(ctx context.Context, sel selection) []Format { return nil }

// read returns errNoCgo for every format, including custom ones registered via
// Register: in a CGO-disabled build the clipboard is unavailable, so the public
// API degrades gracefully (Read returns nil, Write returns nil) rather than
// panicking.
func read(ctx context.Context, sel selection, t Format) (buf []byte, err error) {
	if sel == selPrimary {
		// No primary selection on this platform (see FromPrimary).
		return nil, errUnsupported
	}
	return nil, errNoCgo
}

func readc(t string) ([]byte, error) {
	return nil, errNoCgo
}

func write(ctx context.Context, sel selection, t Format, buf []byte) (<-chan struct{}, error) {
	if sel == selPrimary {
		// No primary selection here, and redirecting to the ordinary clipboard
		// would destroy what the user had copied (see FromPrimary).
		return nil, errUnsupported
	}
	return nil, errNoCgo
}

// writeAll publishes the most preferred item only. This platform has no
// multi-representation clipboard, and writing each item in turn would be worse
// than useless: every write replaces the last, so the *least* preferred
// representation would win — the reverse of what the caller asked for (#151).
func writeAll(ctx context.Context, sel selection, items []Item, loops int) (<-chan struct{}, error) {
	// loops is ignored: this platform's clipboard is a store the OS serves, so
	// no paste request ever reaches this process to be counted (see Loops).
	_ = loops
	return write(ctx, sel, items[0].Format, items[0].Bytes)
}

func watch(ctx context.Context, sel selection, t Format) <-chan []byte {
	// The clipboard is unavailable in a CGO-disabled build. Return a
	// closed channel so that receivers observe completion immediately
	// instead of blocking forever, consistent with the documented
	// behavior when the given context is canceled.
	ch := make(chan []byte)
	close(ch)
	return ch
}
