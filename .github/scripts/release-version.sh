#!/usr/bin/env bash
# Reads a release tag and writes version= and stable= to $GITHUB_OUTPUT, or
# refuses it. Strict SemVer 2.0 with a leading v and no build metadata: a
# docker tag cannot carry "+", and a tag docker/metadata-action would not parse
# as a version must never reach a push that still moves latest.
set -euo pipefail

tag="${1:?usage: release-version.sh TAG}"
: "${GITHUB_OUTPUT:?GITHUB_OUTPUT is not set}"

num='(0|[1-9][0-9]*)'
ident='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
pattern="^v${num}\.${num}\.${num}(-${ident}(\.${ident})*)?$"

if ! [[ "$tag" =~ $pattern ]]; then
	echo "::error::release tag '$tag' is not vMAJOR.MINOR.PATCH[-PRERELEASE] in SemVer 2.0, without build metadata"
	exit 1
fi

version="${tag#v}"
stable=true
[[ "$version" == *-* ]] && stable=false

{
	echo "version=$version"
	echo "stable=$stable"
} >> "$GITHUB_OUTPUT"
echo "release $tag publishes version $version (stable: $stable)"
