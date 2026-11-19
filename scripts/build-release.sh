#!/usr/bin/env bash
# Builds the binaries that ship in bin/ so people playing X4 do not need Go.
# Run from anywhere; needs a Go toolchain on PATH (or GO=...).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go="${GO:-go}"
# VERSION wins, then the git tag/commit, then "dev" (e.g. on a build host
# without git: VERSION=1.0.0 ./scripts/build-release.sh).
version="${VERSION:-$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)}"

mkdir -p "$root/bin"

build() {
    local goos="$1" goarch="$2" out="$3"
    echo "building bin/$out ($version)"
    (
        cd "$root/go_server"
        CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" "$go" build -trimpath \
            -ldflags "-s -w -X main.version=$version" -o "$root/bin/$out" .
    )
}

# X4 runs on Windows; the Linux build is for Proton/Steam Deck setups and servers.
build windows amd64 "x4-notifier-windows-amd64.exe"
build linux amd64 "x4-notifier-linux-amd64"

cp "$root/go_server/.env.example" "$root/bin/.env.example"

ls -lh "$root/bin"
