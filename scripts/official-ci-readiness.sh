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
note_count=0
undecided_hits=0
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
# actionlint reports real problems (syntax, topology, unknown keys) AND
# shellcheck style notes. Only the real ones gate readiness; style notes are
# counted and printed as a note so a style-only tree passes.
LINT_OUT=""
LINT_RC=0
LINT_SKIPPED=0
if command -v actionlint >/dev/null 2>&1; then
  set +e; LINT_OUT=$(actionlint $WF 2>&1); LINT_RC=$?; set -e
elif [ "$OFFLINE" = "1" ]; then
  skip "actionlint not installed and --offline given (would fetch it with go run)"
  LINT_SKIPPED=1
else
  set +e; LINT_OUT=$(go run github.com/rhysd/actionlint/cmd/actionlint@latest $WF 2>&1); LINT_RC=$?; set -e
fi
if [ "$LINT_SKIPPED" = "1" ]; then
  :
elif [ "$LINT_RC" -eq 0 ]; then
  pass "actionlint clean on $(printf '%s\n' "$WF" | wc -l) workflows"
else
  CLASSIFIED=$(printf '%s\n' "$LINT_OUT" | python3 -c '
import re, sys
lines = sys.stdin.read().splitlines()
hdr = re.compile(r"^[^ \t].*?:\d+:\d+: ")
findings, cur = [], None
for ln in lines:
    if hdr.match(ln):
        if cur: findings.append(cur)
        cur = [ln]
    elif cur is not None:
        cur.append(ln)
if cur: findings.append(cur)
style = [f for f in findings if "shellcheck" in f[0]]
real = [f for f in findings if "shellcheck" not in f[0]]
print(f"FINDINGS={len(findings)}")
print(f"STYLE_COUNT={len(style)}")
print(f"REAL_COUNT={len(real)}")
for f in real[:40]:
    sys.stdout.write("\n".join(f) + "\n")
')
  LINT_FINDINGS=$(printf '%s\n' "$CLASSIFIED" | sed -n 's/^FINDINGS=//p')
  LINT_STYLE=$(printf '%s\n' "$CLASSIFIED" | sed -n 's/^STYLE_COUNT=//p')
  LINT_REAL=$(printf '%s\n' "$CLASSIFIED" | sed -n 's/^REAL_COUNT=//p')
  printf '%s\n' "$CLASSIFIED" | sed -n '/^REAL_COUNT=/,$p' | sed '1d'
  if [ "${LINT_FINDINGS:-0}" -eq 0 ]; then
    fail "actionlint exited ${LINT_RC} with output it could not parse (fetch failure?)"
    printf '%s\n' "$LINT_OUT" | head -10
  elif [ "${LINT_REAL:-0}" -gt 0 ]; then
    fail "actionlint: ${LINT_REAL} real problem(s): syntax, topology or unknown keys"
  else
    pass "actionlint: no syntax or topology problems (${LINT_STYLE:-0} shellcheck style note(s) not gated)"
  fi
  if [ "${LINT_STYLE:-0}" -gt 0 ]; then
    note_count=$((note_count + 1))
    echo "NOTE: ${LINT_STYLE} shellcheck style note(s) reported but not gated (style only)"
  fi
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
  'full-bars|scripts/Provider_Install_Mac.sh|Maintained by|operator-kept: maintainer credit in the installer header'
  'full-bars|scripts/Provider_Install_Linux.sh|Maintained by|operator-kept: maintainer credit in the installer header'
  'full-bars|scripts/Provider_Install_FreeBSD.sh|Maintained by|operator-kept: maintainer credit in the installer header'
  'full-bars|scripts/Provider_Install_Win32.ps1|Maintained by|operator-kept: maintainer credit in the installer header'
  'fullbars\.xyz|scripts/Provider_Install_Linux.sh|support@fullbars\.xyz|operator-kept: support address in the installers'
  'full-bars|scripts/test_help_text.sh|assert_contains "full-bars"|operator-kept: the help header credits the maintainer'
)
# Matches the operator has not ruled on yet: report as WARN, never as FAIL.
DECISIONS=(
  'fullbars\.xyz|scripts/Provider_Install_FreeBSD.sh|dl\.fullbars\.xyz|needs operator decision: legacy mirror comment'
  'fullbars\.xyz|scripts/test_docker_update_tarball.sh|dl\.fullbars\.xyz|needs operator decision: legacy mirror redirect test case'
  'full-bars|.github/workflows/upstream_monitor.yml|full-bars/connect|needs operator decision: official CI watches a personal sibling fork'
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
    decided=0
    for row in "${DECISIONS[@]}"; do
      d_pat="${row%%|*}"; rest="${row#*|}"; d_glob="${rest%%|*}"; rest="${rest#*|}"; d_re="${rest%%|*}"; d_why="${rest#*|}"
      [ "$d_pat" = "$pat" ] || continue
      case "$hit" in
        $d_glob:*) if printf '%s' "$hit" | grep -qE "$d_re"; then decided=1; last_dec="$d_why"; fi ;;
      esac
    done
    if [ "$allowed" = "1" ]; then
      pass "operator-kept ${pat}: ${hit%%:*} (${last_why})"
    elif [ "$decided" = "1" ]; then
      warn "${last_dec}: ${hit}"
      undecided_hits=$((undecided_hits + 1))
    else
      fail "personal reference '${pat}': ${hit}"
      personal_hits=$((personal_hits + 1))
    fi
  done < <(grep -rnE "$pat" .github/workflows .github/scripts scripts 2>/dev/null | grep -v '^scripts/official-ci-readiness.sh:' || true)
done
if [ "$personal_hits" -eq 0 ]; then
  pass "no unlisted personal references in workflows or scripts (${undecided_hits} item(s) awaiting an operator decision)"
fi
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
  if [ "$s" = "GITHUB_TOKEN" ]; then p="built in"
  elif [ "$PRESENT" = "unknown" ]; then p="unknown"
  elif [ -z "$PRESENT" ]; then p="unknown"
  elif printf '%s\n' "$PRESENT" | grep -qx "$s"; then p="present"
  else p="MISSING"; fi
  printf '%-20s %-9s %s\n' "$s" "$p" "${DEGRADES[$s]}"
done
if [ "$unlisted" -eq 0 ]; then pass "every referenced secret has a documented degradation"; fi
echo

# ---- verdict ---------------------------------------------------------------
echo "===== CI READINESS: ${FAILS} FAIL(s), ${undecided_hits} awaiting a decision, ${note_count} style note group(s) ====="
[ "$FAILS" -eq 0 ] || exit 1
