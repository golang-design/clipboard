# Design: the primary (middle-click) selection

| | |
|---|---|
| **Status** | Implemented |
| **Issue** | [#67](https://github.com/golang-design/clipboard/issues/67) |
| **Builds on** | [#6](https://github.com/golang-design/clipboard/issues/6) (Wayland data-control), [#151](https://github.com/golang-design/clipboard/issues/151) (`WriteAll`), [#152](https://github.com/golang-design/clipboard/issues/152) (`FmtFiles`) |

## 1. Current state

X11 and Wayland have **two** clipboards. `CLIPBOARD` is the Ctrl+C/Ctrl+V one;
`PRIMARY` holds whatever was last selected with the mouse and is pasted with the
middle button. They are independent: copying does not change the selection, and
selecting does not change the clipboard.

This package only ever touches `CLIPBOARD` — the atom is hardcoded at three call
sites in `clipboard_x11.go`, and the Wayland backend only ever calls
`set_selection`. So a clipboard manager built on this package silently misses
half of what a Linux user does.

Windows and macOS have no equivalent. There is no second clipboard to reach, and
no way to synthesize one that other applications would honor.

## 2. Design decision

> Address the selection with an **option** on the existing calls, not with a
> second set of functions.

```go
// Option configures a clipboard operation.
type Option interface{ apply(*config) }

// FromPrimary directs the operation at the primary selection.
func FromPrimary() Option
```

### The variadic problem, and why Format is an Option

Three of the existing calls already spend their variadic slot, and Go allows
only one:

```go
func Watch(ctx context.Context, t ...Format) <-chan Data
func WriteAll(items ...Item) <-chan struct{}
```

So `Option` is an **interface**, and `Format` and `Item` implement it by
appending themselves to the operation's config. One variadic slot then carries
both the what and the how:

```go
func Watch(ctx context.Context, opts ...Option) <-chan Data
func WriteAll(opts ...Option) <-chan struct{}

clipboard.Watch(ctx, clipboard.FmtText)                       // still compiles
clipboard.Watch(ctx, clipboard.FmtText, clipboard.FromPrimary())
```

Every existing call site keeps compiling, because a `Format` argument still
satisfies the parameter. That source compatibility is the point: `Read`, `Write`
and `Watch` have been public since v0.1.

`WriteFiles` is the exception. Its variadic slot holds `[]string`, and a string
cannot be made an `Option` without swallowing every stray string argument, so it
takes a slice instead: `WriteFiles(paths []string, opts ...Option)`. It shipped
in #152 and is not in any tag, so nothing depends on the old shape.

### Semantics

- Without `FromPrimary()`, everything behaves exactly as before.
- The primary selection is an X11 and Wayland concept. On Windows, macOS, iOS,
  Android and in CGO-disabled builds, a primary-selection read returns nil and a
  write is a no-op — the same graceful degradation the rest of the API uses,
  rather than a panic or a silent write to the wrong clipboard. Writing to the
  clipboard when the caller asked for the selection would be worse than doing
  nothing: it would destroy what the user had copied.

## 3. Per-platform mechanics

| Backend | Mechanism |
|---|---|
| X11, BSD | `PRIMARY` instead of `CLIPBOARD` as the selection atom. Everything else — ownership, `TARGETS`, the serve loop — is identical, because X11 selections are a general mechanism and `CLIPBOARD` is just one atom. |
| Wayland | `set_primary_selection` and the `primary_selection` event, the data-control siblings of `set_selection`/`selection`. |
| Windows, macOS, iOS, Android, CGO-disabled | none; `errUnsupported`. |

The selection is threaded through the backend entry points as an argument rather
than read from a global, so a `FromPrimary` read cannot race a normal one.

### The Wayland version bump

`set_primary_selection` and the `primary_selection` event arrived in **version 2**
of `zwlr_data_control_manager_v1`, and are in `ext_data_control_manager_v1` from
the start. The manager is currently bound at version 1, so it must be bound at
the highest version the compositor advertises, capped at 2.

Binding v2 also means a v2 device sends `primary_selection` events to the
existing loops. They already dispatch on specific opcodes and ignore the rest,
so an unhandled event is skipped rather than mis-read — but that is a property to
keep, not an accident, since the two events differ only by opcode and confusing
them would hand back the wrong clipboard's data.

## 4. Scope

In: read, write, watch and enumerate against the primary selection on X11, BSD
and Wayland.

Out: the X11 `SECONDARY` selection, which nothing uses; clipboard-manager
behavior such as persisting a selection after the owner exits.

## 5. Testing

`TestPrimarySelectionIsIndependent` is the assertion that matters and the one
that fails without this: write different content to each clipboard and read both
back. Before this change the second write would land on the same clipboard and
the first value would be gone.

The two must also stay independent through `Watch` and `Formats`, so those are
asserted against a selection the other clipboard does not hold.

On the platforms without a primary selection, the test instead asserts the
documented degradation — a nil read and a no-op write — and specifically that a
`FromPrimary` write did **not** land on the normal clipboard, which is the
failure mode that would silently destroy a user's copied data.

Wayland is checked cross-process with `wl-paste --primary`, since a data-control
client does not observe its own just-set selection.

## 6. Outcome

Implemented as designed. Two things worth recording for whoever touches this
next.

The Wayland event loops each had a local variable named `selection` and a closure
parameter named `sel`, which the new type and argument would have shadowed. They
are now `current` and `offerID` — worth the churn, because a shadowed selection
argument in a loop that dispatches on selection opcodes is exactly the bug that
returns the other clipboard's data while every same-process test passes.

`WriteFiles` changed shape from `(paths ...string)` to `(paths []string, opts
...Option)`. It shipped in #152 and is in no tag, so nothing depended on it, but
it is the one call in the package that options could not be appended to — a
string cannot implement `Option` without swallowing every stray string argument.
