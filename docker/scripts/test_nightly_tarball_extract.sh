#!/bin/sh
set -e

# Regression test for Bug 3: start_nightly.sh tarball extraction logic
# Tests flat layout, nested layout, and arch-specific asset selection.

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

cleanup() {
    rm -rf "$TEST_DIR"
}

trap cleanup EXIT

TEST_DIR=$(mktemp -d)

###############################################################################
# Test 1: Flat tarball layout (provider at root of archive)
###############################################################################
test_flat_layout() {
    STAGING="$TEST_DIR/flat"
    mkdir -p "$STAGING/build" "$STAGING/extract"
    echo "fake-provider-binary" > "$STAGING/build/provider"
    tar -czf "$STAGING/provider.tar.gz" -C "$STAGING/build" provider

    # Simulate start_nightly.sh logic
    ARCHIVE="$STAGING/provider.tar.gz"
    UPDATE_TMP="$STAGING/extract"
    A_SYS_ARCH="amd64"
    PROVIDER_IN_TARBALL="linux/${A_SYS_ARCH}/provider"
    if ! tar -tzf "$ARCHIVE" "$PROVIDER_IN_TARBALL" >/dev/null 2>&1; then
        PROVIDER_IN_TARBALL="provider"
    fi
    tar -xzf "$ARCHIVE" -C "$UPDATE_TMP" "$PROVIDER_IN_TARBALL"

    if [ -f "$UPDATE_TMP/provider" ]; then
        pass "Flat tarball layout: provider extracted at root"
    else
        fail "Flat tarball layout: provider not found at root"
    fi
}

###############################################################################
# Test 2: Nested tarball layout (provider under linux/<arch>/)
###############################################################################
test_nested_layout() {
    STAGING="$TEST_DIR/nested"
    mkdir -p "$STAGING/build/linux/amd64" "$STAGING/extract"
    echo "fake-provider-binary" > "$STAGING/build/linux/amd64/provider"
    tar -czf "$STAGING/provider.tar.gz" -C "$STAGING/build" linux/amd64/provider

    # Simulate start_nightly.sh logic
    ARCHIVE="$STAGING/provider.tar.gz"
    UPDATE_TMP="$STAGING/extract"
    A_SYS_ARCH="amd64"
    PROVIDER_IN_TARBALL="linux/${A_SYS_ARCH}/provider"
    if ! tar -tzf "$ARCHIVE" "$PROVIDER_IN_TARBALL" >/dev/null 2>&1; then
        PROVIDER_IN_TARBALL="provider"
    fi
    tar -xzf "$ARCHIVE" -C "$UPDATE_TMP" "$PROVIDER_IN_TARBALL"

    if [ -f "$UPDATE_TMP/linux/amd64/provider" ]; then
        pass "Nested tarball layout: provider extracted under linux/amd64/"
    else
        fail "Nested tarball layout: provider not found under linux/amd64/"
    fi
}

###############################################################################
# Test 3: Arch-specific asset selection with jq
###############################################################################
test_asset_selection_amd64() {
    STAGING="$TEST_DIR/asset_amd64"
    mkdir -p "$STAGING"

    ASSETS_JSON='[
        {"name": "provider_linux-amd64.tar.gz", "browser_download_url": "https://example.com/provider_linux-amd64.tar.gz"},
        {"name": "provider_linux-arm64.tar.gz", "browser_download_url": "https://example.com/provider_linux-arm64.tar.gz"},
        {"name": "provider_linux-multiarch.tar.gz", "browser_download_url": "https://example.com/provider_linux-multiarch.tar.gz"}
    ]'
    echo "$ASSETS_JSON" > "$STAGING/assets.json"

    # Simulate the jq selection: prefer arch-specific, fall back to multiarch
    A_SYS_ARCH="amd64"
    ASSET_URL=$(jq -r --arg arch "$A_SYS_ARCH" '
        [.[] | select(.name | test("linux-" + $arch + "\\."))] |
        if length > 0 then .[0].browser_download_url
        else
            [.[] | select(.name | test("multiarch"))] |
            if length > 0 then .[0].browser_download_url
            else empty end
        end
    ' "$STAGING/assets.json")

    if echo "$ASSET_URL" | grep -q "linux-amd64"; then
        pass "Asset selection: amd64 selects linux-amd64 asset"
    else
        fail "Asset selection: amd64 did not select linux-amd64 asset (got: $ASSET_URL)"
    fi
}

test_asset_selection_arm64() {
    STAGING="$TEST_DIR/asset_arm64"
    mkdir -p "$STAGING"

    ASSETS_JSON='[
        {"name": "provider_linux-amd64.tar.gz", "browser_download_url": "https://example.com/provider_linux-amd64.tar.gz"},
        {"name": "provider_linux-arm64.tar.gz", "browser_download_url": "https://example.com/provider_linux-arm64.tar.gz"},
        {"name": "provider_linux-multiarch.tar.gz", "browser_download_url": "https://example.com/provider_linux-multiarch.tar.gz"}
    ]'
    echo "$ASSETS_JSON" > "$STAGING/assets.json"

    A_SYS_ARCH="arm64"
    ASSET_URL=$(jq -r --arg arch "$A_SYS_ARCH" '
        [.[] | select(.name | test("linux-" + $arch + "\\."))] |
        if length > 0 then .[0].browser_download_url
        else
            [.[] | select(.name | test("multiarch"))] |
            if length > 0 then .[0].browser_download_url
            else empty end
        end
    ' "$STAGING/assets.json")

    if echo "$ASSET_URL" | grep -q "linux-arm64"; then
        pass "Asset selection: arm64 selects linux-arm64 asset"
    else
        fail "Asset selection: arm64 did not select linux-arm64 asset (got: $ASSET_URL)"
    fi
}

test_asset_selection_fallback() {
    STAGING="$TEST_DIR/asset_fallback"
    mkdir -p "$STAGING"

    ASSETS_JSON='[
        {"name": "provider_linux-multiarch.tar.gz", "browser_download_url": "https://example.com/provider_linux-multiarch.tar.gz"}
    ]'
    echo "$ASSETS_JSON" > "$STAGING/assets.json"

    A_SYS_ARCH="amd64"
    ASSET_URL=$(jq -r --arg arch "$A_SYS_ARCH" '
        . as $assets |
        [$assets[] | select(.name | test("linux-" + $arch + "\\."))] |
        if length > 0 then .[0].browser_download_url
        else
            [$assets[] | select(.name | test("multiarch"))] |
            if length > 0 then .[0].browser_download_url
            else empty end
        end
    ' "$STAGING/assets.json")

    if echo "$ASSET_URL" | grep -q "multiarch"; then
        pass "Asset selection: falls back to multiarch when arch-specific missing"
    else
        fail "Asset selection: fallback to multiarch failed (got: $ASSET_URL)"
    fi
}

###############################################################################
# Run all tests
###############################################################################
echo "=== Nightly Tarball Extraction Regression Tests ==="
echo ""

test_flat_layout
test_nested_layout
test_asset_selection_amd64
test_asset_selection_arm64
test_asset_selection_fallback

echo ""
echo "=== Results: $PASS_COUNT passed, $FAIL_COUNT failed ==="

if [ "$FAIL_COUNT" -gt 0 ]; then
    exit 1
fi
echo "ALL TESTS PASSED"
exit 0
