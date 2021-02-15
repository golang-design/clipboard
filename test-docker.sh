# Copyright 2021 The golang.design Initiative authors.
# All rights reserved. Use of this source code is governed
# by a GNU GPL-3 license that can be found in the LICENSE file.
#
# Written by Changkun Ou <changkun.de>

# require apt-get install xvfb
Xvfb :99 -screen 0 1024x768x24 > /dev/null 2>&1 &
export DISPLAY=:99.0

go test -v -covermode=atomic ./... 