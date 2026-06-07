# Design: Wayland clipboard support

| | |
|---|---|
| **Status** | Implemented |
| **Issue** | [#6](https://github.com/golang-design/clipboard/issues/6) |
| **Also closes** | [#61](https://github.com/golang-design/clipboard/issues/61) (crash on Wayland, folded into #6) |
| **Related** | [#67](https://github.com/golang-design/clipboard/issues/67) (primary selection), [#25](https://github.com/golang-design/clipboard/issues/25)/[#51](https://github.com/golang-design/clipboard/issues/51) (drop Cgo) |

## 1. Current state

On Linux the package talks to **X11** via `libX11`, loaded with `dlopen` (see
`clipboard_linux.c`; the dlopen pattern landed in `#20`). It works under a Wayland
session **only** through the **XWayland** bridge. On a pure Wayland session (e.g.
Hyprland without XWayland) the X selection request is served against an invalid
window and Xlib's *default* error handler calls `exit(1)` — i.e. the package can
**crash the host process** (#61).

There is no native Wayland backend, and none has ever been written: the prior
attempt in #6 (microo8) stalled before any code.

## 2. Why this is hard (the focus wall)

Wayland's **core** clipboard protocol (`wl_data_device` / `wl_data_source` /
`wl_data_offer`) ties selection access to a **seat with keyboard focus**:
`wl_data_device.set_selection` requires a *serial from an input event*
(`wl_keyboard.enter`). Reading is likewise delivered to the focused client.

This package is a **windowless, focus-less library**. It has no surface and never
receives focus, so the core protocol — the path emersion's reference blog
describes — will reject it. This is *the* reason #6 has stayed open for years; it is
an architectural mismatch, not mere neglect.

The escape hatch is the **data-control** family of protocols, designed precisely
for clipboard managers that have no focus.

## 3. Design decision

> Implement a **pure-Go** `ext-data-control-v1` client (with a `wlr-data-control`
> fallback), speaking the Wayland wire protocol directly over the unix socket. No
> Cgo, no `libwayland`.

Rationale:

- **Focus-less.** `ext-data-control` lets a client read *and* set the selection
  without keyboard focus — exactly our use case.
- **Broad coverage (incl. GNOME).** Per wayland.app, `ext-data-control-v1` is
  implemented by Mutter (GNOME ≥ 49), KWin, Sway, Hyprland, and other wlroots
  compositors. The older `wlr-data-control` is supported by wlroots compositors;
  we bind it as a fallback for compositors that predate `ext`.
- **Cgo-free.** Wayland's wire protocol is simple enough to speak in pure Go
  (`rajveermalviya/go-wayland` proves it). This would be the project's **first
  Cgo-free backend**, advancing the goal in #25/#51 instead of adding another
  `libX11`-style Cgo blob.
- **No external runtime dependency.** Unlike shelling out to `wl-clipboard`
  (proposed and rejected in #6 as an external dependency), this needs nothing on
  the user's `PATH`.

We deliberately do **not** use the core `wl_data_device` protocol (focus wall) and
do **not** link `libwayland` (Cgo). Implementing the *protocol* over the socket is
not the same as depending on the *library* — consistent with emersion's guidance in
#6.

## 4. Protocol subset

Only a small slice of Wayland is needed (hand-rolled, like `clipboard_linux.c`
hand-codes Xlib — no full scanner required):

- `wl_display`, `wl_registry` — connect, discover globals, `sync` roundtrips
- `wl_seat` — the seat the device binds to
- `ext_data_control_manager_v1` → `get_data_device(seat)`, `create_data_source`
- `ext_data_control_device_v1` — request `set_selection`; events `data_offer`,
  `selection`, `finished`
- `ext_data_control_source_v1` — request `offer(mime)`; events `send(mime, fd)`,
  `cancelled`
- `ext_data_control_offer_v1` — event `offer(mime)`; request `receive(mime, fd)`

`wlr-data-control` (`zwlr_data_control_*_v1`) is near-identical and used as a
fallback when `ext` is absent.

## 5. Wire mechanics (pure Go)

- **Connect:** unix socket at `$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY`.
- **Framing:** each message is `[object_id uint32][opcode uint16][size uint16]
  [args…]`, host byte order (little-endian on supported targets); strings are
  length-prefixed and 32-bit padded.
- **File descriptors (the crux):** clipboard data is transferred over a **pipe**,
  not inline. Use `golang.org/x/sys/unix` `Sendmsg`/`Recvmsg` with `SCM_RIGHTS`:
  - send the pipe's write-fd alongside an `offer.receive(mime, fd)` request;
  - receive the requestor's write-fd from a `source.send(mime, fd)` event.
- **Object IDs / dispatch:** maintain a client-side id allocator and a map from
  object id → handler to route events. A single read loop on the socket drives
  everything.

## 6. Mapping to the existing API

The public API is unchanged; the Wayland backend implements the same
`initialize/read/write/watch` functions the X11 backend does.

- **`Write(fmt, buf) <-chan struct{}`** — create a source, `offer` the MIME
  type(s), `set_selection`. Spawn an owner goroutine that answers each
  `send(mime, fd)` by writing `buf` to `fd` and closing it. On the `cancelled`
  event (selection replaced) **close the returned channel** — this matches the
  existing X11 "owner serves until overwritten" semantics and the documented
  channel contract exactly.
- **`Read(fmt) []byte`** — from the device's current `selection` offer:
  `pipe()` → `offer.receive(mime, w)` → close `w` → roundtrip → read `r` to EOF.
- **`Watch(ctx, fmt) <-chan []byte`** — the device emits a `selection` event on
  *every* clipboard change, so Watch becomes **event-driven** (no 1-second polling
  ticker, unlike X11). Closes the channel on `ctx` cancel (same contract; cf. #98).
- **MIME mapping:** `FmtText` → `text/plain;charset=utf-8` (also accept
  `UTF8_STRING`, `text/plain`); `FmtImage` → `image/png`. This dovetails with the
  custom-format design (`specs/custom-clipboard-formats.md`), which already chooses
  MIME as the cross-platform format identity.
- **Bonus — #67:** `ext-data-control` also exposes the **primary selection**, so
  this backend can address primary-selection support on Wayland.

## 7. Backend selection (`Init`)

`clipboard_linux.go`'s `initialize/read/write/watch` become a thin selector over an
internal backend interface with `x11` and `wayland` implementations:

```
Init():
  if WAYLAND_DISPLAY set and (ext|wlr)-data-control global present -> wayland backend
  else if DISPLAY set                                              -> x11 backend (today's path)
  else                                                             -> errUnavailable (clear message)
```

Detection uses `WAYLAND_DISPLAY` (emersion confirmed this is the canonical signal)
plus an actual registry probe for the data-control global, so we degrade to X11 when
the compositor lacks data-control.

## 8. Implementation phases

1. **Graceful-fail (ship first, independent of the rest).** In the X11 path,
   install a custom handler via `XSetErrorHandler` (loaded through `dlsym`) so a
   protocol error can never `exit(1)` the host (#61). Detect a Wayland-only session
   in `Init()` and return a clear error. Small and shippable now.
2. **Wire core.** `clipboard_wayland_linux.go`: socket connect, message read/write
   loop, id allocator, registry bind of `wl_seat` + `ext_data_control_manager_v1`.
   Prove connect + global discovery against a headless compositor.
3. **Read path.** `selection` → `offer` → `receive(fd)` → read to EOF.
4. **Write path.** source + `set_selection` + serve `send(mime, fd)`; wire the
   `cancelled` → close-channel semantics.
5. **Watch path.** event-driven on `selection`.
6. **`wlr-data-control` fallback** for compositors without `ext`.
7. **Dispatch + docs.** `Init` selector; README caveat (GNOME < 49 → XWayland);
   note primary-selection support (#67).

## 9. Testing

Wayland needs a running compositor; we must verify in CI to avoid shipping an
unverified backend. A **headless compositor on the standard ubuntu runner** (no VM):

- `weston --backend=headless` or `sway` with `WLR_BACKENDS=headless`, export the
  resulting `WAYLAND_DISPLAY`, then run `go test`.
- Sway/wlroots provides `wlr-data-control`; use a recent enough sway/weston for
  `ext-data-control`. Exercise **both** protocols if the fallback is implemented.
- Mirror the local `hack/test-linux.sh` approach for reproducibility.

**Every backend phase lands with its test in the same PR** (lesson from the NetBSD
CI attempt, where an untested job was worse than none).

## 10. Caveats & open questions

- **GNOME < 49 / compositors without data-control** → no native support; the X11
  path via XWayland remains the fallback. Documented, not silently broken.
- **`ext` vs `wlr` precedence** — prefer `ext-data-control-v1`; fall back to
  `zwlr_data_control_manager_v1`. Confirm version negotiation against real
  compositors.
- **Security model** — data-control is a *privileged* protocol; some compositors
  may gate it. The probe-then-fallback design handles absence gracefully.
- **Image MIME negotiation** — confirm `image/png` is the de-facto type emitted by
  common apps under Wayland (as PNG is on the other backends).

## 11. Effort & risk

- **Effort:** bounded but real — roughly the 7 phases above. The fd-passing
  (`SCM_RIGHTS`) and the event loop are the only genuinely fiddly parts; the
  protocol slice itself is small and stable.
- **Upside beyond #6:** first Cgo-free backend (momentum for #25/#51),
  event-driven `Watch`, and primary-selection support (#67) on Wayland.
- **Risk:** headless-compositor CI flakiness; mitigated by treating CI setup as
  part of phase 2 rather than an afterthought.

## 12. Outcome

Shipped across **#109–#116** as a phased rollout (graceful-fail #110, wire core
#112, read #113, write #114, watch #115, dispatch #116), tested under headless
sway in CI against independent `wl-copy`/`wl-paste` clients.

**As designed:**
- Native data-control backend supporting `ext_data_control_manager_v1`
  (preferred) and `zwlr_data_control_manager_v1` (fallback); fd passing via
  `SCM_RIGHTS`.
- Backend selection in `initialize()` probes `WAYLAND_DISPLAY` **and** verifies a
  data-control manager is actually advertised (`wlAvailable`), falling back to
  X11/XWayland otherwise.
- `Watch` is **event-driven** (selection events, no polling), with a baseline
  capture matching the X11 contract.

**Deviations / limitations:**
- **Phase 6 (wlr fallback) folded into the wire core** rather than shipping as a
  separate phase; both managers are handled in #112.
- **Primary selection not shipped.** §6/§11 floated it as an upside, but the
  implementation covers only the regular CLIPBOARD selection; PRIMARY remains
  reserved (#67).
- **Same-process self-read limitation (discovered later).** Under data-control a
  process does **not** observe its own just-set *custom* selection from a fresh
  reader connection (built-in text/image happen to work same-process). Found
  while adding custom formats; the same-process tests skip on Wayland and
  cross-process interop is verified with `wl-copy`/`wl-paste` instead
  (`clipboard_custom_linux_test.go`, #131).
- **Custom formats + `Formats()` enumeration added later** (#131/#139), outside
  the original phases — `wlEnumerateFormats` maps offered MIME types to Format
  tokens, registering custom types on demand.
