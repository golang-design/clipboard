// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// sensitiveMarker is the format a password manager adds on this platform to
// say that what it copied is secret.
func sensitiveMarker(t *testing.T) clipboard.Format {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		return clipboard.Register("org.nspasteboard.ConcealedType")
	case "windows":
		return clipboard.Register("ExcludeClipboardContentFromMonitorProcessing")
	case "linux", "freebsd", "openbsd", "netbsd":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			t.Skip("Wayland is covered cross-process (clipboard_sensitive_wayland_test.go)")
		}
		return clipboard.Register("x-kde-passwordManagerHint")
	}
	t.Skipf("%s has no marker for sensitive content", runtime.GOOS)
	return 0
}

// copySecret copies text the way a password manager does: with the marker.
func copySecret(t *testing.T, ctx context.Context, text string) {
	t.Helper()
	if _, err := clipboard.WriteAll(ctx,
		clipboard.Item{Format: clipboard.FmtText, Bytes: []byte(text)},
		clipboard.Item{Format: sensitiveMarker(t), Bytes: []byte("secret")},
	); err != nil {
		t.Fatalf("copying a secret: %v", err)
	}
}

// TestSensitive is #178: a password copied from a password manager carries
// a marker, and a tool that keeps or syncs the clipboard must be able to see
// it. Formats dropped every such marker, since none is MIME-shaped.
func TestSensitive(t *testing.T) {
	skipWithoutClipboard(t)
	ctx := context.Background()
	sensitiveMarker(t) // skips where there is none

	copySecret(t, ctx, "hunter2")
	if secret, err := clipboard.Sensitive(ctx); err != nil || !secret {
		t.Fatalf("after copying a marked password: Sensitive() = %v, %v; want true", secret, err)
	}

	if _, err := clipboard.Write(ctx, clipboard.FmtText, []byte("just text")); err != nil {
		t.Fatal(err)
	}
	if secret, err := clipboard.Sensitive(ctx); err != nil || secret {
		t.Fatalf("after copying plain text: Sensitive() = %v, %v; want false", secret, err)
	}
}

// TestWatchReportsSensitive: Watch is how a syncing tool sees each copy, so
// each value it delivers says whether it was marked.
func TestWatchReportsSensitive(t *testing.T) {
	skipWithoutClipboard(t)
	sensitiveMarker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ch := clipboard.Watch(ctx, clipboard.FmtText)
	time.Sleep(time.Second) // let the watcher take its baseline

	next := func(want string) clipboard.Data {
		t.Helper()
		for {
			select {
			case d, ok := <-ch:
				if !ok {
					t.Fatalf("watch closed before %q arrived", want)
				}
				if string(d.Bytes) == want {
					return d
				}
			case <-ctx.Done():
				t.Fatalf("%q never arrived", want)
			}
		}
	}

	copySecret(t, ctx, "watched-secret")
	if d := next("watched-secret"); !d.Sensitive {
		t.Error("a marked password arrived with Sensitive false")
	}
	if _, err := clipboard.Write(ctx, clipboard.FmtText, []byte("watched-plain")); err != nil {
		t.Fatal(err)
	}
	if d := next("watched-plain"); d.Sensitive {
		t.Error("plain text arrived with Sensitive true")
	}
}

// TestSensitiveUnsupported: where no marker convention can be read, say so
// rather than answer false, which a caller would take as "safe to keep".
func TestSensitiveUnsupported(t *testing.T) {
	switch runtime.GOOS {
	case "js", "android", "ios":
	default:
		t.Skipf("%s reads markers", runtime.GOOS)
	}
	if _, err := clipboard.Sensitive(context.Background()); !errors.Is(err, clipboard.ErrUnsupported) {
		t.Fatalf("Sensitive() error = %v, want ErrUnsupported", err)
	}
}
