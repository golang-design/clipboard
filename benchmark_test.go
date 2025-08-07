// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.

package clipboard

import (
	"context"
	"crypto/rand"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Comprehensive benchmark suite for clipboard operations

func BenchmarkWrite(b *testing.B) {
	err := Init()
	if err != nil {
		b.Skip("clipboard unavailable:", err)
	}

	testSizes := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
		{"10MB", 10 * 1024 * 1024},
	}

	for _, size := range testSizes {
		data := make([]byte, size.size)
		rand.Read(data)

		b.Run("Text_"+size.name, func(b *testing.B) {
			b.SetBytes(int64(size.size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Write(FmtText, data)
			}
		})
	}
}

func BenchmarkRead(b *testing.B) {
	err := Init()
	if err != nil {
		b.Skip("clipboard unavailable:", err)
	}

	// Pre-write test data
	testData := make([]byte, 1024)
	rand.Read(testData)
	Write(FmtText, testData)

	b.Run("Text", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			data := Read(FmtText)
			_ = data
		}
	})
}

func BenchmarkConcurrentOperations(b *testing.B) {
	err := Init()
	if err != nil {
		b.Skip("clipboard unavailable:", err)
	}

	testData := make([]byte, 1024)
	rand.Read(testData)

	concurrencyLevels := []int{1, 2, 4, 8, 16}

	for _, concurrency := range concurrencyLevels {
		b.Run("Write_Concurrent_"+string(rune(concurrency+'0')), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			
			var wg sync.WaitGroup
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					wg.Add(1)
					go func() {
						defer wg.Done()
						Write(FmtText, testData)
					}()
				}
			})
			wg.Wait()
		})
	}
}

func BenchmarkImageOperations(b *testing.B) {
	err := Init()
	if err != nil {
		b.Skip("clipboard unavailable:", err)
	}

	// Create test image
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 255, 255})
		}
	}

	// Encode to PNG
	var imgData []byte
	buf := make([]byte, 0, 10000)
	w := &bytesWriter{buf: buf}
	png.Encode(w, img)
	imgData = w.buf

	b.Run("ImageWrite", func(b *testing.B) {
		b.SetBytes(int64(len(imgData)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Write(FmtImage, imgData)
		}
	})

	// Pre-write image for read test
	Write(FmtImage, imgData)

	b.Run("ImageRead", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			data := Read(FmtImage)
			_ = data
		}
	})
}

func BenchmarkWatch(b *testing.B) {
	err := Init()
	if err != nil {
		b.Skip("clipboard unavailable:", err)
	}

	b.Run("WatchSetup", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
			ch := Watch(ctx, FmtText)
			// Drain channel
			go func() {
				for range ch {
				}
			}()
			cancel()
		}
	})
}

func BenchmarkInit(b *testing.B) {
	b.Run("InitCall", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Test repeated Init calls (should be fast due to sync.Once)
			err := Init()
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMemoryUsage(b *testing.B) {
	err := Init()
	if err != nil {
		b.Skip("clipboard unavailable:", err)
	}

	sizes := []int{1024, 10 * 1024, 100 * 1024, 1024 * 1024}

	for _, size := range sizes {
		data := make([]byte, size)
		rand.Read(data)

		b.Run("MemoryProfile_"+formatSize(size), func(b *testing.B) {
			var m1, m2 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m1)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Write(FmtText, data)
				result := Read(FmtText)
				_ = result
			}
			b.StopTimer()

			runtime.GC()
			runtime.ReadMemStats(&m2)

			b.ReportMetric(float64(m2.Alloc-m1.Alloc)/float64(b.N), "alloc-bytes/op")
			b.ReportMetric(float64(m2.Mallocs-m1.Mallocs)/float64(b.N), "allocs/op")
		})
	}
}

// Helper types and functions

type bytesWriter struct {
	buf []byte
}

func (w *bytesWriter) Write(p []byte) (n int, err error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func formatSize(size int) string {
	if size >= 1024*1024 {
		return string(rune(size/(1024*1024)+'0')) + "MB"
	}
	if size >= 1024 {
		return string(rune(size/1024+'0')) + "KB"
	}
	return string(rune(size+'0')) + "B"
}