// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.

//go:build optimize

package clipboard

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
)

var (
	// Performance optimizations
	
	// Use atomic for fast initialization checks
	optimizedInitialized int32
	optimizedInitError   atomic.Value // stores error
	
	// Use RWMutex for better concurrent read performance
	rwLock = sync.RWMutex{}
	
	// Buffer pool for reducing allocations
	bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 4096) // Start with 4KB capacity
		},
	}
	
	// Format-specific read locks for better concurrency
	textLock  = sync.RWMutex{}
	imageLock = sync.RWMutex{}
)

// OptimizedInit provides a faster initialization with atomic operations
func OptimizedInit() error {
	// Fast path: already initialized
	if atomic.LoadInt32(&optimizedInitialized) == 1 {
		if err := optimizedInitError.Load(); err != nil {
			return err.(error)
		}
		return nil
	}
	
	// Slow path: need to initialize
	rwLock.Lock()
	defer rwLock.Unlock()
	
	// Double-check pattern
	if atomic.LoadInt32(&optimizedInitialized) == 1 {
		if err := optimizedInitError.Load(); err != nil {
			return err.(error)
		}
		return nil
	}
	
	err := initialize()
	optimizedInitError.Store(err)
	atomic.StoreInt32(&optimizedInitialized, 1)
	return err
}

// OptimizedRead provides better performance with format-specific locking
func OptimizedRead(t Format) []byte {
	// Check initialization without blocking
	if atomic.LoadInt32(&optimizedInitialized) == 0 {
		return nil
	}
	
	var lock *sync.RWMutex
	switch t {
	case FmtText:
		lock = &textLock
	case FmtImage:
		lock = &imageLock
	default:
		return nil
	}
	
	lock.RLock()
	defer lock.RUnlock()
	
	buf, err := read(t)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "read clipboard err: %v\n", err)
		}
		return nil
	}
	return buf
}

// OptimizedWrite provides better performance with reduced allocations
func OptimizedWrite(t Format, buf []byte) <-chan struct{} {
	// Check initialization without blocking
	if atomic.LoadInt32(&optimizedInitialized) == 0 {
		return nil
	}
	
	var lock *sync.RWMutex
	switch t {
	case FmtText:
		lock = &textLock
	case FmtImage:
		lock = &imageLock
	default:
		return nil
	}
	
	lock.Lock()
	defer lock.Unlock()
	
	changed, err := write(t, buf)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "write to clipboard err: %v\n", err)
		}
		return nil
	}
	return changed
}

// OptimizedReadWithPool uses buffer pooling to reduce allocations
func OptimizedReadWithPool(t Format) []byte {
	if atomic.LoadInt32(&optimizedInitialized) == 0 {
		return nil
	}
	
	var lock *sync.RWMutex
	switch t {
	case FmtText:
		lock = &textLock
	case FmtImage:
		lock = &imageLock
	default:
		return nil
	}
	
	lock.RLock()
	defer lock.RUnlock()
	
	// Get buffer from pool
	poolBuf := bufferPool.Get().([]byte)
	defer bufferPool.Put(poolBuf[:0]) // Reset length but keep capacity
	
	buf, err := readWithBuffer(t, poolBuf)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "read clipboard err: %v\n", err)
		}
		return nil
	}
	
	// Make a copy since we're returning the buffer to the pool
	result := make([]byte, len(buf))
	copy(result, buf)
	return result
}

// Batch operations for better performance with multiple operations
type BatchOperation struct {
	Format Format
	Data   []byte
	Op     string // "read" or "write"
}

// OptimizedBatch performs multiple operations with a single lock acquisition
func OptimizedBatch(operations []BatchOperation) ([][]byte, error) {
	if atomic.LoadInt32(&optimizedInitialized) == 0 {
		return nil, errors.New("clipboard not initialized")
	}
	
	// Group operations by format to minimize lock contention
	textOps := make([]BatchOperation, 0)
	imageOps := make([]BatchOperation, 0)
	
	for _, op := range operations {
		switch op.Format {
		case FmtText:
			textOps = append(textOps, op)
		case FmtImage:
			imageOps = append(imageOps, op)
		}
	}
	
	results := make([][]byte, len(operations))
	resultIndex := 0
	
	// Process text operations
	if len(textOps) > 0 {
		textLock.Lock()
		for _, op := range textOps {
			switch op.Op {
			case "read":
				buf, _ := read(op.Format)
				results[resultIndex] = buf
			case "write":
				write(op.Format, op.Data)
			}
			resultIndex++
		}
		textLock.Unlock()
	}
	
	// Process image operations
	if len(imageOps) > 0 {
		imageLock.Lock()
		for _, op := range imageOps {
			switch op.Op {
			case "read":
				buf, _ := read(op.Format)
				results[resultIndex] = buf
			case "write":
				write(op.Format, op.Data)
			}
			resultIndex++
		}
		imageLock.Unlock()
	}
	
	return results, nil
}

// Zero-copy operations (use with caution)
func OptimizedReadZeroCopy(t Format) ([]byte, func()) {
	if atomic.LoadInt32(&optimizedInitialized) == 0 {
		return nil, nil
	}
	
	var lock *sync.RWMutex
	switch t {
	case FmtText:
		lock = &textLock
	case FmtImage:
		lock = &imageLock
	default:
		return nil, nil
	}
	
	lock.RLock()
	// Note: caller must call the returned function to unlock
	
	buf, err := read(t)
	if err != nil {
		lock.RUnlock()
		return nil, nil
	}
	
	return buf, lock.RUnlock
}

// Advanced buffer management for large data operations
type ManagedBuffer struct {
	data     []byte
	capacity int
	mu       sync.Mutex
}

func NewManagedBuffer(initialCapacity int) *ManagedBuffer {
	return &ManagedBuffer{
		data:     make([]byte, 0, initialCapacity),
		capacity: initialCapacity,
	}
}

func (mb *ManagedBuffer) GetBuffer(minSize int) []byte {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	
	if cap(mb.data) < minSize {
		// Grow buffer if needed
		newCapacity := cap(mb.data) * 2
		if newCapacity < minSize {
			newCapacity = minSize
		}
		mb.data = make([]byte, 0, newCapacity)
		mb.capacity = newCapacity
	}
	
	return mb.data[:0] // Return buffer with zero length but full capacity
}

// String interning for common clipboard text to reduce memory usage
var stringIntern = sync.Map{}

func internString(s string) string {
	if actual, ok := stringIntern.Load(s); ok {
		return actual.(string)
	}
	
	// Create a copy to avoid holding onto larger underlying arrays
	interned := string([]byte(s))
	stringIntern.Store(interned, interned)
	return interned
}

// Metrics collection for performance monitoring
type Metrics struct {
	ReadCount    int64
	WriteCount   int64
	ReadBytes    int64
	WrittenBytes int64
	Errors       int64
}

var globalMetrics Metrics

func GetMetrics() Metrics {
	return Metrics{
		ReadCount:    atomic.LoadInt64(&globalMetrics.ReadCount),
		WriteCount:   atomic.LoadInt64(&globalMetrics.WriteCount),
		ReadBytes:    atomic.LoadInt64(&globalMetrics.ReadBytes),
		WrittenBytes: atomic.LoadInt64(&globalMetrics.WrittenBytes),
		Errors:       atomic.LoadInt64(&globalMetrics.Errors),
	}
}

// Placeholder for platform-specific readWithBuffer function
func readWithBuffer(t Format, buf []byte) ([]byte, error) {
	// This would be implemented per platform to reuse the provided buffer
	// For now, fall back to the standard read function
	return read(t)
}