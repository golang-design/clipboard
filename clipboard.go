// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

/*
Package clipboard copies and pastes from Go, the same way on macOS, Windows,
Linux, the BSDs, iOS, Android and the browser.

Call Init once, before anything else. It fails if there is no clipboard to talk
to, such as on a Linux server with no display:

	if err := clipboard.Init(); err != nil {
		log.Fatal(err)
	}

# Copy and paste

	clipboard.Write(ctx, clipboard.FmtText, []byte("hello")) // copy
	b, err := clipboard.Read(ctx, clipboard.FmtText)        // paste

There are three built-in formats: FmtText is UTF-8 text, FmtImage is a PNG
image, and FmtFiles is a list of file paths. When a call fails, the error says
why:

	switch {
	case errors.Is(err, clipboard.ErrNoData):      // nothing on the clipboard in this format
	case errors.Is(err, clipboard.ErrUnavailable): // no clipboard to reach
	case errors.Is(err, clipboard.ErrUnsupported): // this platform cannot do that
	}

# Images

Read(FmtImage) always returns PNG. Write(FmtImage, ...) takes PNG as it is, and
converts other encodings to PNG once their decoder is imported:

	import _ "image/jpeg"

	clipboard.Write(ctx, clipboard.FmtImage, jpegBytes) // stored as PNG

To keep the bytes unchanged — a JPEG that stays a JPEG — use a custom format
instead.

# Custom formats

Register turns any MIME type into a Format. Its bytes go on the clipboard
exactly as given, with no conversion:

	html := clipboard.Register("text/html")
	clipboard.Write(ctx, html, []byte("<b>hi</b>"))
	b, err := clipboard.Read(ctx, html)

ReadAs reads and decodes in one step. Formats lists what is on the clipboard,
and Format.MIME names each entry.

# Several formats in one copy

WriteAll puts several representations on the clipboard at once, most preferred
first, and the app you paste into takes the best one it understands. Calling
Write twice does not do this: each write replaces the whole clipboard.

	clipboard.WriteAll(ctx,
		clipboard.Item{Format: html, Bytes: []byte("<b>hi</b>")},
		clipboard.Item{Format: clipboard.FmtText, Bytes: []byte("hi")},
	)

# Files

WriteFiles and ReadFiles copy and paste files the way a file manager does, so
they work with Finder, Explorer, Nautilus and Dolphin:

	clipboard.WriteFiles(ctx, []string{"/home/me/report.pdf"})
	paths, err := clipboard.ReadFiles(ctx)

# Watching for changes

Write returns a channel that fires once, when something else replaces what you
wrote. Watch reports every change until ctx is canceled:

	for data := range clipboard.Watch(ctx, clipboard.FmtText) {
		fmt.Println(string(data.Bytes))
	}

# Passwords

Password managers mark what they copy as sensitive. Sensitive reports that
mark, and so does Data.Sensitive on every value from Watch; a tool that keeps
or syncs the clipboard should skip such content.

# Linux and the BSDs

X11 and Wayland have a second clipboard, the primary selection: whatever was
last selected with the mouse, pasted with the middle button. Pass FromPrimary
to any call to use it. Loops removes a write after it has been pasted a given
number of times. Both work only on X11 and Wayland.

Init chooses the backend by itself: Wayland when WAYLAND_DISPLAY is set and the
compositor offers a data-control protocol (ext-data-control-v1 or
wlr-data-control-unstable-v1), X11 otherwise — which, under an older Wayland
compositor, means XWayland. Neither needs Cgo, libX11 or libwayland. Wayland is
tested on Linux and FreeBSD.

On X11 and Wayland the program that writes serves the data to every app that
pastes, so the data is gone once the program exits, unless a clipboard manager
kept a copy. Keep the program running for as long as the data should be
pasteable.

# Other platforms

On Windows, a program running as a service gets a clipboard of its own, which
the logged-in user never sees. Do the clipboard work in a process inside the
user's session instead.

In the browser only text works, the page must be served over https, and a read
is allowed only after a user action such as a click. Call Read and Write from a
goroutine that the event handler starts, not in the handler itself.

On iOS and Android only text works.
*/
package clipboard // import "golang.design/x/clipboard"

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"sync"
)

// The errors a clipboard operation can report. They exist so a caller can tell
// the ordinary "nothing to paste" case from a clipboard it cannot reach at all —
// a distinction the byte-only API used to flatten into a nil result.
var (
	// ErrUnavailable means the clipboard itself could not be reached: no X
	// server, a display connection that failed, or another application holding
	// the clipboard open past the retry window.
	ErrUnavailable = errors.New("clipboard: unavailable")
	// ErrUnsupported means this platform cannot do what was asked: an image on
	// mobile, a custom format in a CGO-disabled build, the primary selection on
	// Windows.
	ErrUnsupported = errors.New("clipboard: unsupported")
)

var (
	// activate only for running tests.
	debug          = false
	errUnavailable = ErrUnavailable
	errUnsupported = ErrUnsupported
	errNoCgo       = fmt.Errorf("%w: cannot use when CGO_ENABLED=0", ErrUnavailable)
)

// Option configures a clipboard operation. Format and Item are Options too, so
// one argument list says both what an operation acts on and how:
//
//	clipboard.Watch(ctx, clipboard.FmtText, clipboard.FromPrimary())
type Option interface {
	apply(*config)
}

// config is what a call's options add up to.
type config struct {
	// sel is the clipboard to act on: the ordinary one unless FromPrimary was
	// given. It is passed to the backend rather than kept in a global, so one
	// call cannot change which clipboard another is reading.
	sel selection
	// formats and items collect the Format and Item options, in the order the
	// caller gave them.
	formats []Format
	items   []Item
	// loops limits how many times a write is served before the data is
	// dropped; zero means unlimited.
	loops int
}

// selection names one of the two clipboards X11 and Wayland provide.
type selection int

const (
	// selClipboard is the Ctrl+C/Ctrl+V clipboard, and the only one on
	// platforms with a single clipboard.
	selClipboard selection = iota
	// selPrimary is the X11/Wayland primary selection: whatever was last
	// selected with the mouse, pasted with the middle button.
	selPrimary
)

// optionFunc adapts a plain function to Option.
type optionFunc func(*config)

func (o optionFunc) apply(c *config) { o(c) }

// FromPrimary directs the operation at the primary selection instead of the
// clipboard. On X11 and Wayland these are two independent clipboards: the
// primary selection holds whatever was last selected with the mouse and is
// pasted with the middle button, and copying does not disturb it.
//
//	sel, err := clipboard.Read(ctx, clipboard.FmtText, clipboard.FromPrimary())
//
// On Wayland the compositor must offer ext-data-control-v1, or version 2 or
// later of wlr-data-control; current compositors do.
//
// The primary selection exists only on X11 and Wayland. Elsewhere — Windows,
// macOS, iOS, Android, the browser — Read and Write return ErrUnsupported and
// Watch delivers nothing. A write is deliberately not redirected to the
// ordinary clipboard: that would destroy whatever the user had copied.
func FromPrimary() Option { return optionFunc(func(c *config) { c.sel = selPrimary }) }

// withSelection carries an already-resolved selection into a public call. The
// polling watchers go back through Read so the package lock is taken for them,
// and must ask for the selection they were started on: reading without it would
// poll the ordinary clipboard while claiming to watch the primary selection,
// and deliver the wrong clipboard's data.
func withSelection(sel selection) Option { return optionFunc(func(c *config) { c.sel = sel }) }

// Loops limits how many times the written data is handed to a pasting
// application before it is dropped from the clipboard. It is how you put a
// secret on the clipboard and have it disappear once it has been pasted:
//
//	clipboard.Write(ctx, clipboard.FmtText, password, clipboard.Loops(1))
//
// Read this part first: Loops works on X11 and Wayland only, and is silently
// ignored on Windows, macOS, iOS, Android and in CGO-disabled builds. It is
// therefore not a way to clear a secret from a Windows or macOS clipboard —
// there the data stays until something else replaces it.
//
// The difference is in the platforms, not in this package. On X11 and Wayland a
// writer owns the selection and personally answers every paste request, so it
// can count them and give up ownership. On Windows and macOS a write copies the
// bytes into an OS-owned store and returns; no request ever comes back to this
// process, so there is nothing to count and nothing to withdraw.
//
// A serve is one delivery of the data to a requestor, not one paste: an
// application that asks for several formats in one paste consumes one loop per
// format. Asking which formats are available does not consume any. A count of
// zero or less means unlimited, which is the default.
//
// Every reader counts, including this program. A Read in this process consumes a
// serve like anyone else's, and so does each poll of a Watch on the same format
// — so a Watch running alongside a Loops(1) write will usually be the one that
// consumes it. Watch a different format, or do not watch while a serve-limited
// write is outstanding.
func Loops(n int) Option { return optionFunc(func(c *config) { c.loops = n }) }

// newConfig folds the options into a config.
func newConfig(opts []Option) *config {
	c := &config{}
	for _, o := range opts {
		o.apply(c)
	}
	return c
}

// Format represents the format of clipboard data.
type Format int

// apply lets a Format be passed wherever an Option is taken, so Watch and
// WriteAll can accept formats and options in the same argument list.
func (f Format) apply(c *config) { c.formats = append(c.formats, f) }

// The built-in formats.
const (
	// FmtText indicates plain text clipboard format. Its bytes are UTF-8
	// encoded in both directions.
	FmtText Format = iota
	// FmtImage indicates image/png clipboard format. It is PNG-only, not a
	// generic "any image" format: Read returns PNG bytes, and Write encodes
	// what it is given to PNG (see Write for which inputs it can decode).
	//
	// FmtImage is a transcoding format, so it is the wrong tool for bytes
	// that must survive unchanged. To exchange another image encoding
	// verbatim, register its MIME type as a custom format — Register("image/jpeg"),
	// Register("image/svg+xml") — which is raw passthrough.
	FmtImage
	// FmtFiles indicates a list of file paths — what a file manager puts on
	// the clipboard when you copy files. Its bytes are a text/uri-list body
	// (RFC 2483): file URIs, one per line, separated by CRLF.
	//
	// Each platform stores a file list its own way (CF_HDROP on Windows,
	// NSFilenamesPboardType on macOS, text/uri-list on X11 and Wayland) and
	// this format translates between them, so the same code copies files
	// everywhere. Use ReadFiles and WriteFiles to work in paths rather than
	// URIs; Read and Write give the raw text/uri-list bytes.
	//
	// Desktop only: on iOS, Android and in CGO-disabled builds a file list is
	// neither readable nor writable, and the API degrades as it does elsewhere.
	FmtFiles
)

var (
	// Due to the limitation on operating systems (such as darwin),
	// concurrent read can even cause panic, use a global lock to
	// guarantee one read at a time.
	lock      = sync.Mutex{}
	initOnce  sync.Once
	initError error
)

// Init prepares the package and reports whether there is a clipboard to use.
// Call it once, before any other function; calling it again returns the same
// result.
//
//	if err := clipboard.Init(); err != nil {
//		log.Fatal(err)
//	}
//
// It fails, with an error wrapping ErrUnavailable, when there is nothing to
// talk to: on Linux and the BSDs, neither a Wayland compositor offering
// data-control nor an X server; in the browser, no navigator.clipboard; and on
// a platform that needs Cgo, a build with CGO_ENABLED=0. After a failed Init,
// Read and Write return errors and Watch delivers nothing.
func Init() error {
	initOnce.Do(func() {
		initError = initialize()
	})
	return initError
}

// Read returns what is on the clipboard in format t, or ErrNoData if the
// clipboard holds nothing in that format.
//
// The bytes are in the format's encoding: UTF-8 for FmtText, PNG for FmtImage
// whatever the copying app used, and a text/uri-list for FmtFiles. A custom
// format from Register comes back exactly as it sits on the clipboard.
//
// Pass FromPrimary to read the primary selection instead of the clipboard.
func Read(ctx context.Context, t Format, opts ...Option) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock.Lock()
	defer lock.Unlock()

	buf, err := read(ctx, newConfig(opts).sel, t)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "read clipboard err: %v\n", err)
		}
		return nil, err
	}
	if buf == nil {
		// A backend that reached the clipboard and found nothing reports it as
		// the ordinary empty case rather than as a failure.
		return nil, ErrNoData
	}
	return buf, nil
}

// Write puts buf on the clipboard in format t, replacing what was there.
//
// The data is on the clipboard as soon as Write returns. The returned channel
// is optional: it receives one value, and is then closed, when something else
// later replaces the clipboard. If nothing ever does, it never fires, so do not
// wait on it to learn that the write finished.
//
// For FmtImage, buf is converted to PNG first. PNG is stored as it is; another
// encoding is converted if the program has imported its decoder (for example
// _ "image/jpeg" or _ "golang.org/x/image/webp"); anything else is stored
// unchanged.
//
// Pass FromPrimary to write the primary selection instead, or Loops to limit
// how many times the data is pasted.
func Write(ctx context.Context, t Format, buf []byte, opts ...Option) (<-chan struct{}, error) {
	return WriteAll(ctx, append([]Option{Item{Format: t, Bytes: buf}}, opts...)...)
}

// Item is one representation of the content being copied: the format it is
// encoded in, and the bytes in that encoding.
type Item struct {
	Format Format
	Bytes  []byte
}

// apply lets an Item be passed wherever an Option is taken, so WriteAll can
// accept items and options in the same argument list.
func (i Item) apply(c *config) { c.items = append(c.items, i) }

// WriteAll publishes several representations of the same content to the
// clipboard in one operation, so a consuming application can take the richest
// one it understands — plain text and HTML from a single copy, for instance:
//
//	html := clipboard.Register("text/html")
//	clipboard.WriteAll(ctx,
//		clipboard.Item{Format: html, Bytes: []byte("<b>hi</b>")},
//		clipboard.Item{Format: clipboard.FmtText, Bytes: []byte("hi")},
//	)
//
// Order is preference, most preferred first: it is what tells the consumer
// which representation to pick. A format that appears more than once keeps its
// first occurrence, so earlier stays stronger — as do two formats that name the
// same native clipboard type, such as FmtImage and Register("image/png") on the
// platforms where both mean the system's PNG type. Items in FmtImage are
// normalized to PNG exactly as Write normalizes its argument.
//
// A Format passed to WriteAll is ignored — it names a format with no bytes
// attached. Pass Items, or use Write for a single format.
//
// The items replace the clipboard together — there is no moment at which only
// some of them are on it — and calling Write for each format instead would not
// do the same thing: every write replaces the whole clipboard, so only the last
// one would survive.
//
// The returned channel behaves as Write's does: it receives one value, and is
// then closed, when something else later replaces the whole set. Given no
// items, WriteAll does nothing and returns a nil channel and a nil error.
//
// Multi-representation clipboards are a desktop feature. On iOS, Android and in
// the browser only the most preferred item is published.
//
// Pass FromPrimary to publish to the primary selection instead, or Loops to
// limit how many times the set is served.
func WriteAll(ctx context.Context, opts ...Option) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock.Lock()
	defer lock.Unlock()

	c := newConfig(opts)
	items := normalizeItems(c.items)
	if len(items) == 0 {
		return nil, nil
	}

	changed, err := writeAll(ctx, c.sel, items, c.loops)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "write to clipboard err: %v\n", err)
		}
		return nil, err
	}
	return changed, nil
}

// normalizeItems drops the later duplicate of a format, keeping the caller's
// order, and encodes every image item as PNG. It returns a new slice so a
// caller's argument is never modified.
func normalizeItems(items []Item) []Item {
	out := make([]Item, 0, len(items))
	seen := make(map[Format]bool, len(items))
	for _, it := range items {
		if seen[it.Format] {
			continue
		}
		seen[it.Format] = true
		if it.Format == FmtImage {
			it.Bytes = toPNG(it.Bytes)
		}
		out = append(out, it)
	}
	return out
}

// toPNG normalizes an FmtImage payload to canonical PNG: the clipboard stores
// and serves PNG so consumers get a consistent, alpha-aware encoding. If buf is
// already PNG (or not a decodable image) it is returned unchanged; otherwise it
// is decoded and re-encoded as PNG.
//
// Decoding relies on the image decoders the importing program has registered, so
// no decoder is a mandatory dependency of this package: to accept JPEG/GIF/WebP
// input, blank-import the corresponding decoder (e.g. _ "image/jpeg",
// _ "golang.org/x/image/webp"). Unknown or undecodable input passes through
// unchanged, preserving the previous bytes-in behavior.
func toPNG(buf []byte) []byte {
	// Cheap path: already PNG (avoid a needless decode/encode round-trip).
	if len(buf) >= 8 && bytes.Equal(buf[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return buf
	}
	img, _, err := image.Decode(bytes.NewReader(buf))
	if err != nil {
		return buf // not a decodable image (or its decoder isn't registered)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return buf
	}
	return out.Bytes()
}

// ReadFiles returns the paths of the files currently on the clipboard, or nil
// if it holds no file list. It is Read(FmtFiles) with the text/uri-list body
// parsed into paths:
//
//	paths, _ := clipboard.ReadFiles(ctx)
//	for _, path := range paths {
//		fmt.Println(path)
//	}
//
// A URI that does not name a local file — a remote one, or a path this platform
// cannot express — is skipped rather than guessed at, so the result can be
// shorter than what the clipboard holds.
func ReadFiles(ctx context.Context, opts ...Option) ([]string, error) {
	buf, err := Read(ctx, FmtFiles, opts...)
	if err != nil {
		return nil, err
	}
	return pathsFromURIList(buf), nil
}

// WriteFiles publishes a list of file paths, as copying files in a file manager
// does. The paths must be absolute; a relative one has no unambiguous URI and
// is dropped.
//
// It is Write(FmtFiles, ...) over a text/uri-list body, so it replaces the
// clipboard and returns the same channel Write does. To publish a file list
// alongside another format, use WriteAll with an FmtFiles item.
//
// It takes a slice rather than variadic paths because a string cannot be an
// Option without swallowing every stray string argument; the options follow.
func WriteFiles(ctx context.Context, paths []string, opts ...Option) (<-chan struct{}, error) {
	return Write(ctx, FmtFiles, uriListFromPaths(paths), opts...)
}

// Data is a single observed clipboard change: the format the change was
// detected in, together with the raw bytes encoded the same way Read
// returns them (UTF-8 for FmtText, PNG for FmtImage).
type Data struct {
	Format Format
	Bytes  []byte
	// Sensitive reports that the application that copied this marked it as
	// sensitive, as password managers do with passwords; see Sensitive. A
	// tool that keeps or shares what is copied should drop it. It is false
	// wherever the platform has no such marker.
	Sensitive bool
}

// Watch reports each change to the clipboard in the given formats until ctx is
// canceled, and then closes the channel. Each value carries the format it was
// seen in, so one Watch can observe several formats; with none given, it
// observes FmtText, FmtImage and FmtFiles.
//
//	for data := range clipboard.Watch(ctx, clipboard.FmtText) {
//		fmt.Println(string(data.Bytes))
//	}
//
// Pass FromPrimary to watch the primary selection instead of the clipboard.
func Watch(ctx context.Context, opts ...Option) <-chan Data {
	c := newConfig(opts)
	t := c.formats
	if len(t) == 0 {
		t = []Format{FmtText, FmtImage, FmtFiles}
	}

	out := make(chan Data)
	var wg sync.WaitGroup
	for _, f := range t {
		in := watch(ctx, c.sel, f)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range in {
				// Checked once the change is seen, so a value copied and
				// replaced in the same instant may be judged by its
				// successor's marker.
				secret, _ := Sensitive(ctx, withSelection(c.sel))
				select {
				case out <- Data{Format: f, Bytes: b, Sensitive: secret}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
