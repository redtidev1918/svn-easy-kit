#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64) PLATFORM="linux-x64" ;;
  aarch64|arm64) PLATFORM="linux-arm64" ;;
  *) echo "Unsupported CPU architecture: $ARCH" >&2; exit 1 ;;
esac

if [ -x "$SCRIPT_DIR/SvnEasyServer" ]; then
  BINARY="$SCRIPT_DIR/SvnEasyServer"
else
  BINARY="$SCRIPT_DIR/release/$PLATFORM/SvnEasyServer"
fi

if [ ! -f "$BINARY" ]; then
  echo "SvnEasyServer binary not found: $BINARY" >&2
  exit 1
fi

if ! command -v svnadmin >/dev/null 2>&1 || ! command -v svn >/dev/null 2>&1; then
  echo "Installing Subversion..."
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    apt-get install -y subversion
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y subversion
  elif command -v yum >/dev/null 2>&1; then
    yum install -y subversion
  elif command -v zypper >/dev/null 2>&1; then
    zypper --non-interactive install subversion
  elif command -v apk >/dev/null 2>&1; then
    apk add subversion
  else
    echo "No supported package manager found." >&2
    exit 1
  fi
fi

install -m 0755 "$BINARY" /usr/local/bin/svneasy-server
echo "Installed /usr/local/bin/svneasy-server"
/usr/local/bin/svneasy-server doctor
exec /usr/local/bin/svneasy-server
