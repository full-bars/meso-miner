#!/usr/bin/env bash
cd /home/klets/ur/meso-miner
for n in 8 9 11 13 14; do
  echo "=== PR #$n thread comment ==="
  gh api "repos/full-bars/meso-miner/issues/$n/comments?per_page=100" --jq '.[]|select(.user.login|test("coderabbit|rabbit"))|.body' 2>/dev/null | head -8
  echo
done