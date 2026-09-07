#!/bin/bash
# Create or update PR for CFAA sync, and update sync-info JSON with PR URL.
# Args: [target_branch] (default: main)
set -euo pipefail

TARGET_BRANCH="${1:-main}"
BRANCH_NAME=$(git branch --show-current)
PR_TITLE="chore(security): sync CFAA blocklist from upstream"
PR_BODY_FILE="/tmp/cfaa_pr_body.md"

git push --force origin "$BRANCH_NAME"

# Check for existing open PR on this branch targeting the same base
EXISTING_PR=$(gh pr list --head "$BRANCH_NAME" --base "$TARGET_BRANCH" --state open --json number,url --jq '.[0] | select(. != null and .number != null and .url != null) | "\(.number) \(.url)"' 2>/dev/null || true)

if [ -n "$EXISTING_PR" ]; then
  PR_NUMBER=$(echo "$EXISTING_PR" | awk '{print $1}')
  PR_URL=$(echo "$EXISTING_PR" | awk '{print $2}')
  echo "PR #${PR_NUMBER} already exists: ${PR_URL}"
else
  PR_URL=$(gh pr create --title "$PR_TITLE" --body-file "$PR_BODY_FILE" --base "$TARGET_BRANCH" --label "security" | tr -d '\r\n') || PR_URL=""
  echo "Created PR: ${PR_URL}"
fi

# Update sync info JSON with PR URL
PR_URL="$PR_URL" python3 -c "
import json, os
info_path = '/tmp/cfaa_sync_info.json'
try:
    with open(info_path) as f:
        info = json.load(f)
except Exception:
    info = {}
info['pr_url'] = os.environ.get('PR_URL', '')
with open(info_path, 'w') as f:
    json.dump(info, f)
"
