#!/bin/bash
# Offline startup-safety tests: provider, curl and sleeps are stubbed; /app/proxy.txt is redirected. No Docker or network.
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="${REPO_DIR:-$(cd "$HERE/../.." && pwd)}"
tmp="$(mktemp -d)" || exit 1
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/functions" "$tmp/app" "$tmp/bin" "$tmp/ip-tmp"

python3 - "$REPO/docker/scripts/provider_common.sh" "$REPO/docker/scripts/pelican_panel.sh" "$tmp/functions" <<'PY'
import pathlib, re, sys
common, pelican, dest = map(pathlib.Path, sys.argv[1:])
for source, funcs, name in (
    (common, ('func_check_proxy_import', 'func_check_proxy', 'func_ip_checker', 'func_login_retry_loop'), 'common'),
    (pelican, ('func_do_login',), 'pelican'),
):
    text = source.read_text()
    result = []
    for func in funcs:
        match = re.search(r'^' + re.escape(func) + r'\(\) \{.*?^\}\n', text, re.M | re.S)
        if not match:
            raise SystemExit('missing ' + func + ' in ' + str(source))
        result.append(match.group().replace('"/app/proxy.txt"', '"$TEST_APP/proxy.txt"'))
    (dest / (name + '.sh')).write_text('\n'.join(result) + '\n')
PY
if [ "$?" -ne 0 ]; then echo 'FAIL: function extraction'; exit 1; fi

cat > "$tmp/app/urnetwork_amd64_stable" <<'STUB'
#!/bin/sh
case "$1" in
  proxy)
    printf '%s\n' "$@" > "$ARGV_LOG"
    if [ "${PROXY_FAIL:-no}" = yes ]; then
      printf '%s\n' partial > "$HOME/.urnetwork/proxy"
      exit 17
    fi
    [ "${PROXY_FAIL:-no}" != silent ] || exit 0
    rg -q '^[[:space:]]*[^[:space:]#]' "$TEST_APP/proxy.txt" || exit 0
    jq '.servers = {"new:1080":""}' "$HOME/.urnetwork/proxy" > "$HOME/.urnetwork/new" || exit 1
    mv "$HOME/.urnetwork/new" "$HOME/.urnetwork/proxy"
    ;;
  auth)
    printf 'Jwt written to %s\n' "$JWT_FILE"
    if [ "${AUTH_ECHO:-yes}" = yes ]; then
      printf 'echoed %s %s\n' "$USER_AUTH" "$PASSWORD"
    elif [ "${AUTH_ECHO:-yes}" = normalized ]; then
      # the server may echo the address normalized and nothing else; the
      # redaction must catch it on the address alone
      printf 'echoed %s\n' "$(printf '%s' "$USER_AUTH" | tr '[:upper:]' '[:lower:]')"
    else
      printf '%s\n' 'diagnostic output without credentials'
    fi
    printf '%s\n' jwt-token > "$JWT_FILE"
    ;;
  *) exit 1 ;;
esac
STUB
chmod +x "$tmp/app/urnetwork_amd64_stable"

cat > "$tmp/bin/curl" <<'STUB'
#!/bin/sh
output=
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then output=$2; shift 2; else shift; fi
done
case "$CURL_MODE" in
  fetch-fail)
    printf 'printf executed > "%s"\n' "$CURL_MARKER" > "$output"
    exit 22
    ;;
  script-fail) printf '%s\n' 'exit 9' > "$output" ;;
  success) printf '%s\n' 'exit 0' > "$output" ;;
  *) exit 1 ;;
esac
STUB
chmod +x "$tmp/bin/curl"

pass=0
fail=0
rc=0
out="$tmp/out"
argv="$tmp/argv"
run() {
  local funcs="$1" action="$2" home="$3" state="$4" proxy_fail="${5:-no}" curl_mode="${6:-fetch-fail}"
  local user_auth="${7-private-user-123}" password="${8-private-password-456}" auth_echo="${9:-yes}"
  rm -f "$argv"
  if env -i PATH="$tmp/bin:$PATH" HOME="$home" STATE_DIR="$state" \
      TEST_APP="$tmp/app" FUNCTIONS="$tmp/functions/$funcs.sh" ACTION="$action" \
      PROXY_FAIL="$proxy_fail" ARGV_LOG="$argv" JWT_FILE="$state/jwt" \
      USER_AUTH="$user_auth" PASSWORD="$password" AUTH_ECHO="$auth_echo" DEBUG=true \
      ENABLE_IP_CHECKER=true IP_CHECKER_URL=https://invalid.example/check \
      CURL_MODE="$curl_mode" CURL_MARKER="$tmp/executed" TMPDIR="$tmp/ip-tmp" \
      bash -c '
        set -eu
        . "$FUNCTIONS"
        log() { printf "%s\n" "$*"; }
        func_sleep() { :; }
        func_exit_if_stopping() { :; }
        APP_DIR="$TEST_APP"
        A_SYS_ARCH=amd64
        PROVIDER_FLAVOR=stable
        case "$ACTION" in
          proxy) func_check_proxy ;;
          ip) func_ip_checker ;;
          login) func_login_retry_loop ;;
          pelican-login) func_do_login ;;
        esac
      ' > "$out" 2>&1; then
    rc=0
  else
    rc=$?
  fi
}
report() {
  local name="$1"; shift
  if "$@"; then pass=$((pass+1)); printf 'PASS: %s\n' "$name"
  else fail=$((fail+1)); printf 'FAIL: %s\n' "$name"; rg -n '.' "$out" || :
  fi
}
no_proxy_staging_left() {
  for stage in "$state"/.proxy-import.*; do [ ! -e "$stage" ] || return 1; done
}
proxy_kept_without_mount() {
  [ "$rc" -eq 0 ] && cmp -s "$state/proxy" "$original" &&
    rg -q 'kept existing proxy configuration' "$out" && [ ! -e "$argv" ]
}
proxy_kept_after_failure() {
  [ "$rc" -eq 0 ] && cmp -s "$state/proxy" "$original" &&
    rg -q 'kept the existing configuration' "$out" && rg -qx 'proxy' "$argv" && rg -qx 'add' "$argv" && no_proxy_staging_left
}

# A corrupt saved config is not a fallback: the provider cannot load it, so the
# container would end up sending traffic direct and must stop instead.
proxy_corrupt_fatal() {
  [ "$rc" -ne 0 ] && cmp -s "$state/proxy" "$original" &&
    rg -q 'refusing to start without the mounted proxy' "$out" && no_proxy_staging_left
}
proxy_replaced_atomically() {
  [ "$rc" -eq 0 ] && rg -q 'configuration replaced' "$out" &&
    jq -e '.servers == {"new:1080":""} and .auths.keep.user == "alice"' "$state/proxy" >/dev/null &&
    [ "$(stat -c %a "$state/proxy")" = 600 ] && no_proxy_staging_left
}
proxy_none_without_mount() {
  [ "$rc" -eq 0 ] && [ ! -e "$state" ] && rg -q 'none configured' "$out" && [ ! -e "$argv" ]
}
proxy_corrupt_healed() {
  local backup
  local -a backups=("$state"/proxy.corrupt.*)
  [ "${#backups[@]}" -eq 1 ] || return 1
  backup="${backups[0]}"
  [ "$rc" -eq 0 ] && cmp -s "$backup" "$original" &&
    jq -e '.servers == {"new:1080":""}' "$state/proxy" >/dev/null &&
    rg -Fq "$state/proxy" "$out" && rg -Fq "$backup" "$out" &&
    rg -q 'WARN.*corrupt' "$out" && no_proxy_staging_left
}
proxy_empty_rejected() {
  proxy_kept_after_failure && rg -q 'empty|no servers' "$out" &&
    ! rg -q 'configuration replaced' "$out"
}
credentials_diagnostic_visible() {
  [ "$rc" -eq 0 ] && rg -q 'diagnostic output without credentials' "$out" &&
    ! rg -q 'AUTH_OUTPUT=\[redacted\]' "$out"
}
credentials_remaining_redacted() {
  [ "$rc" -eq 0 ] && rg -q 'AUTH_OUTPUT=\[redacted\]' "$out" &&
    ! rg -Fq 'private-user-123' "$out" && ! rg -Fq 'private-password-456' "$out"
}
proxy_first_imported() {
  [ "$rc" -eq 0 ] && jq -e '.servers == {"new:1080":""}' "$state/proxy" >/dev/null &&
    [ "$(stat -c %a "$state/proxy")" = 600 ] && no_proxy_staging_left
}
fetch_failure_not_executed() {
  [ "$rc" -eq 0 ] && [ ! -e "$tmp/executed" ] && rg -q 'IP checker fetch failed' "$out" &&
    ! rg -q 'script ran successfully|script failed' "$out"
}
script_failed_classified() {
  [ "$rc" -eq 0 ] && rg -q 'IP checker script failed' "$out" &&
    ! rg -q 'script ran successfully|fetch failed' "$out"
}
script_success_classified() {
  [ "$rc" -eq 0 ] && rg -q 'IP checker script ran successfully' "$out" &&
    ! rg -q 'fetch failed|script failed' "$out"
}
no_temp_files() {
  set -- "$tmp/ip-tmp"/*
  [ ! -e "$1" ]
}
credentials_redacted() {
  [ "$rc" -eq 0 ] && ! rg -Fq 'private-user-123' "$out" && ! rg -Fq 'private-password-456' "$out" &&
    rg -q 'USER_AUTH set=yes length=' "$out" && rg -q 'PASSWORD set=yes length=' "$out" &&
    rg -q 'AUTH_OUTPUT=\[redacted\]' "$out"
}

# Missing mount and failed imports must preserve the live proxy file.
state="$tmp/home-no-mount/.urnetwork"; home="$tmp/home-no-mount"; mkdir -p "$state"; original="$tmp/proxy-original"
printf '%s\n' '{"servers":{"old:1080":""},"auths":{"keep":{"user":"alice","password":"secret"}}}' > "$state/proxy"
cp "$state/proxy" "$original"
run common proxy "$home" "$state"
report 'proxy config survives when proxy.txt is absent' proxy_kept_without_mount

home="$tmp/home-none"; state="$home/.urnetwork"; mkdir -p "$home"
run common proxy "$home" "$state"
report 'missing proxy mount leaves absent config absent' proxy_none_without_mount

home="$tmp/home-invalid"; state="$home/.urnetwork"; mkdir -p "$state"; original="$tmp/proxy-original-invalid"
printf '%s\n' '{not-json' > "$state/proxy"; cp "$state/proxy" "$original"
printf '%s\n' 'server:port:user:pass' > "$tmp/app/proxy.txt"
run common proxy "$home" "$state"
report 'malformed existing proxy config is backed up and healed' proxy_corrupt_healed

home="$tmp/home-null"; state="$home/.urnetwork"; mkdir -p "$state"; original="$tmp/proxy-original-null"
printf '%s\n' 'null' > "$state/proxy"; cp "$state/proxy" "$original"
run common proxy "$home" "$state"
report 'null existing proxy config is backed up and healed' proxy_corrupt_healed

home="$tmp/home-invalid-fail"; state="$home/.urnetwork"; mkdir -p "$state"; original="$tmp/proxy-original-invalid-fail"
printf '%s\n' '{not-json' > "$state/proxy"; cp "$state/proxy" "$original"
run common proxy "$home" "$state" silent
report 'failed import keeps corrupt original in place' proxy_corrupt_fatal

state="$tmp/home-fail/.urnetwork"; home="$tmp/home-fail"; mkdir -p "$state"; original="$tmp/proxy-original-fail"
printf '%s\n' '{"servers":{"old:1080":""},"auths":{"keep":{"user":"alice","password":"secret"}}}' > "$state/proxy"
cp "$state/proxy" "$original"
printf '%s\n' 'server:port:user:pass' > "$tmp/app/proxy.txt"
run common proxy "$home" "$state" yes
report 'failed proxy import leaves live config unchanged' proxy_kept_after_failure

for scenario in silent empty comments; do
  home="$tmp/home-$scenario"; state="$home/.urnetwork"; mkdir -p "$state"; original="$tmp/proxy-original-$scenario"
  printf '%s\n' '{"servers":{"old:1080":""},"auths":{"keep":{"user":"alice"}}}' > "$state/proxy"
  cp "$state/proxy" "$original"
  case "$scenario" in
    silent) printf '%s\n' 'server:port:user:pass' > "$tmp/app/proxy.txt" ;;
    empty) : > "$tmp/app/proxy.txt" ;;
    comments) printf '%s\n' '# comment' '  # another comment' '' > "$tmp/app/proxy.txt" ;;
  esac
  run common proxy "$home" "$state" "$scenario"
  report "proxy $scenario import rejects empty staging and preserves live bytes" proxy_empty_rejected
done
printf '%s\n' 'server:port:user:pass' > "$tmp/app/proxy.txt"

state="$tmp/home-success/.urnetwork"; home="$tmp/home-success"; mkdir -p "$state"; original="$tmp/proxy-original-success"
printf '%s\n' '{"servers":{"old:1080":""},"auths":{"keep":{"user":"alice","password":"secret"}}}' > "$state/proxy"
cp "$state/proxy" "$original"
run common proxy "$home" "$state"
report 'successful proxy import atomically replaces servers and preserves auth' proxy_replaced_atomically

home="$tmp/home-first"; state="$home/.urnetwork"; mkdir -p "$home"
run common proxy "$home" "$state"
report 'first proxy import initializes config and installs servers' proxy_first_imported

# A failed HTTP fetch must not pipe an error body or partial body to a shell.
state="$tmp/ip-state"; home="$tmp/ip-home"; mkdir -p "$state" "$home"
run common ip "$home" "$state" no fetch-fail
report 'failed IP-checker fetch does not execute the downloaded body' fetch_failure_not_executed
report 'IP-checker temporary files are removed after fetch failure' no_temp_files
run common ip "$home" "$state" no script-fail
report 'IP-checker script failure is distinguished from fetch failure' script_failed_classified
report 'IP-checker temporary files are removed after script failure' no_temp_files
run common ip "$home" "$state" no success
report 'successful IP-checker script is reported' script_success_classified
report 'IP-checker temporary files are removed after success' no_temp_files

# Both shared and Pelican login paths must redact credentials in DEBUG output.
state="$tmp/login-state"; home="$tmp/login-home"; mkdir -p "$state" "$home"
run common login "$home" "$state"
report 'provider login debug output never prints credentials' credentials_redacted
rm -f "$state/jwt"
run pelican pelican-login "$home" "$state"
report 'Pelican login debug output never prints credentials' credentials_redacted

# A server that echoes the address normalized must still be redacted.
rm -f "$state/jwt"
run common login "$home" "$state" no fetch-fail 'Private-User-123' 'private-password-456' normalized
report 'a case-normalized credential echo is still redacted' credentials_remaining_redacted
rm -f "$state/jwt"
run pelican pelican-login "$home" "$state" no fetch-fail 'Private-User-123' 'private-password-456' normalized
report 'Pelican redacts a case-normalized credential echo' credentials_remaining_redacted

visible_and_redacted() {
  [ "$visible" = yes ] && credentials_remaining_redacted
}
for path in common pelican; do
  case "$path" in common) action=login ;; pelican) action=pelican-login ;; esac
  for empty in user password both; do
    case "$empty" in
      user) user_auth=''; password='private-password-456' ;;
      password) user_auth='private-user-123'; password='' ;;
      both) user_auth=''; password='' ;;
    esac
    run "$path" "$action" "$home" "$state" no fetch-fail "$user_auth" "$password" no
    visible=no
    if credentials_diagnostic_visible; then visible=yes; fi
    if [ "$empty" = both ]; then
      report "$path login shows diagnostics with both credentials empty" test "$visible" = yes
    else
      run "$path" "$action" "$home" "$state" no fetch-fail "$user_auth" "$password" yes
      report "$path login shows diagnostics with empty $empty and redacts the other" visible_and_redacted
    fi
  done
done

# The nightly bootstrap must download the binary before the proxy import runs it,
# and a failed import must not abort startup. The function-level cases above cannot
# see either, because they run func_check_proxy outside the real sequence.
bootstrap_order() {
  block=$(sed -n '/^      nightly)/,/^        ;;/p' "$HERE/provider_common.sh")
  update_line=$(printf '%s\n' "$block" | grep -n 'func_check_update' | head -1 | cut -d: -f1)
  proxy_line=$(printf '%s\n' "$block" | grep -n 'func_check_proxy' | head -1 | cut -d: -f1)
  [ -n "$update_line" ] && [ -n "$proxy_line" ] && [ "$update_line" -lt "$proxy_line" ]
}
report 'nightly downloads the binary before importing proxies' bootstrap_order

bootstrap_proxy_fatal() {
  block=$(sed -n '/^      nightly)/,/^        ;;/p' "$HERE/provider_common.sh")
  printf '%s\n' "$block" | grep -q '^        func_check_proxy$'
}
report 'nightly lets a fatal proxy failure reach startup' bootstrap_proxy_fatal

echo "Results: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
