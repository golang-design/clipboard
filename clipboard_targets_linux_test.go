// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

package clipboard

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestClipboardTargetsReflectWrittenFormat is a regression test for #60.
//
// On a TARGETS request the selection owner must advertise the format it
// actually holds. Before #60 the owner always replied with both UTF8_STRING
// and image/png regardless of what was written, so after writing text a
// requestor would still see image/png advertised (and get nothing if it
// asked for it). This test writes text and then enumerates the clipboard's
// advertised TARGETS via xclip, asserting they reflect the written format.
//
// It uses xclip as an independent X11 requestor because cgo cannot be used
// directly from a _test.go file.
func TestClipboardTargetsReflectWrittenFormat(t *testing.T) {
	if _, err := exec.LookPath("xclip"); err != nil {
		t.Skip("xclip not found; skipping TARGETS regression test")
	}
	if err := Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}

	// Become the CLIPBOARD owner with text data.
	if ch, _ := Write(context.TODO(), FmtText, []byte("targets-regression-#60")); ch == nil {
		t.Fatal("Write returned nil channel")
	}

	// Ask the owner for the list of available targets. Retry briefly in case
	// selection ownership has not settled yet.
	var out []byte
	var err error
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		out, err = exec.CommandContext(ctx,
			"xclip", "-selection", "clipboard", "-t", "TARGETS", "-o").Output()
		cancel()
		if err == nil && len(out) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("xclip TARGETS query failed: %v", err)
	}

	targets := map[string]bool{}
	for _, name := range strings.Fields(string(out)) {
		targets[name] = true
	}
	t.Logf("advertised TARGETS: %v", targets)

	if !targets["UTF8_STRING"] {
		t.Errorf("TARGETS should advertise UTF8_STRING after writing text; got %v", targets)
	}
	if targets["image/png"] {
		t.Errorf("TARGETS must not advertise image/png after writing text only (regression #60); got %v", targets)
	}
}
