#!/usr/bin/env bash
cd /home/klets/ur/meso-miner
for n in 8 9 11 13 14; do
  echo "=== PR #$n ==="
  # review comments (inline) from coderabbitai
  rc=$(gh api "repos/full-bars/meso-miner/pulls/$n/comments?per_page=100" --jq '[.[]|select(.user.login=="codacy-production[bot]" or .user.login=="coderabbitai[bot]" or (.user.login|test("coderabbit|rabbit"))) | {id, path, line, body}] | length' 2>/dev/null)
  echo "  coderabbit-inline-comments: ${rc:-0}"
  # issue-level comments
  cc=$(gh api "repos/full-bars/meso-miner/issues/$n/comments?per_page=100" --jq '[.[]|select(.user.login|test("coderabbit|rabbit"))]|length' 2>/dev/null)
  echo "  coderabbit-thread-comments: ${cc:-0}"
  gh api "repos/full-bars/meso-miner/pulls/$n/comments?per_page=100" --jq '.[]|select(.user.login|test("coderabbit|rabbit"))|.body' 2>/dev/null | head -40 > /tmp/cr-$n.txt
  echo "  (cr body saved to /tmp/cr-$n.txt, $(wc -l < /tmp/cr-$n.txt 2>/dev/null) lines)"
done