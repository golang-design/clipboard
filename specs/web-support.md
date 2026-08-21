# Design: the browser clipboard (js/wasm)

| | |
|---|---|
| **Status** | Implemented |
| **Issue** | [#64](https://github.com/golang-design/clipboard/issues/64) |
| **Depends on** | v0.9.0 (`context` + `error`), without which this backend cannot be written honestly |

## 1. Why this needed the API change first

The browser clipboard is asynchronous and permission-gated:

```js
navigator.clipboard.readText()   // Promise, needs a permission grant
navigator.clipboard.writeText(s) // Promise, needs a secure context
```

Behind the old `Read(Format) []byte` there was nothing to await and no way to say
"the user denied this" or "this needs a user gesture" — every one of those became
a silent `nil`, indistinguishable from an empty clipboard. Shipping that would
have been worse than shipping nothing, because the caller could not tell they had
been denied.

With a context in and an error out, the Promise is awaited against the caller's
cancellation and a denial is reported as what it is.

## 2. Scope

**In:** `FmtText` read and write, and an `Init` that says clearly why the
clipboard is unavailable when it is.

**Out, and reported as `ErrUnsupported` rather than pretended:**

- `FmtImage` and custom formats. These need `ClipboardItem` and `Blob`
  plumbing, and browser support is uneven — Chrome implements
  `clipboard.read()`/`write()` for arbitrary types, Firefox only partly.
- `Watch`. Browsers have no dependable clipboard-change event; the proposed
  `clipboardchange` is not something to build on yet. `Watch` returns a closed
  channel, as it does on any backend that cannot observe changes.
- `Formats`. Enumerating would mean calling `clipboard.read()`, which triggers a
  permission prompt as a side effect of asking a question. It returns empty.
- `FromPrimary` and `Loops`. There is no second clipboard and no owner-served
  paste to count, exactly as on Windows and macOS.

## 3. Two constraints that are not this package's to fix

**A read needs a user gesture.** Browsers only permit `readText()` from inside a
user-initiated event handler, with permission granted. `clipboard.Read` called
from `main()` will be denied — by the browser, by design. The package reports
the denial; it cannot grant itself permission. This is documented on the backend
rather than discovered at runtime.

**A blocking call inside a JS callback deadlocks.** Go's wasm scheduler runs on
the JS event loop: a `js.FuncOf` callback must return before the loop can deliver
the Promise settlement. So `Read` must not be called *directly* inside an event
handler — it must run on a goroutine the handler starts. That is a Go/wasm rule
rather than a clipboard one, but it will bite exactly the people who write
`onclick: func() { clipboard.Read(...) }`, so it is documented where they will
look.

## 4. Awaiting a Promise against a context

The `then` callbacks are `js.Func` values that must be released, and releasing
them while the Promise can still settle would crash the program on a callback
into freed memory. Waiting for the settlement before releasing would defeat the
point of the context.

So the result channel is buffered and the callbacks release themselves once they
fire: a cancelled read returns immediately, the settlement lands in the buffer
whenever it arrives, and the release happens then. Nothing leaks and nothing is
used after release.

## 5. Testing

Go's wasm test runner works under Node, and CI runners have it, so this backend
is exercised rather than only compiled — which matters for a backend nobody can
run on their laptop.

Node has a `navigator` global but no `navigator.clipboard`, which is precisely
the "the API is not here" path: `TestInitReportsMissingClipboardAPI` asserts
`Init` reports `ErrUnavailable` and says *why* — mentioning the secure-context
requirement, since that is the usual reason a real browser is missing it — and
`TestDegradesWithoutClipboardAPI` asserts reads and writes report that rather
than panicking or returning a bare nil.

**What this does not cover:** the behavior against a real `navigator.clipboard`.
That needs a browser and a permission grant, which is a headless-Chrome harness
this repository does not have. The gap is stated here rather than implied by a
green check mark.

## 6. Outcome

Implemented as scoped: text in both directions, everything else named as
unsupported.

Adding the CI job exposed that the suite could not run under js/wasm at all. The
generic tests guarded themselves with "is CGO_ENABLED literally 0", which answers
a different question and answers it wrong off the desktop: under js/wasm the
variable is simply unset, so every test ran against a clipboard that does not
exist — three failed and `TestClipboardWriteEmpty` hung until the test binary
timed out.

The guard is now one helper that asks `Init`, which is the actual authority on
whether there is a clipboard to exercise. It preserves desktop behavior exactly
(a CGO-disabled Linux build still runs, because the pure-Go X11 backend still
initializes) and makes the whole suite skip cleanly on any platform without a
clipboard. The full run under Node went from a 45-second timeout to 0.15s.
