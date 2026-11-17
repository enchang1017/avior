#!/usr/bin/env bash
# The last word before a push: the tags about to be pushed are the release's
# version, and latest only for a stable release, and nothing else.
set -euo pipefail

: "${IMAGE:?IMAGE is not set}"
: "${VERSION:?VERSION is empty; the push would carry no version}"
: "${STABLE:?STABLE is not set}"
TAGS="${TAGS-}"

if [[ "$VERSION" == latest ]]; then
	echo "::error::VERSION is 'latest'; the gateway would report no release"
	exit 1
fi

image="$(printf '%s' "$IMAGE" | tr '[:upper:]' '[:lower:]')"
want_version="$image:$VERSION"
want_latest="$image:latest"
saw_version=false
saw_latest=false

while IFS= read -r tag; do
	[[ -n "$tag" ]] || continue
	case "$tag" in
	"$want_version") saw_version=true ;;
	"$want_latest") saw_latest=true ;;
	*)
		echo "::error::unexpected tag '$tag'; only $want_version and $want_latest may be pushed"
		exit 1
		;;
	esac
done <<< "$TAGS"

if [[ "$saw_version" != true ]]; then
	echo "::error::$want_version is not among the tags to push"
	exit 1
fi
if [[ "$STABLE" == true && "$saw_latest" != true ]]; then
	echo "::error::a stable release must move $want_latest"
	exit 1
fi
if [[ "$STABLE" != true && "$saw_latest" == true ]]; then
	echo "::error::a prerelease must not move $want_latest"
	exit 1
fi
echo "pushing $want_version$([[ "$saw_latest" == true ]] && echo " and $want_latest")"
