# clipboard [![PkgGoDev](https://pkg.go.dev/badge/golang.design/x/clipboard)](https://pkg.go.dev/golang.design/x/clipboard) ![](https://changkun.de/urlstat?mode=github&repo=golang-design/clipboard) [![clipboard](https://github.com/golang-design/clipboard/actions/workflows/clipboard.yml/badge.svg?branch=main)](https://github.com/golang-design/clipboard/actions/workflows/clipboard.yml?query=branch%3Amain)

Copy and paste from Go, the same way on every platform.

```go
import "golang.design/x/clipboard"
```

- Text, images, files, and any MIME type you register
- macOS, Windows, Linux, FreeBSD, OpenBSD, NetBSD, iOS, Android and the browser
- No Cgo on the desktop: no C compiler to build, no `libX11` or `libwayland` to run

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	"golang.design/x/clipboard"
)

func main() {
	// Call Init once, before anything else. It fails if there is no
	// clipboard to talk to, such as on a Linux server with no display.
	if err := clipboard.Init(); err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	// Copy.
	if _, err := clipboard.Write(ctx, clipboard.FmtText, []byte("hello, world")); err != nil {
		log.Fatal(err)
	}

	// Paste.
	b, err := clipboard.Read(ctx, clipboard.FmtText)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(b)) // hello, world
}
```

## Platforms

| Platform | Talks to | Needs at runtime |
|---|---|---|
| macOS | the system pasteboard | nothing |
| Windows | the Win32 clipboard | nothing |
| Linux | Wayland or X11 | a Wayland compositor with data-control, or an X server |
| FreeBSD | Wayland or X11 | the same as Linux |
| OpenBSD, NetBSD | X11 (Wayland untested) | an X server |
| iOS, Android | the system clipboard, via [gomobile](https://golang.org/x/mobile) | nothing; text only |
| Browser (js/wasm) | `navigator.clipboard` | https; text only |

On Linux and the BSDs the package picks **Wayland** by itself when the
compositor offers the *data-control* protocol — GNOME 49 and later, KDE Plasma,
Sway, Hyprland and other wlroots compositors — and **X11** otherwise, which under
an older Wayland compositor means XWayland. Nothing needs configuring.

CI runs the full test suite on macOS, Windows, Linux (X11 and Wayland) and
FreeBSD (Wayland), and builds for OpenBSD. NetBSD is best-effort.

## What works where

"Desktop" means macOS, Windows, Linux and the BSDs.

| Feature | API | Where |
|---|---|---|
| Text | `FmtText` | everywhere |
| Images (PNG) | `FmtImage` | desktop |
| Files | `ReadFiles`, `WriteFiles`, `FmtFiles` | desktop |
| Your own MIME types | `Register` | desktop |
| Several formats in one copy | `WriteAll` | desktop |
| List what is on the clipboard | `Formats` | desktop |
| Watch for changes | `Watch` | everywhere except the browser |
| The middle-click clipboard | `FromPrimary` | X11 and Wayland |
| Forget after N pastes | `Loops` | X11 and Wayland |
| Spot passwords from a password manager | `Sensitive`, `Data.Sensitive` | desktop |

Where a feature is missing, the call returns `ErrUnsupported` or does the
nearest safe thing: `WriteAll` publishes only your first item, `Formats` returns
an empty list, and `Loops` is ignored.

## Usage

### Text and images

```go
clipboard.Write(ctx, clipboard.FmtText, []byte("some text")) // UTF-8
clipboard.Write(ctx, clipboard.FmtImage, pngBytes)           // PNG

text, err := clipboard.Read(ctx, clipboard.FmtText)
img, err := clipboard.Read(ctx, clipboard.FmtImage) // always PNG
```

Images always come out as PNG, because PNG keeps transparency and every
platform understands it. `Write` also takes other encodings once you import
their decoder, and converts them to PNG for you:

```go
import _ "image/jpeg" // or _ "golang.org/x/image/webp"

clipboard.Write(ctx, clipboard.FmtImage, jpegBytes) // stored as PNG
```

To put the bytes on the clipboard unchanged — a JPEG that stays a JPEG — use a
[custom format](#your-own-formats) instead.

### Errors

```go
b, err := clipboard.Read(ctx, clipboard.FmtText)
switch {
case errors.Is(err, clipboard.ErrNoData):      // nothing on the clipboard in this format
case errors.Is(err, clipboard.ErrUnavailable): // no clipboard to reach
case errors.Is(err, clipboard.ErrUnsupported): // this platform cannot do that
}
```

Every call takes a `context.Context`. It is checked before the call starts, and
on X11 a read also gives up at your deadline.

### Know when your copy is replaced

`Write` returns a channel that fires once, when something else replaces what
you wrote:

```go
changed, err := clipboard.Write(ctx, clipboard.FmtText, []byte("text data"))
if err != nil {
	log.Fatal(err)
}
<-changed
fmt.Println(`"text data" is no longer on the clipboard`)
```

You can ignore the channel. The data is on the clipboard as soon as `Write`
returns, and the channel never fires if nobody else writes, so do not wait on
it to learn that the write finished.

### Watch for changes

```go
for data := range clipboard.Watch(ctx, clipboard.FmtText) {
	fmt.Println(string(data.Bytes))
}
```

Pass several formats, or none to watch text, images and files together. Each
value says which format changed:

```go
for data := range clipboard.Watch(ctx) {
	switch data.Format {
	case clipboard.FmtText:
		fmt.Println("text:", string(data.Bytes))
	case clipboard.FmtImage:
		fmt.Println("image:", len(data.Bytes), "bytes")
	}
}
```

The channel closes when `ctx` is canceled. Wayland and Windows report changes
as they happen; the other platforms check once a second.

### Your own formats

Any MIME type — `text/html`, `image/jpeg`, `application/pdf` — can be used as a
format:

```go
html := clipboard.Register("text/html")

clipboard.Write(ctx, html, []byte("<b>hi</b>"))
b, err := clipboard.Read(ctx, html)
```

The bytes are moved exactly as given, with no conversion. `Register` returns
the same value every time for the same MIME type, and you can call it before
`Init`. To read and decode in one step, use `ReadAs`:

```go
doc, err := clipboard.ReadAs(ctx, html, parseHTML) // parseHTML: func([]byte) (*Node, error)
```

On macOS and Windows, common MIME types are translated to the names other apps
use there — `image/png` becomes `public.png` on macOS and `PNG` on Windows — so
browsers, Office and Preview understand them. Any other type is used as it is:
it round-trips through this package, but other apps may not recognize it.

### Several formats in one copy

"Copy as rich text" puts the same content on the clipboard in several formats,
and the app you paste into takes the best one it understands. Use `WriteAll`,
most preferred first:

```go
html := clipboard.Register("text/html")
clipboard.WriteAll(ctx,
	clipboard.Item{Format: html, Bytes: []byte("<b>hi</b>")},
	clipboard.Item{Format: clipboard.FmtText, Bytes: []byte("hi")},
)
```

Calling `Write` twice does not do this: every write replaces the whole
clipboard, so only the last format would be left.

### Files

Copy files the way a file manager does, so they paste into Finder, Explorer,
Nautilus or Dolphin:

```go
clipboard.WriteFiles(ctx, []string{"/home/me/report.pdf", "/home/me/notes.txt"})

paths, err := clipboard.ReadFiles(ctx)
```

Paths must be absolute. Only the paths are copied, not the files themselves,
so the files must still exist when someone pastes. For `Read`, `Write`, `Watch`
and `WriteAll`, the same list is available as the `FmtFiles` format, whose
bytes are a `text/uri-list`.

### What is on the clipboard?

```go
formats, err := clipboard.Formats(ctx)
for _, f := range formats {
	fmt.Println(f.MIME()) // text/plain;charset=utf-8, image/png, text/html, ...
}
```

Every format it returns can be passed straight to `Read`. MIME types you have
not registered yet are registered for you.

### The middle-click clipboard

X11 and Wayland have a second clipboard, the *primary selection*: whatever you
last selected with the mouse, pasted with the middle button. Add
`FromPrimary()` to any call to use it:

```go
sel, err := clipboard.Read(ctx, clipboard.FmtText, clipboard.FromPrimary())
clipboard.Write(ctx, clipboard.FmtText, []byte("hi"), clipboard.FromPrimary())
ch := clipboard.Watch(ctx, clipboard.FmtText, clipboard.FromPrimary())
```

On Wayland this needs a compositor offering `ext-data-control-v1`, or version
2 of the wlroots data-control protocol; current ones do. Other platforms have
no second clipboard. There these calls return `ErrUnsupported` rather than
touching the regular clipboard, which would wipe what the user had copied.

### Paste once, then gone

`Loops(n)` removes what you wrote after it has been pasted `n` times, which is
useful for passwords:

```go
clipboard.Write(ctx, clipboard.FmtText, password, clipboard.Loops(1))
```

**This works on X11 and Wayland only.** Everywhere else it is silently ignored,
so it will *not* clear a secret on Windows or macOS. The reason is how those
platforms work: on X11 and Wayland your program hands the data to each app
that pastes, so it can count; on Windows and macOS the system keeps the copy,
and your program never hears about pastes.

Every delivery counts, including a `Read` or `Watch` in your own program, and
an app that asks for two formats in one paste uses up two.

### Leave passwords alone

Password managers mark the passwords they copy, so that clipboard managers
and sync tools don't keep them. `Sensitive` tells you whether the current copy
carries that mark, and every value from `Watch` says it too:

```go
for data := range clipboard.Watch(ctx, clipboard.FmtText) {
	if data.Sensitive {
		continue // a password: don't store or upload it
	}
	save(data.Bytes)
}
```

It reads the marks password managers actually set: `org.nspasteboard.ConcealedType`
on macOS, `ExcludeClipboardContentFromMonitorProcessing` on Windows, and
`x-kde-passwordManagerHint` on X11 and Wayland. iOS, Android and the browser
have no such mark to read, so there `Sensitive` returns `ErrUnsupported`
rather than a false "safe".

## Command-line tool

`gclip` copies and pastes from the shell:

```bash
$ go install golang.design/x/clipboard/cmd/gclip@latest
```

```bash
$ gclip
gclip is a command that provides clipboard interaction.

usage: gclip [-copy|-paste] [-f <file>]

options:
  -copy
        copy data to clipboard
  -f string
        source or destination to a given file path
  -paste
        paste data from clipboard

examples:
gclip -paste                    paste from clipboard and prints the content
gclip -paste -f x.txt           paste from clipboard and save as text to x.txt
gclip -paste -f x.png           paste from clipboard and save as image to x.png

cat x.txt | gclip -copy         copy content from x.txt to clipboard
gclip -copy -f x.txt            copy content from x.txt to clipboard
gclip -copy -f x.png            copy x.png as image data to clipboard
```

`gclip -copy` keeps running until something else replaces what it copied, so
send it to the background when you need the shell back:

```bash
$ cat x.txt | gclip -copy &
```

See [cmd/gclip](./cmd/gclip/README.md) for more, and
[cmd/gclip-gui](./cmd/gclip-gui/README.md) for a demo app for iOS and Android.

## Platform notes

- **Linux and the BSDs: your program owns what it copies.** On X11 and Wayland
  the program that writes serves the data to every app that pastes. When it
  exits, the data goes with it, unless a clipboard manager has kept a copy
  (most keep text; few keep images). Keep your program running for as long as
  the data should be pasteable. The channel `Write` returns tells you when it
  is no longer needed.
- **Wayland needs data-control.** The native backend uses the data-control
  protocol, which works without a window or keyboard focus. Compositors
  without it, such as GNOME before 49, are reached through XWayland instead.
  Under data-control, a program cannot read back its own custom-format
  writes, though other apps can.
- **No display (servers, CI).** Start a virtual X server:

  ```bash
  apt install -y xvfb
  Xvfb :99 -screen 0 1024x768x24 > /dev/null 2>&1 &
  export DISPLAY=:99.0
  ```
- **Browser.** The page must be served over https (or from localhost while you
  develop); `Init` tells you if it is not. The browser allows a read only
  after a user action such as a click, and asks the user for permission. Call
  `Read` and `Write` from a goroutine that your event handler starts, not in
  the handler itself, or the handler never returns. Only text is supported.
- **Windows services.** A service runs in Session 0, which has a clipboard of
  its own that the logged-in user never sees: `Read` and `Write` work, but
  only within the service, and `Watch` never sees what the user copies.
  Windows offers no way around this. Do the clipboard work in a helper process
  inside the user's session instead, for example one started with
  `CreateProcessAsUser` using the token from
  `WTSQueryUserToken(WTSGetActiveConsoleSessionId())`, and talk to it over IPC.
- **File lists.** The "cut" flag Explorer uses to mean *move* is not exposed,
  so every file list reads as a copy.
- **Testing images.** To put a screenshot on the clipboard: `Ctrl+Shift+Cmd+4`
  on macOS, `Ctrl+Shift+PrintScreen` on Ubuntu, `Shift+Win+S` on Windows.

## Migrating to v0.9.0

v0.9.0 made every call that moves data take a `context.Context` and return an
`error`. You can now tell an empty clipboard from one you cannot reach, and put
a deadline on a read. Update calls like this:

| before | after |
|---|---|
| `b := clipboard.Read(f)` | `b, err := clipboard.Read(ctx, f)` |
| `ch := clipboard.Write(f, b)` | `ch, err := clipboard.Write(ctx, f, b)` |
| `ch := clipboard.WriteAll(items...)` | `ch, err := clipboard.WriteAll(ctx, items...)` |
| `fs := clipboard.Formats()` | `fs, err := clipboard.Formats(ctx)` |
| `p := clipboard.ReadFiles()` | `p, err := clipboard.ReadFiles(ctx)` |
| `clipboard.WriteFiles(paths)` | `clipboard.WriteFiles(ctx, paths)` |
| `clipboard.ReadAs(f, dec)` | `clipboard.ReadAs(ctx, f, dec)` |

`Watch` already took a context and is unchanged. Passing `context.TODO()` is a
fine first step, and if you ignored failures before, `_` keeps doing that.

## Who is using this package?

This package was built for [midgard](https://changkun.de/s/midgard), which syncs
the clipboard across machines and can share clipboard content through a public
link. For more projects, see the [wiki](https://github.com/golang-design/clipboard/wiki).

## License

MIT | &copy; 2021 The golang.design Initiative Authors, written by [Changkun Ou](https://changkun.de).
