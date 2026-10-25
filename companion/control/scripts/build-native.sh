#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
# Remove the pre-platform resource left by older Control builds before the
# platform guard too, so Linux/Windows local builds cannot inherit a duplicate
# resource and fail MyGo packaging.
rm -f "$root/resources/virtual-display-helper"
if [ "$(uname -s)" != "Darwin" ]; then
  # The CGVirtualDisplay helper is macOS-only. MyGo still builds the ordinary
  # Control application for Linux and Windows without this resource.
  exit 0
fi

mkdir -p "$root/resources/darwin"

build_helper() {
  arch=$1
  clang -arch "$arch" -fobjc-arc \
    -mmacosx-version-min=12.0 \
    -framework Cocoa \
    -framework CoreGraphics \
    -framework IOSurface \
    "$root/native/virtual_display_helper.m" \
    -o "$root/resources/darwin/virtual-display-helper-$arch"
}

build_helper arm64
build_helper x86_64
lipo -create \
  "$root/resources/darwin/virtual-display-helper-arm64" \
  "$root/resources/darwin/virtual-display-helper-x86_64" \
  -output "$root/resources/darwin/virtual-display-helper"
rm -f "$root/resources/darwin/virtual-display-helper-arm64" "$root/resources/darwin/virtual-display-helper-x86_64"

chmod 755 "$root/resources/darwin/virtual-display-helper"
