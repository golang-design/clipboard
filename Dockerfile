# Copyright 2021 The golang.design Initiative authors.
# All rights reserved. Use of this source code is governed
# by a GNU GPL-3 license that can be found in the LICENSE file.
#
# Written by Changkun Ou <changkun.de>

FROM golang:1.16rc1
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y \
      xvfb libx11-dev \
    && apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
WORKDIR /app
COPY . .
CMD [ "sh", "-c", "./test.sh" ]
