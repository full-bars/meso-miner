# Next release: patch draft (meso)

**Status:** draft. Nothing is tagged or pushed. Base tag `v2026.10.21-1077734740-meso`. Head: `main` after PR 263.
**Note:** `releases/unreleased.md` already stages a larger, older draft (base `v2026.10.19`) for the parity release. This file covers only the work merged after `v2026.10.21`, so the two do not overlap. If both ship together, merge the sections and drop the duplicate deploy notes.
**Tag at the cut:** `scripts/compute-meso-tag.sh` prints the tag. Rename this file to `releases/<tag>.md` then, because the ship workflow reads the body from that path on the tagged commit.
**Sourcing:** condensed from the merge commits and PR bodies in this window. Not re-verified line by line.

Legend: **[redeploy]** needs the built artifact redeployed. **[installer]** needs the installer or unit refresh. **[no redeploy]** docs, CI or tests only.

## Reliability

- **`docker stop` now reaches the provider** **[redeploy]** (<https://github.com/full-bars/meso-miner/pull/258>). This is the 3.23-fix stop contract, ported here. The entrypoint execs a shell, and a shell as PID 1 has no default action for SIGTERM, so the provider never heard the stop and Docker SIGKILLed the container at the grace timeout. The supervisor forwards the signal and waits for the successor before it exits.
- **The updater picks this platform's asset** **[redeploy]** (<https://github.com/full-bars/meso-miner/pull/258>). The nightly path could select another architecture's tarball. It now matches the architecture first.
- **The drift guard matches the old deny-list as a fixed string** **[no redeploy]** (<https://github.com/full-bars/meso-miner/pull/263>). The guard used a regex with a backslash-pipe, which matches a literal pipe, so a reintroduced inline list in another spelling passed. It now fails on the real text.

## Security

- **The CFAA and IP blocker tables refreshed** **[no redeploy]**. 41,202 to 46,733 IPv4 entries and 593 to 597 IPv6 entries, with blocked hosts moving from 163,972 to 164,212. Data only.

## Documentation

- **The Bittensor cost pages show the chain's numbers** **[no redeploy]** (<https://github.com/full-bars/meso-miner/pull/256>). The snapshot carries the time it was taken and links to the live copy.

## Observability

- **PQE handshake stages and negotiated curves** **[redeploy]** (<https://github.com/full-bars/meso-miner/pull/259>, <https://github.com/full-bars/meso-miner/pull/261>). The `[pqe]` line reports attempts, tls_ok, verified and identity_failed, plus a per-curve histogram. `/metrics` carries `urnet_sessions_handshake_total{stage=...}` and `urnet_sessions_curve_total{curve=...}`, and two dashboard panels show both rates. This answers why a classical count sits at zero.
- **Lifetime counters survive a proxy exit** **[redeploy]** (<https://github.com/full-bars/meso-miner/pull/261>). A manager was dropped when its proxy exited, so a `_total` could decrease and `rate()` read it as a reset. The counts fold into a provider-wide accumulator first, and live counts are summed under the registry lock so an exit cannot be counted twice.

## Security

- **The IP blocker table syncs with CFAA** **[no redeploy]** (<https://github.com/full-bars/meso-miner/pull/262>). The scheduled job pulled one file, so the blocker table drifted with no PR to show it. The gap was 163,972 to 164,212 blocked hosts, and the CFAA prefixes were behind too (43,204/569 against 46,733/597). Both files sync now, and a check fails on drift.

## Documentation

- **The v2026.10.21 changes are documented where operators look** **[no redeploy]** (<https://github.com/full-bars/meso-miner/pull/253>). The release had notes, but `FORK_CHANGES.md`, `LOG_REFERENCE.md`, the HotSwap page and the install docs were missing the detail. They now carry it.
- **Four Bittensor claims are corrected** **[no redeploy]** (<https://github.com/full-bars/meso-miner/pull/260>). v1 and v2 claim triggers are described separately. The signed challenge quotes the message. The stopped-daemon answer says unsigned v1 claims still expire. The consent is scoped to one URnetwork account.

## Deploy notes

- **The provider needs a redeploy for the counters and the stop fix.** An old process keeps the old behaviour and sends none of the new series.
- **The dashboard panels stay empty until a node runs this build.**
- **No installer change is required.** Unit files and the install flow are unchanged in this window.

## Caveats

- The stop fix is proven by a unit test and by the shakedown, not by a live fleet stop.
- The curve histogram covers sessions that terminate on this node. Forwarded traffic never opens one here.
- The blocker table sync depends on the upstream feed. A missing file fails the job instead of writing a partial table.

**Full Changelog**: filled at the cut, from the base tag to the new tag.
