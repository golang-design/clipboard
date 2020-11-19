// Copyright 2021 The golang.design Initiative authors.
// All rights reserved. Use of this source code is governed
// by a GNU GPL-3 license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard_test

import (
	"os"
	"reflect"
	"testing"

	"golang.design/x/clipboard"
)

func TestClipboard(t *testing.T) {

	t.Run("image", func(t *testing.T) {
		data, err := os.ReadFile("testdata/clipboard.png")
		if err != nil {
			t.Fatalf("failed to read gold file: %v", err)
		}
		clipboard.Write(clipboard.MIMEImage, data)

		b := clipboard.Read(clipboard.MIMEText)
		if b != nil {
			t.Fatalf("read clipboard that stores image data as text should fail, but got len: %d", len(b))
		}

		b = clipboard.Read(clipboard.MIMEImage)
		if b == nil {
			t.Fatalf("read clipboard that stores image data as image should success, but got: nil")
		}

		if !reflect.DeepEqual(data, b) {
			t.Fatalf("read data from clipbaord is inconsistent with previous written data, got: %d, want: %d", len(b), len(data))
		}
	})

	t.Run("text", func(t *testing.T) {
		data := []byte("golang.design/x/clipboard")
		clipboard.Write(clipboard.MIMEText, data)

		b := clipboard.Read(clipboard.MIMEImage)
		if b != nil {
			t.Fatalf("read clipboard that stores text data as image should fail, but got len: %d", len(b))
		}
		b = clipboard.Read(clipboard.MIMEText)
		if b == nil {
			t.Fatal("read clipboard taht stores text data as text should success, but got: nil")
		}

		if !reflect.DeepEqual(data, b) {
			t.Fatalf("read data from clipbaord is inconsistent with previous written data, got: %d, want: %d", len(b), len(data))
		}
	})
}

func BenchmarkClipboard(b *testing.B) {

	b.Run("text", func(b *testing.B) {
		data := []byte("golang.design/x/clipboard")

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			clipboard.Write(clipboard.MIMEText, data)
			_ = clipboard.Read(clipboard.MIMEText)
		}
	})
}
