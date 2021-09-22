// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build go1.17
// +build go1.17

package cgo

import "runtime/cgo"

type Handle = cgo.Handle

var NewHandle = cgo.NewHandle
