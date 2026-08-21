// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"golang.design/x/clipboard"
)

func errorsReady(t *testing.T) {
	t.Helper()
	if degradesWithoutCgo() {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}
	if err := clipboard.Init(); err != nil {
		t.Skipf("clipboard unavailable: %v", err)
	}
}

// TestReadReportsNoData is the wart this release fixes: a read of a format the
// clipboard does not hold used to return a bare nil, indistinguishable from a
// clipboard that could not be reached at all. There was an unexported debug flag
// in the package whose only job was printing the difference to stderr.
func TestReadReportsNoData(t *testing.T) {
	errorsReady(t)

	// Put text on the clipboard, then ask for a format nothing has written.
	clipboard.Write(context.Background(), clipboard.FmtText, []byte("only text here"))
	absent := clipboard.Register("application/x.golang-design.clipboard-absent")

	got, err := clipboard.Read(context.Background(), absent)
	if got != nil {
		t.Fatalf("Read of an absent format = %q, want nil", got)
	}
	if !errors.Is(err, clipboard.ErrNoData) {
		t.Fatalf("Read of an absent format returned %v, want ErrNoData", err)
	}
}

// TestReadHonorsCancelledContext checks the context is consulted before any work
// happens, so a caller that has already given up does not start clipboard I/O.
func TestReadHonorsCancelledContext(t *testing.T) {
	errorsReady(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := clipboard.Read(ctx, clipboard.FmtText); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read with a cancelled context returned %v, want context.Canceled", err)
	}
	if _, err := clipboard.Write(ctx, clipboard.FmtText, []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Write with a cancelled context returned %v, want context.Canceled", err)
	}
	if _, err := clipboard.Formats(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Formats with a cancelled context returned %v, want context.Canceled", err)
	}
}

// TestErrorsAreDistinguishable checks the sentinels do not collapse into each
// other, which is the only reason to have more than one.
func TestErrorsAreDistinguishable(t *testing.T) {
	if errors.Is(clipboard.ErrNoData, clipboard.ErrUnavailable) ||
		errors.Is(clipboard.ErrUnavailable, clipboard.ErrNoData) {
		t.Fatal("ErrNoData and ErrUnavailable compare equal; a caller cannot tell an empty clipboard from an unreachable one")
	}
	if errors.Is(clipboard.ErrUnsupported, clipboard.ErrUnavailable) ||
		errors.Is(clipboard.ErrUnavailable, clipboard.ErrUnsupported) {
		t.Fatal("ErrUnsupported and ErrUnavailable compare equal")
	}
}
