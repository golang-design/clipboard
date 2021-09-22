// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build !go1.17
// +build !go1.17

// Package cgo is an implementation of golang.org/issue/37033.
//
// See golang.org/cl/294670 for code review discussion.
package cgo

import (
	"reflect"
	"sync"
)

// Handle provides a safe representation to pass Go values between C and
// Go back and forth. The zero value of a handle is not a valid handle,
// and thus safe to use as a sentinel in C APIs.
//
// The underlying type of Handle may change, but the value is guaranteed
// to fit in an integer type that is large enough to hold the bit pattern
// of any pointer. For instance, on the Go side:
//
// 	package main
//
// 	/*
// 	extern void MyGoPrint(unsigned long long handle);
// 	void myprint(unsigned long long handle);
// 	*/
// 	import "C"
// 	import "runtime/cgo"
//
// 	//export MyGoPrint
// 	func MyGoPrint(handle C.ulonglong) {
// 		h := cgo.Handle(handle)
// 		val := h.Value().(int)
// 		println(val)
// 		h.Delete()
// 	}
//
// 	func main() {
// 		val := 42
// 		C.myprint(C.ulonglong(cgo.NewHandle(val)))
// 		// Output: 42
// 	}
//
// and on the C side:
//
// 	// A Go function
// 	extern void MyGoPrint(unsigned long long handle);
//
// 	// A C function
// 	void myprint(unsigned long long handle) {
// 	    MyGoPrint(handle);
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
			panic("cgo: cannot use Handle for nil value")
		}

		k = rv.Pointer()
	default:
		// Wrap and turn a value parameter into a pointer. This enables
		// us to always store the passing object as a pointer, and helps
		// to identify which of whose are initially pointers or values
		// when Value is called.
		v = &wrap{v}
		k = reflect.ValueOf(v).Pointer()
	}

	// v was escaped to the heap because of reflection. As Go do not have
	// a moving GC (and possibly lasts true for a long future), it is
	// safe to use its pointer address as the key of the global map at
	// this moment. The implementation must be reconsidered if moving GC
	// is introduced internally in the runtime.
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

		// If the loaded pointer is inconsistent with the new pointer,
		// it means the address has been used for different objects
		// because of GC and its address is reused for a new Go object,
		// meaning that the Handle does not call Delete explicitly when
		// the old Go value is not needed. Consider this as a misuse of
		// a handle, do panic.
		panic("cgo: misuse of a Handle")
	default:
		panic("cgo: Handle implementation has an internal bug")
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
		panic("cgo: misuse of an invalid Handle")
	}
}

// Value returns the associated Go value for a valid handle.
//
// The method panics if the handle is invalid already.
func (h Handle) Value() interface{} {
	v, ok := m.Load(uintptr(h))
	if !ok {
		panic("cgo: misuse of an invalid Handle")
	}
	if wv, ok := v.(*wrap); ok {
		return wv.v
	}
	return v
}

var m = &sync.Map{} // map[uintptr]interface{}

// wrap wraps a Go value.
type wrap struct {
	v interface{}
}
