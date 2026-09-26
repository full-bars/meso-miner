# NEXT-RELEASE

### Unreleased

Live provider status, runtime internals and a reworked `urnet-tools top`, plus a set of correctness fixes found while porting them.

### Added

- **Live traffic, runtime internals and a reworked `top` view** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the live status block now separates billable from total traffic, so bytes that were not billable — a direct socket, for instance — are visible instead of being folded into one number. Session totals follow proxies being removed or respawning rather than jumping. `top` gains a zoomable graph, a runtime panel (goroutines, heap, descriptors), a menu for theme and graph style, and a layout that adapts to a small terminal. The provider answers three new light control-socket commands — `traffic`, `internals`, `goroutines` — so the 100ms poll no longer rebuilds a full snapshot, and an older provider is detected and falls back to the snapshot's own rates.
- **A stated reason for the node's state** ([#131](https://github.com/full-bars/meso-miner/pull/131)): `starting` and `degraded` now say why — still resolving proxies, a source that could not be read, a source that returned nothing, or how many proxies are dead against how many are configured.

### Changed

- **A node that deliberately runs with no proxies now reads `active`, not `degraded`** ([#131](https://github.com/full-bars/meso-miner/pull/131)): serving on the direct transport with no proxy source configured is a valid, completed configuration. Previously it was reported as a degraded source, which was indistinguishable from a real outage. A source that *was* configured and came back empty still reads degraded, and so does a node whose direct transport is off with nothing to serve — the two cases are now told apart.
- **The idle hint blames auth only when it explains the idleness** ([#131](https://github.com/full-bars/meso-miner/pull/131)): a steady trickle of auth retries on a large healthy pool is no longer reported as an auth outage. Auth is blamed for a failure wave, or when most of the pool is not connected while failures are happening; otherwise the hint says what is true and shows the numbers behind it.
- **The reload summary says where additions came from** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the `reloaded: +N added` line now breaks the additions down by source, and URL-sourced launches get their own line instead of being folded into a bare count.

### Fixed

- **A source that goes empty stops the proxies it used to supply** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the reload returned before the removal pass, so the old proxies kept dialling and the stale count made the status line ignore the new empty-source state. A proxy with live clients is now drained rather than cut, and the state file is reconciled and written on that path.
- **A file source that cannot be read is reported as a failure** ([#131](https://github.com/full-bars/meso-miner/pull/131)): it left the resolution pending, so the status line read `starting: resolving proxies` and the snapshot later read it as a stuck startup. Running proxies are left alone.
- **A proxy source URL cannot leak its token into the logs** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the per-source log lines and the operator warning now use a redacted label that strips the query, the userinfo, and credential-bearing path segments. A source like `…/token/SECRET/list` no longer reaches the important log, and one source no longer produces two different warning keys.
- **A no-source node stops claiming it is retrying** ([#131](https://github.com/full-bars/meso-miner/pull/131)): that configuration has nothing to retry, and the reason now matches the systemd status line.
- **Pool reuse and growth are judged per interval** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the take and create counters are cumulative, so a tag that churned heavily once stayed flagged as low-reuse forever, a warning nothing could clear. A leak is now sustained growth across the window rather than a single burst at the end of it.

### Deploy Notes

1. **No action is needed on deploy.** Every change here is inside the provider and `urnet-tools`; the control-socket commands are additive, so a new `urnet-tools` against an older provider falls back cleanly and an older `urnet-tools` against a new provider is unaffected.
2. **Expect the status line to read differently on a direct-only node.** A node that serves on the direct transport with no proxy source now reports `active` where it used to report `degraded`. That is the intended reading, not a new fault.
3. **The `top` menu now persists.** A theme or graph style chosen with `m` is written to `top.conf` and restored on the next run. A theme forced by the environment (`NO_COLOR`, a dumb terminal) is not saved, so it does not outlive the terminal that forced it.
