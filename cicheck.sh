#!/usr/bin/env bash
cd /home/klets/ur/meso-miner
for n in 10 12; do
  echo "===== PR #$n failing checks ====="
  gh pr checks "$n" --repo full-bars/meso-miner 2>/dev/null | grep -iE "fail|error" | head
done