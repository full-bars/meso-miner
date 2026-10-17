#!/usr/bin/env bash
# test-meso-tag-gating.sh — feeds sample meso tags through the SAME gating
# patterns build.yml uses, so the meso tag scheme cannot silently drift back
# to another format.
#
# It extracts, from .github/workflows/build.yml:
#   - the exact-tag regexp of the docker metadata type=match rule,
#   - the :latest enable expression of the type=raw rule,
#   - the condition of the "Assert a pre-release did not become :latest" step,
# and evaluates them against sample tags with the same semantics GitHub uses
# (startsWith/contains/!/&&/||). Unknown expression forms fail loudly.
#
# Expected outcomes:
#   v2026.10.16-1792123123-meso         exact tag yes, :latest YES, assert no
#   v2026.10.16-1792123123-meso-rc1     exact tag yes, :latest no,  assert YES
#   v2026.10.16-1792123123-meso-alpha1  exact tag yes, :latest no,  assert YES
#   v3.23.0-fix.33.0                    exact tag no,  :latest no,  assert no
set -euo pipefail
cd "$(dirname "$0")/.."

python3 - <<'PY'
import re
import sys

build = open('.github/workflows/build.yml', encoding='utf-8').read()

match_lines = [l for l in build.splitlines() if 'type=match,pattern=' in l]
if len(match_lines) != 1:
    sys.exit(f'expected exactly one type=match rule, found {len(match_lines)}')
m = re.search(r'type=match,pattern=(.*?),group=', match_lines[0])
if not m:
    sys.exit('cannot parse the type=match rule')
tag_pattern = m.group(1)
if 'meso' not in tag_pattern:
    sys.exit(f'the exact-tag pattern does not name the meso scheme: {tag_pattern}')

latest_lines = [l for l in build.splitlines() if 'type=raw,value=latest,enable=' in l]
if len(latest_lines) != 1:
    sys.exit(f'expected exactly one :latest rule, found {len(latest_lines)}')
m = re.search(r'enable=\$\{\{ (.*?) \}\}', latest_lines[0])
if not m:
    sys.exit('cannot parse the :latest enable expression')
latest_expr = m.group(1)
if '-meso' not in latest_expr:
    sys.exit(f'the :latest expression does not gate on the meso scheme: {latest_expr}')

assert_if = None
lines = build.splitlines()
for i, l in enumerate(lines):
    if 'Assert a pre-release did not become :latest' in l:
        for j in range(i, min(i + 4, len(lines))):
            if lines[j].strip().startswith('if:'):
                assert_if = lines[j].strip()[3:].strip()
                break
        break
if not assert_if:
    sys.exit('cannot find the assert step condition')
if '-meso' not in assert_if:
    sys.exit(f'the assert condition does not gate on the meso scheme: {assert_if}')


def eval_clause(clause: str, ref: str) -> bool:
    clause = clause.strip()
    if clause.startswith('(') and clause.endswith(')'):
        return any(eval_clause(c, ref) for c in clause[1:-1].split('||'))
    negate = clause.startswith('!')
    if negate:
        clause = clause[1:].strip()
    m = re.fullmatch(r"(startsWith|contains)\(github\.ref, '([^']*)'\)", clause)
    if not m:
        sys.exit(f'unrecognized condition clause: {clause!r}')
    fn, needle = m.group(1), m.group(2)
    if fn == 'startsWith':
        res = ref.startswith(needle)
    else:
        res = needle in ref
    return (not res) if negate else res


def eval_expr(expr: str, ref: str) -> bool:
    expr = expr.strip()
    if expr.startswith('${{'):
        expr = expr[3:].rstrip('}').strip()
    return all(eval_clause(c, ref) for c in expr.split('&&'))


samples = [
    ('v2026.10.16-1792123123-meso', True, True, False),
    ('v2026.10.16-1792123123-meso-rc1', True, False, True),
    ('v2026.10.16-1792123123-meso-alpha1', True, False, True),
    ('v3.23.0-fix.33.0', False, False, False),
]

fail = 0
print(f"{'tag':36s} {'exact-tag':10s} {':latest':8s} {'assert':8s}")
for tag, want_match, want_latest, want_assert in samples:
    ref = f'refs/tags/{tag}'
    got_match = re.search(tag_pattern, tag) is not None
    got_latest = eval_expr(latest_expr, ref)
    got_assert = eval_expr(assert_if.replace("github.event_name != 'pull_request' && ", ''), ref)
    ok = (got_match, got_latest, got_assert) == (want_match, want_latest, want_assert)
    fail |= 0 if ok else 1
    print(f"{tag:36s} {str(got_match):10s} {str(got_latest):8s} {str(got_assert):8s} {'ok' if ok else 'MISMATCH'}")

if fail:
    sys.exit('meso tag gating check FAILED')
print('meso tag gating check passed')
PY
