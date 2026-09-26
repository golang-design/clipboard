// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard_test

import (
	"context"
	"testing"

	"golang.design/x/clipboard"
)

// TestSensitiveWindowsOptOut: CanIncludeInClipboardHistory and
// CanUploadToCloudClipboard mark content as sensitive only when they are 0;
// set to anything else, they allow what they name.
func TestSensitiveWindowsOptOut(t *testing.T) {
	skipWithoutClipboard(t)
	ctx := context.Background()
	for _, name := range []string{"CanIncludeInClipboardHistory", "CanUploadToCloudClipboard"} {
		for value, want := range map[uint32]bool{0: true, 1: false} {
			dword := []byte{byte(value), 0, 0, 0}
			if _, err := clipboard.WriteAll(ctx,
				clipboard.Item{Format: clipboard.FmtText, Bytes: []byte("text")},
				clipboard.Item{Format: clipboard.Register(name), Bytes: dword},
			); err != nil {
				t.Fatal(err)
			}
			if got, err := clipboard.Sensitive(ctx); err != nil || got != want {
				t.Errorf("%s=%d: Sensitive() = %v, %v; want %v", name, value, got, err, want)
			}
		}
	}
}
