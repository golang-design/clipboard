# Design: event-driven clipboard watch on Windows

| | |
|---|---|
| **Status** | Implemented |
| **Issue** | [#153](https://github.com/golang-design/clipboard/issues/153) (remaining half) |
| **Also helps** | [#147](https://github.com/golang-design/clipboard/issues/147) (first bullet: image copies via Ctrl+C) |
| **Related** | [#54](https://github.com/golang-design/clipboard/issues/54) (tunable interval — declined, out of scope) |

## 1. Current state

`Watch` polls. Every backend except Wayland runs a `time.NewTicker(time.Second)`
loop, compares a change counter, and `Read`s when it moved. On Windows that
counter is `GetClipboardSequenceNumber`.

The correctness half of #153 shipped in #158: every polling watcher now stops its
ticker and selects the send against `ctx.Done()`, so a watcher with a stalled
consumer no longer leaks. What remains is the **latency** half, and #153's own
scoping comment names the fix:

> Still open here: the event-driven Windows watch via `AddClipboardFormatListener`.
> The tunable interval is intentionally *not* in scope — that's the declined #54.

## 2. Why polling is the wrong shape here

1. **Latency.** A change is observed up to a full second after it happens.
2. **Coalescing.** Two changes inside one tick collapse into one delivery: the
   loop reads the *latest* value and jumps the counter past the intermediate one.
   A watcher building a history silently loses entries.
3. **Idle cost.** The loop wakes once a second forever, whether or not anything
   is happening.

None of these is fixable by tuning the interval — a shorter interval trades (1)
and (2) against (3). Windows is the one desktop platform that does not need the
trade: the OS will tell us.

## 3. Design decision

> Watch through a **message-only window** registered with
> `AddClipboardFormatListener`, and keep the polling loop as the fallback.

`AddClipboardFormatListener` (Vista+) posts `WM_CLIPBOARDUPDATE` to a window on
every clipboard change, from any application, for any format. It needs a window
handle but not a visible window: `HWND_MESSAGE` as the parent creates a
message-only window — no screen presence, no z-order, no input, it exists purely
to receive messages.

This is not the older `SetClipboardViewer` chain, which requires each viewer to
forward messages to the next and breaks globally when one link misbehaves.
`AddClipboardFormatListener` has no chain.

### Alternatives considered

| Option | Why not |
|---|---|
| Shorter poll interval | Trades latency for idle wakeups; doesn't fix coalescing; the tunable knob is the declined #54. |
| `SetClipboardViewer` chain | Pre-Vista API, superseded; a single misbehaving viewer breaks the whole chain for every app on the desktop. |
| `WM_CLIPBOARDUPDATE` on a *visible* window | The package is windowless by design; a real window would appear in the alt-tab list of any program that imports it. |

## 4. Mechanics

The watcher goroutine owns a thread and a window for the lifetime of the context:

1. `runtime.LockOSThread()`, and **never unlock**. A window belongs to the thread
   that created it, and its messages are only retrievable on that thread. Letting
   the goroutine exit while locked terminates the thread, which is what we want
   once the window is gone.
2. Register the window class **once per process** (`sync.Once`). `syscall.NewCallback`
   allocates a callback that is never freed and the process has a hard cap on
   them, so a per-`watch()` registration would eventually panic a program that
   starts and cancels watchers in a loop. The class's window procedure is a thin
   pass to `DefWindowProcW`.
3. `CreateWindowExW(..., HWND_MESSAGE, ...)`, then `AddClipboardFormatListener`.
4. Capture the sequence number as the baseline, *then* signal readiness — so a
   change made right after `Watch` returns cannot be missed.
5. `GetMessageW` filtered to our window. `WM_CLIPBOARDUPDATE` → read and deliver;
   a private `WM_APP` stop message → return.
6. Cancellation: a small goroutine waits on `ctx.Done()` and `PostMessageW`s the
   stop message. `PostMessageW` is the only safe way to reach a thread blocked in
   `GetMessageW` from another goroutine, and it is thread-safe by contract.
7. On return: `RemoveClipboardFormatListener`, `DestroyWindow`.

### Preserved semantics

- **No spurious first delivery.** Registration can itself produce a
  `WM_CLIPBOARDUPDATE`. The handler compares `GetClipboardSequenceNumber` against
  the baseline and ignores a message that did not follow a real change, so `Watch`
  still reports only changes that happen *after* it was called.
- **Format filtering is unchanged.** The listener fires for every clipboard
  change; `Read(t)` returning nil means the change was not in the watched format
  and nothing is delivered — the same rule the polling loop used.
- **Cancellation is unchanged.** The send is still selected against `ctx.Done()`,
  and the channel is still closed on cancel.

### Fallback

Every step that can fail degrades to `watchPoll`, the previous loop kept intact:
a missing `user32` export, a failed class registration, a `CreateWindowExW` that
returns 0, an `AddClipboardFormatListener` that returns 0. Window creation is
exactly what fails in a Session 0 service (#145), and returning a dead channel
there would be a regression on top of an open bug. The fallback is chosen before
`watch` returns, so the caller always gets a live channel.

## 5. Scope

In: the Windows `watch` path. Out: the tunable interval (#54, declined), the
other backends' polling loops, `write`'s own sequence-number poll, and the
`CF_HDROP` half of #147.

## 6. Testing

`TestWatchIsEventDriven` (Windows-only) writes three values in sequence and
requires each to arrive well inside the 1 s polling floor. Round 1 is delivered
at the first tick under polling and rounds 2–3 a full second after their write,
so the test fails on every round without the fix and passes with milliseconds to
spare with it. The existing `TestWatchNoGoroutineLeakOnCancel` covers the
teardown path, and `TestClipboardWatch` covers the delivery semantics.

## 7. Outcome

Implemented as designed. `watch` resolves to `watchEvent` when the message window
comes up and to `watchPoll` otherwise. Observed latency in CI is milliseconds
against the 700 ms ceiling the test enforces.
