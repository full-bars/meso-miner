#!/usr/bin/env bash
# Fails when this tree's workflow set drifts from the reference fork's set,
# except names listed in EXCLUSIONS (each with a mandatory reason).
#
# The reference set is the workflow file set on REF_REPO:REF_BRANCH. The local
# set is THIS checkout's .github/workflows. A name present in the reference and
# missing here is a dropped workflow unless it carries a reason; a name present
# here and not in the reference is drift the other way and is reported too.
# Nothing is ever dropped silently.
#
# Usage:
#   scripts/ci-parity-guard.sh [--list-file FILE] [--allow-offline]
#
#   --list-file FILE   read the reference workflow names from FILE (one per
#                      line) instead of the GitHub API; used by offline runs
#                      and by the test suite
#   --allow-offline    a reference-set fetch failure becomes a loud WARN
#                      instead of a FAIL
#
# Env overrides: REF_REPO (default full-bars/urnetwork-3.23-fix),
#                REF_BRANCH (default main).
set -euo pipefail

REF_REPO="${REF_REPO:-full-bars/urnetwork-3.23-fix}"
REF_BRANCH="${REF_BRANCH:-main}"

# Workflows the reference has that this tree deliberately does not run.
# Format: <name>|<reason>. The reason is mandatory: a blank reason is a hard
# error, because an unexplained exclusion is exactly the silent drop this
# guard exists to prevent.
EXCLUSIONS=(
  "wiki-sync.yml|the fork publishes its docs to its own GitHub wiki; the official repo has no wiki to sync"
)

# Workflows this tree has that the reference does not. Same format, same rule.
EXTRA_ALLOWED=(
  "portable-core.yml|added on the v33 development line; the fork's main gains it with the v33 merge. Stricter check while the line lives on integrate/v33-final: REF_BRANCH=integrate/v33-final"
)

LIST_FILE=""
ALLOW_OFFLINE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --list-file) LIST_FILE="${2:-}"; shift 2 ;;
    --allow-offline) ALLOW_OFFLINE=1; shift ;;
    -h|--help) sed -n '2,22p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

FAILS=0
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; FAILS=$((FAILS + 1)); }
warn() { echo "WARN: $*"; }

# ---- build the exclusion maps (name -> reason), rejecting blank reasons -----
declare -A EXCL EXTRA
for entry in "${EXCLUSIONS[@]}"; do
  name="${entry%%|*}"; reason="${entry#*|}"
  [ "$name" != "$entry" ] && [ -n "$reason" ] || { echo "guard config error: exclusion '$entry' has no reason" >&2; exit 2; }
  EXCL["$name"]="$reason"
done
for entry in "${EXTRA_ALLOWED[@]}"; do
  name="${entry%%|*}"; reason="${entry#*|}"
  [ "$name" != "$entry" ] && [ -n "$reason" ] || { echo "guard config error: extra-allow '$entry' has no reason" >&2; exit 2; }
  EXTRA["$name"]="$reason"
done

# ---- reference set ---------------------------------------------------------
REF_LIST=""
if [ -n "$LIST_FILE" ]; then
  [ -f "$LIST_FILE" ] || { echo "reference list file not found: $LIST_FILE" >&2; exit 2; }
  REF_LIST=$(sed -e 's/[[:space:]]*$//' -e '/^$/d' "$LIST_FILE" | sort -u)
  echo "reference set: $LIST_FILE ($(printf '%s\n' "$REF_LIST" | wc -l) entries)"
else
  if RAW=$(curl -fsSL "https://api.github.com/repos/${REF_REPO}/contents/.github/workflows?ref=${REF_BRANCH}" 2>/dev/null); then
    REF_LIST=$(printf '%s' "$RAW" | python3 -c '
import json,sys
data=json.load(sys.stdin)
for e in data:
    if e.get("type")=="file" and e["name"].endswith(".yml"):
        print(e["name"])' | sort -u)
    echo "reference set: ${REF_REPO}@${REF_BRANCH} ($(printf '%s\n' "$REF_LIST" | wc -l) workflows)"
  else
    if [ "$ALLOW_OFFLINE" = "1" ]; then
      warn "could not fetch the reference workflow set from ${REF_REPO}@${REF_BRANCH} (offline); parity NOT verified"
      exit 0
    fi
    fail "could not fetch the reference workflow set from ${REF_REPO}@${REF_BRANCH}; re-run with --allow-offline to skip parity"
    echo; echo "===== PARITY GUARD: ${FAILS} FAIL(s) ====="
    exit 1
  fi
fi
[ -n "$REF_LIST" ] || { echo "reference set is empty, refusing to compare" >&2; exit 2; }

# ---- local set -------------------------------------------------------------
LOCAL_LIST=$(cd "$(dirname "$0")/.." && ls .github/workflows/*.yml 2>/dev/null | xargs -n1 basename | sort -u)
[ -n "$LOCAL_LIST" ] || { echo "no local workflows found; run from the repo root" >&2; exit 2; }

# ---- compare ---------------------------------------------------------------
MISSING=0
while IFS= read -r name; do
  [ -n "$name" ] || continue
  if printf '%s\n' "$LOCAL_LIST" | grep -qx "$name"; then
    continue
  fi
  MISSING=$((MISSING + 1))
  if [ -n "${EXCL[$name]:-}" ]; then
    pass "dropped on purpose: ${name} (${EXCL[$name]})"
  else
    fail "workflow ${name} exists in ${REF_REPO}@${REF_BRANCH} but not in this tree, with no exclusion reason"
  fi
done <<< "$REF_LIST"

EXTRA=0
while IFS= read -r name; do
  [ -n "$name" ] || continue
  if printf '%s\n' "$REF_LIST" | grep -qx "$name"; then
    continue
  fi
  EXTRA=$((EXTRA + 1))
  if [ -n "${EXTRA[$name]:-}" ]; then
    pass "extra on purpose: ${name} (${EXTRA[$name]})"
  else
    fail "workflow ${name} exists in this tree but not in ${REF_REPO}@${REF_BRANCH}, with no reason"
  fi
done <<< "$LOCAL_LIST"

# ---- stale exclusion check -------------------------------------------------
for name in "${!EXCL[@]}"; do
  if ! printf '%s\n' "$REF_LIST" | grep -qx "$name"; then
    warn "exclusion ${name} is stale: it is no longer in the reference set, drop the entry"
  fi
done
for name in "${!EXTRA[@]}"; do
  if printf '%s\n' "$LOCAL_LIST" | grep -qx "$name" && printf '%s\n' "$REF_LIST" | grep -qx "$name"; then
    warn "extra-allow ${name} is stale: the reference has it too, drop the entry"
  fi
done

echo
if [ "$FAILS" -gt 0 ]; then
  echo "===== PARITY GUARD: ${FAILS} FAIL(s) ====="
  exit 1
fi
echo "===== PARITY GUARD: OK ($(printf '%s\n' "$REF_LIST" | wc -l) reference workflows, ${MISSING} excluded, ${EXTRA} extra-allowed) ====="
