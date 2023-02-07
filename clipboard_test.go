// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

func init() {
	clipboard.Debug = true
}

func TestClipboardInit(t *testing.T) {
	t.Run("no-cgo", func(t *testing.T) {
		if val, ok := os.LookupEnv("CGO_ENABLED"); !ok || val != "0" {
			t.Skip("CGO_ENABLED is set to 1")
		}
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not need to check for cgo")
		}

		defer func() {
			if r := recover(); r != nil {
				return
			}
			t.Fatalf("expect to fail when CGO_ENABLED=0")
		}()

		clipboard.Init()
	})
	t.Run("with-cgo", func(t *testing.T) {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
		if runtime.GOOS != "linux" {
			t.Skip("Only Linux may return error at the moment.")
		}

		if err := clipboard.Init(); err != nil && !errors.Is(err, clipboard.ErrUnavailable) {
			t.Fatalf("expect ErrUnavailable, but got: %v", err)
		}
	})
}

func TestClipboard(t *testing.T) {
	if runtime.GOOS != "windows" {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}

	t.Run("image", func(t *testing.T) {
		data, err := os.ReadFile("tests/testdata/clipboard.png")
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

		img1, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("write image is not png encoded: %v", err)
		}
		img2, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("read image is not png encoded: %v", err)
		}

		w := img2.Bounds().Dx()
		h := img2.Bounds().Dy()

		incorrect := 0
		for i := 0; i < w; i++ {
			for j := 0; j < h; j++ {
				var want, got color.Color
				switch img1.(type) {
				case *image.RGBA:
					want = img1.(*image.RGBA).RGBA64At(i, j)
				case *image.NRGBA:
					want = img1.(*image.NRGBA).RGBA64At(i, j)
				}
				switch img2.(type) {
				case *image.RGBA:
					got = img2.(*image.RGBA).RGBA64At(i, j)
				case *image.NRGBA:
					got = img2.(*image.NRGBA).RGBA64At(i, j)
				}

				if !reflect.DeepEqual(want, got) {
					t.Logf("read data from clipbaord is inconsistent with previous written data, pix: (%d,%d), got: %+v, want: %+v", i, j, got, want)
					incorrect++
				}
			}
		}

		// FIXME: it looks like windows can produce incorrect pixels when y == 0.
		// Needs more investigation.
		if incorrect > w {
			t.Fatalf("read data from clipboard contains too much inconsistent pixels to the previous written data, number of incorrect pixels: %v", incorrect)
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
	if runtime.GOOS != "windows" {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}

	data, err := os.ReadFile("tests/testdata/clipboard.png")
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
	if runtime.GOOS != "windows" {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}

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
	if runtime.GOOS != "windows" {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}

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
	if runtime.GOOS != "windows" {
		if val, ok := os.LookupEnv("CGO_ENABLED"); ok && val == "0" {
			t.Skip("CGO_ENABLED is set to 0")
		}
	}

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
			if !bytes.Equal(data, want) {
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

func TestClipboardNoCgo(t *testing.T) {
	if val, ok := os.LookupEnv("CGO_ENABLED"); !ok || val != "0" {
		t.Skip("CGO_ENABLED is set to 1")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows should always be tested")
	}

	t.Run("Read", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				return
			}
			t.Fatalf("expect to fail when CGO_ENABLED=0")
		}()

		clipboard.Read(clipboard.FmtText)
	})

	t.Run("Write", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				return
			}
			t.Fatalf("expect to fail when CGO_ENABLED=0")
		}()

		clipboard.Write(clipboard.FmtText, []byte("dummy"))
	})

	t.Run("Watch", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				return
			}
			t.Fatalf("expect to fail when CGO_ENABLED=0")
		}()

		clipboard.Watch(context.TODO(), clipboard.FmtText)
	})
}
