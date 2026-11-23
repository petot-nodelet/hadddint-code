#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if [ -n "${GO_BIN:-}" ]; then
	GO_COMMAND=$GO_BIN
elif command -v go >/dev/null 2>&1; then
	GO_COMMAND=$(command -v go)
elif [ -x /usr/local/go/bin/go ]; then
	GO_COMMAND=/usr/local/go/bin/go
else
	echo "Go tidak ditemukan. Pasang Go atau atur GO_BIN ke lokasi executable Go." >&2
	exit 1
fi

if [ ! -x "$GO_COMMAND" ]; then
	echo "Executable Go tidak dapat dijalankan: $GO_COMMAND" >&2
	exit 1
fi

cd "$ROOT_DIR/app"
exec "$GO_COMMAND" run ./cmd/sukamoto
