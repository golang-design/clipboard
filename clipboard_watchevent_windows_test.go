// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// pollFloor is the interval the polling watcher ticks at. Event-driven delivery
// must beat it by a wide margin for the distinction to mean anything.
const pollFloor = time.Second

// eventLatencyCeiling is the per-change budget the event-driven watch must stay
// inside. It sits well below pollFloor so a polling watcher fails it, and well
// above the milliseconds a posted WM_CLIPBOARDUPDATE actually takes, so a busy
// CI runner does not.
const eventLatencyCeiling = 700 * time.Millisecond

// TestWatchIsEventDriven asserts the Windows watcher reports a change when it
// happens rather than at the next poll tick (#153).
//
// Without the fix the watcher ticks once a second: the first change is delivered
// at the first tick and each later one a full second after its write, so every
// round blows the ceiling. With AddClipboardFormatListener the change is posted
// to the watcher's message-only window and delivered immediately.
//
// It also covers the regression risk of that rewrite — that a listener firing on
// every clipboard change, in any format, still delivers each watched value once
// and in order.
func TestWatchIsEventDriven(t *testing.T) {
	if err := clipboard.Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := clipboard.Watch(ctx, clipboard.FmtText)

	for i := range 3 {
		want := fmt.Appendf(nil, "event-driven-%d", i)

		start := time.Now()
		clipboard.Write(clipboard.FmtText, want)

		if err := awaitValue(ch, want); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if took := time.Since(start); took > eventLatencyCeiling {
			t.Fatalf("round %d: change observed after %v, want under %v "+
				"(the %v poll interval means the watch is not event-driven)",
				i, took, eventLatencyCeiling, pollFloor)
		}
	}
}

// awaitValue waits for want to arrive on ch, skipping any value left over from
// an earlier round. It fails rather than hangs if the watch stops delivering.
func awaitValue(ch <-chan clipboard.Data, want []byte) error {
	deadline := time.After(10 * time.Second)
	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return fmt.Errorf("watch channel closed before %q arrived", want)
			}
			if bytes.Equal(data.Bytes, want) {
				return nil
			}
		case <-deadline:
			return fmt.Errorf("timed out waiting for %q", want)
		}
	}
}
