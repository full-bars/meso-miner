#!/usr/bin/env bash
set -e
cd /home/klets/ur/meso-miner
merge_pr() {
  local repo=$1 num=$2
  for i in 1 2 3 4 5; do
    ms=$(gh pr view "$num" --repo "$repo" --json mergeable,mergeStateStatus --jq '.mergeStateStatus' 2>/dev/null)
    echo "#$num($repo) mergeState=$ms"
    if [ "$ms" = "CLEAN" ]; then
      gh pr merge "$num" --repo "$repo" --merge 2>&1 | tail -2
      sleep 2
      gh pr view "$num" --repo "$repo" --json state --jq '.state' | sed 's/^/  -> state: /'
      return
    fi
    [ "$ms" = "MERGED" ] && { echo "  already merged"; return; }
    sleep 8
  done
  echo "#$num could not be merged (state not CLEAN/MERGED)"
}
merge_pr full-bars/urnetwork-3.23-fix 474
merge_pr full-bars/meso-miner 15
merge_pr full-bars/meso-miner 16