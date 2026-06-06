//go:build !darwin && !windows && !linux && !cgo

package clipboard

import "context"

func initialize() error {
	return errNoCgo
}

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
