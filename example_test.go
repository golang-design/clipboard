// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build cgo

package clipboard_test

import (
	"context"
	"fmt"
	"time"

	"golang.design/x/clipboard"
)

func ExampleWrite() {
	err := clipboard.Init()
	if err != nil {
		panic(err)
	}

	if _, err := clipboard.Write(context.Background(), clipboard.FmtText, []byte("Hello, 世界")); err != nil {
		panic(err)
	}
	// Output:
}

func ExampleRead() {
	err := clipboard.Init()
	if err != nil {
		panic(err)
	}

	b, err := clipboard.Read(context.Background(), clipboard.FmtText)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
	// Output:
	// Hello, 世界
}

func ExampleWatch() {
	err := clipboard.Init()
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	changed := clipboard.Watch(context.Background(), clipboard.FmtText)
	go func(ctx context.Context) {
		clipboard.Write(ctx, clipboard.FmtText, []byte("你好，world"))
	}(ctx)
	fmt.Println(string((<-changed).Bytes))
	// Output:
	// 你好，world
}

// ExampleRegister shows the escape hatch for bytes that must not be
// transcoded. FmtImage is a PNG format, not a generic image format: it
// re-encodes whatever it is given. A registered MIME type is raw passthrough,
// so the exact bytes go on the clipboard and come back unchanged.
//
// Register needs no Init — it only allocates the token.
func ExampleRegister() {
	jpg := clipboard.Register("image/jpeg")

	fmt.Println(clipboard.FmtImage.MIME()) // re-encoded to this, always
	fmt.Println(jpg.MIME())                // exchanged verbatim
	// Output:
	// image/png
	// image/jpeg
}
