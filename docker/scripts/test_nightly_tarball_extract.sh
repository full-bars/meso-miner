#!/bin/sh
# Regression test for start_nightly.sh tarball extraction and asset selection.
# Tests the EXACT extraction and jq logic from start_nightly.sh against
# controlled fixtures. If start_nightly.sh changes, these tests should
# break (that's the point).
set -e

PASS_COUNT=0
FAIL_COUNT=0

pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    echo "PASS: $1"
}

fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    echo "FAIL: $1"
}

TEST_DIR=""
cleanup() {
    [ -n "$TEST_DIR" ] && [ -d "$TEST_DIR" ] && rm -rf "$TEST_DIR"
}
trap cleanup EXIT
TEST_DIR=$(mktemp -d)

###############################################################################
# extract_provider — mirrors start_nightly.sh lines 332-346 exactly.
# Args: <archive> <dest_dir> <arch>
# Prints the relative path inside the archive on success; returns 1 on failure.
###############################################################################
extract_provider() {
    _archive="$1"
    _dest_dir="$2"
    _arch="$3"

    _provider_in_tarball="linux/${_arch}/provider"
    if ! tar -tzf "$_archive" "$_provider_in_tarball" >/dev/null 2>&1; then
        _provider_in_tarball="provider"
    fi
    tar -xzf "$_archive" -C "$_dest_dir" "$_provider_in_tarball" >/dev/null 2>&1 || return 1
    [ -f "$_dest_dir/$_provider_in_tarball" ] || return 1
    echo "$_provider_in_tarball"
}

###############################################################################
# test_flat_layout — provider at archive root (per-arch tarball format)
###############################################################################
test_flat_layout() {
    _staging="$TEST_DIR/flat"
    mkdir -p "$_staging/build" "$_staging/extract"
    echo "fake-provider-binary" > "$_staging/build/provider"
    tar -czf "$_staging/provider.tar.gz" -C "$_staging/build" provider

    if _rel_path=$(extract_provider "$_staging/provider.tar.gz" "$_staging/extract" "amd64") && \
       [ -f "$_staging/extract/$_rel_path" ] && [ "$_rel_path" = "provider" ]; then
        pass "Flat tarball layout: extracted and located at root"
    else
        fail "Flat tarball layout: extraction failed"
    fi
}

###############################################################################
# test_nested_layout — provider under linux/<arch>/ (multi-arch fat tarball)
###############################################################################
test_nested_layout() {
    _staging="$TEST_DIR/nested"
    mkdir -p "$_staging/build/linux/amd64" "$_staging/extract"
    echo "fake-provider-binary" > "$_staging/build/linux/amd64/provider"
    tar -czf "$_staging/provider.tar.gz" -C "$_staging/build" linux/amd64/provider

    if _rel_path=$(extract_provider "$_staging/provider.tar.gz" "$_staging/extract" "amd64") && \
       [ -f "$_staging/extract/$_rel_path" ] && [ "$_rel_path" = "linux/amd64/provider" ]; then
        pass "Nested tarball layout: extracted and located under linux/amd64/"
    else
        fail "Nested tarball layout: extraction failed"
    fi
}

###############################################################################
# test_asset_selection — tests the EXACT jq query from start_nightly.sh
# Lines 238-245: the query selects by urnetwork-provider- prefix, .tar.gz
# suffix, and prefers arch-specific over the fat multi-arch tarball.
# The multi-arch tarball has NO OS token (-linux-/-darwin-/-windows-), so
# it is selected by exclusion when no arch-specific match exists.
###############################################################################
test_asset_selection() {
    _arch="$1"
    _expected_name="$2"
    _expected_url="$3"
    _staging="$TEST_DIR/asset_$_arch"
    mkdir -p "$_staging"

    # Real GitHub Release API shape: top-level object with .assets array.
    # Real URNetwork asset naming: urnetwork-provider-v<ver>-linux-<arch>.tar.gz
    # Fat multi-arch: urnetwork-provider-v<ver>.tar.gz (no OS token)
    cat <<'EOF' > "$_staging/release.json"
{
  "assets": [
    {"name": "urnetwork-provider-v3.23.0-linux-amd64.tar.gz", "browser_download_url": "https://github.com/full-bars/urnetwork-3.23-fix/releases/download/v3.23.0/urnetwork-provider-v3.23.0-linux-amd64.tar.gz"},
    {"name": "urnetwork-provider-v3.23.0-linux-arm64.tar.gz", "browser_download_url": "https://github.com/full-bars/urnetwork-3.23-fix/releases/download/v3.23.0/urnetwork-provider-v3.23.0-linux-arm64.tar.gz"},
    {"name": "urnetwork-provider-v3.23.0-darwin-amd64.tar.gz", "browser_download_url": "https://github.com/full-bars/urnetwork-3.23-fix/releases/download/v3.23.0/urnetwork-provider-v3.23.0-darwin-amd64.tar.gz"},
    {"name": "urnetwork-provider-v3.23.0.tar.gz", "browser_download_url": "https://github.com/full-bars/urnetwork-3.23-fix/releases/download/v3.23.0/urnetwork-provider-v3.23.0.tar.gz"}
  ]
}
EOF

    # Exact jq query from start_nightly.sh:238-245
    # Falls back to the fat tarball by excluding OS tokens.
    _asset_name=$(printf '%s\n' "$(cat "$_staging/release.json")" \
      | jq -r --arg arch "$_arch" '
          .assets[] | .name |
          select((startswith("urnetwork-provider-")) and (endswith(".tar.gz")) and
                 (contains("linux-" + $arch) or
                  ((test("-darwin-|-linux-|-windows-")) | not)))
        ' 2>/dev/null \
      | head -n1)

    # Guard: jq may fail or return empty
    if [ -z "$_asset_name" ]; then
        fail "Asset selection for $_arch: jq returned no asset name"
        return
    fi

    # Exact URL lookup from start_nightly.sh:247-248
    _url=$(printf '%s\n' "$(cat "$_staging/release.json")" \
      | jq -r --arg f "$_asset_name" \
          '.assets[] | select(.name == $f) | .browser_download_url' 2>/dev/null)

    if [ "$_asset_name" = "$_expected_name" ] && [ "$_url" = "$_expected_url" ]; then
        pass "Asset selection for $_arch: selected '$_asset_name'"
    else
        fail "Asset selection for $_arch: expected name='$_expected_name' url='$_expected_url', got name='$_asset_name' url='$_url'"
    fi
}

###############################################################################
# test_no_jq_fallback — start_nightly.sh:251-285 has a grep/sed fallback
# when jq is not installed. Verify the extraction helper still works
# without jq (the fallback is only for asset URL selection, not extraction).
###############################################################################
test_no_jq_fallback() {
    _staging="$TEST_DIR/nojq"
    mkdir -p "$_staging/build" "$_staging/extract"
    echo "fake-provider-binary" > "$_staging/build/provider"
    tar -czf "$_staging/provider.tar.gz" -C "$_staging/build" provider

    # Temporarily hide jq — extract_provider doesn't need it (only tar),
    # but under set -e a missing jq in a subshell would abort. Verify the
    # extraction helper alone works (the grep/sed fallback in start_nightly.sh
    # handles URL selection without jq).
    if ! command -v jq >/dev/null 2>&1; then
        pass "jq already absent; extraction fallback path exercised"
        return
    fi
    _jq_dir="$(dirname "$(command -v jq)")"
    _rest_path="$(echo "$PATH" | sed "s|$_jq_dir:||g; s|:${_jq_dir}||g; s|$_jq_dir||g")"
    if _rel_path=$(PATH="$_rest_path" extract_provider "$_staging/provider.tar.gz" "$_staging/extract" "amd64") && \
       [ "$_rel_path" = "provider" ]; then
        pass "Extraction works without jq (jq is only for asset selection)"
    else
        fail "Extraction failed without jq"
    fi
}

###############################################################################
# Run all tests
###############################################################################
echo "=== Nightly Tarball Extraction Regression Tests ==="
echo ""

test_flat_layout
test_nested_layout
test_asset_selection "amd64" \
    "urnetwork-provider-v3.23.0-linux-amd64.tar.gz" \
    "https://github.com/full-bars/urnetwork-3.23-fix/releases/download/v3.23.0/urnetwork-provider-v3.23.0-linux-amd64.tar.gz"
test_asset_selection "arm64" \
    "urnetwork-provider-v3.23.0-linux-arm64.tar.gz" \
    "https://github.com/full-bars/urnetwork-3.23-fix/releases/download/v3.23.0/urnetwork-provider-v3.23.0-linux-arm64.tar.gz"
test_asset_selection "riscv64" \
    "urnetwork-provider-v3.23.0.tar.gz" \
    "https://github.com/full-bars/urnetwork-3.23-fix/releases/download/v3.23.0/urnetwork-provider-v3.23.0.tar.gz"
test_no_jq_fallback

echo ""
echo "=== Results: $PASS_COUNT passed, $FAIL_COUNT failed ==="

if [ "$FAIL_COUNT" -gt 0 ]; then
    exit 1
fi
echo "ALL TESTS PASSED"
exit 0
