#!/bin/bash
# Build a macOS .app bundle for pgmemory-tray
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
APP="$ROOT/bin/Pgmemory.app"

echo "Building pgmemory-tray..."
cd "$ROOT"

# Only build if binaries don't already exist (make app builds them first).
if [ ! -f bin/pgmemory-tray ]; then
    go build -o bin/pgmemory-tray ./cmd/pgmemory-tray
fi
if [ ! -f bin/pgmemory ]; then
    go build -ldflags "-X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/pgmemory ./cmd/pgmemory
fi

echo "Creating Pgmemory.app bundle..."
rm -rf "$APP"

mkdir -p "$APP/Contents/MacOS"
mkdir -p "$APP/Contents/Resources"

cp bin/pgmemory-tray "$APP/Contents/MacOS/pgmemory-tray"
cp bin/pgmemory "$APP/Contents/MacOS/pgmemory"

cat > "$APP/Contents/Info.plist" << 'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>Pgmemory</string>
    <key>CFBundleDisplayName</key>
    <string>Pgmemory</string>
    <key>CFBundleIdentifier</key>
    <string>io.pgmemory.pgmemory</string>
    <key>CFBundleVersion</key>
    <string>1.0</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0</string>
    <key>CFBundleExecutable</key>
    <string>pgmemory-tray</string>
    <key>LSUIElement</key>
    <true/>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

# Ad-hoc codesign the bundle so macOS doesn't reject it.
codesign --force --deep -s - "$APP"

echo "Built $APP"
echo "Run: open $APP"
