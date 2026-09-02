#!/bin/sh
# Remove a MyShare install placed by scripts/install.sh. Only removes the
# `myshare` binary and (optionally) the user service. Leaves ~/MyShare data
# untouched.

set -eu

BIN_NAME=myshare
PREFIX=${PREFIX:-"$HOME/.local"}
DEST="$PREFIX/bin/$BIN_NAME"

if command -v "$BIN_NAME" >/dev/null 2>&1; then
	"$BIN_NAME" service uninstall 2>/dev/null || true
fi

if [ -e "$DEST" ]; then
	rm -f "$DEST"
	echo "Removed $DEST"
else
	echo "Nothing to remove at $DEST"
fi

echo "Your data directory (default ~/MyShare) was left in place."
