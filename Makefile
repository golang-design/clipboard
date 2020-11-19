# Copyright 2021 The golang.design Initiative authors.
# All rights reserved. Use of this source code is governed
# by a GNU GPL-3 license that can be found in the LICENSE file.
#
# Written by Changkun Ou <changkun.de>

all:
	go test -v -count=1 -covermode=atomic ./...