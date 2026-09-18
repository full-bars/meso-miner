# Prepare v3.23.0-fix.32.0 release, finalize hub documentation removal

Single-commit docs pass that stages everything the v32.0 release needs and finishes the hub documentation cleanup the v32.0 removal implies.

## Release prep

- `releases/v3.23.0-fix.32.0.md` — covers the hub subsystem removal (`#634`), the selective-ack ordering and provable-hole ack wake fix (`#643`), the docker shakedown alignment (`#644`), and the entrypoint jwt build autodetect. NOTE callout replaces the v31.4 CAUTION, with deploy notes.
- `CHANGELOG.md` — new v32.0 entry.
- `FORK_CHANGES.md` — Current Version bumped (was stale at v31.2), section 165 status corrected to ship in v32.0 (it claimed v31.4), new section 166 for the v32.0 batch.

## Hub documentation finalization

The hub is gone as of `#634`; v32.0 is the release that ships that removal, so the remaining hub doc surface is retired:

- `docs/Hub-Setup.md`, `docs/Hub-Dashboard.md`, `docs/hub-dashboard-preview.png` deleted.
- `README.md`, `PROJECT_STRUCTURE.md`, `docs/Project-Structure.md`, `AI.md`, `LOG_REFERENCE.md` drop hub sections and dead links. `AI.md` is rewritten provider-only (its hub deploy playbook and command reference described deleted `urnet-tools hub *` commands).
- `docs/Configuration.md` and `docs/guides/advanced.md` no longer point at the deleted pages.
- Historical references in CHANGELOG/FORK_CHANGES/MIGRATION_PLAN and the archived command tables in `docs/urnet-tools-go.md` are left intact on purpose.

The wiki re-syncs from `docs/` automatically on merge (wiki-sync.yml); the wiki hub pages were already removed by `#634`, so no manual wiki work is needed.

## Verification

- No file outside CHANGELOG/FORK_CHANGES/MIGRATION_PLAN/releases/ references the deleted doc files.
- No functional code touched; docs and release metadata only.
