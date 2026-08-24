#!/bin/bash
# compute-meso-tag.sh — prints the meso-miner release tag for right now, per
# the URnetwork house versioning convention (upstream urnetwork/warp's
# warpctl/warpctl/docker.go newVersionCode(): version_code = round(seconds
# since the network's 2023-05-23T00:00:00Z founding) * 10).
#
# Tag shape: vYYYY.M.D-<code>-meso
#
# Usage:
#   scripts/compute-meso-tag.sh            # print the tag for right now
#   scripts/compute-meso-tag.sh --verify v2026.8.24-1027650360-meso
#                                           # decode an existing tag's code
#                                           # back to its UTC mint time, to
#                                           # sanity-check it before pushing
set -euo pipefail

FOUNDING_EPOCH=1684800000 # 2023-05-23T00:00:00Z, verified against real
                          # inherited upstream tags (see MESO_MIGRATION.md)

compute_tag() {
	local now founding code date_part
	now=$(date -u +%s)
	founding=$FOUNDING_EPOCH
	code=$(((now - founding) * 10))
	date_part=$(date -u +%Y.%-m.%-d)
	echo "v${date_part}-${code}-meso"
}

verify_tag() {
	local tag="$1" code mint_epoch
	code=$(echo "$tag" | grep -oP '(?<=-)[0-9]+(?=-meso$)') || {
		echo "error: '$tag' does not look like a vYYYY.M.D-<code>-meso tag" >&2
		exit 1
	}
	mint_epoch=$((FOUNDING_EPOCH + code / 10))
	echo "tag:       $tag"
	echo "code:      $code"
	echo "mint time: $(date -u -d "@${mint_epoch}" +"%Y-%m-%dT%H:%M:%SZ")"
}

if [ "${1:-}" = "--verify" ]; then
	if [ -z "${2:-}" ]; then
		echo "usage: $0 --verify <tag>" >&2
		exit 1
	fi
	verify_tag "$2"
else
	compute_tag
fi
