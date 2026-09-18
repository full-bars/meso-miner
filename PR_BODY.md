## What

Release prep for `v2026.9.18-1049118720-meso`:

- `releases/v2026.9.18-1049118720-meso.md` — the full-parity release notes (parity port #92/#93/#94, shakedown parity #96, vt-scan JSON #95, GHCR-only docker #97, jwt autodetect, wiki provisioning) with deploy notes.
- `CHANGELOG.md` — meso-miner's own release entry on top; the inherited 3.23-fix history below is now clearly marked as inherited.
- `FORK_CHANGES.md` — Current Version corrected (was still `v3.23.0-fix.31.2`, inherited).
- `ship-release.yml` — adds an explicit `tag` dispatch input, and replaces the stale notes-staging step: it hardcoded `releases/v3.23.0-fix.30.8.md` (the predecessor's historical notes) as the body copied onto every release. The workflow now requires the tag-named notes file committed to main before dispatch.

## Ship plan

After merge: dispatch Ship Release with `tag=v2026.9.18-1049118720-meso`, `dry_run=false`.
