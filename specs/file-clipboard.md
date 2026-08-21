# Design: file clipboard support

| | |
|---|---|
| **Status** | Implemented |
| **Issue** | [#152](https://github.com/golang-design/clipboard/issues/152) |
| **Also closes** | [#147](https://github.com/golang-design/clipboard/issues/147) (the `CF_HDROP` half; the image half shipped in #163) |
| **History** | [#37](https://github.com/golang-design/clipboard/issues/37), [#50](https://github.com/golang-design/clipboard/issues/50) (both closed as out of scope at the time) |
| **Builds on** | [#17](https://github.com/golang-design/clipboard/issues/17) (custom formats), [#151](https://github.com/golang-design/clipboard/issues/151) (`WriteAll`) |

## 1. Current state

Ctrl+C on files in a file manager is one of the most common things a clipboard
carries, and this package cannot see it. Only `FmtText`, `FmtImage`, and
registered raw-passthrough MIME types exist.

Checking whether the custom-format registry already reaches it, per backend:

| Backend | Reachable today? |
|---|---|
| X11 / Wayland | **Yes.** `Register("text/uri-list")` maps straight to the target atom / MIME type, and the payload really is a URI list, so raw passthrough is correct. |
| macOS | **Partial.** `Register("public.file-url")` reaches `dataForType:` verbatim, but the general pasteboard returns only the *first* item, so a multi-file copy is truncated — and `Formats()` skips the type, since `darwinFormatFor` only registers a name containing `/`. |
| Windows | **No.** `CF_HDROP` is a predefined format id (15), which `RegisterClipboardFormat` cannot return, and its payload is a `DROPFILES` struct rather than a URI list — so it deliberately does not belong in the `windowsNativeNames` alias table, whose rule (set in #160) is that only aliases whose payload is the MIME type's bytes verbatim may be listed.

That asymmetry is the whole argument. A user cannot write one piece of code that
copies files, because the three platforms disagree about what a file list *is*,
and one of them is unreachable through the registry by construction.

## 2. Design decision

> Make the file list a **built-in format** whose portable byte encoding is
> `text/uri-list`, and give it `[]string` accessors.

```go
// FmtFiles indicates a list of file paths.
const FmtFiles Format = ...

// ReadFiles returns the file paths currently on the clipboard.
func ReadFiles() []string

// WriteFiles publishes a list of file paths.
func WriteFiles(paths ...string) <-chan struct{}
```

`FmtFiles` is a normal built-in: it works with `Read`, `Write`, `Watch`,
`WriteAll`, and `Formats`, and `FmtFiles.MIME()` is `"text/uri-list"`.

### Why a built-in and not an alias table entry

The alias tables resolve a MIME type to a native *name*; they are explicitly not
allowed to transcode, because `Read` of a custom format promises the exact bytes
on the clipboard (#160 §"Only aliases whose payload is the MIME type's bytes
verbatim"). A file list needs transcoding on two of four backends — `DROPFILES`
is a struct, and macOS keeps one URL per pasteboard item. Putting that behind
`Register("text/uri-list")` would quietly break the passthrough contract for
every custom format. A built-in format is where transcoding already lives:
`FmtImage` transcodes PNG to `CF_DIBV5` and back.

### Why `text/uri-list` is the portable encoding

Every built-in already has one portable byte encoding — UTF-8 for `FmtText`, PNG
for `FmtImage` — so `Read(FmtFiles)` returns bytes in one encoding on every
platform, and `Watch(FmtFiles)` delivers the same. `text/uri-list` (RFC 2483) is
the obvious choice: it is already the native form on X11 and Wayland, it is a
published standard rather than an invention, and it survives paths that a
newline-separated list would not.

`ReadFiles`/`WriteFiles` are then thin: marshal to and parse from those bytes.
Callers who want the raw form can still use `Read(FmtFiles)`.

## 3. Per-platform representation

| Backend | Native form | Conversion |
|---|---|---|
| X11, BSD, Wayland | `text/uri-list` | none — it *is* the encoding |
| Windows | `CF_HDROP`: a `DROPFILES` header followed by UTF-16 paths, each NUL-terminated, the list terminated by a second NUL | `file://` URI ⇄ `C:\path` |
| macOS | `NSFilenamesPboardType`: a property-list array of path strings | `file://` URI ⇄ `/path` |
| iOS, Android, CGO-disabled | none | `ReadFiles` returns nil, `WriteFiles` is a no-op, as the rest of the API degrades |

### The macOS choice, measured rather than assumed

`NSPasteboardTypeFileURL` holds **one URL per pasteboard item**, and
`dataForType:` only ever sees the first — which is why the registry route
truncates a multi-file copy. Probing the real pasteboard settled which API to
use:

- `setPropertyList:forType:` with `NSFilenamesPboardType` writes the whole list
  in one call, and the system **synthesizes `public.file-url` items from it** —
  two items appeared for a two-path write — so modern applications see URLs
  without this package having to build them.
- The reverse holds: after a `writeObjects:` of `NSURL`s (what a modern app
  does), `propertyListForType:` with `NSFilenamesPboardType` returns the paths.
  So one call reads what either kind of application wrote.
- It **composes with `setData:forType:`**: writing text after the file list left
  both file items intact and added the text type. So `FmtFiles` works inside
  `WriteAll` alongside other formats, which a `writeObjects:`-based write could
  not guarantee.

## 4. Path and URI conversion

`file:` URIs are percent-decoded, and only the `file` scheme with an empty or
`localhost` authority is accepted — a remote URI is not a path this package can
hand back. On Windows `file:///C:/dir/name` maps to `C:\dir\name`; the leading
slash before the drive letter is dropped and separators are flipped. A path that
does not survive the round trip is dropped rather than guessed at.

## 5. Scope

In: the built-in format, the accessors, and the four desktop backends.

Out: drag-and-drop, the "cut" flag Explorer sets alongside `CF_HDROP`
(`Preferred DropEffect`) to distinguish move from copy, and the promise of
delayed file rendering.

## 6. Testing

The conversion layer is pure Go and is unit-tested directly on every platform:
round-trips through `text/uri-list` for paths with spaces, non-ASCII characters,
Windows drive letters, and the CRLF and comment handling RFC 2483 requires.

Each backend gets a round-trip test asserting `WriteFiles` then `ReadFiles`
returns the same paths, plus `Formats()` reporting `FmtFiles` — the enumeration
path, which is separate from the read path on every backend. Windows also gets a
`DROPFILES` layout test, since a wrong header offset produces a payload other
applications silently ignore rather than an error.

## 7. Outcome

Implemented as designed across the four desktop backends. The macOS probe held
up: one `setPropertyList:`/`propertyListForType:` pair reads and writes what
either a legacy or a modern application put on the pasteboard, and it composes
with `WriteAll`.

Two things landed alongside. `read` on the X11 and BSD backends now resolves its
target through `x11TargetFor` instead of repeating the built-in switch, so read
and write cannot disagree about which atom a format uses. `wlRead` and `wlWatch`
likewise go through `wlMIMEsFor`. Both were duplicated switches that would have
needed the same new case in three places.
