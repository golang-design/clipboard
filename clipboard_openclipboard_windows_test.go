// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard

import (
	"runtime"
	"testing"
	"time"
)

// TestOpenClipboardRetryTimeout verifies openClipboardRetry returns an error
// when the clipboard is held open by someone else, instead of busy-waiting
// forever (#144). It holds the clipboard open on a separate OS-locked goroutine,
// then asserts openClipboardRetry gives up within the (shortened) timeout rather
// than hanging.
func TestOpenClipboardRetryTimeout(t *testing.T) {
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		for {
			if r, _, _ := openClipboard.Call(0); r != 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		close(held)
		<-release
		closeClipboard.Call()
	}()
	<-held
	defer close(release)

	old := clipboardOpenTimeout
	clipboardOpenTimeout = 300 * time.Millisecond
	defer func() { clipboardOpenTimeout = old }()

	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := openClipboardRetry()
		if err == nil {
			closeClipboard.Call() // unexpectedly opened; don't leak it
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("openClipboardRetry succeeded while the clipboard was held; expected a timeout error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("openClipboardRetry did not return while the clipboard was held (#144 busy-wait)")
	}
}
