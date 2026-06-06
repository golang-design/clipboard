# Design: drop the Cgo / C-toolchain dependency on darwin

| | |
|---|---|
| **Status** | Proposed |
| **Issue** | [#69](https://github.com/golang-design/clipboard/issues/69) |
| **Supersedes** | [#51](https://github.com/golang-design/clipboard/issues/51) (folded into #69) |
| **Builds on** | [#83](https://github.com/golang-design/clipboard/pull/83) (TotallyGamerJet — purego prototype) |
| **Related** | [#25](https://github.com/golang-design/clipboard/issues/25) (same goal for Linux/X11), [`specs/wayland-support.md`](./wayland-support.md) |

## 1. Current state

On macOS the package talks to **NSPasteboard** through Objective-C, compiled
with Cgo:

- `clipboard_darwin.go` (`//go:build darwin && !ios`) carries the Cgo preamble
  — `#cgo CFLAGS: -x objective-c`, `#cgo LDFLAGS: -framework Foundation
  -framework Cocoa` — and declares five C functions.
- `clipboard_darwin.m` implements them against `NSPasteboard` /
  `NSData`: `clipboard_read_string`, `clipboard_read_image` (with the TIFF→PNG
  fallback added in #95), `clipboard_write_string`, `clipboard_write_image`,
  `clipboard_change_count`.

Consequences of the Cgo dependency:

- Building the package on darwin **requires a C toolchain** (Xcode command-line
  tools / clang) and `CGO_ENABLED=1`. Cross-compiling *to* darwin from a
  Cgo-less environment is impossible.
- Under `CGO_ENABLED=0` the build falls through to `clipboard_nocgo.go`
  (`//go:build !windows && !cgo`), so on darwin the clipboard silently degrades
  to the no-op stubs (#94) — `Read`/`Write` return nil, `Watch` returns a closed
  channel — even though macOS itself needs no external runtime dependency.

Windows already has a **Cgo-free** backend (`clipboard_windows.go`, pure
`syscall`). Darwin is the remaining desktop platform that drags in a C compiler
purely for binding system APIs.

## 2. Goal & non-goals

**Goal:** the `golang.design/x/clipboard` library and the `cmd/gclip` CLI build
and run on macOS with **`CGO_ENABLED=0`** and **no C toolchain installed**, with
behavior identical to today (text + image read/write/watch, including the TIFF
read fallback).

**Non-goals:**

- `cmd/gclip-gui` stays Cgo-bound — it depends on Fyne/OpenGL, which needs Cgo
  regardless of how the clipboard is bound. This spec does not touch it.
- **iOS is unchanged.** `clipboard_ios.go` / `clipboard_ios.m` keep their Cgo
  path; only the `darwin && !ios` files change.
- No public API change. No new clipboard formats (that is the separate
  custom-format track, #17/#101).

## 3. Why this is feasible (and the dependency question)

macOS exposes the full Objective-C runtime (`/usr/lib/libobjc`) and the AppKit
framework as plain dynamic libraries. They can be reached at runtime via
`dlopen`/`dlsym` and `objc_msgSend` **without** linking through the C compiler —
which is exactly what [`github.com/ebitengine/purego`](https://github.com/ebitengine/purego)
(and its `purego/objc` helper) provides: a pure-Go FFI + Objective-C bridge with
no Cgo.

> **Decision:** Replace the Cgo/Objective-C binding with a pure-Go
> `purego`/`purego/objc` binding to `NSPasteboard`. Delete `clipboard_darwin.m`.

### Isn't this just trading one dependency for another?

No — and this is worth stating because the sibling Wayland spec deliberately
*rejected* taking on `libwayland`. The distinction is the **kind** of dependency:

- `libwayland` (and Cocoa today) are **C libraries** — pulling them in requires
  the C toolchain at build time, which is the precise thing #69 wants to remove.
- `purego` is **pure Go**. Adding it removes the toolchain requirement instead of
  adding one. It *advances* the no-Cgo goal rather than working against it.

So the consistent rule across both specs is "no C toolchain dependency," not "no
new Go module." purego satisfies it; `libwayland`/Cocoa do not.

### Feasibility note: no struct returns

Every call this backend makes returns a scalar, a pointer (`id`), a `BOOL`, or an
`NSInteger` — there are **no struct return values** (no `NSRect`, no `NSSize`).
That means the awkward `objc_msgSend_stret` amd64 ABI case never arises, which is
the main thing that makes a hand-rolled `objc_msgSend` binding fragile. This
keeps the purego surface small and well within its proven envelope on both
`amd64` and `arm64`.

## 4. Alternatives considered

- **Shell out to `pbcopy`/`pbpaste`.** Rejected: an external-process dependency
  (parallel to the `wl-clipboard` proposal rejected in #6), it complicates image
  fidelity, and it is slower and racier than in-process pasteboard access.
- **Hand-rolled `objc_msgSend` via `syscall`/assembly.** This is essentially what
  purego does, but unmaintained and per-arch. No reason to re-derive it.
- **Keep Cgo.** The status quo; fails the goal.

purego is the only option that is in-process, image-faithful, cross-arch, and
free of a C toolchain.

## 5. The binding (read/write/watch parity)

A straight port of the five operations to `purego`/`purego/objc`. Sketch
(following the #83 prototype):

```go
appkit := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", ...)
// NSPasteboardType* are exported NSString * symbols — Dlsym + deref:
NSPasteboardTypeString := dlsymDeref(appkit, "NSPasteboardTypeString")
NSPasteboardTypePNG    := dlsymDeref(appkit, "NSPasteboardTypePNG")
NSPasteboardTypeTIFF   := dlsymDeref(appkit, "NSPasteboardTypeTIFF")

classNSPasteboard := objc.GetClass("NSPasteboard")
classNSData       := objc.GetClass("NSData")
// selectors: generalPasteboard, dataForType:, length, getBytes:length:,
//            clearContents, setData:forType:, dataWithBytes:length:, changeCount
```

- **`read(FmtText)`** → `[NSPasteboard generalPasteboard] dataForType:String`,
  then `length` + `getBytes:length:` into a Go slice.
- **`read(FmtImage)`** → same with PNG; **TIFF fallback** when PNG absent (§6).
- **`write(FmtText/FmtImage)`** → `+[NSData dataWithBytes:length:]`,
  `clearContents`, `setData:forType:`; map `BOOL` → the existing
  `errUnavailable` on failure. Returns the same change-count channel as today.
- **`changeCount()`** → `[[NSPasteboard generalPasteboard] changeCount]`,
  driving both the `write` notification goroutine and the `watch` poller exactly
  as the Cgo version does (the 1-second polling cadence is unchanged).

The global `lock` (clipboard.go) still serializes access; no concurrency change.

## 6. TIFF read fallback — and a transcode-location fork

**This is the item the #83 prototype predates and must not regress.** #95 made
`read(FmtImage)` fall back to the pasteboard's TIFF representation (the default
for screenshots and many apps' "Copy Image") and transcode it to PNG, so callers
always get PNG. `clipboard_tiff_darwin_test.go` enforces it. The purego port must
reproduce this. There are two ways, and the spec calls it out as a real fork:

**Option A — port the ObjC transcode (mirror today's `.m`).** Through objc:
`+[NSBitmapImageRep imageRepWithData:tiff]` then
`-[rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}]`.
- *Pro:* byte-for-byte the current behavior; `NSBitmapImageRep` decodes anything
  the OS can put on the pasteboard.
- *Con:* widens the objc surface (extra class, two selectors, the
  `NSBitmapImageFileTypePNG` enum value, an empty `NSDictionary`); the transcode
  is only testable with a real pasteboard.

**Option B — fetch raw TIFF bytes, transcode in pure Go.** ObjC returns the raw
`NSPasteboardTypeTIFF` data; decode with `golang.org/x/image/tiff` (already an
indirect dep via `x/image`) and re-encode with `image/png`.
- *Pro:* smaller objc surface; the transcode becomes a pure-Go, unit-testable
  function with no clipboard needed.
- *Con:* `x/image/tiff` supports uncompressed/LZW/Deflate/PackBits but may choke
  on exotic OS-emitted TIFF variants that `NSBitmapImageRep` would accept.

**Discriminating constraint:** robustness to arbitrary OS-produced TIFF (favors
A) vs. smaller native surface + testability (favors B). The existing test feeds
`sips`-generated TIFF, which `x/image/tiff` handles, so **Option B passes CI**;
the only residual risk is real-world screenshot variants. **Recommendation:
Option B**, with a clear fallback to A if a real-world TIFF variant is found to
fail. Decide before implementation; do not silently default.

## 7. Memory & runtime correctness (must-handle)

These won't surface as CI failures, so they are called out explicitly:

- **Autorelease pools.** `dataForType:` and `+[NSData dataWithBytes:length:]`
  return *autoreleased* objects. Go goroutines run on arbitrary OS threads with
  no `NSAutoreleasePool` and no run loop, so these objects leak ("autoreleased
  with no pool in place — just leaking"), and `Watch` calls `Read` every second
  *forever*. **Each operation must wrap its pasteboard work in an autorelease
  pool** (drain on return). Confirm the exact `purego/objc` helper at
  implementation time; if none exists, create/drain `NSAutoreleasePool` directly
  via objc.
- **Keep Go buffers alive across calls.** When passing a Go slice's backing
  pointer into `getBytes:length:` (read) and `dataWithBytes:length:` (write),
  guard against the GC moving/collecting the buffer mid-call (`runtime.KeepAlive`
  around the `Send`). Reads copy *into* a Go-allocated slice and writes copy
  *out*, so no ObjC object retains Go memory past the call — the only window is
  the call itself.
- **Threading.** Pasteboard read/write off the main thread is fine for this
  use (no AppKit UI run loop involved); the existing global `lock` keeps one
  pasteboard operation in flight at a time.

## 8. Files touched

| File | Change |
|---|---|
| `clipboard_darwin.go` | Replace Cgo preamble + bodies with purego/objc binding; keep `//go:build darwin && !ios`. Now builds under `CGO_ENABLED=0`. |
| `clipboard_darwin.m` | **Delete.** |
| `clipboard_nocgo.go` | Build tag `!windows && !cgo` → `!darwin && !windows && !cgo` (darwin no longer degrades). Leave the `readc` stub alone — it belongs to the custom-format track (#101), out of scope here. |
| `clipboard_test.go` | `TestClipboardInit` / `TestClipboardNoCgo` skip darwin (it no longer returns `ErrCgoDisabled`). |
| `clipboard_tiff_darwin_test.go` | **Remove** the `CGO_ENABLED=0` skip so the TIFF path runs in *both* cgo modes. If Option B (§6): add a pure-Go transcode unit test that needs no clipboard. |
| `go.mod` / `go.sum` | Add `github.com/ebitengine/purego` (latest stable; confirm `objc` API at impl time). |
| `README.md` | `macOS: require Cgo, no dependency` → `macOS: no Cgo, no build dependency` (gains a pure-Go module dep, loses the C-toolchain dep). |

## 9. Testing & CI

CI already does the heavy lifting — `.github/workflows/clipboard.yml` runs, on
`macos-latest`, `go test` under **both** `CGO_ENABLED=1` and `CGO_ENABLED=0`. So:

- With no source change to CI, the purego backend gets exercised on a real Mac
  in both modes; the `CGO_ENABLED=0` run is the one that proves the toolchain is
  gone.
- The `gclip` build step must keep passing under `CGO_ENABLED=0`; the `gclip-gui`
  build remains Cgo (unchanged, out of scope).
- The TIFF test (§6/§8) must pass in both modes after dropping its skip.
- Land the binding and its test adjustments in the **same PR** — no untested
  intermediate state.

## 10. Implementation phases

1. **Port the binding** (text + image PNG + write + changeCount) via
   purego/objc; delete `.m`; flip the `nocgo` build tag and the two cgo-related
   test skips. Green on `CGO_ENABLED=1`.
2. **TIFF fallback** per §6 (recommended Option B + unit test); drop the TIFF
   test's `CGO_ENABLED=0` skip. Green on **both** cgo modes.
3. **Autorelease + KeepAlive** hardening (§7).
4. **Docs:** README dependency line; note in the package doc that macOS is now
   Cgo-free.

## 11. Risk & effort

- **Effort:** small — the prototype in #83 already demonstrates the core binding
  (~100 lines). The genuinely new work is the TIFF fallback port and the
  autorelease/KeepAlive correctness, not the happy path.
- **Risk:**
  - *TIFF variant coverage* under Option B — mitigated by the A fallback.
  - *Autorelease leaks* in long-running `Watch` — mitigated by §7 (and invisible
    to CI, hence flagged as must-handle).
  - *purego API drift* — pin a known-good release and verify the `objc` surface
    at impl time rather than trusting #83's `v0.10.0`.
- **Upside:** darwin joins Windows as a Cgo-free desktop backend; cross-compiling
  to macOS from a toolchain-less environment becomes possible; momentum for the
  parallel Linux goal (#25).
