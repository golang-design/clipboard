# Design: context and errors on the byte-moving API

| | |
|---|---|
| **Status** | Implemented |
| **Target** | v0.9.0 (breaking) |
| **Unblocks** | [#64](https://github.com/golang-design/clipboard/issues/64) (web support) |
| **Builds on** | [#67](https://github.com/golang-design/clipboard/issues/67) (the `Option` axis) |

## 1. Three problems, one shape

**Errors are swallowed.** `Read` returns `nil` for every kind of failure. There
is an unexported `debug` flag in this package whose entire purpose is to print
those errors to stderr, which is an admission that callers need them and cannot
have them. A caller cannot tell "the clipboard holds no image" from "there is no
X server".

**Operations cannot be bounded.** `x11Read` waits up to five seconds on a wire
read. A caller with its own deadline has no way to say so, and no way to cancel a
read that is going nowhere.

**The browser cannot be expressed at all.** `navigator.clipboard.readText()`
returns a Promise, and the browser only permits a read from inside a user-gesture
handler with a permission grant. Behind `Read(Format) []byte` there is nothing to
await and no way to report "this needs a user gesture" — the caller would get a
silent `nil`, which is exactly the failure mode #64 would otherwise ship.

All three are the same missing pieces: a `context.Context` going in, an `error`
coming out.

## 2. Why now

The module is at **v0.8.0**. Go's import-versioning rules permit breaking changes
below v1 without a `/v2` path, so this costs a minor-version bump and a migration
note. After v1 it would cost a new module path and a permanent fork of the docs.
This is the cheapest this change will ever be, and every feature added later —
each one of which would need a `Context` twin under the alternative — makes it
more expensive.

## 3. The change

```go
func Read(ctx context.Context, t Format, opts ...Option) ([]byte, error)
func Write(ctx context.Context, t Format, buf []byte, opts ...Option) (<-chan struct{}, error)
func WriteAll(ctx context.Context, opts ...Option) (<-chan struct{}, error)
func Formats(ctx context.Context, opts ...Option) ([]Format, error)
func ReadFiles(ctx context.Context, opts ...Option) ([]string, error)
func WriteFiles(ctx context.Context, paths []string, opts ...Option) (<-chan struct{}, error)
func ReadAs[T any](ctx context.Context, f Format, decode func([]byte) (T, error), opts ...Option) (T, error)
```

`Watch` is unchanged: it already takes a context, and its channel closing is
already how it reports that it is done.

### One rule, applied everywhere

A half-converted API is worse than either end, because it teaches two rules and
forces every later feature to pick one. Every call that moves bytes takes a
context first and returns an error last.

## 4. What the errors say

The point is to distinguish causes that the old API flattened into `nil`, so the
sentinels are exported and the internal ones become them:

| Error | Meaning |
|---|---|
| `ErrNoData` | The clipboard is reachable and holds nothing in that format. The ordinary "nothing to paste" case. |
| `ErrUnavailable` | The clipboard itself could not be reached — no X server, a display connection that failed, `OpenClipboard` timing out against another application. |
| `ErrUnsupported` | This platform cannot do this: an image on mobile, a custom format in a CGO-disabled build, the primary selection on Windows. |

`ErrNoData` already existed for `ReadAs`; it now carries the same meaning for
every read, which is what it should always have meant.

Backends conflated "format absent" with "clipboard unavailable" because the
platform checks conflate them — `IsClipboardFormatAvailable` failing means the
format is not there, not that the clipboard is broken. Those sites return
`ErrNoData` now, so the common case reports the common cause.

## 5. What the context does

`ctx` is honored to the extent the platform allows, and the doc says which:

- **Every backend** checks the context before starting work, so a cancelled
  context fails immediately rather than doing I/O that is already unwanted.
- **X11** applies the context's deadline to the wire read, capped by the existing
  five-second ceiling. A caller that wants to wait 200 ms for a paste can now say
  so; before, five seconds was the only answer.
- **The rest** are effectively immediate (a pasteboard call, a clipboard-store
  call), so the entry check is the whole of it. Promising more would be a lie.

This is the seam the browser needs: `readText()`'s Promise is awaited against the
context, and a denied permission becomes an error rather than a silent `nil`.

## 6. Migration

Mechanical. `context.TODO()` is a correct first step everywhere, and callers that
ignored errors before can keep ignoring them with `_`:

```go
// before
b := clipboard.Read(clipboard.FmtText)

// after
b, err := clipboard.Read(context.TODO(), clipboard.FmtText)
```

The README carries a migration table, and the package doc's examples are updated,
since those are what people copy.

## 7. Testing

The existing suite is the regression test: every call site changes, so the whole
package exercises the new signatures. Beyond that:

- `TestReadReportsNoData` — a read of a format the clipboard does not hold
  returns `ErrNoData` rather than a bare nil, which is the wart being fixed.
- `TestReadHonorsContext` — an already-cancelled context fails without doing the
  work, and an X11 read with a short deadline returns near that deadline rather
  than at the five-second ceiling.
- `TestErrorsAreDistinguishable` — the sentinels are `errors.Is`-comparable and
  do not collapse into each other.

## 8. Outcome

Implemented as designed across all seven backends and every call site.

The deadline test is a unit test on `x11Deadline`, not an end-to-end read, and
deliberately so: the first attempt at an end-to-end version passed in 0.00s and
would have passed with the context ignored entirely. Every reachable read returns
promptly for a reason of its own — an unowned selection short-circuits (#168), and
an owned one refuses an unknown target — so demonstrating that a deadline cuts a
wait short needs a second, uncooperative process. Testing the calculation is
honest; testing the read there would have been theatre.

CI found the half-done half of §4. The spec said backends conflating "format
absent" with "clipboard unavailable" would report `ErrNoData`, and only some of
them did: Windows still returned `ErrUnavailable` from its
`IsClipboardFormatAvailable` check, and so did the `CF_HDROP` and
`NSFilenamesPboardType` reads when the list was empty. They report `ErrNoData`
now.

The Wayland job failed for the opposite reason, and correctly: `TestReadAsNoData`
never called `Init`, so with xwayland disabled it fell through to X11 with no X
server — genuinely unavailable. The old `ReadAs` mapped every failure to
`ErrNoData`, which is exactly the flattening this change exists to undo, so the
test had been asserting the right answer for the wrong reason for as long as it
had existed. It initializes first now.

`ErrUnavailable` was previously re-exported from `export_test.go` for the test
build. It is public now, so that re-export is gone.
