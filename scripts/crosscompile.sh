#!/bin/sh
# Runs inside the dev container: builds the single binary for every platform we ship.
set -e
for t in linux/amd64 linux/arm64 windows/amd64 darwin/arm64; do
  os=${t%/*}
  arch=${t#*/}
  out=/tmp/wardline-$os-$arch
  [ "$os" = windows ] && out=$out.exe
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -o "$out" ./cmd/wardline
  echo "ok  $t  $(du -h "$out" | cut -f1)"
done
