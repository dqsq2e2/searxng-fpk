#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf "${test_root}"' EXIT

export TRIM_APPDEST="${test_root}/target"
export TRIM_PKGETC="${test_root}/protected/appconf/searxng"
export TRIM_PKGVAR="${test_root}/protected/appdata/searxng"
export TRIM_TEMP_LOGFILE="${test_root}/lifecycle-error.log"

mkdir -p "${TRIM_APPDEST}" "${test_root}/protected/appconf" "${test_root}/protected/appdata"
chmod 555 "${test_root}/protected/appconf" "${test_root}/protected/appdata"

"${repo_root}/fpk/cmd/install_init"
test ! -e "${TRIM_APPDEST}/branding/settings.yml"
test ! -e "${TRIM_PKGETC}"
test ! -e "${TRIM_PKGVAR}"

cp -a "${repo_root}/fpk/app/." "${TRIM_APPDEST}/"
chmod 755 "${test_root}/protected/appconf" "${test_root}/protected/appdata"
mkdir -p "${TRIM_PKGETC}" "${TRIM_PKGVAR}"
"${repo_root}/fpk/cmd/install_callback"
"${repo_root}/fpk/cmd/install_callback"

test -f "${TRIM_PKGETC}/searxng/settings.yml"
test -f "${TRIM_PKGETC}/searxng/branding/searxng.png"
test -f "${TRIM_PKGETC}/searxng/branding/favicon.svg"
test -d "${TRIM_PKGVAR}/control"
grep -q 'favicon_resolver: "google"' "${TRIM_PKGETC}/searxng/settings.yml"
grep -q 'autocomplete: "bing"' "${TRIM_PKGETC}/searxng/settings.yml"
grep -q 'autocomplete_min: 4' "${TRIM_PKGETC}/searxng/settings.yml"
grep -A1 '^  - name: baidu$' "${TRIM_PKGETC}/searxng/settings.yml" | grep -q 'disabled: false'
grep -A2 '^  - name: chinaso news$' "${TRIM_PKGETC}/searxng/settings.yml" | grep -q 'inactive: true'
if grep -q '@SECRET_KEY@' "${TRIM_PKGETC}/searxng/settings.yml"; then
  echo "settings secret placeholder was not replaced" >&2
  exit 1
fi

python3 - "${repo_root}" <<'PY'
import json
import pathlib
import struct
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

uninstall = json.loads((root / "fpk/wizard/uninstall").read_text(encoding="utf-8"))
data_action = next(item for item in uninstall[0]["items"] if item.get("field") == "wizard_data_action")
assert data_action["type"] == "radio"
assert data_action["initValue"] == "keep"
assert {option["value"] for option in data_action["options"]} == {"keep", "delete"}

def png_size(relative):
    data = (root / relative).read_bytes()
    assert data[:8] == b"\x89PNG\r\n\x1a\n", relative
    return struct.unpack(">II", data[16:24])

assert png_size("assets/icon_64.png") == (256, 256)
assert png_size("assets/icon_256.png") == (1024, 1024)
PY

grep -q '^checkport=false$' "${repo_root}/fpk/manifest"
grep -q '^maintainer=searxng$' "${repo_root}/fpk/manifest"
grep -q '^maintainer_url=https://github.com/searxng/searxng$' "${repo_root}/fpk/manifest"
grep -q '^distributor=dqsq2e2$' "${repo_root}/fpk/manifest"
grep -q '^distributor_url=https://github.com/dqsq2e2/searxng-fpk$' "${repo_root}/fpk/manifest"
grep -q "poster.png?raw=true" "${repo_root}/fpk/manifest"
grep -q '<strong>主要功能</strong>' "${repo_root}/fpk/manifest"
grep -q 'container_name: searxng-admin-fpk' "${repo_root}/fpk/app/docker/docker-compose.yaml"
grep -q 'container_name: searxng-apply-fpk' "${repo_root}/fpk/app/docker/docker-compose.yaml"
grep -A4 'container_name: searxng-apply-fpk' "${repo_root}/fpk/app/docker/docker-compose.yaml" | grep -q 'group_add:'
grep -q '/var/run/docker.sock:/var/run/docker.sock:rw' "${repo_root}/fpk/app/docker/docker-compose.yaml"
grep -q 'branding/favicon.svg:/usr/local/searxng/searx/static/themes/simple/img/favicon.svg:ro' "${repo_root}/fpk/app/docker/docker-compose.yaml"
grep -q 'searxng/searxng:2026.7.15-7b2199ecd@sha256:268fdb05efbb7b4fdc5957a20c42389bfb1b1b27b5eddeb98f75ec80c45b960f' "${repo_root}/fpk/app/docker/docker-compose.yaml"
grep -q -- '--default-settings' "${repo_root}/fpk/app/docker/docker-compose.yaml"

printf 'keep-config\n' > "${TRIM_PKGETC}/keep-marker"
printf 'keep-data\n' > "${TRIM_PKGVAR}/keep-marker"
wizard_data_action=keep "${repo_root}/fpk/cmd/uninstall_init"
wizard_data_action=keep "${repo_root}/fpk/cmd/uninstall_callback"
test -f "${TRIM_PKGETC}/keep-marker"
test -f "${TRIM_PKGVAR}/keep-marker"

wizard_data_action=unexpected "${repo_root}/fpk/cmd/uninstall_callback" 2>/dev/null
test -f "${TRIM_PKGETC}/keep-marker"
test -f "${TRIM_PKGVAR}/keep-marker"

wizard_data_action=delete "${repo_root}/fpk/cmd/uninstall_init"
wizard_data_action=delete "${repo_root}/fpk/cmd/uninstall_callback"
test ! -e "${TRIM_PKGETC}"
test ! -e "${TRIM_PKGVAR}"

echo "FPK lifecycle test passed"
