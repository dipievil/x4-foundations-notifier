#!/usr/bin/env bash
# Builds dist/fleet_notifier.zip with the extension folder at the zip root, the
# shape X4 expects inside extensions/.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$root/dist/fleet_notifier.zip"

cd "$root"
mkdir -p dist
rm -f "$out"

if command -v zip >/dev/null 2>&1; then
    zip -qr "$out" fleet_notifier
elif command -v python3 >/dev/null 2>&1; then
    python3 - "$out" <<'PY'
import os, sys, zipfile

out = sys.argv[1]
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as zf:
    for base, _, files in os.walk("fleet_notifier"):
        for name in files:
            path = os.path.join(base, name)
            zf.write(path, path)
PY
else
    echo "need either zip or python3 to package the mod" >&2
    exit 1
fi

echo "wrote $out"
