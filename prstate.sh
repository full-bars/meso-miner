#!/usr/bin/env bash
cd /home/klets/ur/meso-miner
for n in 8 9 11 13 14; do
  st=$(gh pr view "$n" --repo full-bars/meso-miner --json state,mergedAt 2>/dev/null)
  echo "#$n -> $st"
done