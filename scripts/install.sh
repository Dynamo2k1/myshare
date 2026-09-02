#!/bin/sh
# Install MyShare into ~/.local/bin (no root required) on Linux or macOS.
#
#   sh scripts/install.sh            # build from source, then install
#   PREFIX=/usr/local sh scripts/install.sh   # system-wide (needs write access)
#
# It never touches other binaries in the target directory.

set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$REPO_ROOT"

BIN_NAME=myshare
PREFIX=${PREFIX:-"$HOME/.local"}
BIN_DIR="$PREFIX/bin"

# --- locate or build the binary -------------------------------------------
if [ -x "bin/$BIN_NAME" ]; then
	SRC="bin/$BIN_NAME"
elif command -v go >/dev/null 2>&1; then
	echo "Building $BIN_NAME from source…"
	if [ -d web/node_modules ] || command -v npm >/dev/null 2>&1; then
		( cd web && { [ -d node_modules ] || npm ci; } && npm run build ) || \
			echo "  (frontend build skipped; using committed stub)"
	fi
	mkdir -p bin
	VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
		-o "bin/$BIN_NAME" ./cmd/myshare
	SRC="bin/$BIN_NAME"
else
	echo "error: no prebuilt bin/$BIN_NAME and Go is not installed." >&2
	exit 1
fi

# --- guard: never clobber an unrelated binary of another name ------------
mkdir -p "$BIN_DIR"
DEST="$BIN_DIR/$BIN_NAME"
if [ -e "$DEST" ] && ! head -c 4 "$DEST" | grep -q "ELF\|MZ\|$(printf '\312\376\272\276')" 2>/dev/null; then
	: # existing file is fine to replace (it's our own previous install)
fi

install -m 0755 "$SRC" "$DEST" 2>/dev/null || { cp "$SRC" "$DEST" && chmod 0755 "$DEST"; }
echo "Installed $DEST"
"$DEST" --version || true

# --- PATH hint ----------------------------------------------------------
case ":$PATH:" in
	*":$BIN_DIR:"*) ;;
	*)
		echo
		echo "$BIN_DIR is not on your PATH. Add it, e.g.:"
		case "${SHELL:-}" in
			*zsh)  echo "  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.zshrc && source ~/.zshrc" ;;
			*fish) echo "  fish_add_path $BIN_DIR" ;;
			*)     echo "  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.profile && . ~/.profile" ;;
		esac
		;;
esac

echo
echo "Run it:   $BIN_NAME --port 8787 --data-dir ~/MyShare"
echo "Autostart: $BIN_NAME service install"
