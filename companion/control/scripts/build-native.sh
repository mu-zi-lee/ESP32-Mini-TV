#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mkdir -p "$root/resources"

clang -fobjc-arc \
  -mmacosx-version-min=12.0 \
  -framework Cocoa \
  -framework CoreGraphics \
  -framework IOSurface \
  "$root/native/virtual_display_helper.m" \
  -o "$root/resources/virtual-display-helper"

chmod 755 "$root/resources/virtual-display-helper"
