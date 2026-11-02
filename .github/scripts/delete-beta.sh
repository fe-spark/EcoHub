#!/usr/bin/env bash
# 正式版发布后删除全部 beta Release、tag 和 GHCR 镜像。
# 只删 tag 名含 -beta 的版本。latest 和正式版 tag 不动。
# 同一镜像版本若同时挂了 beta 和正式 tag，直接失败，避免误删正式版。
set -euo pipefail

repo="${GITHUB_REPOSITORY:?}"
owner="${GITHUB_REPOSITORY_OWNER:?}"
package="ecohub"

if [ -z "${GH_TOKEN:-}" ]; then
	echo "GH_TOKEN is empty" >&2
	exit 1
fi

is_beta_tag() {
	printf '%s' "$1" | grep -Eq -- '-beta'
}

delete_release() {
	local tag="$1"
	if ! gh release view "$tag" --repo "$repo" >/dev/null 2>&1; then
		echo "release already gone: $tag"
		return 0
	fi
	gh release delete "$tag" --repo "$repo" --yes --cleanup-tag
	echo "release deleted: $tag"
}

delete_tag() {
	local tag="$1"
	local err
	if err=$(git push origin --delete "$tag" 2>&1); then
		echo "tag deleted: $tag"
		return 0
	fi
	if printf '%s\n' "$err" | grep -q "remote ref does not exist"; then
		echo "tag already gone: $tag"
		return 0
	fi
	printf '%s\n' "$err" >&2
	return 1
}

delete_image_version() {
	local id="$1"
	local tags="$2"
	local resp
	resp=$(gh api --method DELETE -i "/users/${owner}/packages/container/${package}/versions/${id}" 2>&1) || true
	if printf '%s\n' "$resp" | grep -Eq 'HTTP/[0-9.]+ (204|404)'; then
		echo "image deleted: $tags"
		return 0
	fi
	printf '%s\n' "$resp" >&2
	return 1
}

echo "Deleting beta GitHub releases"
release_tags=$(gh release list --repo "$repo" --limit 1000 --json tagName --jq '.[] | select(.tagName | test("(?i)-beta")) | .tagName')
while IFS= read -r tag; do
	[ -n "$tag" ] || continue
	delete_release "$tag"
done <<<"$release_tags"

echo "Deleting leftover beta tags"
remote_tags=$(git ls-remote --tags origin | awk '{print $2}' | sed 's#refs/tags/##')
while IFS= read -r tag; do
	case "$tag" in
		*'-beta'*) ;;
		*) continue ;;
	esac
	if printf '%s' "$tag" | grep -q '{'; then
		continue
	fi
	delete_tag "$tag"
done <<<"$remote_tags"

echo "Checking GHCR beta image versions"
versions=$(gh api --paginate "/users/${owner}/packages/container/${package}/versions?per_page=100" \
	--jq '.[] | select(.metadata.container.tags != null) | select(any(.metadata.container.tags[]; test("(?i)-beta"))) | "\(.id)\t\(.metadata.container.tags | join(","))"')

mixed=""
pure=""
while IFS=$'\t' read -r id tags; do
	[ -n "${id:-}" ] || continue
	pure_beta=1
	IFS=',' read -r -a parts <<<"$tags"
	for part in "${parts[@]}"; do
		if ! is_beta_tag "$part"; then
			pure_beta=0
			break
		fi
	done
	if [ "$pure_beta" -eq 1 ]; then
		pure+="${id}"$'\t'"${tags}"$'\n'
	else
		mixed+="${id}"$'\t'"${tags}"$'\n'
	fi
done <<<"$versions"

if [ -n "$mixed" ]; then
	echo "beta tag shares an image version with a non-beta tag; refuse to delete:" >&2
	printf '%s' "$mixed" >&2
	exit 1
fi

echo "Deleting beta GHCR image versions"
while IFS=$'\t' read -r id tags; do
	[ -n "${id:-}" ] || continue
	delete_image_version "$id" "$tags"
done <<<"$pure"

echo "Beta releases, tags, and images deleted"
