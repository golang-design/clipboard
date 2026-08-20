# Design: atomic multi-format write

| | |
|---|---|
| **Status** | Implemented |
| **Issue** | [#151](https://github.com/golang-design/clipboard/issues/151) |
| **History** | [#59](https://github.com/golang-design/clipboard/issues/59) (requested, closed in triage without delivery) |
| **Builds on** | [#17](https://github.com/golang-design/clipboard/issues/17) (custom formats), [#160](https://github.com/golang-design/clipboard/issues/160) (native-name resolution) |

## 1. Current state

`Write(Format, []byte)` publishes exactly one representation, and each call
**replaces** the whole clipboard — `EmptyClipboard` on Windows, `clearContents`
on macOS, a fresh selection ownership on X11 and Wayland. So the obvious
sequence does not do what it looks like:

```go
clipboard.Write(clipboard.FmtText, plain) // clipboard = text
clipboard.Write(html, markup)             // clipboard = html; the text is gone
```

There is no way to publish `text/plain` *and* `text/html` together and let the
consumer pick the richest one it understands — the behavior every native
clipboard has, and what a rich-text copy is.

## 2. Design decision

> Add one call that publishes an **ordered** list of representations inside the
> single clipboard transaction each backend already performs.

```go
// Item is one representation of the content being copied.
type Item struct {
	Format Format
	Bytes  []byte
}

// WriteAll publishes several representations of the same content in one
// clipboard transaction, most preferred first.
func WriteAll(items ...Item) <-chan struct{}
```

### Why ordered, and not the map the issue sketched

`map[Format][]byte` reads well but is wrong here. Go randomizes map iteration,
and on Windows and macOS the **order** representations are set in is exactly
what tells a consuming application which one to prefer: the first
`SetClipboardData` and the first `setData:forType:` win. A map would make "which
format the paste target picks" vary run to run for the same program. A slice
makes preference explicit and stable.

`Item` is a struct rather than a `(Format, []byte)` pair list because it is the
thing the API is about, and because it leaves room to grow (per-item options)
without a breaking change.

### Semantics

- **Order is preference**, most preferred first.
- **Duplicate formats**: the first occurrence wins, later ones are dropped, so
  the rule stays "earlier is stronger" rather than "last writer wins".
- **`FmtImage` items are normalized to PNG** exactly as `Write` does, per item.
- **No items**: nothing is written and the returned channel is nil, matching
  `Write`'s failure return.
- The returned channel keeps `Write`'s contract: one value then closed, when the
  whole set is replaced by another writer.
- `Write(f, b)` is now `WriteAll(Item{f, b})`. One code path, so a single-format
  write cannot drift from a multi-format one.

## 3. Per-platform mechanics

Every backend already opens or owns the clipboard once per write. The change is
to put N payloads inside that one transaction rather than one.

| Backend | Mechanism |
|---|---|
| Windows | one `OpenClipboard`/`EmptyClipboard`, then `SetClipboardData` per format, then `CloseClipboard`. |
| macOS | one `clearContents`, then `setData:forType:` per resolved pasteboard type. |
| X11 | the selection owner advertises every target in `TARGETS` and serves whichever a requestor asks for. |
| Wayland | the data source `offer`s every MIME type and serves the payload matching the type named in each `send` event. |
| iOS, Android, CGO-disabled | no multi-representation clipboard (or no clipboard at all): publish the most preferred item alone. |

The X11 and Wayland owner loops live in this repository — `serveSelection` in
`clipboard_x11.go` and `wlServeSend` in `clipboard_wayland_linux.go` — so this
needs no change to the `golang.design/x/x11` module.

On the mobile and CGO-disabled backends, publishing only the first item is the
honest degradation. Writing each item in turn would be worse than useless: each
write replaces the last, so the *least* preferred representation would win,
inverting the ordering the caller asked for.

## 4. Native-type resolution is unchanged

Each item's format resolves to its native type exactly as a single write does —
`windowsNativeNames` and `darwinNativeTypes` (#160), the target atom on X11, the
MIME string on Wayland. `WriteAll` composes the existing resolution; it does not
add a second way to name a type.

## 5. Testing

`TestWriteAll` (all desktop backends) publishes `text/html` and `FmtText`
together and asserts **both** read back — which is the thing that cannot happen
before this change, since the second `Write` would have dropped the first.
`TestWriteAllOrderIsPreference` and `TestWriteAllNormalizesImages` cover
duplicate-format precedence and per-item PNG normalization. `Write`'s existing
suite covers the single-item path, which now runs through the same code.

## 6. Scope

Out: a `[]string` file-list helper (#152), the `PRIMARY` selection (#67), and
any change to `Read`, which already reads one format at a time and needs none.

## 7. Outcome

Implemented as designed across all four desktop backends. `Write` is a one-item
`WriteAll`.
