ARG VARIANT=1
FROM mcr.microsoft.com/devcontainers/go:${VARIANT}

RUN apt-get update && apt-get install -y --no-install-recommends \
  libegl1-mesa-dev \
  libgles2-mesa-dev \
  libx11-dev \
  xvfb \
  && rm -rf /var/lib/apt/lists/*
