# Design: drop the Cgo / C-toolchain dependency on Linux (X11)

| | |
|---|---|
| **Status** | Proposed |
| **Issue** | [#25](https://github.com/golang-design/clipboard/issues/25) |
| **Related** | [#69](https://github.com/golang-design/clipboard/issues/69)/[#117](https://github.com/golang-design/clipboard/pull/117) (darwin, done), [`specs/wayland-support.md`](./wayland-support.md) (the pure-Go precedent), [#55](https://github.com/golang-design/clipboard/issues/55) (BSD) |
| **Builds on** | [#20](https://github.com/golang-design/clipboard/pull/20) (runtime `dlopen` of libX11) |

## 1. Current state

On Linux the package talks to **X11** through Cgo:

- `clipboard_linux.go` carries the Cgo preamble (`#cgo LDFLAGS: -ldl`) and
  declares the C entry points.
- `clipboard_linux.c` `dlopen`s `libX11.so`/`libX11.so.6` (the dynamic-load
  pattern from #20) and drives the **X11 CLIPBOARD selection** protocol via
  Xlib: `XOpenDisplay`, `XCreateSimpleWindow`, `XInternAtom`,
  `XSetSelectionOwner`/`XGetSelectionOwner`, `XConvertSelection`,
  `XChangeProperty`/`XGetWindowProperty`/`XDeleteProperty`, `XSendEvent`,
  `XNextEvent`, plus `XSetErrorHandler`/`XSync` for the #61 crash guard.

Consequences:

- Building on Linux **requires a C toolchain** and the **libX11 headers**
  (`libx11-dev`) at compile time, and `CGO_ENABLED=1`. The compiled binary
  still needs **`libX11.so` present at runtime** (dlopen).
- Under `CGO_ENABLED=0` the cgo file is dropped by the toolchain, so Linux falls
  through to the no-op `clipboard_nocgo.go` stubs (#94) — Read/Write return nil,
  Watch returns a closed channel.

A subtle consequence of the second point: the package **already has a pure-Go
Wayland backend** (`clipboard_wayland_linux.go`, from #6 — hand-rolled wire
protocol, no `libwayland`), but because the *top-level* `initialize/read/write/
watch` live in the cgo `clipboard_linux.go`, you **cannot reach the Wayland
backend in a `CGO_ENABLED=0` build today**. Removing X11's Cgo dependency also
unlocks the existing Wayland backend for cgo-free builds.

This is the Linux counterpart to the darwin work (#69/#117). Windows is already
Cgo-free (pure `syscall`); after this, Linux joins it.

## 2. Goal & non-goals

**Goal:** the library and `cmd/gclip` build and run on Linux with
**`CGO_ENABLED=0`** and **no C toolchain / no libX11 headers**, with behavior
identical to today across both the X11 and Wayland backends.

**Non-goals:**

- `cmd/gclip-gui` (Fyne/OpenGL) stays Cgo-bound — unchanged.
- **Android** keeps its existing path (`clipboard_android.go`, gomobile) —
  the `linux && !android` constraint is preserved.
- **BSD is out of scope** for this PR. The BSDs use the same X11 and could later
  share a pure-Go backend (see §8), but #25 is Linux-scoped; keeping BSD on its
  current cgo path bounds risk.
- No public API change; no new clipboard formats.

## 3. The decision — and a real fork to settle first

Two mechanisms can drop the build-time C toolchain. They differ in what they
leave behind, so the maintainer should pick before implementation.

### Option A (recommended): hand-rolled pure-Go X11 wire protocol

Speak the **X11 wire protocol directly over the display socket** in pure Go —
`net` + `encoding/binary` + `os` (to read `~/.Xauthority`). No Cgo, no
`libX11.so`, no third-party packages.

- **Matches the explicit #25 preference.** The issue states: *"the preference is
  to write the bindings ourselves without introducing third-party dependencies
  outside `golang.org`"* — which rules out `xgb`/XCB and, by the same logic,
  favors a self-contained implementation here.
- **Mirrors the in-repo precedent.** The Wayland backend already hand-rolls its
  wire protocol over the socket; this is the same shape for X11 and would make
  both Linux backends consistent and dependency-free.
- **Removes the runtime dependency too.** Unlike every approach that binds
  `libX11`, this needs **nothing on the user's system** — no `libx11-dev` to
  build, no `libX11.so` to run. Linux becomes as self-contained as Windows.
- **Cost:** the most code. The connection handshake + authentication (§5) is the
  genuinely new, fiddly part; the selection protocol itself is a near 1:1
  translation of the existing C.

### Option B: `purego` + `dlopen(libX11.so)`

Reuse the approach we just landed for darwin (#117): `dlopen` `libX11.so.6` via
`github.com/ebitengine/purego` (already a module dependency) and call the same
Xlib functions the C code calls today.

- **Far less code** — a near-mechanical port of `clipboard_linux.c`'s ~14 calls
  to purego, consistent with the darwin backend.
- **Removes the C toolchain** (the stated goal of #25) and the libX11 *headers*.
- **But keeps the `libX11.so` runtime dependency** (same as today's dlopen), and
  reintroduces Xlib's **process-global error handler** concern (#61) — solvable
  by installing a Go callback via `purego.NewCallback`, but it is back on the
  table.
- **Tension with #25:** purego is a third-party dependency outside `golang.org`.
  It is already vendored for darwin, so this is "consistent with darwin" rather
  than "new dependency," but it is still counter to the issue's stated "write it
  ourselves" preference.

### Recommendation

**Option A.** It honors the explicit maintainer preference in #25, matches the
Wayland precedent, and is the only option that makes Linux truly
dependency-free (no toolchain *and* no `libX11.so`). Option B is the pragmatic
lower-effort path if the maintainer prefers consistency with darwin and is
content to keep the `libX11.so` runtime requirement. **The rest of this spec
assumes Option A**; the build-tag/test/CI sections apply to either.

We also reject, as in the Wayland design: **shelling out to `xclip`/`xsel`**
(external runtime dependency) and **`xgb`/XCB** (third-party dep, #25).

## 4. Protocol subset (Option A)

The ~14 Xlib calls collapse to a small set of X11 requests, hand-coded like the
C hand-codes Xlib (no full protocol scanner):

- **Setup:** connection handshake + auth, parse the setup reply.
- `InternAtom` — resolve `CLIPBOARD`, `UTF8_STRING`, `image/png`, `TARGETS`,
  `GOLANG_DESIGN_DATA`, and `ATOM`.
- `CreateWindow` — a 1×1 unmapped window to own/request the selection (resource
  id from the allocator).
- `SetSelectionOwner` / `GetSelectionOwner` — take and confirm ownership.
- `ConvertSelection` — request the selection into our property (read path).
- `ChangeProperty` / `GetProperty` / `DeleteProperty` — move the bytes.
- `SendEvent` — deliver the `SelectionNotify` reply to a requestor (write path).
- **Event loop** — read events and dispatch `SelectionRequest`,
  `SelectionNotify`, `SelectionClear`.

This is exactly the set `clipboard_linux.c` uses; the logic (TARGETS reply,
owner-serves-until-`SelectionClear`, single-shot property transfer) ports
directly.

## 5. Wire mechanics (the genuinely new part)

The selection protocol is a straight port; the **connection + auth** has no
analogue in the Wayland backend and is where the real work is:

- **Display parsing:** `$DISPLAY` = `[host]:display[.screen]`. The common case
  (`:0`) is a **unix socket** at `/tmp/.X11-unix/X<display>` (also accept the
  abstract socket `@/tmp/.X11-unix/X<display>`); `host:` forms use TCP
  `host:6000+display`.
- **Authentication:** read `~/.Xauthority` (path overridable by
  `$XAUTHORITY`), parse its binary records, and select the entry matching the
  connection family + display number. Send `MIT-MAGIC-COOKIE-1` + the 16-byte
  cookie in the connection setup request. (No cookie / no `.Xauthority` over a
  local unix socket is common and often still authorized; handle "no auth"
  gracefully.)
- **Setup reply:** parse `resource-id-base`, `resource-id-mask` (drives the id
  allocator), `root` window of screen 0, and `maximum-request-length`.
- **Framing:** requests are 4-byte-aligned `[opcode][data][length]`; replies and
  events are 32 bytes (replies may carry a trailing variable part). Byte order
  is declared in the setup (use little-endian on supported targets).
- **Error handling = #61 dissolved.** We own the event/reply read loop. An X11
  **Error** is just a 32-byte reply whose first byte is `0`; we decode and
  **ignore** it. There is no Xlib default handler and therefore **no `exit(1)`
  path** — the #61 crash class becomes *structurally impossible*, with no
  `XSetErrorHandler` to install. (See §7 for the test consequence.)

`golang.org/x/sys` is **not** required: X11 needs no `SCM_RIGHTS` fd-passing
(unlike Wayland), so plain `net`/`os`/`encoding/binary` suffice.

## 6. Mapping to the existing API

The public API is unchanged; the selector mirrors today's `initialize` (Wayland
first, then X11):

```
initialize():
  if wlAvailable() { useWayland = true; return nil }   // unchanged
  else open the X11 display (pure Go); cache failure (fail-fast, cf. #85)
```

- **`Write`** — own `CLIPBOARD`, then run the selection-serving event loop in a
  goroutine until `SelectionClear`, answering `TARGETS` and the data target via
  `ChangeProperty`+`SendEvent`. Close the returned channel on `SelectionClear`
  (the documented "owner serves until overwritten" contract). This replaces the
  `cgo.NewHandle`/`syncStatus` bridge with a plain goroutine — simpler than the
  C path.
- **`Read`** — `ConvertSelection` → wait for `SelectionNotify` → `GetProperty`
  → `DeleteProperty`. Same `UTF8_STRING` / `image/png` targets.
- **`Watch`** — unchanged: the existing 1-second polling loop over `Read` (X11
  has no selection-change event; only Wayland's backend is event-driven).
- **MIME/atoms** — identical to today: `FmtText`→`UTF8_STRING`,
  `FmtImage`→`image/png`.

## 7. Files & build tags

| File | Change |
|---|---|
| `clipboard_x11_linux.go` | **New (pure Go, `//go:build linux && !android`).** Holds the top-level `initialize/read/write/watch` selector (Wayland-or-X11) plus the X11 backend. |
| `clipboard_linux.go` | **Delete** (its Cgo binding; the non-cgo selector/watch logic moves to the new file). |
| `clipboard_linux.c` | **Delete.** |
| `clipboard_nocgo.go` | Build tag `!darwin && !windows && !cgo` → `!darwin && !windows && !linux && !cgo`, so Linux no longer degrades to stubs. (BSD/other still degrade under cgo=0.) |
| `clipboard_errorhandler_linux_test.go` | **Remove** (it is `//go:build … && cgo` and tests `triggerProtocolError`, which no longer exists). Optionally replace with a pure-Go test that feeds a synthetic X11 Error into the read loop and asserts it is ignored (§9). |
| `clipboard_test.go` | Un-skip the read/write/watch round-trip tests under `CGO_ENABLED=0` for Linux (same stale-guard fix as darwin #117 — see §9). |
| `README.md` | Linux dependency line: drop "require X11 dev package"; note no Cgo, no build dependency, and (Option A) no `libX11.so` runtime dependency. |

Net effect: Linux compiles the **same pure-Go files** under `CGO_ENABLED=1` and
`=0`, and the Wayland backend becomes reachable in cgo-free builds.

## 8. Bonus & follow-ups

- **Wayland under cgo=0** comes for free (see §1) — call it out and test it.
- **BSD (#55).** The pure-Go X11 backend is OS-agnostic; a later change can widen
  the build tag to `(linux || freebsd || openbsd || netbsd) && !android` and
  retire `clipboard_bsd.c` too. Deliberately deferred to keep this PR scoped.
- **PRIMARY selection (#67/#22)** and **INCR** (below) remain future work.

## 9. Testing & CI

CI already runs Linux under **both** `CGO_ENABLED=1` and `=0` with `Xvfb` and
`xclip` installed (`.github/workflows/clipboard.yml`), so:

- **The CGO=0 job must actually exercise the backend.** Today the round-trip
  tests carry a `CGO_ENABLED=0 → t.Skip` guard (they degraded to nocgo). With
  Linux now working cgo-free, that guard makes the CGO=0 job hollow — exactly the
  trap caught in darwin #117 (coverage 45%→87%). Exempt Linux from the skip so
  both cgo modes run the real tests. **This is the load-bearing CI change.**
- Keep the **TARGETS regression** (#60/#99, `clipboard_targets_linux_test.go`)
  and the existing read/write tests green against the new backend.
- Add **wire-level unit tests** that need no X server: `.Xauthority` record
  parsing, `$DISPLAY` parsing (unix/abstract/TCP), request/reply framing, atom
  and property encoding.
- Replace the removed #61 test with a pure-Go check that an injected `Error`
  reply in the read loop is dropped without aborting.
- Mirror `hack/test-linux.sh` for local reproducibility.

Each backend phase lands with its test in the same PR (the project's standing
rule).

## 10. Implementation phases (Option A)

1. **Connection core.** `$DISPLAY` parse, socket connect, setup handshake +
   `.Xauthority`/`MIT-MAGIC-COOKIE-1`, parse setup reply, id allocator. Prove
   connect against `Xvfb` in CI. (Unit-test the parsers offline.)
2. **Atoms + window.** `InternAtom`, `CreateWindow`.
3. **Read path.** `ConvertSelection` → `SelectionNotify` → `GetProperty`.
4. **Write path.** `SetSelectionOwner` + event loop serving `TARGETS`/data;
   `SelectionClear` → close channel.
5. **Dispatch + cleanup.** Move the selector/watch into the new file; delete
   `clipboard_linux.c`/`clipboard_linux.go`; flip the `nocgo` tag; remove the
   cgo #61 test.
6. **Test flips + docs.** Un-skip CGO=0 round-trip tests; README; package doc.

## 11. Risk & effort

- **Effort:** the largest of the Cgo-removal efforts. The selection protocol is a
  direct port; **connection setup + `.Xauthority` auth is the real work** and the
  main risk. Bounded and well-documented (the X11 core protocol is stable and
  frozen).
- **Risks:**
  - *Auth/transport coverage* — cookie matching, abstract vs. filesystem unix
    sockets, the no-`.Xauthority` local case. Mitigated by offline parser tests +
    the `Xvfb` integration run.
  - *INCR / large transfers* — the current C code does **single-shot**
    `ChangeProperty`/`GetProperty` and does **not** implement INCR; very large
    images can exceed `maximum-request-length`. Option A **matches current
    behavior** (no regression) and documents INCR as a known limitation /
    follow-up rather than silently changing semantics.
  - *Hollow CI* — addressed head-on by §9.
- **Upside:** Linux joins Windows (and now macOS) as a Cgo-free backend;
  cross-compiling to Linux needs no C toolchain; under Option A the `libX11`
  runtime dependency disappears entirely; and the existing Wayland backend
  becomes usable in cgo-free builds.
