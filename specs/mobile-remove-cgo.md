# Design: Cgo on mobile (iOS, Android) — investigation and conclusion

| | |
|---|---|
| **Status** | Accepted — **negative result: mobile keeps Cgo** (iOS and Android) |
| **Issue** | [#125](https://github.com/golang-design/clipboard/issues/125) (iOS — closed not-feasible), [#126](https://github.com/golang-design/clipboard/issues/126) (Android — tracked blocker); see also [#8](https://github.com/golang-design/clipboard/issues/8), [#70](https://github.com/golang-design/clipboard/issues/70) |
| **Builds on** | [#117](https://github.com/golang-design/clipboard/pull/117) / [`specs/darwin-remove-cgo.md`](./darwin-remove-cgo.md) — the purego/objc binding iOS was expected to mirror |
| **Related** | [#25](https://github.com/golang-design/clipboard/issues/25) (Linux/X11), [#69](https://github.com/golang-design/clipboard/issues/69) (darwin) — the desktop Cgo-removal track that *did* succeed |

> **Correction (this revision).** The first version of this spec concluded that
> **iOS was actionable** (port `UIPasteboard` to purego/objc, mirroring darwin
> #117) while only Android stayed Cgo. An implementation attempt proved that
> **wrong**: iOS cannot drop the C-toolchain dependency either. §4 records the two
> independent blockers and §6 records why the original feasibility check was a
> false positive. The corrected conclusion: **neither mobile platform can drop
> Cgo**, for different reasons. No source change lands from this spec.

## 1. Current state

The two mobile backends both bind native clipboard APIs through Cgo:

- **iOS** — `clipboard_ios.go` (`//go:build ios`) carries a Cgo preamble
  (`#cgo CFLAGS: -x objective-c`, `#cgo LDFLAGS: -framework Foundation -framework
  UIKit -framework MobileCoreServices`) and declares two C functions;
  `clipboard_ios.m` implements them against `UIPasteboard` (`generalPasteboard`,
  `string`, `setString:`). Only `FmtText` is supported; `FmtImage` returns
  `errUnsupported`. It imports no `golang.org/x/mobile` package — iOS
  `UIPasteboard` is process-global and needs no Activity/Context handle.
- **Android** — `clipboard_android.go` (`//go:build android`) declares two C
  functions; `clipboard_android.c` implements them with **JNI** against the
  Android `ClipboardManager`. The JNI calls need a live `JNIEnv` + app `Context`,
  obtained via `golang.org/x/mobile/app.RunOnJVM(func(vm, env, ctx uintptr) ...)`.

After darwin (#117), Linux (#120), and BSD (#121) went Cgo-free, these two were
the remaining backends dragging in a C compiler. This spec set out to remove
them and concluded it cannot be done.

## 2. The bar a platform must clear

Dropping the C-toolchain dependency requires **two** things, not one:

1. **The library compiles Cgo-free** — every package it imports builds under
   `CGO_ENABLED=0` for that `GOOS`.
2. **An artifact can be produced Cgo-free** — the Go toolchain can link a binary
   (or the platform's app build can package one) for that `GOOS` without a C
   linker.

Desktop platforms clear both: darwin/linux/windows/bsd compile pure-Go backends
*and* internal-link `CGO_ENABLED=0` binaries. **Both mobile platforms fail —
iOS fails both bars, Android fails bar 1.** The details:

| | bar 1: compiles cgo-free? | bar 2: links/packages cgo-free? | verdict |
|---|---|---|---|
| **iOS** | ❌ purego forbids it (§4.1) | ❌ Go's iOS port forces external/C linking (§4.2) | **not feasible** |
| **Android** | ❌ needs `x/mobile/app` → `C.lockJNI` (§5) | ❌ gomobile builds `-buildmode=c-shared` via the NDK (§5) | **not feasible** |

## 3. Goal & conclusion

**Original goal:** build the library for `GOOS=ios` (and, aspirationally, the
Android backend) with `CGO_ENABLED=0` and no C toolchain.

**Conclusion: not achievable on either mobile platform.** Both stay Cgo. This is
now a documented non-goal with the supporting analysis below, so the question is
not re-investigated from scratch. `cmd/gclip-gui` (Fyne/OpenGL, Cgo regardless)
was never in scope and is untouched.

## 4. iOS — why it stays Cgo

iOS fails **both** bars in §2, each independently fatal.

### 4.1 purego requires Cgo on iOS (fails bar 1)

The intended binding reused `github.com/ebitengine/purego` + `purego/objc`, as
darwin does. But purego deliberately refuses to build for iOS under
`CGO_ENABLED=0`. `purego/is_ios.go` (effective constraint `ios && !cgo`) is a
compile-time tripwire:

```go
//go:build !cgo
package purego
// if you are getting this error it means that you have
// CGO_ENABLED=0 while trying to build for ios.
// purego does not support this mode yet.
// the fix is to set CGO_ENABLED=1 which will require a C compiler.
var _ = _PUREGO_REQUIRES_CGO_ON_IOS
```

So the moment the iOS backend imports `purego/objc`, the library no longer
compiles Cgo-free for `GOOS=ios`:

```
$ GOOS=ios GOARCH=arm64 CGO_ENABLED=0 go build .
# github.com/ebitengine/purego
.../purego@v0.10.1/is_ios.go:13:9: undefined: _PUREGO_REQUIRES_CGO_ON_IOS
```

Still true on the latest release (v0.10.1; only `v0.11.0-alpha*` exist beyond).
This is the same toolchain that made darwin succeed — it just draws an explicit
line at iOS. Hand-rolling an `objc_msgSend` bridge to avoid purego is the work
purego itself declined to do on iOS; out of scope and self-defeating.

### 4.2 Go's iOS port forces external (C) linking (fails bar 2)

Even setting purego aside, the Go toolchain cannot link an iOS artifact without
Cgo. A bare empty program proves it, independent of this package:

```
$ printf 'package main\nfunc main(){}\n' > m.go
$ GOOS=ios GOARCH=arm64 CGO_ENABLED=0 go build m.go
ios/arm64 requires external (cgo) linking, but cgo is not enabled
```

For contrast, **darwin** (non-iOS) internal-links the same program with
`CGO_ENABLED=0` — which is precisely why #117 worked and this does not. Any iOS
binary, and any gomobile iOS app, therefore needs a C toolchain to link,
regardless of how the clipboard is bound.

**Net for iOS:** the library can be neither compiled nor linked for iOS without
Cgo. The `clipboard_ios.go` / `clipboard_ios.m` Cgo backend stays as-is.

## 5. Android — why it stays Cgo

Android fails bar 1 on a hard dependency chain, and bar 2 on the build model:

1. **The JNI bridge is Cgo by construction (bar 1).** `clipboard_android.c` makes
   JNI calls and needs a live `JNIEnv` + app `Context`, obtained from
   `golang.org/x/mobile/app.RunOnJVM` → `internal/mobileinit.RunOnJVM`, which is
   implemented with **`C.lockJNI` / `C.unlockJNI` / `C.checkException`**
   (`ctx_android.go`, `import "C"`). The backend pulls Cgo transitively before
   its own C file. Empirically, `GOOS=android CGO_ENABLED=0 go build .` fails on
   the `x/mobile/app` import.
2. **The gomobile app model is Cgo by construction (bar 2).** Android gomobile
   apps build `-buildmode=c-shared` and load via a `NativeActivity` through the
   NDK; `CGO_ENABLED=0` is not a supported Android flow. The NDK is required to
   package the app regardless of clipboard binding.

**Considered and rejected — purego-over-JNI.** purego supports Android `dlopen`
(`dlfcn_android.go`), so one could call the `JNIEnv` function-table pointers from
Go and delete `clipboard_android.c`. Rejected: it removes neither the
`x/mobile/app` Cgo dependency nor the NDK requirement — churn and added `unsafe`
risk with zero movement toward the goal. Revisit only if the JVM/`Context`
lifecycle becomes obtainable without a Cgo `x/mobile` dependency.

## 6. Why the first conclusion was wrong (the false-positive check)

The original spec's iOS feasibility rested on this passing:

```
$ GOOS=ios GOARCH=arm64 CGO_ENABLED=0 go build std   # ✅ — but proves nothing
```

`go build std` **compiles individual standard-library packages but never links a
binary and never imports purego**, so it sidesteps *both* §4 blockers. It was a
false green. The check that would have caught it is building the actual
library-with-purego and linking a target:

```
$ GOOS=ios GOARCH=arm64 CGO_ENABLED=0 go build .            # purego tripwire (§4.1)
$ GOOS=ios GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/gclip  # external-linking (§4.2)
```

**Lesson for future platform specs:** a `go build std` (or any compile-only)
check is not evidence a platform can drop Cgo. Verify by (a) building the real
package with its FFI dependency under `CGO_ENABLED=0`, and (b) linking an actual
binary for that `GOOS`. Both bars in §2 must be checked explicitly.

## 7. Net result & files touched

**No source change.** Both mobile backends remain Cgo:

| File | Change |
|---|---|
| `clipboard_ios.go`, `clipboard_ios.m` | **Unchanged** — Cgo `UIPasteboard` backend stays (§4). |
| `clipboard_android.go`, `clipboard_android.c` | **Unchanged** — Cgo JNI backend stays (§5). |
| `clipboard_nocgo.go` | **Unchanged** — keeps its `!android` exclusion; `darwin` already covers `ios`, so iOS resolves to the Cgo backend, not the stubs. |
| `README.md` | The existing "iOS/Android: collaborate with gomobile" line is already accurate; no edit required. |

`clipboard_test.go`'s `degradesWithoutCgo` already classifies the mobile
platforms correctly (they degrade without Cgo), consistent with this conclusion.

## 8. Risk & effort

- **Effort:** none (no code). The value delivered is the *negative result* —
  iOS and Android are now known-and-documented non-goals, with reproducible
  evidence, so the investigation is not repeated.
- **Residual upside path:** revisit iOS if purego lifts its `ios && !cgo`
  restriction *and* Go's iOS port stops mandating external linking (neither is on
  the horizon); revisit Android if the JVM/`Context` lifecycle becomes reachable
  without a Cgo `x/mobile` dependency. Until then, mobile stays Cgo.
