#!/usr/bin/env bash
# Stamps a release version into the platform metadata files.
# The repository always keeps 0.0.1 as the placeholder; CI runs this before building.
#
#   scripts/set-version.sh 1.2.3        (a leading "v" is accepted)
set -euo pipefail
cd "$(dirname "$0")/.."

v="${1#v}"
if [[ ! "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "usage: $0 X.Y.Z[-prerelease]" >&2
  exit 1
fi
numeric="${v%%-*}" # Windows manifests only accept numeric versions

perl -pi -e "s/version: \"0\.0\.1\"/version: \"$v\"/" build/config.yml build/linux/nfpm/nfpm.yaml
perl -0pi -e "s#(<key>CFBundle(?:ShortVersionString|Version)</key>\s*<string>)0\.0\.1#\${1}$numeric#g" build/darwin/Info.plist
perl -pi -e "s/\"0\.0\.1\"/\"$numeric\"/g" build/windows/info.json
perl -pi -e "s/version=\"0\.0\.1\"/version=\"$numeric.0\"/" build/windows/wails.exe.manifest
perl -pi -e "s/INFO_PRODUCTVERSION \"0\.0\.1\"/INFO_PRODUCTVERSION \"$numeric\"/" build/windows/nsis/wails_tools.nsh

echo "version set to $v"
