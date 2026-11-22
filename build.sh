#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "$0")"

printf 'Checking Go formatting\n'
unformatted=$(gofmt -l cmd internal)
if [[ -n "$unformatted" ]]; then
  printf 'Go files need formatting:\n%s\n' "$unformatted" >&2
  exit 1
fi

go mod verify
go vet ./...
CGO_ENABLED=1 go test -race ./...

version=$(git describe --tags --long --always --dirty 2>/dev/null || echo dev)
mkdir -p build
for arch in amd64 arm64; do
  for command in tuya-beacon-mesh-p2-controller tuya-beacon-mesh-p2-controller-server; do
    printf 'Building %s for linux/%s\n' "$command" "$arch"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -buildvcs=false -ldflags "-X 'example.com/tuya-beacon-mesh-p2-controller/internal/version.description=$version'" \
      -o "build/$command-linux-$arch" "./cmd/$command"
  done
done
printf 'Build complete in build/\n'
