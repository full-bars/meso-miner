<!--
STAGING FILE — not the shipped filename.

At tag time:
  1. TAG=$(scripts/compute-meso-tag.sh)
  2. cp releases/unreleased.md releases/${TAG}.md
  3. replace every <computed-tag> placeholder below with ${TAG}
  4. commit to main, then dispatch .github/workflows/ship-release.yml
     (ship-release refuses to tag when releases/${TAG}.md is missing)

The tag is vYYYY.M.D-<code>-meso, and the code is derived from the seconds
between 2023-05-23T00:00:00Z and the moment the tag is computed, so it cannot
be precomputed. Do not invent one.
-->

# Next meso release (draft)

**Status:** this draft covers everything merged since `v2026.10.22-1078667370-meso`, including PRs 294, 297, 298, 299 and 302. Nothing is tagged or pushed. The version is not chosen yet.
**Previous release:** `v2026.10.22-1078667370-meso`. Everything that shipped in `v2026.10.19`, `v2026.10.20`, `v2026.10.21` and `v2026.10.22` is in those release files and is not repeated here.

Legend: **[redeploy]** needs the built artifact redeployed. **[installer]** needs the installer or unit refresh. **[no redeploy]** docs, CI or tests only.

> [!NOTE]
> The headline of this window is the outage of 2026-10-23. Three of its failures are fixed here: proxied H3 nodes bled egress into a silent platform edge, the degraded reaper culled half a pool it could not rank, and a thrash restart was reported to the operator as a rejected sign-in. The rest is the v2026 extender stack and the switchable Chrome ClientHello ported from 3.23-fix, plus small operator-facing fixes.

> [!IMPORTANT]
> Two behaviours change for operators who do nothing. `smart_dialer` is now restart-required: `set smart_dialer on` is persisted and takes effect at the next start. And a node with `h3` set to `all`, a count or `auto` now makes its proxied H3 dials wait on the shared attempt budget. Nodes on the default `h3 direct` are not affected by the H3 change.

## Outage response

- **A silent platform edge no longer drains egress through proxied H3** **[redeploy]** ([#302](https://github.com/full-bars/meso-miner/pull/302)). With `h3` set to `all`, a count or `auto`, every proxy identity runs the H3, H3-over-dns and dns-pump carriers beside its H1. When the edge answers nothing, each carrier redials on its own 30 to 60 second timer and every dial runs the whole QUIC retransmit ladder, about 33 KB. One node on `h3 all` spent a flat 15.5 to 16.4 Mbit/s for about 15 hours of the 2026-10-23 outage, roughly 30 times more tx than rx, while the governor read `verdict=ok rate=200.0/s`. Three things hid it, and each is fixed: QUIC bytes were never charged to the connect attempt, so the failed-byte breaker saw zero; a canceled dial (two of every three, because the per-transport H3 gate cancels a lower-priority carrier when a better one waits) was dropped with its bytes; and H3 dials never waited on the attempt budget. The handshake's wire bytes are now charged once, a canceled attempt's bytes count toward the breaker without counting as an attempt, and a proxied identity's H3 waits on the shared budget like its H1. The direct identity, proxy index 0 and family-pinned transports never wait. meso-miner lacked the carve-out that local teardown is not a backend outcome at the H3 `recordAttempt` call, and this release takes the superset. In an outage the governor now logs `[govern] budget cut ... reason=bytes`, which was silent before.
- **The degraded reaper holds off when the scores cannot rank the pool** **[redeploy]** ([#294](https://github.com/full-bars/meso-miner/pull/294)). `scoreDegradedProxies` ranks by lifetime bytes and contracts, and when the platform is unreachable neither moves, so every candidate tied and the reaper still dropped half the pool (249 of 498 proxies on one node in the 2026-10-23 outage). A round where every candidate scored the same, or there are too few to rank, now holds off instead of culling.
- **A thrash restart is no longer reported as a rejected sign-in** **[redeploy]** ([#296](https://github.com/full-bars/meso-miner/pull/296)). Exit code 75 has two producers: a failed sign-in quarantines the child, and the thrash watchdog and the liveness self-exit use the same code for a planned restart. The supervisor only knew the first, so a planned restart read as a rejected network sign-in. The planned-restart paths now write a one-shot `thrash_exit.<pid>` marker beside `thrash_cap.json` just before they exit, and the supervisor consumes the marker for the pid it reaped. Binding to the process, not to the file's mtime, matters: `thrash_cap.json` is also written mid-life, and a time comparison read a later genuine quarantine as a thrash restart. Only the operator log line changes. Both branches make the same state changes, exit code 75 is unchanged, and the systemd units, container start scripts and OpenRC all still restart on it. `docs/Troubleshooting.md` lists both causes.
- **URL-sourced proxies no longer wait up to an hour behind proxies that never sign in** **[redeploy]** ([#288](https://github.com/full-bars/meso-miner/pull/288)). `paceMonitor` declares warmup done only when more than 90% of registered proxies are up and fewer than 5 are connecting, and its total counts proxies that will never come up. One node sat at `up=363 down=40 dead=0` with 22 to 24 connecting, 85 to 88%, and the only exit was the 60 minute backstop. Each restart held every newly qualified URL proxy behind dead ones while the running pool drained from 481 to 420, and the URL fetcher parked on the same flag. The hold is now bounded to five minutes from process start (`urlWarmupLaunchGrace`), shared by the reload gate and the fetcher. The file ramp still gets its uncontested window.

## Resource and memory

- **The memory-headroom warning fires at the cap on a host whose RAM is unreadable** **[redeploy]** ([#298](https://github.com/full-bars/meso-miner/pull/298)). `EffectiveRAMLimit()` returns 850 MiB with the source `fallback default, host memory unreadable` when it cannot read host memory, which the headroom threshold turned into its 150 MiB floor, so the warning fired far too late on such a host. The limit is now classified by its source, never its value: an unreadable host gets the 400 MiB cap, a real 850 MiB host keeps the floor, and the note explaining the threshold goes to the persisted log once.
- **A failed OOM-cap state write is reported, not discarded** **[redeploy]** ([#301](https://github.com/full-bars/meso-miner/pull/301)). The startup OOM-cap decision wrote its state with the result discarded. That state carries the cap and the clean-window clock, so a lost write could let the next start read a stale window and relax the cap straight after a fresh OOM kill, and the applied path could ledger a cap that nothing enforces. Both branches now report the failure in the warnings, and the applied path leaves the timeline to the last cap that really was persisted.
- **The smart dialer costs nothing while it is off, and is restart-required** **[redeploy]** ([#297](https://github.com/full-bars/meso-miner/pull/297)). The idle preference's off state still cost a goroutine, a context and a timer per proxy, so a node with thousands of proxies carried thousands of idle goroutines. The start function is gated, so while the preference is off there is none. Because the loop now only exists when the preference was on at start, `set smart_dialer on` is persisted and takes effect at the next start, the CLI says so, and `set` prints the same "takes effect at the next start" wording for `profile` and `ramlogs`, which never applied live. The default is unchanged: off.
- **The disk benchmark samples by time, not a fixed 16 MiB** **[redeploy]** ([#300](https://github.com/full-bars/meso-miner/pull/300)). The benchmark stopped at a fixed 16 MiB, which an NVMe writes in about 20 ms, so the rate reported at startup and in `urnet_disk_*` was a burst figure that moved between runs. The 3 s budget now governs, up to 256 MiB: microSD is unchanged at about 20 MiB, eMMC samples about 240 MiB, NVMe the 256 MiB ceiling in about 85 ms. The result carries its duration, so the log reads `900.0 MB/s (256 MB written in 85 ms)`. The container guard, IO-pressure skip, space cap, per-device cache and hung-write watchdog are unchanged.
- **`psi_some_avg60` now appears in the host baseline** **[redeploy]** ([#291](https://github.com/full-bars/meso-miner/pull/291)). The psi figure lives inside `/proc/pressure/memory`, but the call asked for a resource named after the line, so the read errored and the omission rule dropped the field from every sample on every node. The call now asks for the memory resource, and a package-level seam pins the name.

## Observability and operator tools

- **`urnet-tools proxy trim` with no count reports the cap** **[redeploy]** ([#282](https://github.com/full-bars/meso-miner/pull/282), [#284](https://github.com/full-bars/meso-miner/pull/284)). The command used to refuse to run without a count, which is the one form that answers what a box is capped at. It now prints the cap in force and its source, the configured pool before the cap, how many proxies the cap holds back, and the running counts. `urnet-tools status` carries a `trim` row on the live block whenever a cap applies, and `status --json` gains `configured`, `trim_cap`, `trim_cap_source` and `trim_reported`. The provider snapshot is read first, so an unreadable `proxy_trim` file cannot hide a cap the provider is enforcing; the file is used when the provider is stopped or predates the fields. `trim_reported` tells a recorded zero cap on an empty pool apart from a provider that does not report. The reload records the selection only when `proxy_url.json` was readable, so an unreadable cache cannot understate the pool.
- **The lifetime counters carry the date they began** **[redeploy]** ([#285](https://github.com/full-bars/meso-miner/pull/285), [#287](https://github.com/full-bars/meso-miner/pull/287)). The lifetime lines printed totals with no start date, and two scopes were both labelled all-time. The store now persists a `since` stamp written once; the `[lifetime]` and `[pqe]` all-time lines print it, and the process-scoped `since-start` figures print their own. `/metrics` exports `urnet_lifetime_start_timestamp_seconds` and `urnet_lifetime_start_approximate`. A store written before this change keeps its totals and is pinned to its load time, flagged approximate, with the wording `start approximate, totals include earlier history`. The first cut let that date roll forward on every idle restart because the migration did not mark the store dirty; that is fixed, and a year-one timestamp reads as unknown. The trailing `(since-start ...)` on the health line keeps its position so log parsers that match the current order still work.

## Extender and ClientHello (ported from 3.23-fix)

- **The v2026 extender stack, with the provider serving role** **[redeploy]** ([#289](https://github.com/full-bars/meso-miner/pull/289)). The extender v2 library and server, the feed and gossip composition layer and the pinned directory candidate. A provider can serve the extender role on its direct slot (TCP 443, then QUIC 443 and DNS 4053). It is opt-in with `URNETWORK_EXTENDER`, and forwards are limited to the hosts in `URNETWORK_EXTENDER_ALLOWED_HOSTS`. A node that sets neither is unchanged.
- **Extender review findings** **[redeploy]** ([#290](https://github.com/full-bars/meso-miner/pull/290)). The feed read deadline is set before the cancellation hook, so a canceled read cannot be blocked by a later deadline. A zero connect timeout stays unbounded on the HTTP/3 carrier, matching the TCP helpers, and now aborts when the context ends. Issued-key pruning walks the table only on an epoch change. Service and refusal counts from retired servers fold into the base counters once, sampled after the drain, so cumulative metrics keep their totals. VLESS response-header parsing keeps the bytes already consumed, completes the header before reporting a read error, and can resume after a timeout.
- **The TLS ClientHello is a runtime switch** **[redeploy]** ([#289](https://github.com/full-bars/meso-miner/pull/289)). `urnet-tools set tls-hello on|off` makes the normal and resilient dialers present Chrome's ClientHello instead of Go's on new connections, live, with `URNETWORK_TLS_HELLO` as the startup default. Open sessions keep their handshake. It is off by default while it soaks, so a box that never sets the key keeps Go's hello exactly as before; a persisted `off` beats `URNETWORK_TLS_HELLO=on`, and clearing the key hands the decision back to the environment. The provider logs `[control] applied tls_hello=<v> (was <old>)` on each change. A drift harness checks the dialers' hellos and the QUIC initial in CI against a synthetic golden, not a captured Chrome.
- **Smaller subsystems ported with it** **[redeploy]** ([#289](https://github.com/full-bars/meso-miner/pull/289)). The blocker gate in the packet path (the host lookup runs only after an IP miss), an opt-in blocklist generator, lag messages and naming, a control-result and wallet-mapping API, and four dataplane leaves: egress TTL realism (inactive at the zero mode), H3 datagram auth compatibility, request-scoped redirect, and DoH dial observation.

## Installer and Windows

- **A symlinked shell rc is refused when the installer adds PATH** **[installer]** ([#295](https://github.com/full-bars/meso-miner/pull/295)). The FreeBSD installer runs as root, appends a PATH line to a file in a directory the service user owns, and then chowns it, and both steps follow a symlink. A link planted at `.profile` sent root's append and ownership change into a file of the planter's choosing. A symlinked rc is now refused with a warning and the install carries on, since the provider is already installed by then. A real file is rebuilt in a `mktemp` file beside it, chowned there, and renamed into place, with its mode preserved. The PATH line appears once across repeated runs.
- **Three installer fixes** **[installer]** ([#292](https://github.com/full-bars/meso-miner/pull/292)). A symlinked home or state directory is refused before the download, so a refusal cannot leave a half-done install. A binary that is not a non-empty executable is refused before it replaces a working one. The release tag no longer depends on python3, which FreeBSD base does not ship: jq, then python3, then a sed extraction.
- **A hotkey wallet write no longer fails on Windows** **[redeploy]** ([#293](https://github.com/full-bars/meso-miner/pull/293)). After the rename, the write flushed the parent directory, and Windows answers that with access denied, so a write that had landed was reported failed. The flush goes through `internal/durablesync`, which absorbs only the errnos meaning the platform has no directory flush; every other failure still reaches the caller, and Unix keeps the same fsync. The Windows job now runs the helper's tests.

## Security

- **The CFAA and IP blocker tables refreshed** **[redeploy]** ([#275](https://github.com/full-bars/meso-miner/pull/275)). 46,733 to 47,632 IPv4 entries and 597 to 619 IPv6 entries, with blocked hosts moving from 164,212 to 164,225. Data only; it takes effect once a node runs this release.

## CI and documentation

- **The scheduled live-numbers commits are signed** **[no redeploy]** ([#286](https://github.com/full-bars/meso-miner/pull/286)). The job committed unsigned. It now imports a dedicated signing key from the `CI_GPG_PRIVATE_KEY` secret, signs both commits, runs `git verify-commit` before each push, and stops on a missing secret or an unsigned commit. The key expires on 2027-10-23, at which point the job fails loudly. Earlier unsigned commits are not rewritten.
- **The installer shell-rc test runs in CI** **[no redeploy]** ([#299](https://github.com/full-bars/meso-miner/pull/299)). `scripts/test_provider_install_shellrc_symlink.sh` proves the PATH hint never writes through a name the service user controls, and it never ran in CI. It now runs with full history and fails, not skips, when its falsification pin is missing under `REQUIRE_FALSIFICATION`.
- **Live-numbers script, workflow and Bittensor pages** **[no redeploy]** ([#276](https://github.com/full-bars/meso-miner/pull/276), [#277](https://github.com/full-bars/meso-miner/pull/277), [#278](https://github.com/full-bars/meso-miner/pull/278)). The page carries the time the numbers were read. The RPC retry also catches read-phase resets and short reads, the price lookup logs each failing source and retries with backoff, and the wiki mirror runs on every refresh. The head-tier page says the figures come from a scheduled job, that dollar amounts are estimates from an external price feed, and that GitHub can delay scheduled jobs by hours.
- **Wiki links open the wiki page** **[no redeploy]** ([#280](https://github.com/full-bars/meso-miner/pull/280)). Links with a `.md` suffix redirected to raw markdown. In-wiki links now point at the wiki and repo-relative targets at blob URLs.
- **Smaller docs changes** **[no redeploy]** ([#283](https://github.com/full-bars/meso-miner/pull/283), [#274](https://github.com/full-bars/meso-miner/pull/274), [#279](https://github.com/full-bars/meso-miner/pull/279), [#281](https://github.com/full-bars/meso-miner/pull/281)). The Docker wrapper is labelled optional but recommended, and the v2026.10.22 release notes were corrected (duplicated sections merged, previous release named, stop proofs cited).

## Deploy notes

1. **Redeploy the provider.** Every behaviour change above needs the new binary or image; `urnet-tools update` picks it up. There is no new required setting and no migration.
2. **`smart_dialer` is restart-required.** `urnet-tools set smart_dialer on` is persisted and takes effect at the next start, and the CLI says so. The default is still off.
3. **Only nodes with `h3` set to `all`, a count or `auto` change for the H3 fix.** Watch for `[govern] budget cut ... reason=bytes` during an outage.
4. **The first disk benchmark after the update reports the new sample.** On fast media the rate is a representative figure, no longer a 20 ms burst.
5. **FreeBSD hosts need the installer refresh** for the symlink refusals and the tag parsing. The provider binary does not depend on it.
6. **The extender role and the Chrome ClientHello are both off unless set.** `URNETWORK_EXTENDER` and `urnet-tools set tls-hello on` are explicit opt-ins.
7. **The blocker table update is data only.** Policy and verdicts are unchanged.
8. **Rollback is the usual one.** Keep the previous provider binary and a state tarball, stop the unit, restore the binary, start.

## Caveats

- The H3 fix has not been run against a live outage, and the budget floor still spends about 0.5 Mbit/s.
- The disk figures (about 20 MiB on microSD, about 240 MiB on eMMC, 256 MiB in about 85 ms on NVMe) are the measurements in the PR body, not a fleet survey.
- The thrash-exit marker changes only the log line. Nothing in the restart behaviour was changed to depend on it.
- The extender role and the ClientHello switch are ported from 3.23-fix. The Chrome hello is off while it soaks,; neither has been verified in production on this fork.
- The live-numbers signing key was exercised in a clean keyring by its author; the first scheduled run on the repository is the real test.

## What's Changed

- fix(governor): price QUIC dials in bytes and admit proxied H3 under the attempt budget (#302)
- fix(provider): hold the reaper when the scores cannot rank the pool (#294)
- Classify unreadable host RAM by source so the headroom warning fires at the cap (#298)
- fix(provider): report an oom cap state write that fails (#301)
- Stop the smart dialer spawning an idle goroutine per proxy (#297)
- perf(provider): size the disk sample by time instead of 16 MiB (#300)
- Do not read a thrash restart as a rejected sign-in (#296)
- fix(provider): bound the url-sourced warmup hold to five minutes (#288)
- Read the memory pressure resource in the baseline host (#291)
- feat(urnet-tools): report the proxy trim cap and show it in status (#282), fix(provider): report trim state only from a complete pool (#284)
- fix(logs): date the lifetime counters so all-time means something (#285), fix(logs): stop the lifetime start rolling forward on idle restarts (#287)
- Port the v2026 extender stack, blocker wiring, fingerprint, leaves and small subsystems (#289), fix: act on the remaining extender review findings (#290)
- Refuse a symlinked shell rc when adding PATH (root write) (#295)
- Installer follow-up: symlinked path, unusable binary, tag parsing (#292)
- fix(hotkeywallet): finish a wallet write without a directory flush (#293)
- chore(security): sync upstream blocklists (CFAA + IP blocker) (#275)
- ci(live-numbers): sign the commits the scheduled job makes (#286)
- ci: run the installer shellrc symlink test, and fail if its pin is missing (#299)
- ci(bittensor): keep the live table honest about its own freshness (#276), fix(docs): act on the review of the live-numbers refresh (#277), docs(bittensor): say where the numbers come from and how fresh they are (#278)
- docs: link wiki pages instead of raw markdown (#280), docs: label the docker wrapper as optional but recommended (#283)
- docs(release): merge the duplicated note sections (#274), name the previous release and the stop test (#279), cite the shakedown stop check (#281)

**Full Changelog**: <https://github.com/full-bars/meso-miner/compare/v2026.10.22-1078667370-meso...<computed-tag>>
