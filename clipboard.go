// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

/*
Package clipboard provides cross platform clipboard access and supports
macOS/Linux/Windows/BSD/Android/iOS platform. Before interacting with the
clipboard, one must call Init to assert if it is possible to use this
package:

	err := clipboard.Init()
	if err != nil {
		panic(err)
	}

The most common operations are `Read` and `Write`. To use them:

	// write/read text format data of the clipboard, and
	// the byte buffer regarding the text are UTF8 encoded.
	clipboard.Write(clipboard.FmtText, []byte("text data"))
	clipboard.Read(clipboard.FmtText)

	// write/read image format data of the clipboard, and
	// the byte buffer regarding the image are PNG encoded.
	clipboard.Write(clipboard.FmtImage, []byte("image data"))
	clipboard.Read(clipboard.FmtImage)

FmtImage is PNG: the clipboard serves images PNG-encoded because PNG
carries an alpha channel, which other graphical software relies on.
Read(FmtImage) therefore always returns PNG bytes. Write(FmtImage, ...)
takes PNG as-is and normalizes other encodings to PNG when the program
has registered the matching decoder, so a JPEG or WebP input still
arrives on the clipboard as PNG:

	import _ "image/jpeg" // register the decoder you feed Write

	clipboard.Write(clipboard.FmtImage, jpegBytes) // stored as PNG

To move image bytes that must not be transcoded — a JPEG that stays a
JPEG, an SVG, a WebP, a camera raw — register that MIME type as a custom
format instead of using FmtImage; custom formats are raw passthrough:

	jpg := clipboard.Register("image/jpeg")
	clipboard.Write(jpg, jpegBytes)  // exact bytes, no conversion

To copy or paste files — what a file manager puts on the clipboard when you
press Ctrl+C on a selection — use WriteFiles and ReadFiles:

	clipboard.WriteFiles([]string{"/home/me/report.pdf", "/home/me/notes.txt"})

	for _, path := range clipboard.ReadFiles() {
		println(path)
	}

Each platform stores a file list its own way and FmtFiles translates between
them, so the same code interoperates with Explorer, Finder, Nautilus and
Dolphin. See FmtFiles for the details.

To publish several representations of the same content at once — plain text
and HTML from one copy, so the destination takes whichever it understands
best — use WriteAll. Calling Write twice does not do this: each write
replaces the whole clipboard, so only the last format would survive.

	html := clipboard.Register("text/html")
	clipboard.WriteAll(
		clipboard.Item{Format: html, Bytes: []byte("<b>hi</b>")},
		clipboard.Item{Format: clipboard.FmtText, Bytes: []byte("hi")},
	)

On X11 and Wayland there are two clipboards. The primary selection holds
whatever was last selected with the mouse and is pasted with the middle
button, independently of the Ctrl+C clipboard. Reach it with FromPrimary,
which every operation accepts:

	sel := clipboard.Read(clipboard.FmtText, clipboard.FromPrimary())
	ch := clipboard.Watch(ctx, clipboard.FmtText, clipboard.FromPrimary())

Windows and macOS have no second clipboard, so there a primary read returns
nil and a primary write is a no-op rather than touching the clipboard.

In addition, `clipboard.Write` returns a channel that can receive an
empty struct as a signal, which indicates the corresponding write call
to the clipboard is outdated, meaning the clipboard has been overwritten
by others and the previously written data is lost. For instance:

	changed := clipboard.Write(clipboard.FmtText, []byte("text data"))

	select {
	case <-changed:
		println(`"text data" is no longer available from clipboard.`)
	}

You can ignore the returning channel if you don't need this type of
notification. Furthermore, when you need more than just knowing whether
clipboard data is changed, use the watcher API:

	ch := clipboard.Watch(context.TODO(), clipboard.FmtText)
	for data := range ch {
		// print out clipboard data whenever it is changed
		println(string(data.Bytes))
	}

Watch is variadic and each value is tagged with its format, so a single
call can observe more than one format at once (passing no format watches
all supported ones):

	ch := clipboard.Watch(context.TODO())
	for data := range ch {
		switch data.Format {
		case clipboard.FmtText:
			println("text:", string(data.Bytes))
		case clipboard.FmtImage:
			println("image bytes:", len(data.Bytes))
		}
	}

Besides the built-in FmtText and FmtImage, Register maps a MIME type to a
custom Format token usable with Read, Write, and Watch. Custom formats are
raw passthrough: the exact bytes are exchanged under that MIME type with no
conversion. Use ReadAs to decode into a typed value. Custom formats are
supported on the desktop backends (macOS, Windows, Linux/X11, BSD/X11, and
Linux/Wayland for cross-application exchange); on iOS, Android, and
CGO-disabled builds they degrade gracefully like the rest of the API.

To discover what is currently on the clipboard, Formats reports the available
formats (registering any custom MIME types it finds on demand), and
Format.MIME reports a token's MIME identity. Enumeration works on the desktop
backends and returns an empty slice on iOS, Android, and CGO-disabled builds.

# Platform-specific caveats

On Linux/X11 the clipboard follows the X11 selection-ownership model:
the process that calls Write owns the selection and serves its content
to other applications on demand. This means the written data only stays
available for as long as the writing process is alive, unless a
clipboard manager is running to take over ownership when the process
exits. In practice plain text often survives because most clipboard
managers cache it, whereas larger image data is usually dropped. To keep
data available after your program exits, keep the process running (the
channel returned by Write reports when the data is no longer needed) or
rely on a clipboard manager.

Both X11 selections are reachable: the CLIPBOARD one by default, and PRIMARY
(middle-click paste) with FromPrimary. SECONDARY is not exposed, since
nothing uses it.

On Windows, a program running as a service does not share the logged-in
user's clipboard. Services run in Session 0 on their own non-interactive
window station, and a clipboard belongs to a window station: the service
gets a working clipboard that nothing else can see, so Read and Write
succeed among themselves while Watch never fires for anything the user
copies. Init still reports success, because from the process's own point of
view the clipboard is available. Run the clipboard work in a process inside
the interactive session instead.

Wayland sessions are supported natively: when WAYLAND_DISPLAY is set and
the compositor exposes a data-control manager (ext-data-control-v1 or
wlr-data-control-unstable-v1), Init selects the Wayland backend, which needs
no X server. Otherwise the package falls back to X11 — under a compositor
without data-control that means the XWayland bridge, as before. FromPrimary
needs version 2 of the data-control manager, which is where the primary
selection was added; under an older one a primary read returns nil.
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

var (
	// activate only for running tests.
	debug          = false
	errUnavailable = errors.New("clipboard unavailable")
	errUnsupported = errors.New("unsupported format")
	errNoCgo       = errors.New("clipboard: cannot use when CGO_ENABLED=0")
)

// Option configures a clipboard operation. Format and Item are Options too, so
// one variadic argument list carries both what an operation acts on and how:
//
//	clipboard.Watch(ctx, clipboard.FmtText, clipboard.FromPrimary())
//
// That is why Option is an interface rather than a function type — Watch and
// WriteAll had already spent their variadic slot on Format and Item, and Go
// allows only one.
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
//	sel := clipboard.Read(clipboard.FmtText, clipboard.FromPrimary())
//
// The primary selection exists only on X11 and Wayland. Elsewhere — Windows,
// macOS, iOS, Android, CGO-disabled builds — a read returns nil and a write is
// a no-op. A write is deliberately not redirected to the ordinary clipboard:
// that would destroy whatever the user had copied.
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
//	clipboard.Write(clipboard.FmtText, password, clipboard.Loops(1))
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

// All sorts of supported clipboard data
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

// Init initializes the clipboard package. It returns an error
// if the clipboard is not available to use. This may happen if the
// target system lacks required dependency, such as libx11-dev in X11
// environment. For example,
//
//	err := clipboard.Init()
//	if err != nil {
//		panic(err)
//	}
//
// If Init returns an error because of a runtime dependency failure
// (such as a missing libx11-dev), any subsequent Read/Write/Watch call
// may result in an unrecoverable panic. In a CGO-disabled build
// (CGO_ENABLED=0), Init returns an error and Read/Write/Watch degrade
// gracefully instead of panicking: Read and Write return nil, and
// Watch returns a closed channel.
func Init() error {
	initOnce.Do(func() {
		initError = initialize()
	})
	return initError
}

// Read returns a chunk of bytes of the clipboard data if it presents
// in the desired format t presents. Otherwise, it returns nil.
//
// Pass FromPrimary to read the primary selection instead of the clipboard.
//
// The bytes are encoded the way the format defines: UTF-8 for FmtText and
// PNG for FmtImage, whatever encoding the source application used. A custom
// format registered with Register is raw passthrough, so Read returns its
// bytes exactly as they sit on the clipboard.
func Read(t Format, opts ...Option) []byte {
	lock.Lock()
	defer lock.Unlock()

	buf, err := read(newConfig(opts).sel, t)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "read clipboard err: %v\n", err)
		}
		return nil
	}
	return buf
}

// Write writes a given buffer to the clipboard in a specified format.
//
// The data is on the clipboard as soon as Write returns; consuming the
// returned channel is optional. That channel receives a single empty
// struct, and is then closed, only when the clipboard is later overwritten
// by another writer (detected via the platform clipboard sequence number).
// If nothing else ever overwrites the clipboard, the channel never fires —
// so do not block on it expecting it to report that this write completed.
//
// If format t indicates an image, buf is normalized to PNG before being placed
// on the clipboard. PNG input is stored as-is; other formats are accepted if the
// program has registered the matching image decoder (e.g. blank-import
// _ "image/jpeg" or _ "golang.org/x/image/webp"), and undecodable input passes
// through unchanged. The clipboard therefore always serves PNG, regardless of
// the input encoding.
func Write(t Format, buf []byte, opts ...Option) <-chan struct{} {
	return WriteAll(append([]Option{Item{Format: t, Bytes: buf}}, opts...)...)
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
//	clipboard.WriteAll(
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
// The returned channel behaves as Write's does: it receives a single empty
// struct, and is then closed, only when the whole set is later replaced by
// another writer. WriteAll returns nil if it is given no items, or if the write
// fails.
//
// Multi-representation clipboards are a desktop feature. On iOS, Android and in
// CGO-disabled builds only the most preferred item is published.
//
// Pass FromPrimary to publish to the primary selection instead, or Loops to
// limit how many times the set is served.
func WriteAll(opts ...Option) <-chan struct{} {
	lock.Lock()
	defer lock.Unlock()

	c := newConfig(opts)
	items := normalizeItems(c.items)
	if len(items) == 0 {
		return nil
	}

	changed, err := writeAll(c.sel, items, c.loops)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "write to clipboard err: %v\n", err)
		}
		return nil
	}
	return changed
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
//	for _, path := range clipboard.ReadFiles() {
//		fmt.Println(path)
//	}
//
// A URI that does not name a local file — a remote one, or a path this platform
// cannot express — is skipped rather than guessed at, so the result can be
// shorter than what the clipboard holds.
func ReadFiles(opts ...Option) []string {
	buf := Read(FmtFiles, opts...)
	if buf == nil {
		return nil
	}
	return pathsFromURIList(buf)
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
func WriteFiles(paths []string, opts ...Option) <-chan struct{} {
	return Write(FmtFiles, uriListFromPaths(paths), opts...)
}

// Data is a single observed clipboard change: the format the change was
// detected in, together with the raw bytes encoded the same way Read
// returns them (UTF-8 for FmtText, PNG for FmtImage).
type Data struct {
	Format Format
	Bytes  []byte
}

// Watch returns a receive-only channel that receives the clipboard data
// whenever any change of clipboard data in one of the desired formats
// happens. Each received value carries the format it was detected in, so a
// single Watch call can observe multiple formats at once. If no format is
// given, all built-in formats (FmtText, FmtImage and FmtFiles) are observed.
//
// Pass FromPrimary to watch the primary selection instead of the clipboard.
//
// The returned channel will be closed once the given context is canceled.
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
				select {
				case out <- Data{Format: f, Bytes: b}:
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
