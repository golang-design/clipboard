# Design: drop the Cgo / C-toolchain dependency on mobile (iOS, Android)

| | |
|---|---|
| **Status** | Proposed |
| **Issue** | _none open_ — closes the mobile gap left by #69/#25; see [#8](https://github.com/golang-design/clipboard/issues/8) (Android support), [#70](https://github.com/golang-design/clipboard/issues/70) (Android fails to build) |
| **Builds on** | [#117](https://github.com/golang-design/clipboard/pull/117) / [`specs/darwin-remove-cgo.md`](./darwin-remove-cgo.md) — the purego/objc pasteboard binding this mirrors |
| **Related** | [#25](https://github.com/golang-design/clipboard/issues/25) (Linux/X11), [#69](https://github.com/golang-design/clipboard/issues/69) (darwin) — the desktop Cgo-removal track this completes |

## 1. Current state

The two mobile backends both bind native clipboard APIs through Cgo:

- **iOS** — `clipboard_ios.go` (`//go:build ios`) carries a Cgo preamble
  (`#cgo CFLAGS: -x objective-c`, `#cgo LDFLAGS: -framework Foundation -framework
  UIKit -framework MobileCoreServices`) and declares two C functions;
  `clipboard_ios.m` implements them against `UIPasteboard` (`generalPasteboard`,
  `string`, `setString:`). Only `FmtText` is supported; `FmtImage` returns
  `errUnsupported`. **It imports no `golang.org/x/mobile` package** — iOS
  `UIPasteboard` is process-global and needs no Activity/Context handle.
- **Android** — `clipboard_android.go` (`//go:build android`) declares two C
  functions; `clipboard_android.c` implements them with **JNI** against the
  Android `ClipboardManager`. The JNI calls need a live `JNIEnv` + app `Context`,
  obtained via `golang.org/x/mobile/app.RunOnJVM(func(vm, env, ctx uintptr) ...)`.

Consequences of the Cgo dependency (same shape as #69/#25 on the desktop):

- Building either backend **requires a C toolchain** and `CGO_ENABLED=1`.
- Under `CGO_ENABLED=0` a file with `import "C"` is dropped from the build, and
  `clipboard_nocgo.go` excludes `darwin` (so it never covers iOS). The result is
  the same silent-degradation bug #69 fixed on darwin — verified empirically:

  ```
  $ GOOS=ios     GOARCH=arm64 CGO_ENABLED=0 go build .
  ./clipboard.go:144:15: undefined: initialize   # …read/write/watch too
  $ GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build .
  ./clipboard.go:144:15: undefined: initialize   # same
  ```

  Neither a working backend *nor* the graceful `ErrCgoDisabled` stubs are linked.

Now that darwin (#117), Linux (#120), and BSD (#121) are Cgo-free, the two mobile
backends are the last ones that drag in a C compiler.

## 2. The decisive question, applied per platform

The single test that decides feasibility: **can the platform's clipboard backend
function without importing a package that itself requires Cgo?**

| | library compiles cgo-free? | reachable API | verdict |
|---|---|---|---|
| **iOS** | ✅ stdlib builds (`GOOS=ios CGO_ENABLED=0 go build std` ✓); backend imports no cgo package | `UIPasteboard` via `purego/objc` (process-global, no Context) | **actionable** |
| **Android** | ❌ backend needs `x/mobile/app.RunOnJVM` → `mobileinit.RunOnJVM` → `C.lockJNI` (cgo) | `ClipboardManager` via JNI, but only reachable through the cgo JVM bridge | **non-goal** |

iOS passes; Android fails on a hard dependency. The rest of the spec follows from
this split. There is no Android escape hatch within the gomobile model (§7).

## 3. Goal & non-goals

**Goal:** the `golang.design/x/clipboard` **library** builds for `GOOS=ios` with
**`CGO_ENABLED=0`** and **no C toolchain**, with behavior identical to today
(text read/write/watch; image remains unsupported on iOS). This also fixes the
silent-degradation bug (§1) for iOS.

**Non-goals — and be precise about the iOS one, because it is easy to overclaim:**

- **A gomobile *app* does not become Cgo-free.** This spec removes Cgo from the
  *clipboard library*. `golang.org/x/mobile/app`'s `darwin_ios.go` is itself
  `import "C"`, so any gomobile iOS app still links Cgo via x/mobile's own
  scaffolding regardless of this package. The win is at the **library** level:
  the package cross-compiles to iOS from a toolchain-less environment, stops
  degrading to undefined symbols / no-op stubs, and stops *forcing* Cgo on
  consumers that bind iOS clipboard access without the full gomobile app harness.
  The three-way framing:

  | platform | library forces Cgo? | typical consumer links Cgo? |
  |---|---|---|
  | darwin (#117) | no | no |
  | **iOS (this spec)** | **no** | yes, from x/mobile app scaffolding (not from us) |
  | **Android (this spec)** | **yes** (unavoidable) | yes |

- **Android stays Cgo.** See §7 — it is documented as a non-goal with its full
  dependency analysis, not silently skipped.
- No public API change; no new clipboard formats; `cmd/gclip-gui` (Fyne/OpenGL,
  Cgo regardless) is untouched.

## 4. iOS — the binding (mirror darwin)

A near-copy of the darwin purego/objc backend (`clipboard_darwin.go`), retargeted
from `NSPasteboard` to `UIPasteboard`. Because `UIPasteboard` is process-global,
this is *simpler* than darwin: text-only, no image, no TIFF fallback.

```go
//go:build ios

class_UIPasteboard := objc.GetClass("UIPasteboard")
class_NSString     := objc.GetClass("NSString")
sel_generalPasteboard := objc.RegisterName("generalPasteboard")
sel_string            := objc.RegisterName("string")
sel_setString         := objc.RegisterName("setString:")
// + NSString <-> bytes via initWithBytes:length:encoding: / dataUsingEncoding:
```

- **`read(FmtText)`** → `[[UIPasteboard generalPasteboard] string]` → an
  `NSString`; convert to bytes with `dataUsingEncoding:` (NSUTF8 = 4) and copy via
  the shared `nsdataBytes` helper. `read(FmtImage)` → `errUnsupported` (unchanged).
- **`write(FmtText)`** → build an `NSString` with
  `+[NSString stringWithUTF8String:]` (or `-initWithBytes:length:encoding:` to be
  NUL-safe) and `[pasteboard setString:]`. `write(FmtImage)` → `errUnsupported`.
- **`watch`** → keep the existing poll-`Read` diff loop (1 s cadence), unchanged.
  `UIPasteboard.changeCount` exists but is not needed to preserve current
  behavior; not adopting it keeps the objc surface minimal.

**Class reachability:** in a UIKit process `UIPasteboard`/`NSString` are already
loaded, so `objc.GetClass` returns them without a `Dlopen`. No exported-symbol
constants are needed (encodings are compile-time enum values), so unlike darwin
there is **no `purego.Dlsym`**. As with darwin there are **no struct return
values**, keeping the binding inside purego's proven `objc_msgSend` envelope on
`arm64`.

### Correctness (carry over the darwin lessons)

- **Autorelease pools.** `-string` and the `NSString`/`NSData` accessors return
  *autoreleased* objects; these goroutines run on arbitrary OS threads with no
  pool, so without draining they leak — including in the per-second `watch` loop.
  Wrap each operation in an autorelease pool (`defer newAutoreleasePool()()`).
- **Keep Go buffers alive.** `runtime.KeepAlive` around any `Send` that passes a
  Go slice's backing pointer (write path), exactly as darwin does.
- **Fixes a latent use-after-free.** Today's `clipboard_ios.m`
  `return (char *)[str UTF8String];` hands back a pointer into autoreleased
  storage with no copy — a use-after-free the moment the pool drains. The purego
  port copies bytes out via `nsdataBytes`, eliminating it. A read test asserting
  round-tripped bytes is the regression guard.

### Shared darwin/iOS code

`newAutoreleasePool`, `nsdataBytes`, and the `must`/`must2` helpers in
`clipboard_darwin.go` are platform-neutral within Apple — only the pasteboard
class and selectors differ. The build tag `darwin` **includes** `ios`, so factor
them into a `clipboard_apple.go` (`//go:build darwin`) shared file, leaving
`clipboard_darwin.go` (`darwin && !ios`) and `clipboard_ios.go` (`ios`) to hold
only their pasteboard-specific bindings. Decide at impl time; do not duplicate.

## 5. Files touched (iOS)

| File | Change |
|---|---|
| `clipboard_ios.go` | Replace Cgo preamble + bodies with a purego/objc `UIPasteboard` binding; keep `//go:build ios`. Builds under `CGO_ENABLED=0`. |
| `clipboard_ios.m` | **Delete.** |
| `clipboard_apple.go` *(new, optional)* | `//go:build darwin` shared `newAutoreleasePool`/`nsdataBytes`/`must` lifted from `clipboard_darwin.go`. |
| `clipboard_nocgo.go` | Add `&& !ios` (and keep `!android`) so iOS no longer falls through to stubs. Android **still** falls through — see §7. |
| `clipboard_test.go` | Any `TestClipboardNoCgo`/`TestClipboardInit` assumptions about iOS returning `ErrCgoDisabled` must change (iOS no longer does). |
| `README.md` | `iOS/Android: collaborate with gomobile` → split: iOS gains a "no Cgo (library)" note like darwin; Android keeps the gomobile/Cgo note with the §7 rationale. |
| `go.mod` | No change — `github.com/ebitengine/purego` is already a dependency (darwin). |

## 6. Testing & CI (iOS)

iOS cannot run `go test` on GitHub-hosted runners (no device/simulator). Mirror
the existing build-only jobs (`freebsd_build`, `openbsd_build`, `windows_386_build`):

- Add a **build-only** job: `GOOS=ios GOARCH=arm64 CGO_ENABLED=0 go build .`
  (the `std` half of this already passes; see §1/§2). This is the proof the
  toolchain is gone and the silent-degradation bug is fixed.
- Add a pure-Go unit test for the byte round-trip / use-after-free fix where it
  can run without a device — i.e. a small helper test, since the live pasteboard
  path itself is device-only.
- **Runtime verification is device-only**, via the `gclip-gui` demo on a real
  iOS device. Flag at impl time: confirm `UIPasteboard`/`NSString` are reachable
  through purego/objc on-device (cannot be exercised in CI).
- Land the binding and its test/CI adjustments in the **same PR** — no untested
  intermediate state.

## 7. Android — why it stays Cgo (non-goal, with the analysis)

Android cannot reach the goal, and the reason is a hard dependency chain, not an
implementation gap:

1. **The package's JNI bridge is Cgo by construction.** `clipboard_android.c`
   makes JNI calls (`GetMethodID`, `CallObjectMethod`, …), and it needs a live
   `JNIEnv` + app `Context`. Those come from
   `golang.org/x/mobile/app.RunOnJVM`, which delegates to
   `internal/mobileinit.RunOnJVM` — implemented with **`C.lockJNI` / `C.unlockJNI`
   / `C.checkException`** (`ctx_android.go`, `import "C"`). So the backend pulls
   Cgo transitively even before its own C file.
2. **The gomobile app model is Cgo by construction.** Android gomobile apps are
   built `-buildmode=c-shared` and loaded by a `NativeActivity` through the NDK —
   `CGO_ENABLED=0` is not a supported gomobile Android flow at all. The C
   toolchain (NDK) is required to *package* the app regardless of how the
   clipboard is bound. Empirically, `GOOS=android CGO_ENABLED=0 go build .`
   already fails on the x/mobile/app import.

**Considered and rejected — purego-over-JNI.** purego supports Android `dlopen`
(`dlfcn_android.go`), so in principle one could replace `clipboard_android.c` by
calling the `JNIEnv` function-table pointers directly from Go. Rejected: it would
**not** remove the C-toolchain requirement (x/mobile/app stays Cgo; the NDK
c-shared build stays). It is churn and added `unsafe` risk with **zero** movement
toward the goal. Revisit only if/when the JVM/Context lifecycle can be obtained
without a cgo x/mobile dependency — out of scope here. (Tracks the upstream
constraint noted around #70.)

**Net for Android:** no source change. It continues to require Cgo + the NDK, and
`clipboard_nocgo.go` keeps its `!android` exclusion so a `CGO_ENABLED=0` build of
the library on a non-mobile host still resolves to the graceful stubs.

## 8. Implementation phases

1. **iOS binding** — port `clipboard_ios.go` to purego/objc on `UIPasteboard`
   (text read/write/watch); delete `clipboard_ios.m`; factor the shared Apple
   helpers (§4); flip the `nocgo` tag (`!ios`); fix the affected test
   assumptions. Verify `GOOS=ios CGO_ENABLED=0 go build .` is green.
2. **CI** — add the `ios_build` build-only job (§6).
3. **Docs** — README iOS line (no-Cgo at library level) + Android rationale (§7);
   note iOS is now Cgo-free for the library in the package doc.

## 9. Risk & effort

- **Effort:** small. iOS is a strict subset of the already-shipped darwin binding
  (text-only, no image, no TIFF, no `Dlsym`); much of the code is shared verbatim.
- **Risk:**
  - *On-device reachability* of `UIPasteboard` via purego/objc — cannot be
    CI-verified; mitigated by the `gclip-gui` device check (§6).
  - *Autorelease leaks* in long-running `watch` — mitigated by reusing darwin's
    `newAutoreleasePool` (§4), and invisible to CI, hence flagged.
  - *Overclaiming the benefit* — mitigated by the explicit library-vs-app framing
    (§3); the spec deliberately does not promise a Cgo-free gomobile app.
- **Upside:** iOS joins darwin/Linux/Windows/BSD as a Cgo-free library backend;
  cross-compiling the library to iOS without a toolchain becomes possible; the
  iOS silent-degradation + use-after-free bugs are fixed. Android's status is now
  documented rather than ambiguous.
