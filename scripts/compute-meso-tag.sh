#!/bin/bash
# compute-meso-tag.sh — prints the meso-miner release tag for right now, per
# the URnetwork house versioning convention (upstream urnetwork/warp's
# warpctl/warpctl/docker.go newVersionCode(): version_code = round(seconds
# since the network's 2023-05-23T00:00:00Z founding) * 10).
#
# Tag shape: vYYYY.M.D-<code>-meso or vYYYY.M.D-<code>-meso.dev
#
# Usage:
#   scripts/compute-meso-tag.sh                         # print the tag for right now
#   scripts/compute-meso-tag.sh --suffix -meso.dev      # override the suffix locally
#   scripts/compute-meso-tag.sh --verify v2026.8.24-1027650360-meso
#                                                       # decode an existing tag's code
#                                                       # back to its UTC mint time
set -euo pipefail

FOUNDING_EPOCH=1684800000 # 2023-05-23T00:00:00Z, verified against real
                          # inherited upstream tags (see MESO_MIGRATION.md)

suffix=""
verify=""
while (($#)); do
	case "$1" in
		--suffix)
			if [ -z "${2:-}" ]; then
				echo "usage: $0 [--suffix -meso|-meso.dev] [--verify <tag>]" >&2
				exit 1
			fi
			suffix="$2"
			shift 2
			;;
		--verify)
			if [ -z "${2:-}" ]; then
				echo "usage: $0 [--suffix -meso|-meso.dev] --verify <tag>" >&2
				exit 1
			fi
			verify="$2"
			shift 2
			;;
		*)
			echo "usage: $0 [--suffix -meso|-meso.dev] [--verify <tag>]" >&2
			exit 1
			;;
	esac
done

if [ -z "$suffix" ]; then
	case "${GITHUB_REPOSITORY:-}" in
		urfoundation/meso-miner) suffix="-meso" ;;
		"") suffix="-meso" ;;
		*) suffix="-meso.dev" ;;
	esac
fi
case "$suffix" in
	-meso|-meso.dev) ;;
	*) echo "error: unsupported suffix '$suffix' (use -meso or -meso.dev)" >&2; exit 1 ;;
esac

compute_tag() {
	local now code date_part
	now=$(date -u +%s)
	code=$(((now - FOUNDING_EPOCH) * 10))
	date_part=$(date -u +%Y.%-m.%-d)
	echo "v${date_part}-${code}${suffix}"
}

verify_tag() {
	local tag="$1" code mint_epoch
	if [[ "$tag" =~ ^v[0-9]{4}\.[0-9]+\.[0-9]+-([0-9]+)-meso(\.dev)?$ ]]; then
		code="${BASH_REMATCH[1]}"
	else
		echo "error: '$tag' does not look like a vYYYY.M.D-<code>-meso[.dev] tag" >&2
		exit 1
	fi
	mint_epoch=$((FOUNDING_EPOCH + code / 10))
	echo "tag:       $tag"
	echo "code:      $code"
	echo "mint time: $(date -u -d "@${mint_epoch}" +"%Y-%m-%dT%H:%M:%SZ")"
}

if [ -n "$verify" ]; then
	verify_tag "$verify"
else
	compute_tag
fi
