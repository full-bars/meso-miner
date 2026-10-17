#!/usr/bin/env bash
# Is the official tree's CI ready to run? One clear PASS/FAIL.
#
# Checks, in order:
#   1. workflow-set parity against the reference fork (scripts/ci-parity-guard.sh)
#   2. actionlint on every workflow
#   3. every repo-local path a workflow references actually exists
#   4. personal-reference scan (fork names, hostnames, operator identifiers)
#   5. secrets matrix: needed by what, present or not, and what degrades if absent
#
# Usage: scripts/official-ci-readiness.sh [--offline] [--repo OWNER/NAME]
#   --offline   skip checks that need the network (parity reference set,
#               actionlint download, secret presence) and say so per check
#   --repo      repository whose secrets are probed (default urfoundation/meso-miner)
set -euo pipefail

OFFLINE=0
TARGET_REPO="urfoundation/meso-miner"
while [ $# -gt 0 ]; do
  case "$1" in
    --offline) OFFLINE=1; shift ;;
    --repo) TARGET_REPO="${2:-}"; shift 2 ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

cd "$(dirname "$0")/.."
FAILS=0
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; FAILS=$((FAILS + 1)); }
warn() { echo "WARN: $*"; }
skip() { echo "SKIP: $*"; }

WF=$(ls .github/workflows/*.yml 2>/dev/null | sort)
[ -n "$WF" ] || { echo "no workflows found; run from the repo root" >&2; exit 2; }
echo "official CI readiness: $(printf '%s\n' "$WF" | wc -l) workflows, repo ${TARGET_REPO}, offline=${OFFLINE}"
echo

# ---- 1. workflow-set parity ------------------------------------------------
echo "--- 1. workflow-set parity vs the reference fork"
set +e
if [ "$OFFLINE" = "1" ]; then
  guard_out=$(scripts/ci-parity-guard.sh --allow-offline 2>&1); guard_rc=$?
else
  guard_out=$(scripts/ci-parity-guard.sh 2>&1); guard_rc=$?
fi
set -e
printf '%s\n' "$guard_out" | sed -n '/^FAIL:/p'
if [ "$guard_rc" -eq 0 ]; then
  if printf '%s\n' "$guard_out" | grep -q '^WARN:.*offline'; then
    warn "parity not verified (offline)"
  else
    pass "workflow set matches the reference fork (exclusions explained)"
  fi
else
  fail "workflow set differs from the reference fork (see the FAIL lines above)"
fi
echo

# ---- 2. actionlint ---------------------------------------------------------
echo "--- 2. actionlint"
if command -v actionlint >/dev/null 2>&1; then
  set +e
  lint_out=$(actionlint $WF 2>&1); lint_rc=$?
  set -e
  if [ "$lint_rc" -eq 0 ]; then pass "actionlint clean on $(printf '%s\n' "$WF" | wc -l) workflows"
  else fail "actionlint reported problems"; printf '%s\n' "$lint_out" | head -40; fi
elif [ "$OFFLINE" = "1" ]; then
  skip "actionlint not installed and --offline given (would fetch it with go run)"
else
  set +e
  lint_out=$(go run github.com/rhysd/actionlint/cmd/actionlint@latest $WF 2>&1); lint_rc=$?
  set -e
  if [ "$lint_rc" -eq 0 ]; then pass "actionlint clean (via go run) on $(printf '%s\n' "$WF" | wc -l) workflows"
  else fail "actionlint reported problems (or could not be fetched)"; printf '%s\n' "$lint_out" | head -40; fi
fi
echo

# ---- 3. referenced repo-local paths exist ----------------------------------
echo "--- 3. referenced paths exist"
# repo-local script paths, anchored so docker/scripts/x.sh is not mistaken
# for a bare scripts/x.sh, and so variable expansions stay out
mapfile -t REFS < <(grep -rhoE '(^|[^A-Za-z0-9_./$-])((\.github|docker|monitoring|scripts)(/[A-Za-z0-9_.-]+)+\.(sh|ps1|py))' $WF | sed -E 's/^[^A-Za-z0-9_.]//' | sort -u)
missing_paths=0
for p in "${REFS[@]}"; do
  [ -n "$p" ] || continue
  if [ ! -f "$p" ]; then
    # a failed run-path probe (e.g. the shakedown installer URL) is not a
    # repo path; only files that should be in the tree count
    fail "workflow references ${p}, which does not exist in this tree"
    missing_paths=$((missing_paths + 1))
  fi
done
if [ "$missing_paths" -eq 0 ]; then pass "all ${#REFS[@]} referenced script paths exist"; fi
echo

# ---- 4. personal-reference scan --------------------------------------------
echo "--- 4. personal references"
# pattern|path-glob|allowed-line-regex|reason. A match is allowed only when it
# hits an ALLOW row's line regex; anything else fails.
PATTERNS=(
  'full-bars'
  '3cape'
  'fullbars\.xyz'
  'Ryanmello07'
  'klets'
  'mesocyclone'
)
ALLOW=(
  'full-bars|scripts/ci-parity-guard.sh|REF_REPO|the reference fork name is the guard input'
  'full-bars|.github/workflows/functional-soak.yml|network_id.*production.*full-bars|production-network refusal guard must stay'
  'full-bars|.github/workflows/tool-functional-smoke.yml|network_id.*production.*full-bars|production-network refusal guard must stay'
  'mesocyclone|.github/workflows/shakedown.yml|REFUSING: minted JWT is the production account|production-account refusal guard must stay'
  'mesocyclone|.github/workflows/docker-shakedown.yml|REFUSING: minted JWT is the production account|production-account refusal guard must stay'
  'mesocyclone|.github/workflows/shakedown.yml|== .mesocyclone.|production-account refusal guard must stay'
  'mesocyclone|.github/workflows/docker-shakedown.yml|== .mesocyclone.|production-account refusal guard must stay'
)
# the scanner itself is skipped on purpose: its pattern table has to contain
# the literal patterns to be able to search for them
personal_hits=0
for pat in "${PATTERNS[@]}"; do
  while IFS= read -r hit; do
    [ -n "$hit" ] || continue
    allowed=0
    for row in "${ALLOW[@]}"; do
      a_pat="${row%%|*}"; rest="${row#*|}"; a_glob="${rest%%|*}"; rest="${rest#*|}"; a_re="${rest%%|*}"; a_why="${rest#*|}"
      [ "$a_pat" = "$pat" ] || continue
      case "$hit" in
        $a_glob:*) if printf '%s' "$hit" | grep -qE "$a_re"; then allowed=1; last_why="$a_why"; fi ;;
      esac
    done
    if [ "$allowed" = "1" ]; then
      pass "allowed ${pat}: ${hit%%:*} (${last_why})"
    else
      fail "personal reference '${pat}': ${hit}"
      personal_hits=$((personal_hits + 1))
    fi
  done < <(grep -rnE "$pat" .github/workflows .github/scripts scripts 2>/dev/null | grep -v '^scripts/official-ci-readiness.sh:' || true)
done
if [ "$personal_hits" -eq 0 ]; then pass "no unlisted personal references in workflows or scripts"; fi
echo

# ---- 5. secrets matrix -----------------------------------------------------
echo "--- 5. secrets matrix"
# name|degrades-if-absent
KNOWN_SECRETS=(
  'GITHUB_TOKEN|built in, always present'
  'GAUNTLET_USER|shakedown/docker-shakedown/docker-multi-container/functional-soak/tool-functional-smoke cannot mint the disposable JWT and exit 75 (ENV_BLOCKER, not a pass)'
  'GAUNTLET_PW|same as GAUNTLET_USER: no mint, exit 75'
  'GEMINI_API_KEY|the shakedown AI-review step is skipped; the suite still runs'
  'VIRUS_TOTAL|the release scan job fails on tag runs by design (never gates publication)'
  'BUILD_NOTIFY|Discord release notification is skipped with a printed note'
  'DISCORD_WEBHOOK|upstream_monitor and cfaa-blocklist-sync notifications are skipped'
  'OPENCODE_API_KEY|upstream_monitor model calls are skipped for that lane'
  'DEEPSEEK_API_KEY|upstream_monitor model calls are skipped for that lane'
  'OPENROUTER_API_KEY|cfaa-blocklist-sync model calls are skipped'
  'GPG_RELEASE_KEY|cfaa-blocklist-sync signed-commit path degrades to unsigned or skips'
)
declare -A DEGRADES
for row in "${KNOWN_SECRETS[@]}"; do DEGRADES["${row%%|*}"]="${row#*|}"; done

mapfile -t USED < <(grep -rhoE 'secrets\.[A-Z0-9_]+' $WF | sort -u | sed 's/^secrets\.//')
PRESENT=""
if [ "$OFFLINE" = "1" ]; then
  PRESENT="unknown"
  skip "secret presence not probed (--offline); the matrix below is needed/degradation only"
else
  if command -v gh >/dev/null 2>&1; then
    set +e
    PRESENT=$(gh secret list -R "$TARGET_REPO" --json name -q '.[].name' 2>/dev/null); prc=$?
    set -e
    if [ "$prc" -ne 0 ]; then PRESENT=""; warn "could not read secrets on ${TARGET_REPO} (no access); presence shown as unknown"; fi
  else
    PRESENT=""; warn "gh not installed; presence shown as unknown"
  fi
fi

unlisted=0
printf '%-20s %-9s %s\n' "SECRET" "PRESENT" "DEGRADES IF ABSENT"
for s in "${USED[@]}"; do
  if [ -z "${DEGRADES[$s]:-}" ]; then
    printf '%-20s %-9s %s\n' "$s" "?" "UNDOCUMENTED: add a degradation row before relying on it"
    fail "secret ${s} is used by a workflow but has no documented degradation"
    unlisted=$((unlisted + 1))
    continue
  fi
  if [ "$PRESENT" = "unknown" ]; then p="unknown"
  elif [ -z "$PRESENT" ]; then p="unknown"
  elif printf '%s\n' "$PRESENT" | grep -qx "$s"; then p="present"
  else p="MISSING"; fi
  printf '%-20s %-9s %s\n' "$s" "$p" "${DEGRADES[$s]}"
done
if [ "$unlisted" -eq 0 ]; then pass "every referenced secret has a documented degradation"; fi
echo

# ---- verdict ---------------------------------------------------------------
echo "===== CI READINESS: ${FAILS} FAIL(s) ====="
[ "$FAILS" -eq 0 ] || exit 1
