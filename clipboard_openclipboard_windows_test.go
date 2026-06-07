// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard

import (
	"testing"
	"time"
)

// TestOpenClipboardRetryTimeout verifies openClipboardRetry gives up with an
// error after the timeout, retrying with backoff in between, instead of
// busy-waiting forever when the clipboard never opens (#144). Contention is
// simulated via the openClipboardOnce seam, since a real second holder can only
// be another process.
func TestOpenClipboardRetryTimeout(t *testing.T) {
	oldOpen, oldTimeout := openClipboardOnce, clipboardOpenTimeout
	defer func() { openClipboardOnce, clipboardOpenTimeout = oldOpen, oldTimeout }()

	calls := 0
	openClipboardOnce = func() bool { calls++; return false } // never opens
	clipboardOpenTimeout = 200 * time.Millisecond

	start := time.Now()
	err := openClipboardRetry()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("openClipboardRetry returned nil when the clipboard never opens; expected a timeout error")
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("returned after %v; should keep retrying until ~the %v timeout", elapsed, clipboardOpenTimeout)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took %v; the timeout is not bounding the loop", elapsed)
	}
	if calls < 2 {
		t.Fatalf("openClipboardOnce called %d times; expected multiple backoff retries", calls)
	}
}
