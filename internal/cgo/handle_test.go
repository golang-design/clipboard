package cgo

import (
	"testing"
)

func TestValueHandle(t *testing.T) {
	v := 42

	h1 := NewHandle(v)
	h2 := NewHandle(v)

	if uintptr(h1) == uintptr(h2) {
		t.Fatalf("duplicated Go values should have different handles")
	}

	h1v := h1.Value().(int)
	h2v := h2.Value().(int)
	if h1v != h2v {
		t.Fatalf("the Value of duplicated Go values are different: want %d, got %d", h1v, h2v)
	}
	if h1v != v {
		t.Fatalf("the Value of a handle does not match origin: want %v, got %v", v, h1v)
	}

	h1.Delete()
	h2.Delete()

	siz := 0
	m.Range(func(k, v interface{}) bool {
		siz++
		return true
	})
	if siz != 0 {
		t.Fatalf("handles are not deleted, want: %d, got %d", 0, siz)
	}
}

func TestPointerHandle(t *testing.T) {
	v := 42

	p1 := &v
	p2 := &v

	h1 := NewHandle(p1)
	h2 := NewHandle(p2)

	if uintptr(h1) != uintptr(h2) {
		t.Fatalf("pointers to the same value should have same handle")
	}

	h1v := h1.Value().(*int)
	h2v := h2.Value().(*int)
	if h1v != h2v {
		t.Fatalf("the Value of a handle does not match origin: want %v, got %v", v, h1v)
	}

	h1.Delete()

	siz := 0
	m.Range(func(k, v interface{}) bool {
		siz++
		return true
	})
	if siz != 0 {
		t.Fatalf("handles are not deleted: want %d, got %d", 0, siz)
	}

	defer func() {
		if r := recover(); r != nil {
			return
		}
		t.Fatalf("double Delete on a same handle did not trigger a panic")
	}()

	h2.Delete()
}

func TestNilHandle(t *testing.T) {
	var v *int

	defer func() {
		if r := recover(); r != nil {
			return
		}
		t.Fatalf("nil should not be created as a handle successfully")
	}()

	_ = NewHandle(v)
}

func f1() {}
func f2() {}

type foo struct{}

func (f *foo) bar() {}
func (f *foo) wow() {}

func TestFuncHandle(t *testing.T) {
	h1 := NewHandle(f1)
	h2 := NewHandle(f2)
	h3 := NewHandle(f2)

	if h1 == h2 {
		t.Fatalf("different functions should have different handles")
	}
	if h2 != h3 {
		t.Fatalf("same functions should have same handles")
	}

	f := foo{}
	h4 := NewHandle(f.bar)
	h5 := NewHandle(f.bar)
	h6 := NewHandle(f.wow)

	if h4 != h5 {
		t.Fatalf("same methods should have same handles")
	}

	if h5 == h6 {
		t.Fatalf("different methods should have different handles")
	}
}
func BenchmarkHandle(b *testing.B) {
	b.Run("non-concurrent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			h := NewHandle(i)
			_ = h.Value()
			h.Delete()
		}
	})
	b.Run("concurrent", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			var v int
			for pb.Next() {
				h := NewHandle(v)
				_ = h.Value()
				h.Delete()
			}
		})
	})
}
