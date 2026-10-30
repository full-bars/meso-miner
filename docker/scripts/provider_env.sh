#!/bin/sh
# provider_env.sh: map the v33 operator environment variables onto
# `provider provide` flags for the Docker start scripts.
#
# Sourced by start_stable.sh, start_nightly.sh and start_jwt.sh. Every
# variable is optional. With none of them set, provider_env_flags prints
# nothing, so the provide command line is exactly the legacy `provide`.
#
# The flags are spelled as in the v33 usage (provider/main.go):
#   --all-operators, --auto-register, --hotkey_seed_file=<file>,
#   --allow-client-registration, --max-memory=<mem>.
#
#   ALL_OPERATORS=1|true|yes|y|on
#       provide --all-operators: one child provider per listed operator.
#   AUTO_REGISTER=1|true|yes|y|on
#       With ALL_OPERATORS only: sign in to each operator with the hotkey.
#       Needs HOTKEY_SEED_FILE.
#   HOTKEY_SEED_FILE=<path>
#       With ALL_OPERATORS only: the 32-byte hotkey seed the supervisor signs
#       in with. The path must not contain whitespace.
#   ALLOW_CLIENT_REGISTRATION=auto|true|false|1|0|yes|no|y|n|on|off
#       true adds the flag, false omits it, and auto adds it only when the
#       state directory has neither .provider.key nor an operators/ directory.
#   OPERATORS_URL=<url>
#       With ALL_OPERATORS only: --operators-url.
#   OPERATOR=<domain>
#       Not supported by this provider Docker startup; the provider has no
#       single-operator provide mode.
#   MAX_MEMORY=<mem>
#       --max-memory=<mem>, for example 2gib. Must not contain whitespace.
#   PROVIDER_ARGS="<args>"
#       Extra arguments appended to `provide`, split on whitespace. Use this
#       for flags with no dedicated variable, for example --operators-refresh=30m.
#       Operator-list flags require ALL_OPERATORS=true.

# Print 1 for a true value, 0 for a false or empty value, and return 2 for
# anything else so callers can fail on a typo instead of silently changing
# the command line.
provider_env_bool() {
  case "$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')" in
    ""|0|false|no|off|n) printf '0\n' ;;
    1|true|yes|on|y)     printf '1\n' ;;
    *)
      _provider_env_error "${2:-boolean} must be true/1/yes/y/on or false/0/no/n/off${3:+ or $3} (case-insensitive; got '${1:-}')"
      return 2
      ;;
  esac
}

# True when ALL_OPERATORS asks for provide --all-operators.
provider_env_all_operators() {
  [ "$(provider_env_bool "${ALL_OPERATORS:-}" ALL_OPERATORS)" = "1" ]
}

# True when ALLOW_CLIENT_REGISTRATION=auto should add the flag: the state
# directory has no existing identity (.provider.key) and no operators/ tree.
provider_env_auto_allow_client_registration() {
  _pe_state_dir="${1:-${HOME:-/root}/.urnetwork}"
  [ ! -f "$_pe_state_dir/.provider.key" ] && [ ! -d "$_pe_state_dir/operators" ]
}

_provider_env_error() {
  if command -v log >/dev/null 2>&1; then
    log "[ERROR] $*" >&2
  else
    printf 'ERROR: %s\n' "$*" >&2
  fi
}

# True when PROVIDER_ARGS contains an operator-list flag. Values are split the
# same way as the eventual argv mapping, but pathname expansion stays disabled.
provider_env_args_have_operator_list_flag() {
  _pe_kind="${1:-all}"
  _pe_glob_off=0
  case $- in *f*) _pe_glob_off=1 ;; esac
  set -f
  _pe_found=0
  for _pe_arg in ${PROVIDER_ARGS:-}; do
    case "$_pe_arg" in
      --operators-url|--operators-url=*) _pe_found=1; break ;;
      --operators-refresh|--operators-refresh=*)
        [ "$_pe_kind" = url ] || { _pe_found=1; break; }
        ;;
    esac
  done
  [ "$_pe_glob_off" = 1 ] || set +f
  unset _pe_kind _pe_glob_off _pe_arg
  [ "$_pe_found" = 1 ]
}

# Print the extra `provider provide` arguments, one per line. Returns non-zero
# with an ERROR line on stderr when the environment is inconsistent.
provider_env_flags() {
  _pe_state_dir="${1:-${HOME:-/root}/.urnetwork}"

  if ! _pe_all="$(provider_env_bool "${ALL_OPERATORS:-}" ALL_OPERATORS)"; then
    return 1
  fi
  if ! _pe_auto="$(provider_env_bool "${AUTO_REGISTER:-}" AUTO_REGISTER)"; then
    return 1
  fi

  if [ -n "${OPERATORS_URL:-}" ]; then
    case "$OPERATORS_URL" in
      *[[:space:]]*)
        _provider_env_error "OPERATORS_URL must not contain whitespace"
        return 1
        ;;
    esac
    if [ "$_pe_all" != "1" ]; then
      _provider_env_error "OPERATORS_URL requires ALL_OPERATORS=true; this Docker startup only provides the full operator list"
      return 1
    fi
  fi
  if [ -n "${OPERATORS_URL:-}" ] && provider_env_args_have_operator_list_flag url; then
    _provider_env_error "set OPERATORS_URL or PROVIDER_ARGS --operators-url, not both"
    return 1
  fi
  if [ "$_pe_all" != "1" ] && provider_env_args_have_operator_list_flag all; then
    _provider_env_error "PROVIDER_ARGS operator-list flags require ALL_OPERATORS=true"
    return 1
  fi

  _pe_hotkey="${HOTKEY_SEED_FILE:-}"
  case "$_pe_hotkey" in
    *[[:space:]]*)
      _provider_env_error "HOTKEY_SEED_FILE must not contain whitespace (got '$_pe_hotkey')"
      return 1
      ;;
  esac

  # --hotkey_seed_file and --auto-register exist only in the
  # `provide --all-operators` usage. Refuse them on plain provide rather
  # than emit flags the binary would reject.
  if [ "$_pe_all" != "1" ]; then
    if [ -n "$_pe_hotkey" ]; then
      _provider_env_error "HOTKEY_SEED_FILE requires ALL_OPERATORS=1 (the flag exists only in provide --all-operators)"
      return 1
    fi
    if [ "$_pe_auto" = "1" ]; then
      _provider_env_error "AUTO_REGISTER requires ALL_OPERATORS=1 (the flag exists only in provide --all-operators)"
      return 1
    fi
  fi

  if [ "$_pe_all" = "1" ]; then
    printf '%s\n' --all-operators
    if [ "$_pe_auto" = "1" ]; then
      if [ -z "$_pe_hotkey" ]; then
        _provider_env_error "AUTO_REGISTER needs HOTKEY_SEED_FILE"
        return 1
      fi
      printf '%s\n' --auto-register
    fi
    [ -z "$_pe_hotkey" ] || printf '%s\n' "--hotkey_seed_file=$_pe_hotkey"
  fi

  _pe_max_memory="${MAX_MEMORY:-}"
  case "$_pe_max_memory" in
    *[[:space:]]*)
      _provider_env_error "MAX_MEMORY must not contain whitespace (got '$_pe_max_memory')"
      return 1
      ;;
  esac
  [ -z "$_pe_max_memory" ] || printf '%s\n' "--max-memory=$_pe_max_memory"

  _pe_allow="$(printf '%s' "${ALLOW_CLIENT_REGISTRATION:-}" | tr '[:upper:]' '[:lower:]')"
  case "$_pe_allow" in
    "")
      ;;
    auto)
      provider_env_auto_allow_client_registration "$_pe_state_dir" \
        && printf '%s\n' --allow-client-registration
      ;;
    *)
      if ! _pe_allow_bool="$(provider_env_bool "$_pe_allow" ALLOW_CLIENT_REGISTRATION auto)"; then
        return 1
      fi
      [ "$_pe_allow_bool" = "1" ] && printf '%s\n' --allow-client-registration
      ;;
  esac

  [ -z "${OPERATORS_URL:-}" ] || printf '%s\n' "--operators-url=$OPERATORS_URL"

  if [ -n "${PROVIDER_ARGS:-}" ]; then
    case $- in *f*) _pe_glob_off=1 ;; *) _pe_glob_off=0 ;; esac
    set -f
    # shellcheck disable=SC2086  # PROVIDER_ARGS is deliberately split on whitespace
    printf '%s\n' ${PROVIDER_ARGS}
    [ "$_pe_glob_off" = "1" ] || set +f
  fi
}

# Validate before authentication or stored-session changes, including direct
# start-script invocations.
provider_env_validate() {
  _pe_state_dir="${1:-${HOME:-/root}/.urnetwork}"
  provider_env_flags "$_pe_state_dir" >/dev/null || return 1

  if [ -n "${OPERATOR:-}" ]; then
    if ! provider_env_all_operators; then
      _provider_env_error "OPERATOR alone cannot provide: the provider has no single-operator provide mode; set ALL_OPERATORS=true to run all listed operators"
    else
      _provider_env_error "OPERATOR-scoped authentication is not supported by this provider Docker startup; omit OPERATOR and use ALL_OPERATORS=true for the full list"
    fi
    return 1
  fi
}
