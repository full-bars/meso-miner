## What

`vt-scan.py` gains the `VT_JSON_FILE` machine-readable export: when the env var is set, the scan writes one JSON object per scanned file (`path`, `sha`, `verdict`, `malicious`/`suspicious`/`harmless`/`undetected` counts) for downstream automation, instead of only the human-readable summary.

## Why

`release.yml` already sets `VT_JSON_FILE: ${{ github.workspace }}/vt-scan-results.json` — the workflow was wired for this export, but the script-side half never made it across in the parity port, so the variable was inert.

Ported from the predecessor repo (script is repo-agnostic; byte-identical copy). `VT_FAIL_THRESHOLD` / `VT_REVIEW_THRESHOLD` behavior unchanged.
