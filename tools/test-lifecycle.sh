#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf "${test_root}"' EXIT

export TRIM_APPDEST="${test_root}/target"
export TRIM_PKGETC="${test_root}/etc"
export TRIM_PKGVAR="${test_root}/var"
export TRIM_TEMP_LOGFILE="${test_root}/lifecycle-error.log"

mkdir -p "${TRIM_APPDEST}" "${TRIM_PKGETC}" "${TRIM_PKGVAR}"

"${repo_root}/fpk/cmd/install_init"
test ! -e "${TRIM_APPDEST}/branding/settings.yml"

cp -a "${repo_root}/fpk/app/." "${TRIM_APPDEST}/"
"${repo_root}/fpk/cmd/install_callback"
"${repo_root}/fpk/cmd/install_callback"

test -f "${TRIM_PKGETC}/searxng/settings.yml"
test -f "${TRIM_PKGETC}/searxng/branding/searxng.png"
if grep -q '@SECRET_KEY@' "${TRIM_PKGETC}/searxng/settings.yml"; then
  echo "settings secret placeholder was not replaced" >&2
  exit 1
fi

python3 - "${repo_root}" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
for relative in (
    "fpk/config/privilege",
    "fpk/config/resource",
    "fpk/app/ui/config",
    "fpk/wizard/install",
    "fpk/wizard/uninstall",
):
    json.loads((root / relative).read_text(encoding="utf-8"))
PY

echo "FPK lifecycle test passed"
