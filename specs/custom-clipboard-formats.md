# Design: Custom clipboard formats

| | |
|---|---|
| **Status** | Proposed |
| **Issue** | [#17](https://github.com/golang-design/clipboard/issues/17) |
| **Supersedes** | POC in [#43](https://github.com/golang-design/clipboard/pull/43) |
| **Also closes** | [#40](https://github.com/golang-design/clipboard/issues/40) (raw read/write) |

## 1. Current state

**Platforms:** macOS (Cgo), Linux/X11 (Cgo), Windows (pure syscall), iOS + Android
(gomobile), and FreeBSD/OpenBSD/NetBSD (added in `aa2c0d4`).

**Formats:** only `FmtText` (UTF-8) and `FmtImage` (PNG). `FmtImage` performs
*conversion* under the hood — DIB↔PNG on Windows, TIFF→PNG on macOS.

**API:** `Init`, `Read(Format) []byte`, `Write(Format, []byte) <-chan struct{}`,
`Watch(ctx, Format) <-chan []byte`, with `type Format int` and consts
`FmtText = 0`, `FmtImage = 1`.

## 2. Why the #43 POC stalled (idea check)

The POC validated the *idea* but its shape isn't shippable:

1. Redefines `Format` from `int` → `interface{}`, breaking type safety and the
   `iota` consts.
2. A custom format is an `unsafe.Pointer(C.NSPasteboardTypePDF)` — this **forces
   user code to import Cgo + Cocoa**, which defeats the cross-platform purpose and
   collides directly with the purego migration in #83.
3. Only darwin is implemented; Linux / Windows / mobile are untouched.
4. `Handler` carries only `Format() interface{}`; the read/write transforms are an
   unfinished `// TODO: generics`.

The POC has served its purpose and should be closed in favour of this design.

## 3. Design principle

> A custom format is a **portable MIME-type string**. The package maps it to each
> platform's native type behind an opaque `Format` token. No platform/Cgo detail
> ever leaks into user code.

MIME is not an arbitrary choice — it is the *native data model* of the platforms we
still want to add (see §6): Wayland advertises `mime_type` strings on
`wl_data_offer`; the Web Clipboard API keys `ClipboardItem` by MIME type. X11
targets are already MIME-shaped (`image/png`), and macOS `UTType` bridges to MIME.
Choosing MIME as the identity makes the abstraction *forward-compatible* with those
backends instead of fighting them.

## 4. Public API

```go
// type Format int stays exactly as-is. FmtText = 0, FmtImage = 1 are unchanged.

// Register maps a MIME type to a Format token usable with Read/Write/Watch.
// Idempotent: Register("text/html") always returns the same token.
// Safe to call before Init and concurrently.
func Register(mime string) Format

// ReadAs decodes a custom format into a typed value. This relocates the
// generic idea from the original issue sketch to where generics actually
// compose — a free helper — instead of a heterogeneous registry that would
// have to box every func([]byte)(T,error) back to `any`.
func ReadAs[T any](f Format, decode func([]byte) (T, error)) (T, error)
```

That's the entire new surface: one function plus one generic helper. `Read`,
`Write`, `Watch` are unchanged and accept the new tokens directly.

### Why not the original `Register[T any](fmt, read, write)` sketch?

A package-level generic registry can't store `func([]byte)(T, error)` for varying
`T` without erasing it to `any` — so the generics buy nothing at the registry
level. And `Read(f) []byte` already returns bytes; a *typed* decode can't be looked
up by format at runtime, so it belongs in caller code. `ReadAs[T]` keeps the typed
ergonomics exactly where they're sound.

### Semantics

- **Raw byte passthrough.** Custom formats do **no** conversion (unlike `FmtImage`'s
  DIB/PNG/TIFF handling). Bytes in = bytes out. This is precisely what #40 asks
  for, so #40 is subsumed: `Register("application/octet-stream")` (or any native
  type) gives raw access, and it's also the escape hatch around the #48 Windows
  image-conversion bug.
- **Idempotent registration**, guarded by an `RWMutex`-protected registry.
- **nocgo build:** `Register` returns a token and never panics; `Read`/`Write`
  degrade exactly like the existing nocgo path (nil / closed channel).

## 5. Per-platform resolution (the load-bearing part)

A Go-level indirection — `resolve(Format) (nativeType, error)` — keeps all platform
specifics out of the public API and out of user code.

| Platform | Native type | MIME → native |
|---|---|---|
| Linux/X11 | target atom (`XInternAtom`) | MIME ≈ atom directly (`text/html`, `application/pdf`) |
| macOS | `NSPasteboardType` (NSString) | any string round-trips with itself; cross-app interop via best-effort MIME→UTI alias table (`text/html`→`public.html`, `application/pdf`→`com.adobe.pdf`) |
| Windows | `CF_*` or `RegisterClipboardFormat` | predefined `CF_*` for known MIME, else register the MIME string as a named format |

**Honest scope:** *self round-trip* (this library writing and reading its own data)
works with any string on every desktop platform. *Cross-application interop* is
best-effort and depends on the alias tables matching what the other app expects. We
should not overclaim portability here.

Because `resolve` is pure Go, it adds **no new Cgo surface** and stays compatible
with the purego migration (#83) — the exact opposite of the #43 POC.

## 6. Is the abstraction wide enough? (all open platform/feature issues)

| Issue | Bucket | How the design relates |
|---|---|---|
| #40 raw r/w | **Subsumed** | Raw passthrough *is* the custom-format path. |
| #6 Wayland | **Fits natively** | Wayland's data model is MIME strings → the registry maps 1:1. New backend, same API. |
| #64 web (wasm) | **Fits natively** | `ClipboardItem` is MIME-keyed → same. (Caveats below.) |
| #67 Linux 2nd selection | **Orthogonal axis** | PRIMARY vs CLIPBOARD is a *selection* axis, not a data-type axis. Must NOT be folded into `Format`. |
| #89 watch-all + type | **Orthogonal axis** | Needs *enumeration* (“what's on the clipboard?”), reserved as `Formats() []Format`. |
| #48 Windows img bug | **Escape hatch** | Raw passthrough lets users bypass the lossy DIB/PNG conversion. |
| #25/#69/#83 remove Cgo | **Unaffected** | `resolve` is pure Go; compatible with purego. |
| #22 throttle reads | **Unaffected** | X11 serving behavior; independent. |

### Axes the design deliberately keeps separate

To stay wide without becoming a combinatorial mess, `Format` is **only** the
data-type axis. Three independent axes are reserved for future work and must not be
overloaded onto `Format`:

1. **Selection** (#67): CLIPBOARD vs PRIMARY (X11), general vs find pasteboard
   (macOS). Future: a functional-option parameter on Read/Write/Watch, e.g.
   `clipboard.Read(f, clipboard.Primary())` — never a new `Format`.
2. **Enumeration** (#89): discover available formats. Future: `func Formats() []Format`
   plus a typed watch. The MIME registry is what makes this expressible.
3. **Capability / async** (#64 web, Wayland permissions): the web clipboard is async
   and permission-gated, and today's `Read() []byte` swallows errors as `nil`.
   `ReadAs[T]` (which returns `error`) is the seam where an error-returning /
   capability-aware path can grow without breaking the byte-based core.

## 7. Example

```go
clipboard.Init()

html := clipboard.Register("text/html")
clipboard.Write(html, []byte("<b>hi</b>"))
b := clipboard.Read(html)

// typed decode via the generic helper:
doc, err := clipboard.ReadAs(html, func(b []byte) (*Node, error) {
    return parseHTML(b)
})
```

## 8. Scope of the first PR

- `Register`, `ReadAs[T]`, the registry, and `resolve` on all desktop platforms.
- Round-trip tests per platform; doc + README update (incl. the BSD platforms
  currently missing from the README).
- Out of scope (reserved, §6.x): selection axis (#67), enumeration / watch-all
  (#89), Wayland/web backends (#6/#64) — each lands as its own PR on top of this
  foundation.
