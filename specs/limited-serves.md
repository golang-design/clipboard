# Design: limiting how many times a write is served

| | |
|---|---|
| **Status** | Implemented |
| **Issue** | [#22](https://github.com/golang-design/clipboard/issues/22) |
| **Builds on** | [#67](https://github.com/golang-design/clipboard/issues/67) (the `Option` axis) |
| **Prior art** | `xclip -loops N` |

## 1. What is being asked

> Allow only a limited number of Read operations on the clipboard before the
> content gets flushed.

The motivating shape is a secret: put a password on the clipboard, let it be
pasted once, and have it disappear rather than sit there until something else
overwrites it. The reporter was shelling out to `xclip -loops` and wanted to stop.

## 2. Why this cannot be portable, and why that is not a reason to refuse

There are two kinds of clipboard underneath this package.

**X11 and Wayland are owner-served.** `Write` takes ownership of a selection and
this process then answers each paste request as it arrives — `SelectionRequest`
on X11, the data source's `send` event on Wayland. The owner sees every paste, so
it can count them and drop ownership when it has served enough.

**Windows and macOS are stores.** `Write` copies the bytes into an OS-owned
buffer and returns. No request ever reaches this process again; the OS hands the
data out to anyone who asks, and never says that it did. There is nothing to
count, and nothing to flush.

That asymmetry is a property of the platforms, not a gap in this package, and no
API can hide it. What it *does* mean is that the option must be honest about
where it applies — see §4, which is the load-bearing part of this design.

## 3. Design decision

> An option on the write, carried by the `Option` axis added in #67.

```go
// Loops limits how many times the written data is served before it is dropped.
func Loops(n int) Option

clipboard.Write(clipboard.FmtText, secret, clipboard.Loops(1))
```

### What counts as one serve

A serve is one delivery of the payload to a requestor: an answered
`SelectionRequest` for one of the advertised targets on X11, one `send` event on
Wayland.

The `TARGETS` metadata request does **not** count. A normal paste asks what is
available and then asks for one of those, so counting metadata would halve every
limit and make `Loops(1)` mean "serve nothing".

A consumer that asks for several formats from one paste counts once per format.
That is inherent to owner-served clipboards — the owner sees requests, not
pastes — and is documented rather than papered over, because guessing at which
requests belong to the same paste would be worse than saying so.

## 4. The dangerous half

On Windows, macOS, iOS, Android and CGO-disabled builds, `Loops` does nothing.
The write succeeds and the data stays on the clipboard until something replaces
it.

This is the one degradation in this package that can *hurt* rather than merely
disappoint: someone reaching for `Loops(1)` is usually holding a secret. A
graceful no-op is the wrong instinct if the caller cannot tell it happened, so
the godoc leads with the limitation rather than mentioning it at the end, and
says plainly that it is not a way to clear a secret from a Windows or macOS
clipboard.

Refusing the write outright on those platforms was considered and rejected: it
would make an ordinary cross-platform program fail on two platforms for asking
for a best-effort lifetime hint, and callers would route around it by dropping
the option entirely — which is strictly worse than a documented no-op.

## 5. Mechanics

| Backend | Mechanism |
|---|---|
| X11, BSD | `serveSelection` counts answered data requests and returns once it has served enough, which closes the connection and drops selection ownership. The `changed` channel then fires as it does for any other loss of ownership. |
| Wayland | The source-serving loop counts `send` events and destroys the source, which clears the selection. |
| Windows, macOS, iOS, Android, CGO-disabled | Ignored. |

`Loops(0)` and any negative value mean unlimited, which is the existing behavior
and the default.

## 6. Testing

`TestLoopsDropsAfterServing` writes with `Loops(1)`, reads once and gets the
data, then reads again and must get nothing. Without the limit the second read
succeeds, so the test fails on exactly the behavior being added.

`TestLoopsIgnoredWhereUnsupported` pins the documented no-op on the store
platforms, so the degradation is a decision on the record rather than an
accident.

Wayland is checked cross-process with two `wl-paste` runs, since a data-control
client does not observe its own selection and the count is kept by the serving
side.

## 7. Outcome

Implemented as designed. `answerSelectionRequest` now reports whether it served
the data rather than the `TARGETS` list, which is what the X11 count keys on.

**Every reader consumes a serve, including this program.** That is obvious in
hindsight and was not obvious in the tests: `TestLoopsDropsAfterServing`
originally used `FmtText`, and this package's own polling watchers read that
format once a second. A watcher left over from an earlier test — cancelled but
not yet exited, which `TestWatchNoGoroutineLeakOnCancel` explicitly tolerates —
ate the single serve before the test's own read, which then timed out against a
dying owner. It passed under CGO_ENABLED=1 and failed under CGO_ENABLED=0 purely
on timing.

The tests now use a private MIME type nothing else polls, and wait on the
channel `Write` returns rather than polling for the drop — the channel fires
exactly when the limit is reached and ownership is given up, so there is nothing
to race. The godoc says the same thing to callers: a `Watch` running alongside a
`Loops(1)` write will usually be the one that consumes it.

One cleanup fell out: `write` on the darwin, Windows, X11 and Wayland backends
had become dead once `Write` started going through `WriteAll` in #151, and it
would otherwise have needed a `loops` parameter nothing passed. It is gone; the
mobile and CGO-disabled backends still use theirs, since their `writeAll`
delegates to it.
