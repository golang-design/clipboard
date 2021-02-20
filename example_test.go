package clipboard_test

import (
	"context"
	"fmt"
	"time"

	"golang.design/x/clipboard"
)

func ExampleWrite() {
	clipboard.Write(clipboard.FmtText, []byte("Hello, 世界"))
	// Output:
}

func ExampleRead() {
	fmt.Printf(string(clipboard.Read(clipboard.FmtText)))
	// Output:
	// Hello, 世界
}

func ExampleWatch() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	changed := clipboard.Watch(context.Background(), clipboard.FmtText)
	go func(ctx context.Context) {
		clipboard.Write(clipboard.FmtText, []byte("你好，world"))
	}(ctx)
	fmt.Printf(string(<-changed))
	// Output:
	// 你好，world
}
