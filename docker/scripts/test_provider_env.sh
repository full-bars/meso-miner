#!/bin/bash
# test_provider_env.sh: verifies boolean parsing, operator-mode validation and
# the mapping from Docker environment variables to `provider provide` flags.
# Offline only: provider commands are stubbed, and no credentials or network are used.
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="${REPO_DIR:-$(cd "$HERE/../.." && pwd)}"
HELPER="$REPO/docker/scripts/provider_env.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0
t() {
    local name="$1"
    shift
    if "$@" >/dev/null 2>&1; then
        pass=$((pass+1))
        echo "PASS: $name"
        return 0
    else
        fail=$((fail+1))
        echo "FAIL: $name"
        return 1
    fi
}

# Run provider_env_flags in a clean environment. The first argument is the
# state directory; the rest are VAR=value assignments.
flags() {
    local state_dir="$1"
    shift
    env -i PATH="$PATH" HOME="$tmp/home" STATE_DIR="$state_dir" "$@" \
        sh -c '. "$0"; provider_env_flags "$STATE_DIR"' "$HELPER"
}

validate() {
    local state_dir="$1"
    shift
    env -i PATH="$PATH" HOME="$tmp/home" STATE_DIR="$state_dir" "$@" \
        sh -c '. "$0"; provider_env_validate "$STATE_DIR"' "$HELPER"
}

expect_flags() {
    local name="$1"
    local want="$2"
    local state_dir="$3"
    shift 3
    local got
    got="$(flags "$state_dir" "$@")"
    t "$name" test "$got" = "$want"
}

mkdir -p "$tmp/home" "$tmp/state"

# --- Legacy: no variables means no flags -----------------------------------
expect_flags "no variables: no flags" "" "$tmp/state"

# --- ALL_OPERATORS ----------------------------------------------------------
expect_flags "ALL_OPERATORS=1" "--all-operators" "$tmp/state" ALL_OPERATORS=1
expect_flags "ALL_OPERATORS=true (case-insensitive)" "--all-operators" "$tmp/state" ALL_OPERATORS=TRUE
expect_flags "ALL_OPERATORS=0 turns it off" "" "$tmp/state" ALL_OPERATORS=0
for spelling in y Y yes true on 1; do
    expect_flags "ALL_OPERATORS=$spelling is true" "--all-operators" "$tmp/state" "ALL_OPERATORS=$spelling"
done
for spelling in n N no false off 0; do
    expect_flags "ALL_OPERATORS=$spelling is false" "" "$tmp/state" "ALL_OPERATORS=$spelling"
done
out="$(flags "$tmp/state" ALL_OPERATORS=maybe 2>&1)"; rc=$?
t "ALL_OPERATORS=maybe fails" test "$rc" -ne 0
t "ALL_OPERATORS=maybe names the variable" grep -q "ALL_OPERATORS must be" <<<"$out"

# --- ALLOW_CLIENT_REGISTRATION ---------------------------------------------
expect_flags "ALLOW_CLIENT_REGISTRATION=Y" "--allow-client-registration" "$tmp/state" ALLOW_CLIENT_REGISTRATION=Y
expect_flags "ALLOW_CLIENT_REGISTRATION=y (lowercase)" "--allow-client-registration" "$tmp/state" ALLOW_CLIENT_REGISTRATION=y
expect_flags "ALLOW_CLIENT_REGISTRATION=N" "" "$tmp/state" ALLOW_CLIENT_REGISTRATION=N
expect_flags "ALLOW_CLIENT_REGISTRATION=n (lowercase)" "" "$tmp/state" ALLOW_CLIENT_REGISTRATION=n
expect_flags "ALLOW_CLIENT_REGISTRATION=on" "--allow-client-registration" "$tmp/state" ALLOW_CLIENT_REGISTRATION=on
expect_flags "ALLOW_CLIENT_REGISTRATION=off" "" "$tmp/state" ALLOW_CLIENT_REGISTRATION=off

fresh="$tmp/fresh"
mkdir -p "$fresh"
expect_flags "auto on a fresh state dir adds the flag" "--allow-client-registration" "$fresh" ALLOW_CLIENT_REGISTRATION=auto

with_key="$tmp/with_key"
mkdir -p "$with_key"
printf 'seed' > "$with_key/.provider.key"
expect_flags "auto with .provider.key adds nothing" "" "$with_key" ALLOW_CLIENT_REGISTRATION=auto

with_operators="$tmp/with_operators"
mkdir -p "$with_operators/operators"
expect_flags "auto with operators/ adds nothing" "" "$with_operators" ALLOW_CLIENT_REGISTRATION=auto

both="$tmp/both"
mkdir -p "$both/operators"
printf 'seed' > "$both/.provider.key"
expect_flags "auto with both markers adds nothing" "" "$both" ALLOW_CLIENT_REGISTRATION=auto

out="$(flags "$tmp/state" ALLOW_CLIENT_REGISTRATION=maybe 2>&1)"; rc=$?
t "ALLOW_CLIENT_REGISTRATION=maybe fails" test "$rc" -ne 0
t "ALLOW_CLIENT_REGISTRATION=maybe names the values" rg -q "ALLOW_CLIENT_REGISTRATION must be true/1/yes/y/on or false/0/no/n/off.*or auto" <<<"$out"

# --- HOTKEY_SEED_FILE and AUTO_REGISTER ------------------------------------
expect_flags "ALL_OPERATORS with hotkey seed, no auto-register" \
    $'--all-operators\n--hotkey_seed_file=/run/secrets/hotkey' "$tmp/state" \
    ALL_OPERATORS=1 HOTKEY_SEED_FILE=/run/secrets/hotkey
expect_flags "ALL_OPERATORS with auto-register and hotkey seed" \
    $'--all-operators\n--auto-register\n--hotkey_seed_file=/run/secrets/hotkey' "$tmp/state" \
    ALL_OPERATORS=1 AUTO_REGISTER=1 HOTKEY_SEED_FILE=/run/secrets/hotkey

out="$(flags "$tmp/state" AUTO_REGISTER=1 2>&1)"; rc=$?
t "AUTO_REGISTER without ALL_OPERATORS fails" test "$rc" -ne 0
t "AUTO_REGISTER without ALL_OPERATORS names the cause" grep -q "AUTO_REGISTER requires ALL_OPERATORS=1" <<<"$out"

out="$(flags "$tmp/state" HOTKEY_SEED_FILE=/run/secrets/hotkey 2>&1)"; rc=$?
t "HOTKEY_SEED_FILE without ALL_OPERATORS fails" test "$rc" -ne 0
t "HOTKEY_SEED_FILE without ALL_OPERATORS names the cause" grep -q "HOTKEY_SEED_FILE requires ALL_OPERATORS=1" <<<"$out"

out="$(flags "$tmp/state" ALL_OPERATORS=1 AUTO_REGISTER=1 2>&1)"; rc=$?
t "AUTO_REGISTER without a hotkey seed fails" test "$rc" -ne 0
t "AUTO_REGISTER without a hotkey seed names the cause" grep -q "AUTO_REGISTER needs HOTKEY_SEED_FILE" <<<"$out"

out="$(flags "$tmp/state" ALL_OPERATORS=1 HOTKEY_SEED_FILE="/run/secret key" 2>&1)"; rc=$?
t "hotkey path with whitespace fails" test "$rc" -ne 0
t "hotkey path with whitespace names the cause" grep -q "HOTKEY_SEED_FILE must not contain whitespace" <<<"$out"

# --- MAX_MEMORY -------------------------------------------------------------
expect_flags "MAX_MEMORY maps to --max-memory" "--max-memory=2gib" "$tmp/state" MAX_MEMORY=2gib
expect_flags "empty MAX_MEMORY adds nothing" "" "$tmp/state" MAX_MEMORY=

out="$(flags "$tmp/state" MAX_MEMORY="2 gib" 2>&1)"; rc=$?
t "MAX_MEMORY with whitespace fails" test "$rc" -ne 0
t "MAX_MEMORY with whitespace names the cause" grep -q "MAX_MEMORY must not contain whitespace" <<<"$out"

# --- PROVIDER_ARGS and operator URL gates ----------------------------------
expect_flags "PROVIDER_ARGS splits on whitespace" $'--one\n--two' "$tmp/state" \
    PROVIDER_ARGS="--one --two"
expect_flags "PROVIDER_ARGS does not glob" "--file=/tmp/*.txt" "$tmp/state" PROVIDER_ARGS="--file=/tmp/*.txt"
out="$(flags "$tmp/state" PROVIDER_ARGS="--operators-url=https://example.test/ops.yml" 2>&1)"; rc=$?
t "PROVIDER_ARGS operators URL without ALL_OPERATORS fails" test "$rc" -ne 0
t "PROVIDER_ARGS operators URL refusal names ALL_OPERATORS" rg -q "PROVIDER_ARGS operator-list flags require ALL_OPERATORS=true" <<<"$out"
expect_flags "PROVIDER_ARGS operator URL is allowed with ALL_OPERATORS" \
    $'--all-operators\n--operators-url=https://example.test/ops.yml' "$tmp/state" \
    ALL_OPERATORS=true PROVIDER_ARGS="--operators-url=https://example.test/ops.yml"
out="$(flags "$tmp/state" OPERATORS_URL=https://example.test/ops.yml 2>&1)"; rc=$?
t "OPERATORS_URL without ALL_OPERATORS fails" test "$rc" -ne 0
t "OPERATORS_URL refusal names ALL_OPERATORS" rg -q "OPERATORS_URL requires ALL_OPERATORS=true" <<<"$out"
expect_flags "OPERATORS_URL maps to provide with ALL_OPERATORS" \
    $'--all-operators\n--operators-url=https://example.test/ops.yml' "$tmp/state" \
    ALL_OPERATORS=on OPERATORS_URL=https://example.test/ops.yml
out="$(flags "$tmp/state" ALL_OPERATORS=1 OPERATORS_URL=https://example.test/ops.yml \
    PROVIDER_ARGS="--operators-url=https://example.test/ops.yml" 2>&1)"; rc=$?
t "duplicate operators URLs are rejected" test "$rc" -ne 0
t "duplicate operators URL error names both settings" rg -q "set OPERATORS_URL or PROVIDER_ARGS --operators-url" <<<"$out"

out="$(validate "$tmp/state" OPERATOR=example.invalid 2>&1)"; rc=$?
t "OPERATOR without ALL_OPERATORS is refused" test "$rc" -ne 0
t "OPERATOR refusal names the missing provide mode" rg -q "OPERATOR alone cannot provide:.*no single-operator provide mode" <<<"$out"
out="$(validate "$tmp/state" ALL_OPERATORS=true OPERATOR=example.invalid 2>&1)"; rc=$?
t "unsupported OPERATOR auth is refused even in all-operators mode" test "$rc" -ne 0
t "OPERATOR auth refusal explains the provider startup limitation" rg -q "OPERATOR-scoped authentication is not supported" <<<"$out"

# --- Full combination and order --------------------------------------------
expect_flags "all flags: exact order, PROVIDER_ARGS last" \
    $'--all-operators\n--auto-register\n--hotkey_seed_file=/run/secrets/hotkey\n--max-memory=4gib\n--allow-client-registration\n--operators-url=https://example.test/ops.yml\n--operators-refresh=30m' \
    "$fresh" \
    ALL_OPERATORS=1 AUTO_REGISTER=1 HOTKEY_SEED_FILE=/run/secrets/hotkey \
    MAX_MEMORY=4gib ALLOW_CLIENT_REGISTRATION=auto \
    PROVIDER_ARGS="--operators-url=https://example.test/ops.yml --operators-refresh=30m"

# --- Start script wiring: the flags reach the provider binary --------------
extract_func() {
    awk -v fn="$2" '
        index($0, fn"(){") == 1 || index($0, fn"() {") == 1 { grab = 1 }
        grab { print }
        grab && /^}/ { exit }
    ' "$1"
}
run_provide_case() {
    local script_file="$1"
    shift
    local test_dir="$tmp/case_${RANDOM}_$$"
    mkdir -p "$test_dir/app" "$test_dir/home/.urnetwork"
    printf 'initial-jwt-token' > "$test_dir/home/.urnetwork/jwt"

    cat > "$test_dir/app/urnetwork_amd64_stable" << 'STUB_EOF'
#!/bin/sh
case "$1" in
    --version)
        echo "3.23.test"
        exit 0
        ;;
    provide)
        shift
        : > "$TEST_DIR/provide_args.txt"
        for arg in "$@"; do
            printf '%s\n' "$arg" >> "$TEST_DIR/provide_args.txt"
        done
        exit 0
        ;;
    *)
        exit 0
        ;;
esac
STUB_EOF
    chmod +x "$test_dir/app/urnetwork_amd64_stable"
    cp "$test_dir/app/urnetwork_amd64_stable" "$test_dir/app/urnetwork_amd64_nightly"

    local runner="$test_dir/runner.sh"
    {
        # -e mirrors production: provider_common.sh runs under set -e.
        echo "set -eu"
        echo "export TEST_DIR='$test_dir'"
        echo "APP_DIR='$test_dir/app'"
        echo "A_SYS_ARCH='amd64'"
        echo "HOME='$test_dir/home'"
        echo "STATE_DIR='$test_dir/home/.urnetwork'"
        echo "JWT_FILE='$test_dir/home/.urnetwork/jwt'"
        echo "USER_AUTH=''"
        echo "PASSWORD=''"
        echo "export URNETWORK_AUTH_CODE=''"
        echo "log() { :; }"
        echo "sleep() { :; }"
        echo "pgrep() { return 1; }"
        case "$script_file" in
            start_stable.sh)  _mode=stable;  _flavor=stable ;;
            start_nightly.sh) _mode=nightly; _flavor=nightly ;;
            start_jwt.sh)     _mode=jwt;     _flavor=stable ;;
            *)                _mode=stable;  _flavor=stable ;;
        esac
        echo "PROVIDER_MODE='$_mode'"
        echo "PROVIDER_FLAVOR='$_flavor'"
        # provider_common.sh initializes these at the top; the extracted loop
        # functions reference them under set -u, so set them here too.
        echo "PROVIDER_STOPPING=0"
        echo "PROVIDER_CHILD_PID=\"\""
        echo ". '$HELPER'"
        case "$script_file" in
            start_stable.sh)  _fn=func_start_provider_stable ;;
            start_nightly.sh) _fn=func_start_provider_nightly ;;
            start_jwt.sh)     _fn=func_start_provider_jwt ;;
            *)                _fn=func_start_provider ;;
        esac
        extract_func "$REPO/docker/scripts/provider_common.sh" resolve_provider_env_args
        extract_func "$REPO/docker/scripts/provider_common.sh" func_exit_if_stopping
        extract_func "$REPO/docker/scripts/provider_common.sh" func_sleep
        extract_func "$REPO/docker/scripts/provider_common.sh" func_run_provider
        extract_func "$REPO/docker/scripts/provider_common.sh" "$_fn"
        echo "$_fn"
    } > "$runner"

    env -i PATH="$PATH" HOME="$test_dir/home" TEST_DIR="$test_dir" "$@" \
        bash "$runner" >/dev/null 2>&1 || true
    echo "$test_dir"
}

provide_args() {
    cat "$1/provide_args.txt" 2>/dev/null || true
}

# Legacy: every start script still calls bare provide when nothing is set.
for script in start_stable.sh start_nightly.sh start_jwt.sh; do
    dir="$(run_provide_case "$script")"
    t "$script: legacy provide has no extra args" test -z "$(provide_args "$dir")"
done

# Stable: the full flag set reaches argv in order.
dir="$(run_provide_case start_stable.sh \
    ALL_OPERATORS=1 AUTO_REGISTER=1 HOTKEY_SEED_FILE=/run/secrets/hotkey \
    ALLOW_CLIENT_REGISTRATION=Y MAX_MEMORY=2gib \
    PROVIDER_ARGS="--operators-url=https://example.test/ops.yml")"
want=$'--all-operators\n--auto-register\n--hotkey_seed_file=/run/secrets/hotkey\n--max-memory=2gib\n--allow-client-registration\n--operators-url=https://example.test/ops.yml'
t "start_stable.sh: mapped flags reach provide" test "$(provide_args "$dir")" = "$want"

# Nightly: hotkey without auto-register.
dir="$(run_provide_case start_nightly.sh ALL_OPERATORS=1 HOTKEY_SEED_FILE=/run/secrets/hotkey)"
want=$'--all-operators\n--hotkey_seed_file=/run/secrets/hotkey'
t "start_nightly.sh: mapped flags reach provide" test "$(provide_args "$dir")" = "$want"

# JWT: MAX_MEMORY only, legacy auth flow untouched.
dir="$(run_provide_case start_jwt.sh MAX_MEMORY=1gib)"
t "start_jwt.sh: mapped flags reach provide" test "$(provide_args "$dir")" = "--max-memory=1gib"

# Stable: ALLOW_CLIENT_REGISTRATION=auto on a fresh state dir.
dir="$(run_provide_case start_stable.sh ALLOW_CLIENT_REGISTRATION=auto)"
t "start_stable.sh: auto adds the flag on a fresh state dir" \
    test "$(provide_args "$dir")" = "--allow-client-registration"

# Both entrypoint and direct starts must reject OPERATOR before any login or provider call.
case_dir="$tmp/operator-entrypoint"
mkdir -p "$case_dir/home" "$case_dir/app" "$case_dir/state"
printf '%s\n' stale-jwt > "$case_dir/state/jwt"
entry_log="$case_dir/dispatch"
entry_out="$case_dir/out"
if env -i PATH="$PATH" HOME="$case_dir/home" APP_DIR="$case_dir/app" DISPATCH_LOG="$entry_log" \
    AUTH=authcode URNETWORK_AUTH_CODE=fake-code OPERATOR=example.invalid \
    URNETWORK_STATE_DIR="$case_dir/state" sh "$REPO/docker/scripts/entrypoint.sh" > "$entry_out" 2>&1; then rc=0; else rc=$?; fi
t "entrypoint refuses OPERATOR without ALL_OPERATORS" test "$rc" -ne 0
t "entrypoint names the missing single-operator provide mode" rg -q "OPERATOR alone cannot provide:.*no single-operator provide mode" "$entry_out"
t "entrypoint refuses before dispatching auth" test ! -e "$entry_log"
t "entrypoint validates operator settings before touching the stored session" rg -Fqx stale-jwt "$case_dir/state/jwt"

case_dir="$tmp/operators-url-entrypoint"
mkdir -p "$case_dir/home" "$case_dir/app"
entry_log="$case_dir/dispatch"
entry_out="$case_dir/out"
if env -i PATH="$PATH" HOME="$case_dir/home" APP_DIR="$case_dir/app" DISPATCH_LOG="$entry_log" \
    AUTH=authcode URNETWORK_AUTH_CODE=fake-code \
    PROVIDER_ARGS=--operators-url=https://example.test/ops.yml \
    URNETWORK_STATE_DIR="$case_dir/state" sh "$REPO/docker/scripts/entrypoint.sh" > "$entry_out" 2>&1; then rc=0; else rc=$?; fi
t "entrypoint refuses operators URL before auth without ALL_OPERATORS" test "$rc" -ne 0
t "entrypoint names the required operator mode" rg -q "PROVIDER_ARGS operator-list flags require ALL_OPERATORS=true" "$entry_out"
t "operators URL refusal happens before startup dispatch" test ! -e "$entry_log"

case_dir="$tmp/operator-direct-start"
mkdir -p "$case_dir/home" "$case_dir/app"
start_out="$case_dir/out"
if env -i PATH="$PATH" HOME="$case_dir/home" APP_DIR="$case_dir/app" \
    OPERATOR=example.invalid URNETWORK_STATE_DIR="$case_dir/state" \
    sh "$REPO/docker/scripts/start_stable.sh" > "$start_out" 2>&1; then rc=0; else rc=$?; fi
t "direct start refuses OPERATOR without ALL_OPERATORS before credential checks" test "$rc" -ne 0 &&
    rg -q "OPERATOR alone cannot provide:.*no single-operator provide mode" "$start_out" &&
    ! rg -q "USER_AUTH or PASSWORD not set" "$start_out"

echo ""
echo "Results: $pass passed, $fail failed"
exit $((fail > 0))
