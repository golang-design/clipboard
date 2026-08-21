// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build js && wasm

package clipboard

import (
	"context"
	"fmt"
	"sync"
	"syscall/js"
)

// The browser backend talks to the asynchronous Clipboard API
// (navigator.clipboard). Two of its constraints belong to the browser rather
// than to this package, and neither can be worked around here:
//
// A read needs a user gesture. Browsers only permit readText() from inside a
// user-initiated event handler with permission granted, so Read called from
// main will be denied. That denial is reported as an error rather than as an
// empty clipboard, which is the whole reason this backend waited for the
// context-and-error API.
//
// A blocking call inside a JS callback deadlocks. Go's wasm scheduler runs on
// the JS event loop, so a js.FuncOf callback must return before the loop can
// deliver a Promise settlement. Read and Write must therefore run on a
// goroutine the handler starts, not directly inside the handler:
//
//	js.FuncOf(func(js.Value, []js.Value) any {
//		go func() { b, err := clipboard.Read(ctx, clipboard.FmtText); ... }()
//		return nil
//	})

// clipboardAPI returns navigator.clipboard, or a falsy value when the browser
// does not expose it — which is what a page served over plain HTTP sees, since
// the Clipboard API requires a secure context.
func clipboardAPI() js.Value {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() {
		return js.Undefined()
	}
	return nav.Get("clipboard")
}

func initialize() error {
	if !clipboardAPI().Truthy() {
		return fmt.Errorf("%w: navigator.clipboard is not available; the Clipboard API "+
			"requires a secure context (https, or localhost during development)", ErrUnavailable)
	}
	return nil
}

// await resolves a JS Promise, giving up when ctx does.
//
// The then-callbacks are js.Func values that must be released, and releasing
// them while the Promise can still settle would call into freed memory. Waiting
// for the settlement before releasing would defeat the context. So the channel
// is buffered and each callback releases once it fires: a cancelled await
// returns immediately, the settlement lands in the buffer whenever it arrives,
// and the release happens then.
func await(ctx context.Context, promise js.Value) (js.Value, error) {
	if !promise.Truthy() {
		return js.Undefined(), ErrUnavailable
	}

	type settled struct {
		value js.Value
		err   error
	}
	ch := make(chan settled, 1)

	var (
		once           sync.Once
		onOK, onReject js.Func
	)
	release := func() { once.Do(func() { onOK.Release(); onReject.Release() }) }

	onOK = js.FuncOf(func(_ js.Value, args []js.Value) any {
		var v js.Value
		if len(args) > 0 {
			v = args[0]
		}
		ch <- settled{value: v}
		release()
		return nil
	})
	onReject = js.FuncOf(func(_ js.Value, args []js.Value) any {
		ch <- settled{err: promiseError(args)}
		release()
		return nil
	})
	promise.Call("then", onOK, onReject)

	select {
	case s := <-ch:
		return s.value, s.err
	case <-ctx.Done():
		return js.Undefined(), ctx.Err()
	}
}

// promiseError turns a rejected Promise's reason into an error. A denial is the
// common one and the one worth naming: it is what a page gets for reading
// without a user gesture or a permission grant, and a caller who cannot tell it
// apart from an empty clipboard will chase the wrong problem.
func promiseError(args []js.Value) error {
	if len(args) == 0 || !args[0].Truthy() {
		return fmt.Errorf("%w: the clipboard request was rejected", ErrUnavailable)
	}
	name := args[0].Get("name").String()
	message := args[0].Get("message").String()
	if name == "NotAllowedError" {
		return fmt.Errorf("%w: the browser denied clipboard access (%s); a read needs "+
			"a user gesture and a permission grant", ErrUnavailable, message)
	}
	return fmt.Errorf("%w: %s: %s", ErrUnavailable, name, message)
}

// enumerateFormats reports nothing. Asking the browser what is on the clipboard
// means calling clipboard.read(), which raises a permission prompt as a side
// effect of asking the question — too rude for an enumeration call.
func enumerateFormats(ctx context.Context, sel selection) []Format { return nil }

func read(ctx context.Context, sel selection, t Format) ([]byte, error) {
	if sel == selPrimary {
		// Browsers have one clipboard (see FromPrimary).
		return nil, errUnsupported
	}
	if t != FmtText {
		// Images and custom formats need ClipboardItem and Blob plumbing, and
		// browser support for them is uneven. Saying so beats returning
		// nothing and letting the caller guess.
		return nil, fmt.Errorf("%w: the browser backend reads text only", ErrUnsupported)
	}
	api := clipboardAPI()
	if !api.Truthy() {
		return nil, initialize()
	}

	v, err := await(ctx, api.Call("readText"))
	if err != nil {
		return nil, err
	}
	s := v.String()
	if s == "" {
		return nil, nil // an empty clipboard, which Read reports as ErrNoData
	}
	return []byte(s), nil
}

func writeAll(ctx context.Context, sel selection, items []Item, loops int) (<-chan struct{}, error) {
	if sel == selPrimary {
		// No second clipboard here, and redirecting to the only one would
		// destroy what the user had copied (see FromPrimary).
		return nil, errUnsupported
	}
	// loops is ignored: the browser clipboard is a store, so no paste request
	// ever reaches this program to be counted (see Loops).
	_ = loops

	it := items[0]
	if it.Format != FmtText {
		return nil, fmt.Errorf("%w: the browser backend writes text only", ErrUnsupported)
	}
	api := clipboardAPI()
	if !api.Truthy() {
		return nil, initialize()
	}

	if _, err := await(ctx, api.Call("writeText", string(it.Bytes))); err != nil {
		return nil, err
	}

	// Nothing in the browser reports that another application replaced the
	// clipboard, so this channel never fires. That is within Write's contract,
	// which says the channel only reports an overwrite and may never do so.
	return make(chan struct{}), nil
}

// watch reports nothing. Browsers have no dependable clipboard-change event —
// the proposed clipboardchange is not something to build on yet — so the
// channel is closed rather than left open on a promise that cannot be kept.
func watch(ctx context.Context, sel selection, t Format) <-chan []byte {
	ch := make(chan []byte)
	close(ch)
	return ch
}
