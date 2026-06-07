# Design: Clipboard format enumeration

| | |
|---|---|
| **Status** | Proposed |
| **Issue** | [#89](https://github.com/golang-design/clipboard/issues/89) (enumeration half) |
| **Builds on** | custom formats ([#17](https://github.com/golang-design/clipboard/issues/17)), tagged `Watch` ([#124](https://github.com/golang-design/clipboard/pull/124)) |

## 1. Current state

`Read`/`Write`/`Watch` operate on a `Format` the caller names up front: the
built-in `FmtText`/`FmtImage`, or a custom token from `Register(mime)`. `Watch`
is variadic and tags each change with its `Format` (#124), which delivered the
"watch all content types and return a type with the content" ask of #89.

What is still missing is the other half of #89: **asking the clipboard what it
currently holds** without guessing format-by-format. There is no way to answer
"what's on the clipboard right now?".

## 2. Goal

A single call that reports the formats currently available on the clipboard:

```go
func Formats() []Format
```

Returned tokens are usable directly with `Read`. The result is a snapshot; like
`Read`, it reflects the clipboard at call time.

## 3. The design fork

The hard question is what to do with a native type that is present but is
neither a built-in nor something the caller has `Register`-ed.

### Option A — report only known formats (no discovery)

`Formats()` returns the subset of `{FmtText, FmtImage}` plus the
already-registered custom tokens whose native type is currently present.

- **Pro:** trivial and consistent; every returned token already has a portable
  MIME identity the caller knows; no auto-registration side effects; no
  per-platform reverse mapping.
- **Con:** cannot discover a format you did not register first — slightly
  circular ("register `text/html`, then ask whether it's present").
- **Cost:** one native enumeration per call (or N availability probes), then a
  local set intersection.

### Option B — discover everything (recommended)

`Formats()` enumerates the native types present, maps each to a `Format`:
built-ins for text/image, and for any other type with a MIME identity it
`Register`s the MIME (idempotent) and returns that token. To make a discovered
token interpretable, expose its identity:

```go
// MIME returns the MIME type a Format denotes: the registered string for a
// custom format, a canonical type for the built-ins ("text/plain;charset=utf-8"
// for FmtText, "image/png" for FmtImage), or "" for an unregistered token.
func (f Format) MIME() string
```

- **Pro:** answers "what's on the clipboard?" for real, including formats put
  there by other apps; tokens round-trip straight into `Read`.
- **Con:** more per-platform code, and the native→MIME direction is best-effort
  on macOS (UTI↔MIME), exactly like the forward map in the custom-format design
  (custom-clipboard-formats.md §5). Native types with no MIME mapping are
  skipped.
- **Cost:** one native enumeration per call plus a reverse-map lookup.

**Recommendation: Option B.** It is the version that makes `Formats()` worth
having, reuses the MIME-identity model already established for custom formats,
and `Format.MIME()` is independently useful (e.g., logging what `Watch`
delivered). The macOS best-effort caveat is already accepted for custom formats.

## 4. Per-platform enumeration (Option B)

| Platform | Enumerate present types | Native → Format |
|---|---|---|
| Linux/X11, BSD | `ConvertSelection(TARGETS)` → ATOM list → `GetAtomName` | atom name is the MIME/target directly (`text/html`, `image/png`, `UTF8_STRING`→`FmtText`) |
| Linux/Wayland | the current data offer's advertised `mime_type` list (already collected when reading) | MIME directly |
| Windows | `EnumClipboardFormats` loop | predefined `CF_UNICODETEXT`→`FmtText`, `CF_DIBV5`/`CF_DIB`→`FmtImage`; registered formats via `GetClipboardFormatName` |
| macOS | `[NSPasteboard types]` | `public.utf8-plain-text`→`FmtText`, `public.png`/`public.tiff`→`FmtImage`; otherwise best-effort UTI→MIME alias table (reverse of §5), else skip |

A `TARGETS`/types request needs the new read-side atom-name / format-name
lookups, but no new selection-ownership behavior — the write side already
advertises `TARGETS` (#60).

### Normalization

- **Built-in precedence.** If several native text types are present
  (`UTF8_STRING`, `text/plain`, …), report `FmtText` once rather than a token per
  alias; likewise `FmtImage` for `image/png`/TIFF/DIB. Other MIME types each map
  to one token.
- **De-duplicate** tokens and return a stable order: built-ins first
  (`FmtText`, `FmtImage`), then custom tokens in registration order.

## 5. nocgo / mobile

`Formats()` returns an empty slice (never nil-panics) on CGO-disabled builds and
on iOS/Android, matching how the rest of the API degrades. `Format.MIME()` is
pure (registry lookup) and works everywhere.

## 6. Orthogonal axes (unchanged)

`Formats()` stays on the data-type axis. It does **not** take a selection
argument; the selection axis (PRIMARY vs CLIPBOARD, #67) remains reserved as a
future functional option applied uniformly to `Read`/`Write`/`Watch`/`Formats`.

## 7. Scope / rollout

Spec-first, then one PR per scope (mirroring the custom-format rollout):

1. Core: `Formats()` skeleton + `Format.MIME()` + the built-in/registry mapping,
   with each backend's `enumerate()` returning nothing yet (degrade).
2. Linux/X11 + BSD — `TARGETS` enumeration.
3. Linux/Wayland — offered-MIME enumeration.
4. Windows — `EnumClipboardFormats` + `GetClipboardFormatName`.
5. macOS — `NSPasteboard types` + UTI mapping.
6. Docs + mobile/nocgo confirmation.

Each lands green on CI with a round-trip test that writes a known format and
asserts `Formats()` reports it.
