#!/usr/bin/env bash
set -euo pipefail
version="${1#v}"
[ -n "$version" ] || { echo "usage: $0 vX.Y.Z" >&2; exit 2; }
grep -Eq "^## \[${version}\]" CHANGELOG.md || {
  echo "CHANGELOG.md is missing a section for ${version}" >&2
  exit 1
}
echo "CHANGELOG.md contains ${version}"
