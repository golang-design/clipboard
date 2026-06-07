// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// TestWatchNoGoroutineLeakOnCancel ensures a watcher whose consumer has stopped
// reading still exits when its context is canceled, instead of blocking forever
// on the send (#153). It starts several watchers, never reads them, drives enough
// clipboard changes that each fills its buffer and blocks on the next send, then
// cancels all and asserts the goroutines return to baseline.
func TestWatchNoGoroutineLeakOnCancel(t *testing.T) {
	if degradesWithoutCgo() {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}
	if err := clipboard.Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}

	settle := func() int {
		for i := 0; i < 8; i++ {
			runtime.GC()
			time.Sleep(50 * time.Millisecond)
		}
		return runtime.NumGoroutine()
	}
	base := settle()

	const n = 15
	cancels := make([]context.CancelFunc, n)
	for i := range cancels {
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		clipboard.Watch(ctx, clipboard.FmtText) // never read the channel
	}

	// Continuously change the clipboard (across several poll intervals) so every
	// watcher detects a change and fills its size-1 buffer, then detects another
	// and blocks trying to send it (nobody is reading).
	for i := 0; i < 16; i++ {
		clipboard.Write(clipboard.FmtText, fmt.Appendf(nil, "leak-probe-%d", i))
		time.Sleep(250 * time.Millisecond)
	}

	for _, cancel := range cancels {
		cancel()
	}

	// With the fix the watcher goroutines exit; poll for the count to return to
	// baseline (with tolerance for unrelated churn). Without it, ~n goroutines
	// stay blocked on the send forever.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := settle(); got <= base+n/3 {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("watch goroutines leaked after cancel: baseline %d, now %d (started %d watchers)", base, got, n)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
