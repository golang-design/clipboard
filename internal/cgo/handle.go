// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

// Package cgo is an implementation of golang.org/issue/37033.
//
// See golang.org/cl/294670 for code review discussion.
package cgo

import (
	"reflect"
	"sync"
)

// Handle provides a safe representation to communicate Go values between
// C and Go. The zero value of a handle is not a valid handle, and thus
// safe to use as a sentinel in C APIs.
//
// The underlying type of Handle may change, but the value is guaranteed
// to fit in an integer type that is large enough to hold the bit pattern
// of any pointer. For instance, on the Go side:
//
// 	package main
//
// 	/*
// 	extern void GoPrint(unsigned long long handle);
// 	void printFromGo(unsigned long long handle);
// 	*/
// 	import "C"
// 	import "runtime/cgo"
//
// 	//export GoPrint
// 	func GoPrint(handle C.ulonglong) {
// 		h := cgo.Handle(handle)
// 		val := h.Value().(int)
// 		println(val)
// 		h.Delete()
// 	}
//
// 	func main() {
// 		val := 42
//
// 		C.printFromGo(C.ulonglong(cgo.NewHandle(val))) // prints 42
// 	}
//
// and on the C side:
//
// 	// This function is from Go side.
// 	extern void GoPrint(unsigned long long handle);
//
// 	// A C function
// 	void printFromGo(unsigned long long handle) {
// 	    GoPrint(handle);
// 	}
type Handle uintptr

// NewHandle returns a handle for a given value. If a given value is a
// pointer, slice, map, channel, or function that refers to the same
// object, the returned handle will also be the same. Besides, nil value
// must not be used.
//
// The handle is valid until the program calls Delete on it. The handle
// uses resources, and this package assumes that C code may hold on to
// the handle, so a program must explicitly call Delete when the handle
// is no longer needed.
//
// The intended use is to pass the returned handle to C code, which
// passes it back to Go, which calls Value. See an example in the
// comments of the Handle definition.
func NewHandle(v interface{}) Handle {
	var k uintptr

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.UnsafePointer, reflect.Slice,
		reflect.Map, reflect.Chan, reflect.Func:
		if rv.IsNil() {
			panic("cannot use handle for nil value")
		}

		k = rv.Pointer()
	default:
		k = reflect.ValueOf(&v).Pointer()
	}

	// v escapes to the heap, always. As Go do not have a moving GC (and
	// possibly lasts true for a long future), it is safe to use its
	// pointer address as the key of the global map at this moment.
	// The implementation must be reconsidered if moving GC is
	// introduced internally.
	actual, loaded := m.LoadOrStore(k, v)
	if !loaded {
		return Handle(k)
	}
	arv := reflect.ValueOf(actual)
	switch arv.Kind() {
	case reflect.Ptr, reflect.UnsafePointer, reflect.Slice,
		reflect.Map, reflect.Chan, reflect.Func:
		// The underlying object of the given Go value already have
		// its existing handle.
		if arv.Pointer() == k {
			return Handle(k)
		}

		// If the loaded actual value is inconsistent with the new
		// value, it means the address has been used for different
		// objects, and we should fallthrough, see comments below.
		fallthrough
	default:
		// If a Go value is garbage collected and its address is reused
		// for a new Go value, meaning that the Handle does not call
		// Delete explicitly when the old Go value is not needed.
		// Consider this as a misuse of a handle, do panic.
		panic("misuse of a handle")
	}
}

// Delete invalidates a handle. This method must be called when C code no
// longer has a copy of the handle, and the program no longer needs the
// Go value that associated with the handle.
//
// The method panics if the handle is invalid already.
func (h Handle) Delete() {
	_, ok := m.LoadAndDelete(uintptr(h))
	if !ok {
		panic("misuse of a handle")
	}
}

// Value returns the associated Go value for a valid handle.
//
// The method panics if the handle is invalid already.
func (h Handle) Value() interface{} {
	v, ok := m.Load(uintptr(h))
	if !ok {
		panic("misuse of a handle")
	}
	return v
}

var m = &sync.Map{} // map[uintptr]interface{}
