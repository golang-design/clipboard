// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard

// for debugging errors. ErrUnavailable is public now (see clipboard.go), so it
// is no longer re-exported here.
var (
	Debug          = debug
	ErrCgoDisabled = errNoCgo
)
