#!/bin/sh
set -e

mkdir -p dist
for OS in windows linux freebsd openbsd netbsd darwin; do
    echo "Building for $OS"
    GOOS=$OS GOARCH=amd64 go build -o "dist/torget-$OS" -ldflags '-w -s' torget.go
done
mv dist/torget-windows dist/torget.exe
