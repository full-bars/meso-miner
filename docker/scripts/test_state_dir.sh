#!/bin/bash
# Offline state-dir tests: actual entrypoint/provider_common run with staged scripts and a stub provider; no Docker or network.
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="${REPO_DIR:-$(cd "$HERE/../.." && pwd)}"
tmp="$(mktemp -d)" || exit 1
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/app" "$tmp/common" "$tmp/bin"
cp "$REPO/docker/scripts/provider_common.sh" "$REPO/docker/scripts/provider_env.sh" "$REPO/docker/scripts/jwt_verify.sh" "$tmp/common/"

cat > "$tmp/app/start_jwt.sh" <<'STUB'
#!/bin/sh
printf '%s\n' "${URNETWORK_STATE_DIR:-}" "${URNETWORK_STATE_DIR_LOGGED:-}" > "$DISPATCH_LOG"
STUB
chmod +x "$tmp/app/start_jwt.sh"

cat > "$tmp/app/urnetwork_amd64_stable" <<'STUB'
#!/bin/sh
case "$1" in
  --version) printf '%s\n' stub-version ;;
  choose_network)
    printf '%s\n' "$URNETWORK_STATE_DIR" > "$NETWORK_LOG"
    printf '%s\n' network > "$URNETWORK_STATE_DIR/network.json"
    ;;
  provide)
    printf 'provide:%s\n' "$URNETWORK_STATE_DIR" >> "$PROVIDER_LOG"
    ;;
  *) exit 1 ;;
esac
STUB
chmod +x "$tmp/app/urnetwork_amd64_stable"

cat > "$tmp/bin/mountpoint" <<'STUB'
#!/bin/sh
[ "$#" -eq 2 ] && [ "$1" = -q ] && [ "$2" = /root/.urnetwork ]
STUB
chmod +x "$tmp/bin/mountpoint"

cat > "$tmp/common/runner.sh" <<'RUNNER'
#!/bin/sh
set -eu
PROVIDER_MODE=stable
PROVIDER_FLAVOR=stable
. "$COMMON_DIR/provider_common.sh"
A_SYS_ARCH=amd64
pgrep() { return 1; }
func_choose_network
if [ "${RUN_UPDATE_TEST:-no}" = yes ]; then
  : > "$STATE_DIR/update-pending"
  func_start_provider_stable
fi
printf 'state=%s\njwt=%s\nexported=%s\nlogged=%s\n' \
  "$STATE_DIR" "$JWT_FILE" "$URNETWORK_STATE_DIR" "$URNETWORK_STATE_DIR_LOGGED"
RUNNER
chmod +x "$tmp/common/runner.sh"

pass=0
fail=0
rc=0
out="$tmp/out"

t() {
  local name="$1"; shift
  if "$@"; then pass=$((pass+1)); printf 'PASS: %s\n' "$name"
  else fail=$((fail+1)); printf 'FAIL: %s\n' "$name"; rg -n '.' "$out" || :
  fi
}

run_entrypoint() {
  local case_dir="$1"
  shift
  mkdir -p "$case_dir/home"
  : > "$case_dir/dispatch"
  dispatch="$case_dir/dispatch"
  if (cd "$case_dir" && env -i PATH="$tmp/bin:$PATH" HOME="$case_dir/home" APP_DIR="$tmp/app" \
      DISPATCH_LOG="$case_dir/dispatch" AUTH=authcode "$@" \
      sh "$REPO/docker/scripts/entrypoint.sh" > "$out" 2>&1); then rc=0; else rc=$?; fi
}
read_dispatch() {
  { IFS= read -r dispatch_state; IFS= read -r dispatch_logged; } < "$dispatch"
}
entry_dispatch_ok() {
  [ "$rc" -eq 0 ] && read_dispatch && [ "$dispatch_state" = "$expected" ] && [ "$dispatch_logged" = 1 ]
}
make_jwt() {
  local now payload b64
  now="$(date +%s)"
  payload="{\"sub\":\"state-dir-test\",\"exp\":$((now + 3600))}"
  b64="$(printf '%s' "$payload" | base64 | tr -d '\r\n[:space:]' | tr '+/' '-_' | tr -d '=')"
  printf '%s.%s.synthetic_sig\n' 'eyJhbGciOiJub25lIn0' "$b64"
}

# Entrypoint resolves relative overrides, checks sessions there, and exports the canonical path.
case_dir="$tmp/entry-override"
mkdir -p "$case_dir/state dir"
make_jwt > "$case_dir/state dir/jwt"
run_entrypoint "$case_dir" URNETWORK_STATE_DIR='  state dir  ' AUTHCODE=
expected="$case_dir/state dir"
entry_override_ok() {
  [ "$rc" -eq 0 ] && entry_dispatch_ok && rg -Fq 'Existing session found' "$out" &&
    rg -Fq "State directory: $expected" "$out" &&
    rg -Fq "WARNING: Without a persistent volume mounted at $expected" "$out" &&
    [ "$(rg -c 'State directory:' "$out")" -eq 1 ]
}
t 'entrypoint reuses JWT and exports the trimmed relative state dir' entry_override_ok

# The unset override still resolves to HOME/.urnetwork and passes it to the child.
case_dir="$tmp/entry-default"
run_entrypoint "$case_dir" AUTHCODE= URNETWORK_AUTH_CODE=fake-code
expected="$case_dir/home/.urnetwork"
entry_default_ok() {
  [ "$rc" -eq 0 ] && entry_dispatch_ok && rg -Fq "State directory: $expected" "$out" &&
    rg -Fq "WARNING: Without a persistent volume mounted at $expected" "$out" &&
    [ "$(rg -c 'State directory:' "$out")" -eq 1 ]
}
t 'entrypoint defaults state dir under HOME and exports it once' entry_default_ok

# Run the real common startup against a stub binary; both network and update state must use the resolved directory.
run_common() {
  local case_dir="$1" update_mode="$2"
  shift 2
  mkdir -p "$case_dir/home"
  : > "$case_dir/network.log"
  : > "$case_dir/provider.log"
  if (cd "$case_dir" && env -i PATH="$PATH" HOME="$case_dir/home" APP_DIR="$tmp/app" \
      COMMON_DIR="$tmp/common" NETWORK_LOG="$case_dir/network.log" PROVIDER_LOG="$case_dir/provider.log" \
      UR_API_URL=https://api.example.invalid UR_CONNECT_URL=wss://connect.example.invalid \
      RUN_UPDATE_TEST="$update_mode" "$@" sh "$tmp/common/runner.sh" > "$out" 2>&1); then rc=0; else rc=$?; fi
}
common_state_ok() {
  [ "$rc" -eq 0 ] && rg -Fqx "state=$expected" "$out" && rg -Fqx "jwt=$expected/jwt" "$out" &&
    rg -Fqx "exported=$expected" "$out"
}
network_written() {
  rg -Fqx "$expected" "$case_dir/network.log" && rg -Fqx network "$expected/network.json"
}

case_dir="$tmp/common-override"
run_common "$case_dir" yes URNETWORK_STATE_DIR=' custom state '
expected="$case_dir/custom state"
t 'provider_common resolves the override and sets JWT under it' common_state_ok
t 'provider choose_network writes in the override state dir' network_written
update_marker_consumed() {
  [ ! -e "$expected/update-pending" ] && [ "$(rg -Fxc "provide:$expected" "$case_dir/provider.log")" -eq 2 ]
}
t 'update-pending is consumed from the override state dir' update_marker_consumed

case_dir="$tmp/common-default"
run_common "$case_dir" no
expected="$case_dir/home/.urnetwork"
t 'provider_common defaults to HOME/.urnetwork when unset' common_state_ok
t 'provider choose_network writes in the default state dir' network_written

echo "Results: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
