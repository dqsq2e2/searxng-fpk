#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 7 ]]; then
  echo "usage: $0 <arch-name> <platform> <server-bin> <ui-dist> <version> <fnpack-bin> <output-dir>" >&2
  exit 2
fi

arch_name="$1"
platform="$2"
server_bin="$3"
ui_dist="$4"
version="$5"
fnpack_bin="$6"
output_dir="$7"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
stage_dir="${repo_root}/build/fpk-${arch_name}"

rm -rf "${stage_dir}"
mkdir -p "${stage_dir}" "${output_dir}"
cp -a "${repo_root}/fpk/." "${stage_dir}/"

rm -rf "${stage_dir}/app/admin-ui"
mkdir -p "${stage_dir}/app/admin-ui" "${stage_dir}/app/server" "${stage_dir}/app/ui/images"
cp -a "${ui_dist}/." "${stage_dir}/app/admin-ui/"
install -m 0755 "${server_bin}" "${stage_dir}/app/server/searxng-admin"

sed -i "s/@VERSION@/${version}/g; s/@PLATFORM@/${platform}/g" "${stage_dir}/manifest"

cp "${repo_root}/assets/icon_64.png" "${stage_dir}/app/ui/images/icon_64.png"
cp "${repo_root}/assets/icon_256.png" "${stage_dir}/app/ui/images/icon_256.png"
cp "${stage_dir}/app/ui/images/icon_64.png" "${stage_dir}/ICON.PNG"
cp "${stage_dir}/app/ui/images/icon_256.png" "${stage_dir}/ICON_256.PNG"

find "${stage_dir}/cmd" -type f -exec chmod 0755 {} +
find "${stage_dir}" -name '.DS_Store' -delete
find "${stage_dir}" -name '.gitkeep' -delete

(cd "${stage_dir}" && "${fnpack_bin}" build)

package_file="$(find "${stage_dir}" -maxdepth 1 -type f -name '*.fpk' -print -quit)"
if [[ -z "${package_file}" ]]; then
  echo "fnpack did not produce an FPK file" >&2
  exit 1
fi

target="${output_dir}/searxng-${arch_name}-${version}.fpk"
mv "${package_file}" "${target}"
echo "${target}"
