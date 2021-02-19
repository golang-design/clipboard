// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

func TestXX(t *testing.T) {
	data, err := os.ReadFile("testdata/clipboard.png")
	if err != nil {
		t.Fatalf("failed to read gold file: %v", err)
	}
	clipboard.Write(clipboard.FmtImage, data)
}

func TestClipboard(t *testing.T) {
	t.Run("image", func(t *testing.T) {
		data, err := os.ReadFile("testdata/clipboard.png")
		if err != nil {
			t.Fatalf("failed to read gold file: %v", err)
		}
		clipboard.Write(clipboard.FmtImage, data)

		b := clipboard.Read(clipboard.FmtText)
		if b != nil {
			t.Fatalf("read clipboard that stores image data as text should fail, but got len: %d", len(b))
		}

		b = clipboard.Read(clipboard.FmtImage)
		if b == nil {
			t.Fatalf("read clipboard that stores image data as image should success, but got: nil")
		}

		if !reflect.DeepEqual(data, b) {
			t.Fatalf("read data from clipbaord is inconsistent with previous written data, got: %d, want: %d", len(b), len(data))
		}
	})

	t.Run("text", func(t *testing.T) {
		data := []byte("golang.design/x/clipboard")
		clipboard.Write(clipboard.FmtText, data)

		b := clipboard.Read(clipboard.FmtImage)
		if b != nil {
			t.Fatalf("read clipboard that stores text data as image should fail, but got len: %d", len(b))
		}
		b = clipboard.Read(clipboard.FmtText)
		if b == nil {
			t.Fatal("read clipboard taht stores text data as text should success, but got: nil")
		}

		if !reflect.DeepEqual(data, b) {
			t.Fatalf("read data from clipbaord is inconsistent with previous written data, got: %d, want: %d", len(b), len(data))
		}
	})
}

func TestClipboardMultipleWrites(t *testing.T) {
	data, err := os.ReadFile("testdata/clipboard.png")
	if err != nil {
		t.Fatalf("failed to read gold file: %v", err)
	}
	chg := clipboard.Write(clipboard.FmtImage, data)

	data = []byte("golang.design/x/clipboard")
	clipboard.Write(clipboard.FmtText, data)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatalf("failed to receive clipboard change notification")
	case _, ok := <-chg:
		if !ok {
			t.Fatalf("change channel is closed before receiving the changed clipboard data")
		}
	}
	_, ok := <-chg
	if ok {
		t.Fatalf("changed channel should be closed after receiving the notification")
	}

	b := clipboard.Read(clipboard.FmtImage)
	if b != nil {
		t.Fatalf("read clipboard that should store text data as image should fail, but got: %d", len(b))
	}

	b = clipboard.Read(clipboard.FmtText)
	if b == nil {
		t.Fatalf("read clipboard that should store text data as text should success, got: nil")
	}

	if !reflect.DeepEqual(data, b) {
		t.Fatalf("read data from clipbaord is inconsistent with previous write, want %s, got: %s", string(data), string(b))
	}
}

func TestClipboardConcurrentRead(t *testing.T) {
	// This test check that concurrent read/write to the clipboard does
	// not cause crashes on some specific platform, such as macOS.
	done := make(chan bool, 2)
	go func() {
		defer func() {
			done <- true
		}()
		clipboard.Read(clipboard.FmtText)
	}()
	go func() {
		defer func() {
			done <- true
		}()
		clipboard.Read(clipboard.FmtImage)
	}()
	<-done
	<-done
}

func TestClipboardWriteEmpty(t *testing.T) {
	chg1 := clipboard.Write(clipboard.FmtText, nil)
	if got := clipboard.Read(clipboard.FmtText); got != nil {
		t.Fatalf("write nil to clipboard should read nil, got: %v", string(got))
	}
	clipboard.Write(clipboard.FmtText, []byte(""))
	<-chg1

	if got := clipboard.Read(clipboard.FmtText); string(got) != "" {
		t.Fatalf("write empty string to clipboard should read empty string, got: `%v`", string(got))
	}
}

func TestClipboardWatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	// clear clipboard
	clipboard.Write(clipboard.FmtText, []byte(""))
	lastRead := clipboard.Read(clipboard.FmtText)

	changed := clipboard.Watch(ctx, clipboard.FmtText)

	want := []byte("golang.design/x/clipboard")
	go func(ctx context.Context) {
		t := time.NewTicker(time.Millisecond * 500)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				clipboard.Write(clipboard.FmtText, want)
			}
		}
	}(ctx)
	for {
		select {
		case <-ctx.Done():
			if string(lastRead) == "" {
				t.Fatalf("clipboard watch never receives a notification")
			}
			t.Log(string(lastRead))
			return
		case data, ok := <-changed:
			if !ok {
				if string(lastRead) == "" {
					t.Fatalf("clipboard watch never receives a notification")
				}
				return
			}
			if bytes.Compare(data, want) != 0 {
				t.Fatalf("received data from watch mismatch, want: %v, got %v", string(want), string(data))
			}
			lastRead = data
		}
	}
}

func BenchmarkClipboard(b *testing.B) {

	b.Run("text", func(b *testing.B) {
		data := []byte("golang.design/x/clipboard")

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			clipboard.Write(clipboard.FmtText, data)
			_ = clipboard.Read(clipboard.FmtText)
		}
	})
}
