## [Unreleased]

Everything merged after `v2026.10.24-1080497440-meso`.

### Added

- **A FreeBSD provider installer, and the provider now lives in a root-owned tree** ([#309](https://github.com/full-bars/meso-miner/pull/309)): FreeBSD had no installer of its own, and the docs pointed at a script that did not cover the host. The script installs the flat per-architecture tarball the release publishes (`urnetwork-provider-<tag>-freebsd-<arch>.tar.gz`, with `provider` and `urnet-tools` at the top level) into `/usr/local/libexec/urnetwork-provider`, owned by `root:wheel` with group and other write removed, so the account that runs the relay cannot rewrite the binary root executes on every start, restart and update. The state directory is the one thing handed to the service user, created `0700` in that user's home because the login token and proxy state live there. Every root write goes through `priv`, which uses `sudo` when the installer is not root, and the `rc.d` script is written from one template so the root and non-root paths cannot drift. `sysrc` sets `urnetwork_enable` and `urnetwork_user` only when they are unset, so an operator's choice survives every update. Discovery learns the new path, so a stopped install is still found, and the installer and the dispatcher try `curl`, then the base system's `fetch`, then `wget`, keeping the exit status that tells a caller the asset does not exist.

### Changed

- **The CFAA and IP blocker tables refreshed** ([#306](https://github.com/full-bars/meso-miner/pull/306), [#308](https://github.com/full-bars/meso-miner/pull/308)): 45,570 blocked prefixes after the two upstream syncs, with the provenance comment recording each feed's digest and parse counts. Data only; it takes effect once a node runs this release.

### Fixed

- **Docker startup honors `URNETWORK_STATE_DIR`** (`0ee2d3087`): the startup read the default state path even when the variable was set, so a container with a custom volume lost its sessions, saved proxies and restart markers on every start.
- **The probe asks every address before it convicts a route** ([#304](https://github.com/full-bars/meso-miner/pull/304)): each probe loop read only the first address a host resolved to, so a host whose first address failed was recorded as a proxy failure even when its second answered, and a v6-only target reached the v4 encoder, where a nil `to4` left the frame at atyp ipv4 with an all-zero destination and the proxy answered for `0.0.0.0`. The SOCKS5 frame now uses a family-aware encoder and carries the real 16-byte destination, every resolved address is asked once per loop and the host counted once, and a round where the limiter cut the address set short reports no evidence unless something positive was seen. Two dead probe hosts are replaced with reachable ones, and the reputation-class hosts stay out of the health table.
- **The URL fetch failure reason stays in the important log** ([#305](https://github.com/full-bars/meso-miner/pull/305)): the cycle summary says `N failed`, but the cause — timeout, DNS, status code — is on the per-source `fetch failed for` line, which the important log did not keep and the ring rolled out within minutes.
- **The RAM log trim stays off the writer's lock, and the descriptor swap is retried** ([#307](https://github.com/full-bars/meso-miner/pull/307)): a failed reopen left the reader on an unlinked file that nothing would trim, and the fixed temp name could be blocked in a shared `/dev/shm`. The swap is retried every pass, the temp file is unique, and the tail hints use `-F` so they follow the name after a rename.
- **The paste prompt names the key that ends input on Windows** ([#311](https://github.com/full-bars/meso-miner/pull/311)): the hint hardcoded Ctrl+D, which does nothing in a native Windows console where stdin ends on Ctrl+Z then Enter. It names both on Windows, because a pty there (Git Bash, MSYS) still ends on Ctrl+D and Ctrl+Z only suspends the process, and a unit test pins both branches.
- **An extender source that asks again at once is answered** (db1abbcc): the in-flight slot was freed after the answer was written, so a source that answered and asked again immediately was refused as already having a query in flight. The slot is freed before the answer goes on the wire, the deferred release stays for early returns, and a test observes the slot at write time.
- **Docker startup keeps valid sessions and proxy configuration** ([#319](https://github.com/full-bars/meso-miner/pull/319)): the auth-code argument now reaches the provider, and invalid operator-mode settings are rejected before authentication or session-state changes. Nightly starts reuse a valid saved JWT. The nightly startup checks for and downloads a provider update before importing `/app/proxy.txt`, so a fresh nightly container has its binary before the import. A failed import keeps a valid saved proxy configuration as fallback; startup refuses to continue when that import fails and no valid saved JSON configuration remains.

### Security

- **Docker auth debug output redacts echoed credentials** ([#319](https://github.com/full-bars/meso-miner/pull/319)): with `DEBUG=true`, the login output is replaced with `[redacted]` if it contains the configured username or password, including a case-normalized echo.
- **The FreeBSD installer's root writes are fenced** ([#309](https://github.com/full-bars/meso-miner/pull/309)): the state-directory `chown -R -h` recursed into a tree the service user owns, where a hard link to a root-owned file would have been handed to that user — `-h` only guards symlinks. The chown now owns the top directory only and warns about anything inside owned by someone else. The `cd` also sat in its own subshell, so the recursive chown and chmod ran as root in whatever directory the installer was started from; the `cd` and both writes now happen in one subshell that requires `pwd -P` to match the service home before touching anything. `urnet-tools` is removed, and the failure reported, when it cannot be chowned to `root` instead of leaving a root-executed binary owned by the invoker, and `urnet-tools update` no longer hands the libexec tree back to the service user after a swap. Root's auto-update cron now schedules only the root-owned staged tool and logs under `/var/log/urnetwork`, because the old line ran the invoker's executable and logged into the service user's home. The running check scans only the service user's processes, so a local user cannot fake the provider's argv and make `start` report already running while `stop` signals the fake. `stdout.log` is kept only when `stat -f '%HT %l %Su'` says it is a regular file with one link owned by the service user; a fifo, a hard link or a foreign owner is removed before `daemon(8)` opens it as root.

### Documentation

- **The FreeBSD pages describe the tree the installer creates** ([#309](https://github.com/full-bars/meso-miner/pull/309), d63d33e7): `docs/FreeBSD.md` says the provider is root-owned under `/usr/local/libexec/urnetwork-provider` with only the state directory in the service user's home, lays out the six-step service-user order, and tells a root-shell install to name an account that already exists — the installer refuses to run the service as root and never creates the account. It also says what a bare host needs before it installs (nothing but `fetch`, which is in the base system), and the Installation page names FreeBSD wherever it lists platforms and uses the `fetch` form.
- **The proxy guides show paste as the interactive flow it is** ([#310](https://github.com/full-bars/meso-miner/pull/310)): pasting a proxy list, the key that ends the prompt on each platform, and the file-backed alternative are documented in `docs/Adding-Proxies.md` and the proxy-management page.

## [v2026.10.24-1080497440-meso] — 2026-10-24

### Added

- **The v2026 extender stack, with the provider serving role** ([#289](https://github.com/full-bars/meso-miner/pull/289), [#290](https://github.com/full-bars/meso-miner/pull/290)): the extender v2 library and server, the feed and gossip composition layer and the pinned directory candidate, ported from 3.23-fix. A provider can serve the extender role on its direct slot (TCP 443, then QUIC 443 and DNS 4053). It is opt-in with `URNETWORK_EXTENDER`, and forwards are limited to the hosts listed in `URNETWORK_EXTENDER_ALLOWED_HOSTS`. A node that sets neither is unchanged.
- **The TLS ClientHello is a runtime switch** (`tls_hello`, [#289](https://github.com/full-bars/meso-miner/pull/289)): `urnet-tools set tls-hello on|off` makes the normal and resilient dialers present Chrome's TLS ClientHello instead of Go's on new connections, live and without a restart, with `URNETWORK_TLS_HELLO` as the startup default. It affects new connections only: an open session keeps the handshake it made and follows the setting the next time it dials. The Chrome hello is **off by default** while it soaks, so a box that never sets the key keeps Go's hello exactly as before; a persisted `off` beats `URNETWORK_TLS_HELLO=on`, and clearing the key hands the decision back to the environment. The provider logs `[control] applied tls_hello=<v> (was <old>)` on each change. A fingerprint drift harness gates the dialers' hellos in CI.
- **Smaller subsystems ported with the extender stack** ([#289](https://github.com/full-bars/meso-miner/pull/289)): the blocker gate in the packet path (the host lookup runs only after an IP miss, with IPv6, UDP, mid-connection and setter-race tests), an opt-in blocklist generator, lag messages and naming, a control-result and wallet-mapping API, and four dataplane leaves: egress TTL realism (inactive at the zero mode), H3 datagram auth compatibility, request-scoped redirect, and DoH dial observation.
- **`urnet-tools proxy trim` with no count reports the cap** ([#282](https://github.com/full-bars/meso-miner/pull/282), [#284](https://github.com/full-bars/meso-miner/pull/284)): it prints the cap in force and its source, the configured pool before the cap, how many proxies the cap holds back, and the running counts. `urnet-tools status` carries the same as a `trim` row on the live block whenever a cap applies, and `status --json` gains `configured`, `trim_cap`, `trim_cap_source` and `trim_reported` in the `proxies` block. The cap is read from the provider snapshot first, so an unreadable `proxy_trim` file cannot hide a cap the provider is enforcing; the file is used when the provider is stopped or predates the fields. The reload records the selection only when `proxy_url.json` was readable, so an unreadable cache cannot understate the pool.
- **The lifetime counters carry the date they began** ([#285](https://github.com/full-bars/meso-miner/pull/285), [#287](https://github.com/full-bars/meso-miner/pull/287)): the `[lifetime]` and `[pqe]` all-time lines and the `since-start` figures now print their start, and `/metrics` exports `urnet_lifetime_start_timestamp_seconds` and `urnet_lifetime_start_approximate`. A store written before this change keeps its totals and is pinned to its load time, flagged approximate, with the wording `start approximate, totals include earlier history`.

### Changed

- **The smart dialer costs nothing while it is off, and is now restart-required** ([#297](https://github.com/full-bars/meso-miner/pull/297)): the off state still cost a goroutine, a context and a timer per proxy, so a node with thousands of proxies carried thousands of idle goroutines. The start function is gated, so while the preference is off there is none. Because the loop only exists when the preference was on at start, `smart_dialer` is reclassified restart-required: `set smart_dialer on` is persisted and takes effect at the next start, the CLI says so, and `set` prints the same "takes effect at the next start" wording for `profile` and `ramlogs`, which never applied live. The default is unchanged: off.
- **The disk benchmark samples by time, not a fixed 16 MiB** ([#300](https://github.com/full-bars/meso-miner/pull/300)): the benchmark stopped at a fixed 16 MiB, which an NVMe writes in about 20 ms, so the rate reported at startup and in `urnet_disk_*` was a burst figure that moved between runs. The 3 s budget now governs, up to 256 MiB: microSD is unchanged at about 20 MiB, eMMC samples about 240 MiB, NVMe the 256 MiB ceiling in about 85 ms. The result carries its duration, so the log reads `900.0 MB/s (256 MB written in 85 ms)`. The container guard, IO-pressure skip, space cap, per-device cache and hung-write watchdog are unchanged.
- **The CFAA and IP blocker tables refreshed** ([#275](https://github.com/full-bars/meso-miner/pull/275)): 46,733 to 47,632 IPv4 entries and 597 to 619 IPv6 entries, with blocked hosts moving from 164,212 to 164,225. Data only; it takes effect once a node runs this release.

### Fixed

- **A silent platform edge cost proxied H3 nodes about 16 Mbit/s that the governor could not see** ([#302](https://github.com/full-bars/meso-miner/pull/302)): with `h3` set to `all`, a count or `auto`, every proxy identity's H3, H3-over-dns and dns-pump carriers redial on their own timer, and a dial the edge never answers runs the whole QUIC retransmit ladder, about 33 KB. The governor counted no QUIC bytes and dropped canceled dials, and H3 never waited on the attempt budget, so the node stayed at `verdict=ok` while it spent a flat 15.5 to 16.4 Mbit/s for about 15 hours. H3 attempts now carry their handshake bytes, a canceled attempt's bytes count toward the failed-byte breaker without counting as an attempt, and a proxied identity's H3 waits on the shared budget like its H1. Nodes on the default `h3 direct` were not affected.
- **The degraded reaper culled half the pool when the scores could not rank it** ([#294](https://github.com/full-bars/meso-miner/pull/294)): `scoreDegradedProxies` ranks by lifetime bytes and contracts, and when the platform is unreachable neither moves, so every candidate tied and the reaper still dropped half the pool (249 of 498 proxies on one node in the 2026-10-23 outage). A round where every candidate scored the same, or there are too few to rank, now holds off instead of culling.
- **The memory-headroom warning fired too late on a host whose RAM it could not read** ([#298](https://github.com/full-bars/meso-miner/pull/298)): `EffectiveRAMLimit()` returns 850 MiB with the source `fallback default, host memory unreadable` when it cannot read host memory, which the headroom threshold turned into its 150 MiB floor. The limit is now classified by its source, never its value: an unreadable host gets the 400 MiB cap, a real 850 MiB host keeps the floor, and the note explaining the threshold goes to the persisted log once.
- **A failed OOM-cap state write was discarded** ([#301](https://github.com/full-bars/meso-miner/pull/301)): that state carries the cap and the clean-window clock, so a lost write could let the next start read a stale window and relax the cap straight after a fresh OOM kill, and the applied path could ledger a cap that nothing enforces. Both branches now report the failure in the warnings, and the applied path leaves the timeline to the last cap that really was persisted.
- **URL-sourced proxies waited up to an hour behind proxies that never sign in** ([#288](https://github.com/full-bars/meso-miner/pull/288)): warmup counted every registered proxy, so a file list with dead entries held the ratio below 90% and the only exit was the 60 minute backstop, which also parked the URL fetcher. On one node that held the pool draining from 481 to 420 across a restart. The hold is now bounded to five minutes from process start.
- **A thrash restart was reported as a rejected sign-in** ([#296](https://github.com/full-bars/meso-miner/pull/296)): exit code 75 has two producers, and the supervisor only knew the failed-sign-in one. The planned-restart paths now write a one-shot `thrash_exit.<pid>` marker that the supervisor consumes for the child it reaped. Only the operator log line changes; exit code 75 and the restart behaviour are unchanged, and `docs/Troubleshooting.md` lists both causes.
- **`psi_some_avg60` never appeared in the host baseline** ([#291](https://github.com/full-bars/meso-miner/pull/291)): the reader asked for a resource file named after the line instead of `/proc/pressure/memory`, so the read errored and the field was omitted on every node.
- **A stale lifetime start rolled forward on idle restarts** ([#287](https://github.com/full-bars/meso-miner/pull/287)): the legacy-store migration did not mark the store dirty, so a node that moved no counters re-pinned a later date on each restart. A year-one timestamp now reads as unknown, not as an exact start.
- **The FreeBSD installer gets three fixes** ([#292](https://github.com/full-bars/meso-miner/pull/292)): a symlinked home or state directory is refused before the download, a binary that is not a non-empty executable is refused before it replaces a working one, and the release tag no longer needs python3 (jq, then python3, then a sed extraction).
- **A hotkey wallet write reported failure on Windows** ([#293](https://github.com/full-bars/meso-miner/pull/293)): after the rename the write flushed the parent directory, which Windows answers with access denied, so a write that had landed was reported failed. The flush goes through `internal/durablesync`, which absorbs only the errnos that mean the platform has no directory flush. Unix keeps the same fsync.
- **Extender review findings** ([#290](https://github.com/full-bars/meso-miner/pull/290)): the feed read deadline is set before the cancellation hook, a zero connect timeout stays unbounded on the HTTP/3 carrier and now aborts when the context ends, issued-key pruning runs only on an epoch change, retired servers' counts fold into the cumulative counters once, and VLESS response-header parsing can resume after a read timeout without losing the payload.

### Security

- **The FreeBSD installer no longer writes through a symlinked shell rc** ([#295](https://github.com/full-bars/meso-miner/pull/295)): the installer runs as root and appends a PATH line to a file in a directory the service user owns, and both the append and the `chown` followed a symlink. A symlinked rc is now refused with a warning and the install carries on. A real file is rebuilt in a `mktemp` file beside it, keeps its mode, and is renamed into place.

### CI

- **The scheduled live-numbers commits are signed** ([#286](https://github.com/full-bars/meso-miner/pull/286)): the hourly job committed without a signing key, so every refresh landed unsigned. It now imports a dedicated signing key from the `CI_GPG_PRIVATE_KEY` secret, signs both commits, runs `git verify-commit` before each push, and stops when the secret is missing or a commit is unsigned. The key expires on 2027-10-23, at which point the job fails loudly.
- **The installer shell-rc symlink test runs in CI** ([#299](https://github.com/full-bars/meso-miner/pull/299)): `scripts/test_provider_install_shellrc_symlink.sh` never ran in CI. It now runs with full history, and fails, not skips, when its falsification pin is missing under `REQUIRE_FALSIFICATION`, so a shallow checkout cannot turn it into a silent pass.
- **The live-numbers script and workflow are updated** ([#276](https://github.com/full-bars/meso-miner/pull/276), [#277](https://github.com/full-bars/meso-miner/pull/277)): the page carries the time the numbers were read, the RPC retry also catches read-phase resets and short reads, the price lookup logs each failing source and retries both with backoff, and the wiki mirror runs on every refresh.

### Documentation

- **Bittensor pages say where the numbers come from** ([#278](https://github.com/full-bars/meso-miner/pull/278), [#277](https://github.com/full-bars/meso-miner/pull/277)): the chain figures come from a scheduled job, every dollar amount is an estimate from an external price feed, and GitHub can delay scheduled jobs by hours, so the last-changed line is the freshness signal. The oracle row describes the address check, and the Fleet note says an edited manifest needs a new generation and a publish.
- **Wiki links open the wiki page** ([#280](https://github.com/full-bars/meso-miner/pull/280)): links carried a `.md` suffix and redirected to raw markdown. In-wiki links now point at the wiki pages and repo-relative targets at blob URLs.
- **The Docker wrapper is labelled optional but recommended** ([#283](https://github.com/full-bars/meso-miner/pull/283)).
- **The v2026.10.22 release notes were corrected** ([#274](https://github.com/full-bars/meso-miner/pull/274), [#279](https://github.com/full-bars/meso-miner/pull/279), [#281](https://github.com/full-bars/meso-miner/pull/281)): duplicated sections merged, the previous release named, and the stop caveat now cites the container stop harness and the shakedown's X3 check.
- **Release notes for this release** ([#303](https://github.com/full-bars/meso-miner/pull/303)).

## [v2026.10.22-1078667370-meso] — 2026-10-22 (`urfoundation/meso-miner` tag)

### Added

- **PQE handshake stages and negotiated curves** ([#259](https://github.com/full-bars/meso-miner/pull/259), [#261](https://github.com/full-bars/meso-miner/pull/261)): the `[pqe]` line reports `attempts`, `tls_ok`, `verified` and `identity_failed`, plus a per-curve histogram. `/metrics` carries `urnet_sessions_handshake_total{stage=...}` and `urnet_sessions_curve_total{curve=...}`, and two dashboard panels show both rates.

### Fixed

- **A `docker stop` skipped the shutdown path** ([#258](https://github.com/full-bars/meso-miner/pull/258)): a shell as PID 1 has no default action for SIGTERM, so the provider never heard the stop and Docker SIGKILLed the container at the grace timeout. The supervisor forwards the signal and waits for the successor.
- **The nightly updater could pick another platform's tarball** ([#258](https://github.com/full-bars/meso-miner/pull/258)): the asset is now matched against this architecture.
- **A completed handshake could go uncounted** ([#259](https://github.com/full-bars/meso-miner/pull/259)): a superseded epoch returned before the count, and a failed identity proof could be counted by every concurrent verifier. Both count once now.
- **Lifetime session counters dropped on proxy exit** ([#261](https://github.com/full-bars/meso-miner/pull/261)): a `_total` could decrease and `rate()` read it as a reset. A departing manager's counts fold into a provider-wide accumulator first.
- **The deny-list drift guard matched a literal pipe** ([#263](https://github.com/full-bars/meso-miner/pull/263)): it now matches the old inline list as a fixed string.

### Security

- **The CFAA and IP blocker tables refreshed** ([#265](https://github.com/full-bars/meso-miner/pull/265)): 41,202 to 46,733 IPv4 entries and 593 to 597 IPv6 entries, with blocked hosts moving from 163,972 to 164,212. Data only.
- **The IP blocker table syncs with CFAA** ([#262](https://github.com/full-bars/meso-miner/pull/262)): the scheduled job pulled one file, so the blocker table drifted unseen. Both files sync now, and a check fails on drift.

### CI

- **The soak and smoke client-id reuse checks restart the provider** ([#270](https://github.com/full-bars/meso-miner/pull/270), [#272](https://github.com/full-bars/meso-miner/pull/272)): both ran `urnet-tools hot-restart -f`, which is not a command, and the trailing `|| echo` turned the failure into a pass. They now terminate the provider, start it from the same state directory, compare the ids from `urnet-tools proxy ids`, and fail when a slot's id changed.

### Documentation

- **SN25 costs on the Bittensor pages come from the chain** ([#256](https://github.com/full-bars/meso-miner/pull/256)): the head-tier and quick-start pages show a live cost snapshot with the time it was taken and a link to the live copy, refreshed hourly by a scheduled job.
- **A Hot-Restart page, and the dead toggle command fixed** ([#268](https://github.com/full-bars/meso-miner/pull/268), [#269](https://github.com/full-bars/meso-miner/pull/269), [#271](https://github.com/full-bars/meso-miner/pull/271)): `docs/Hot-Restart.md` covers client identity reuse, and the docs now use `urnet-tools set` for the toggle instead of `hot-restart on`, which does not exist.
- **Four Bittensor claims are corrected** ([#260](https://github.com/full-bars/meso-miner/pull/260)): the v1 and v2 claim triggers, the quoted challenge message, the stopped-daemon caveat for unsigned v1 claims, and the consent scope.
- **The v2026.10.21 changes are documented where operators look** ([#253](https://github.com/full-bars/meso-miner/pull/253)): `FORK_CHANGES.md`, `LOG_REFERENCE.md`, the HotSwap page and the install docs now carry the counter persistence, the swap behaviour and the install detail.
- **Release notes for this release** ([#264](https://github.com/full-bars/meso-miner/pull/264), [#267](https://github.com/full-bars/meso-miner/pull/267), [#273](https://github.com/full-bars/meso-miner/pull/273)).

## [v2026.10.22-1078651310-meso] — 2026-10-22

### Added

- **Cumulative monitoring counters survive restarts and handoffs** ([#236](https://github.com/full-bars/meso-miner/pull/236), [#240](https://github.com/full-bars/meso-miner/pull/240), [#241](https://github.com/full-bars/meso-miner/pull/241)): monotonic `/metrics` totals are restored before the listener serves, retained across proxy churn and flushed by the HotSwap parent before it exits. Process-start data lets dashboards avoid reading a restored total as a traffic spike.
- **PQE handshake stages and negotiated curves are exposed** ([#259](https://github.com/full-bars/meso-miner/pull/259), [#261](https://github.com/full-bars/meso-miner/pull/261)): the log and `/metrics` distinguish attempts, completed TLS, verified identities and identity failures, and report the negotiated curve; Grafana gains panels for both series.

### Fixed

- **`docker stop` now reaches the provider** ([#258](https://github.com/full-bars/meso-miner/pull/258)): the supervisor forwards termination signals and waits for a HotSwap successor instead of letting Docker kill the container at its grace timeout.
- **The updater selects a stable asset for this platform** ([#258](https://github.com/full-bars/meso-miner/pull/258)): asset selection matches the operating system and architecture, and stable-release lookup no longer stalls on a docs-only or unflagged release.
- **The exported DoH failure counter no longer resets on scrape** ([#236](https://github.com/full-bars/meso-miner/pull/236)): the scrape now reads a cumulative total while the health heartbeat keeps its interval count.

### Removed

- **The retired hub's reporting commands and settings** ([#245](https://github.com/full-bars/meso-miner/pull/245)): bandwidth and heartbeat reporters, their CLI commands and configuration keys are gone; existing saved report URLs are ignored so old state still loads.

### Security

- **Go 1.27.3 is required for release builds** ([#238](https://github.com/full-bars/meso-miner/pull/238)): the toolchain includes the certificate-verifier fix for the compromised eMudhra intermediates; already-built binaries need a rebuild to pick it up.
- **The CFAA and IP blocker tables were refreshed together** ([#235](https://github.com/full-bars/meso-miner/pull/235), [#252](https://github.com/full-bars/meso-miner/pull/252), [#265](https://github.com/full-bars/meso-miner/pull/265)): the final sync carries 46,733 IPv4 and 597 IPv6 CFAA entries and 164,212 blocked hosts. The scheduled workflow now checks both generated tables for drift ([#262](https://github.com/full-bars/meso-miner/pull/262)).

### CI

- **The installer matrix gates publication**: the real installer runs against a fake release on four distributions, and the release waits for that tier.
- **The artifact scan reports incomplete coverage** ([#237](https://github.com/full-bars/meso-miner/pull/237)): the scanner retries rate limits, handles large artifacts and reports a per-file error instead of a false pass when a scan is incomplete.
- **Restart checks now restart the provider and compare identities** ([#270](https://github.com/full-bars/meso-miner/pull/270), [#272](https://github.com/full-bars/meso-miner/pull/272)): soak and smoke tests use the same state directory and fail if a proxy identity changes.

### Documentation

- **Bittensor operator guidance was refreshed** ([#239](https://github.com/full-bars/meso-miner/pull/239), [#249](https://github.com/full-bars/meso-miner/pull/249), [#251](https://github.com/full-bars/meso-miner/pull/251), [#260](https://github.com/full-bars/meso-miner/pull/260)): the quick start, head-tier and operations pages clarify claim versions, consent scope and current rollout caveats.
- **Live SN25 costs and Hot-Restart behavior are documented** ([#256](https://github.com/full-bars/meso-miner/pull/256), [#268](https://github.com/full-bars/meso-miner/pull/268)): the cost snapshot is refreshed from chain data, and the new guide explains client identity reuse and the supported toggle command.

## [v2026.10.21-1077734740-meso] — 2026-10-21 (`urfoundation/meso-miner` tag)

### Added
- **Cumulative metrics counters persist across restarts**: the counters behind `/metrics` (`urnet_*_total`) are stored in `~/.urnetwork/metrics_counters.json` and restored before the listener serves, so a restart, reboot or update no longer resets a 24-hour attribution window to zero. The store is separate from `lifetime_metrics.json`; writes are atomic, every 45 seconds and on shutdown, and skipped when nothing changed. The bundled dashboard gates its rate panels for one window after a start-time change so a restored total is not read as a spike.

### Fixed
- **`urnet_doh_failures_total` no longer drops to zero on every scrape**: the exported counter was fed by a read that swaps the count to zero for the health heartbeat, so a scrape saw the series fall back to zero. A cumulative counter now backs it.
- **The HotSwap parent flushes the counters before it exits**: the drain path exits without running the normal shutdown block, so the promoted candidate could restore a value up to one interval old. The parent now flushes its final counters at the end of the drain, and the candidate still catches up after the drain window.

### Security
- **The CFAA endpoint blocklist is refreshed from upstream**: 41,425 to 41,202 IPv4 entries and 548 to 593 IPv6 entries. Data only; it takes effect once a node runs this release.
- **Go 1.27.3 blocks the eight compromised eMudhra intermediates (`CVE-2026-111943`)**: `go.mod`, the Dockerfile builder and every `setup-go` pin Go 1.27.3, so binaries built from this release carry the denylist. A binary built before it may continue trusting the intermediates, although an updated operating-system trust store may already reject them.

### CI
- **Every release artifact is scanned**: the VirusTotal job covers the full published set, gives a duplicate artifact the verdict of its source, honors an HTTP-date `Retry-After` with a capped wait, and runs its own tests in lint.
- **The installer tier gates publication**: a job runs the real installer on four distributions and the release refuses to publish when it fails.

### Removed
- **The retired fleet hub's report commands and settings**: `urnet-tools report`, `urnet-docker report`, the `report-url` and `report-interval` settings, `URNETWORK_REPORT_URL`, `URNETWORK_REPORT_INTERVAL` and `URNETWORK_HEARTBEAT_INTERVAL` and the reporters behind them are gone. Nothing received the reports once the hub was removed. A saved `report_url` is ignored, so a node that had one still starts.

### Changed
- **Docs rewritten for meso-miner**: a plain start-here, a Bittensor quick start, the head tier, a glossary, project-structure pages that match the real tree, and the Hub and report-URL pages removed. The changelog backfills `2026.9.9` and `2026.8.28`.

---

## [v2026.10.20-1076956260-meso] — 2026-10-20

### Added

- **The v34 parity port brings the modern 33.1/34 connect generation into meso** ([#231](https://github.com/full-bars/meso-miner/pull/231)): a three-way merge of the fork's `release/34.0` at the shared merge-base, with the Go side taken from 3.23-fix and meso's branding, docs, workflows and scripts preserved. `transport_family.go` is family-pinned platform transports and the group that runs them: the direct identity runs a v4 pin, a v6 pin and a family-agnostic standby, and a pin dials only its own family, declares its family in the auth header or frame, never feeds the process-wide backend-degraded gate, is held rather than spun on a device with no path of its family, and idles under a contradicting Force. `transport_sessions.go` gives a session a lifetime and a close cause, `transport_authorization.go` and `transport_credential_refusal.go` park a refused credential instead of re-dialing into the same refusal, and `transport_family_stats.go` and `stream_metrics.go` add per-family and per-stream accounting. Transfer-side attempts are owned, and budget reservations are released by their owner on every path.
- **The direct native identity becomes a dual-stack transport group** ([#231](https://github.com/full-bars/meso-miner/pull/231)): `provider/family_transport.go` builds the group when the identity is native, has no proxy settings, both family platform urls exist and the connect host is the one whose `-v4` and `-v6` names were verified live, logging `[ipv6] direct identity platform transports: dualstack group` or `legacy single (reason=...)`; a proxied identity never gets it. The new `ip_version` control key (`urnet-tools set ip-version dual|ipv4|ipv6|legacy`) re-pins the running group live, breaking before making, where `legacy` is one family-agnostic transport. A host with no usable global IPv6 still builds the group and parks the v6 pin until a network change, and IPv4 stays primary and is never demoted.
- **H1+ framing and the name and dial internals arrive with the merge** ([#231](https://github.com/full-bars/meso-miner/pull/231)): an H1 connection can upgrade to a framed custom carrier that keeps H1's lanes, limits and batching but encodes ready payloads once through the shared framer; it falls back to a plain websocket on any failure, is on by default, `urnet-tools set h1-plus off` turns it off, and `[h1plus] last 10m:` summarizes how every H1 dial ended. The DoH internals resolve a protected name through the in-process resolver, and progressive UDP dial candidates consume the first usable address while the other family's query is still running instead of waiting for both.
- **The control-plane family policy and the family-aware dial** ([#222](https://github.com/full-bars/meso-miner/pull/222)): `control_family.go` steers the provider's own control-plane dials (the api https client, the platform websocket and the h3/quic name path) and leaves the tunnelled data plane alone. The policy is `Auto` in every shipped configuration, so the live part is the demotion ledger: a dial that connects and then times out in its handshake demotes that family for 5 minutes, doubling to a 6-hour cap, cleared on any network change, refused when the other family is not usable on the device and never recorded for a dial that went through a proxy. `control_family_dial.go` retries that failure once over the other family. Both halves only narrow a name dial; a dial to an IP literal is left alone.
- **Egress resolution, macOS pinning and the platform transport budget** ([#223](https://github.com/full-bars/meso-miner/pull/223)): `egress_dial.go` resolves a control dial's hostname through an in-process resolver whose queries ride egress-bound sockets, so a provider pinned to the physical interface does not deadlock behind the tunnel it is building. It is inert unless a platform supplies an egress resolver: Windows supplies one whose dial hook is inert until an egress interface is set, and Linux keeps the live system resolver and refreshes only a server made stale by a resolv.conf transition. `egress_darwin.go` adds macOS socket pinning, and `platform_transport_budget.go` is the aggregate budget the transport group admits against, with exact claim accounting, background priority for pinned transports and a make-before-break pairing. The route-manager change here is a test seam only: an exactly published route-state generation, with no production observer installed.
- **The 33.1 provider batch: `h3 auto`, UDP relay checking, the network refresh and the consolidated Docker entrypoint** ([#224](https://github.com/full-bars/meso-miner/pull/224)): `h3 auto` sizes the set of proxies that get the fallback transports from the effective memory limit and the existing pressure signal (one slot per 64 MiB, floor 2 GiB, ceiling 64, shed at 0.75 smoothed pressure, held between 0.30 and 0.50, grown 4 at a time below 0.30) and logs `[h3] auto:` only when the ranked set changes; `h3` is now a set of modes (`off`, `direct`, `on`, a positive count, `all`, `auto`). A proxy's UDP relay is proved, and where it can be learned its UDP exit is matched to its TCP exit, before H3 is used through it (`provider/proxy_udp_check.go`, `provider/socks5_udp.go`). `provider/network_refresh.go` renews the network JWT at `POST /auth/network-refresh` in one call, keeping the token's create time, roles, principal and guest mode, and falls back to the legacy code-create/code-login flow only for a server that predates the route or refuses it; a 401 is terminal. `entrypoint.sh`, `provider_common.sh` and `provider_env.sh` own the shared startup logic and `start_stable.sh`, `start_nightly.sh` and `start_jwt.sh` are thin wrappers, with the invocation, the `BUILD` values and the volume layout unchanged.
- **The ICE resolver and the light routing classifier** ([#228](https://github.com/full-bars/meso-miner/pull/228)): `ice_resolve_net.go` binds native WebRTC address resolution to the peer generation's context, so a STUN or TURN hostname lookup is cancelled when that generation is retired instead of sitting parked on a socket read while `PeerConnection.Close` joins the gatherer. `routing_class.go` and `routing_classifier_light.go` add a latency, throughput or bulk class and a light classifier; it is inert, because no consumer is installed and the newer generation's scoring and placement paths are not ported.
- **The shared UDP socket lifecycle** ([#229](https://github.com/full-bars/meso-miner/pull/229)): `ip_udp_socket_poller.go` with its `epoll`, `kqueue` and `unsupported` backends replaces one blocking reader, one send loop and one idle timer per UDP flow with a small fixed set of readiness workers per family, keeps a flow on one shard so its datagrams keep arrival order, and drains a bounded delivery queue on a second worker so a slow receive callback stays off the descriptor drain. It is inert: the poller is only built when `SocketReadShardCount` is set and no shipped setting sets it. The port documents the tradeoff to measure first — the blocking enqueue runs on the shard's single drain loop, so one slow callback couples every flow hashed to that shard.
- **The transfer memory budget core** ([#230](https://github.com/full-bars/meso-miner/pull/230)): `transfer_memory_budget.go` is a byte budget that transfer queues share across sequences, so aggregate queue memory stays flat as peers grow while a single fast peer can still borrow a deep queue. A queue keeps a guaranteed floor it never borrows against, reserves only the bytes above it, releases on every removal path, and a parented budget composes every admitted byte through its ancestors under one admission lock for the tree, so children with overlapping ceilings cannot overdraw the root. It is inert: the reserved settings are honored where they are set and the defaults set neither, so no shipped configuration builds a budget.
- **The receive window is advertised** ([#219](https://github.com/full-bars/meso-miner/pull/219)): `Ack` gains `receive_window_byte_count` (proto field 8, additive and backward compatible) and the provider sizes it from its receive queue, which is what the platform's modern-provider gate looks for beside post-quantum encryption; an older peer ignores the field.
- **Contract lifecycle logs carry the proxy identity** ([#226](https://github.com/full-bars/meso-miner/pull/226)): `ProxyDescription` appears in the contract create and close lines, so one contract can be traced to the proxy that served it.
- **The 33.1/34 provider commands are subcommands only** ([#231](https://github.com/full-bars/meso-miner/pull/231)): `provider claim-daemon`, `provider claim-daemon-v2`, `provider fleet` and the wallet hotkey upkeep commands (`provider wallet hotkey challenge|set|status`), with their docs. Nothing runs unless an operator invokes one.
- **A failed attempt is charged to the DNS address it used**: addresses that stall repeatedly are skipped for a few minutes and one address is pinned for each connect attempt, so one bad address of a resolved set no longer costs every proxy a timeout.
- **The DNS-over-HTTPS resolver uses its IPv6 endpoints only on hosts with working global IPv6**.
- **An observe-only client for the signed backend-health feed**: off by default, it records what it would tell the governor and never acts.
- **The provider captures goroutine and heap profiles on the way into a stall**: a provider that runs away on memory stops answering within minutes, so there was never any evidence of what had piled up. It now samples its goroutine count and its heap against the soft limit every 10 seconds and, on a build-up (1.5 times the 30-minute low and at least 20,000 goroutines, heap at 90%, heap at 120%), writes `summary.txt` (the biggest goroutine stacks), the full goroutine profile, a heap profile and the numbers to `~/.urnetwork/incidents/`. At most 6 a day, newest 8 kept, disk only. `URNETWORK_INCIDENT_CAPTURE=0` turns it off.
- **The combined TCP-segment + TLS-record fragmentation dialer** ([#183](https://github.com/full-bars/meso-miner/pull/183)): parity port of the 3.23-fix `fragment+segment` dialer ([#792](https://github.com/full-bars/urnetwork-3.23-fix/pull/792)). The new dialer cuts every fragmented ClientHello record a second time, across two TCP segments at an interior byte, so a middlebox that reassembles either layer alone cannot stitch the hello. It needs no raw sockets, so it works where the ttl reorder technique cannot. It registers at the resilient tier (priority 50) for direct connections only, not behind a SOCKS proxy where the proxy re-segments the stream and the mode would equal plain `fragment`, and the segment cut is best effort. It fails closed exactly like the fragment path, and changes no existing dialer's behavior.
- **A swap-thrash watchdog with a supervised restart** ([#180](https://github.com/full-bars/meso-miner/pull/180)): memory pressure alone does not say "thrashing". The watchdog senses PSI memory `full`, swap activity, page refaults and direct reclaim, and tracks one state: `calm`, `under-pressure`, `thrashing`, `critical`. It first freezes pool growth; when the condition persists it restarts the provider by exiting with status 75, which the shipped unit (`Restart=on-failure`) turns into a supervised restart. The restart is capped at 3 per 24 hours with a growing backoff, is skipped while a hot-swap is in flight, and refuses to act when the swap belongs to another process. A restart leaves a thrash cap in `~/.urnetwork/thrash_cap.json` so the next start begins leaner — about 60% of the previous running count, at least one proxy, and no new cap when nothing was running before the restart. It rides the existing self-heal switch; with self-heal off it senses and logs but never restarts anything.
- **`urnet-tools status` prints the pressure summary sentence**: the live block's pressure row shows the provider's one-sentence summary (the `summary` the provider persists to `~/.urnetwork/pressure_status`), instead of only the score. Older providers keep the bare score.
- **The installer warns when a restart-policy override weakens the watchdog**: a drop-in with `Restart=no`, `Restart=on-success`, `Restart=on-abnormal`, `Restart=on-watchdog` or `Restart=on-abort`, a `RestartPreventExitStatus` listing 75, or a `SuccessExitStatus` marking 75 a success under `Restart=on-failure` would leave a detected thrash unrecovered; the installer now says so. The policy itself is never rewritten.

### Fixed

- **The network-switch notice only prints when the effective network changes** ([#227](https://github.com/full-bars/meso-miner/pull/227)): `--reset` is decided before the write, and an unreadable config counts as changed.
- **The plain-DNS resolver rotates per lookup, not per process** (34.1): the fallback counter was shared by the whole cache and advanced once per attempt, so a concurrent lookup could move it between another lookup's two attempts and both could land on the same address family; each lookup now seeds its own attempt counter from the shared one, which advances once per lookup. DoH stays primary, so only the plain-DNS fallback changes.
- **The remaining v34.1 review follow-ups**: `DefaultReceiveBufferSettings().ReliablePackHandoffTimeout` is 0 instead of -1, in a field no production caller reads, so nothing about a running box changes; a network-refresh 401 is terminal and no longer falls back to code-create; the Windows egress fail-closed behaviour is documented in a comment with no code change; and Docker public-IP detection no longer masks curl's exit status.
- **The shared UDP poller returns the rest of a datagram batch when a drain is canceled** (34.1): a canceled drain used to return only the packet it refused and leave the rest of the slice counted as taken. The poller stays inert.
- **The normal strategy dial is pinned to the egress control** (34.1, test only): dropping `NetDialer`'s `Control` field now fails the build.
- **The Docker build log no longer doubles the version prefix** ([#220](https://github.com/full-bars/meso-miner/pull/220)).
- **A provider that is alive but stalled is now restarted** (found on a 2 GiB single-core canary that sat wedged for hours, 90 percent CPU, no log output, self-heal reading "calm"): the garbage collector, running against a heap far over its soft limit with part of it swapped out, left no goroutine able to make progress, so none of the in-process responders ran and systemd saw a healthy unit. Two changes. A unit that sets `WatchdogSec=` is now fed `WATCHDOG=1` only while the pressure monitor keeps ticking; after 10 quiet minutes the ping stops, a lean start cap is recorded (60% of the running proxies, in the thrash restart ring), and systemd restarts the provider. And the thrash watchdog counts a heap at least 1.4 times its soft limit on a box short of free RAM as severe on its own, so the supervised restart can fire in the minutes before the stall, while PSI still reads calm. The feed starts at READY with a 30 minute startup grace, follows the self-heal switch and the thrash restart budget (3 per 24 hours), keeps a standing cap instead of compounding it, and takes the cap back out if the stall resolves itself. A heap-runaway episode on a box without PSI now relaxes when the heap recovers. The watchdog is off unless the unit sets `WatchdogSec=`; see the Configuration page.

- **A failed renewal replaced a client identity** ([#190](https://github.com/full-bars/meso-miner/pull/190)): when a stored client token had expired and renewing it failed, `provideAuth` minted a new client and abandoned the old client id and the history attached to it, even when the failure was only a timeout or a dropped connection. A renewal that fails without a platform verdict (a transport error, a 5xx, a 408 or a 429, a cancelled context) is now transient: the identity is kept and the caller's backoff retries. A definite answer (the client does not exist, a mismatched client id, a permanent 4xx) still mints a fresh client as before.
- **`provider claim` hid the platform's refusal** ([#190](https://github.com/full-bars/meso-miner/pull/190)): the platform answers a refused claim as HTTP 200 with an error object, and the client had no field for it, so the message was dropped and the command ended in `claim has no no_id`. It now prints the platform's own message, for example `No claimable epoch.`.
- **`urnet-tools` did not run on Windows** ([#185](https://github.com/full-bars/meso-miner/pull/185)): the log views ran `tail -n 20 -f`, which Windows does not have, `urnet-tools start` set the provider's working directory before the state directory existed so Windows refused to launch it, and the sudo checks had no Windows branch. The commands now run natively. A native Go follower prints the last lines and then streams new output, and it is the fallback wherever `tail` is missing. It opens the log with delete sharing, so a rotation, a clear or an update is no longer blocked by a sharing violation. If the path starts naming a different file, the follower finishes the old one and follows the new one from its start, and a truncated log is followed from its start. It also opens paths longer than `MAX_PATH`, and a 64 KiB tail window that starts exactly on a line boundary keeps its first line.

- **Three `provider claim` rough edges** (follow-ups to the claim credential change): `--legacy-coldkey` now refuses an expired network token locally instead of sending it and reading an opaque server refusal; `--store-client=<address>` now finds the identity at that address even when the store key carries a proxy user (it refuses, without echoing usernames, when several accounts share the address); and a platform refusal prints `claim: the platform refused: <message>` and exits 1 instead of panicking with a stack trace.

### Changed

- **A direct-only node now runs H3 by default** ([#224](https://github.com/full-bars/meso-miner/pull/224)): a node whose proxy resolution settles as direct-only resolves `h3` to on, because the one-socket-set-per-proxy-identity argument does not apply when there is exactly one identity. `h3_datagram` and `h3_datagram_send` default on everywhere, as upstream offers and sends datagrams by default. An explicit key always wins, clearing a key returns to the new default, and a node that never settles direct-only and never sets `h3` is unchanged.
- **The dropped bucket is labeled on its own, and the idle hint reads again** ([#169](https://github.com/full-bars/meso-miner/pull/169)): the live status and `top` view called the was-up-now-down bucket `down`, the same word the whole-pool figure uses for dead plus dropped. The breakdown bucket is `dropped` now, matching the proxy health report, so `down` has one meaning. The idle hint no longer calls every offline proxy dead. Labels and wording only; the reaper inputs are unchanged.
- **The provider's operator logs are readable sentences** ([#180](https://github.com/full-bars/meso-miner/pull/180)): the pressure regime, pool target, trim receipt and apply, reload summary, health verdict, auth limiter and GC governor lines say what happened in plain words, with machine counters kept in a trailing `(...)` for grep. The new `[health] Verdict:` line summarizes state in one sentence. Dialer and connect lines are unchanged.
- **`provider claim` needs an explicit client credential** ([#190](https://github.com/full-bars/meso-miner/pull/190)): the platform looks a claim up by the client in the token, and serves a network token only for an explicit legacy read of an epoch with no provider artifact. Claim takes exactly one of `--store-client=<key>` (a client token from the identity store), `--provider-jwt=<path>` (a client token in a file) or `--legacy-coldkey=<ss58>` (the network token plus a coldkey). With none it refuses and says why, because a node holds hundreds of identities and only one that served traffic has a payout. A token without a client id, and an expired one, is refused locally. Tokens and proxy keys are never printed. Scripts that called `provider claim` without options must now choose a credential.
- **`provider wallet set` and `provide --wallet` refuse the unsigned network wallet request** ([#192](https://github.com/full-bars/meso-miner/pull/192)): the platform is moving wallet binding to a signed consent, and whether it still accepts the unsigned request is a server-side policy the binary cannot see. Both now refuse unless `--legacy-network-wallet` is given, and the message names the app or web account route. `provide --wallet` still starts providing after printing the refusal. A running provider that does not pass `--wallet` is unaffected.

### Fixed

- **A failing auxiliary H3 no longer retries in lockstep or spins**: its failure wait is spread by 20% either way, has a one second floor so an unset reconnect timeout cannot make a tight dial loop, and is cut short when the H3 gate is switched, so turning H3 off parks the transport at once.

### CI

- **Both shakedowns run together with a jittered mint** ([#225](https://github.com/full-bars/meso-miner/pull/225)): CI only.

---

## [v2026.10.19-1075696970-meso] — 2026-10-19

### Added

- **A reconnect governor and a connect-attempt budget protect against reconnect storms during backend outages** ([#208](https://github.com/full-bars/meso-miner/pull/208), [#209](https://github.com/full-bars/meso-miner/pull/209)): a provider runs one transport per proxy, and when the backend slows or fails every one of them redials at once. The governor observes connect outcomes, separates failures that are the proxy's from failures that are the platform's, and judges each window against the host's own baseline. While the backend is failing the budget meters connects (with a byte breaker, recovery priority for the proxies that earned before the outage, and shedding under memory pressure), and sessions that die young are held back before they redial. It defaults to shadow on this line: it observes and logs `[govern]` lines without delaying a connect. `URNETWORK_GOVERN=enforce` acts and `URNETWORK_GOVERN=off` disables it.
- **The outage-protection data plane bounds retransmission and releases dead clients** ([#208](https://github.com/full-bars/meso-miner/pull/208)), with a shadow-by-default rollout path.
- **OpenRC lifecycle and installer support** ([#202](https://github.com/full-bars/meso-miner/pull/202)), with completed OpenRC and FreeBSD platform guides ([#203](https://github.com/full-bars/meso-miner/pull/203)).

### Changed

- **The startup disk audit is bounded and honest** ([#210](https://github.com/full-bars/meso-miner/pull/210), [#213](https://github.com/full-bars/meso-miner/pull/213)): the benchmark is bounded, safe and skipped where it cannot help, deep state directories are measured, and an unmeasurable state directory is no longer reported as a full disk.
- **The macOS and FreeBSD installers do a single GitHub download with retries** instead of a duplicate fallback path.

### Fixed

- **The governor is hardened against the cases it used to miss** ([#218](https://github.com/full-bars/meso-miner/pull/218)): a per-host attempt cap when almost every attempt fails in classes the verdict ignores, a crash loop shorter than the startup grace counted as an outage restart, sustained early deaths after a fresh start counted, no probe-harder doubling right after a bytes or verdict cut, and the eviction threshold and heap-over-limit exception documented and enforced.
- **A slow backend no longer deletes saved client identities** ([#218](https://github.com/full-bars/meso-miner/pull/218)): a plain timeout counted as an auth failure, so a backend episode evicted identities and re-minted them; now only an explicit refusal (HTTP 401 or 403) counts, never while the backend is degraded, and automatic eviction is off unless `URNETWORK_EVICT_REVOKED_IDENTITIES=1`.
- **The hourly reconnect pulse is skipped while the backend is degraded** ([#209](https://github.com/full-bars/meso-miner/pull/209)): a pulse that reset every stalled transport's backoff sent every failing proxy back to the struggling backend together.
- **Diagnostics no longer judge goroutine growth during the startup ramp** ([#198](https://github.com/full-bars/meso-miner/pull/198)), so a busy start stops reading as a leak.
- **Retained sequences whose destination acknowledges nothing are ended** ([#204](https://github.com/full-bars/meso-miner/pull/204)) instead of being held forever.
- **Resends are bounded by a retry budget** ([#205](https://github.com/full-bars/meso-miner/pull/205)).
- **The retained-byte budget is charged and released with the same amount** ([#206](https://github.com/full-bars/meso-miner/pull/206)), so accounting cannot drift under churn.
- **A container provider restarts on exit 75 without an opt-in** ([#199](https://github.com/full-bars/meso-miner/pull/199)), and the start scripts treat 75 as a planned restart rather than a crash.
- **A stalled container provider is recovered too** ([#200](https://github.com/full-bars/meso-miner/pull/200)).

### Security

- **IPv4-mapped IPv6 destinations are judged by the IPv4 tables, and special-use IPv6 ranges are refused** ([#211](https://github.com/full-bars/meso-miner/pull/211)).
- **CFAA blocklist refreshed** ([#201](https://github.com/full-bars/meso-miner/pull/201), [#212](https://github.com/full-bars/meso-miner/pull/212), [#214](https://github.com/full-bars/meso-miner/pull/214)–[#217](https://github.com/full-bars/meso-miner/pull/217)), matching the 33.0 cutoff. Data only: port policy and verdict semantics are unchanged.

### Maintenance

- **Go 1.27.2 is required to build from source.**

---

## [v2026.10.6-1064547980-meso] — 2026-10-06

### Added

- **H3 (QUIC) beside H1 for the direct identity, behind `URNETWORK_H3`** ([#159](https://github.com/full-bars/meso-miner/pull/159)): the engine already carried a working H3 platform transport that Auto mode never launched. With `URNETWORK_H3=on` the direct (non-proxied) identity starts it beside H1. A proxied identity never does, because H3 opens a host UDP socket that would bypass the proxy. H3 is auxiliary: H1 stays the health signal, so an H3 connect failure reads as "mode unavailable", backs off quietly up to ten minutes, and is not counted as a backend failure or a proxy auth failure. The H3 socket's bytes count into the identity's total traffic. Costs one extra platform connection per box. No platform-side gain is proven.
- **The H3 runtime gate and per-mode transport counters** ([#162](https://github.com/full-bars/meso-miner/pull/162)): H3 can be switched on and off live with no restart, a persisted `off` beats `URNETWORK_H3=on`, and clearing the key hands the decision back to `URNETWORK_H3`. The provider now reports what each transport mode carries, and measures the H3 share against the direct identity's H1 rather than against every proxy. `urnet-tools set h3 on|off`, or `URNETWORK_H3` as the startup default.
- **The QUIC DATAGRAM receive lane** ([#162](https://github.com/full-bars/meso-miner/pull/162)): a versioned bounded message layer with offer and accept negotiation, behind the `h3_datagram` control key. Receive side only and off by default. Everything the provider sends still goes on the reliable stream.
- **The QUIC DATAGRAM send lane** ([#165](https://github.com/full-bars/meso-miner/pull/165)): on an H3 connection where the server accepted DATAGRAM, a dispatcher chooses the lane per message. A frame that fits one datagram goes as a datagram, and anything larger goes to the reliable stream. The lane falls back to the stream when it is full, when a send errors, or when a blackhole guard trips. Behind `urnet-tools set h3-datagram-send on|off`, default off, read per message so it takes effect at once.
- **The baseline transport series and its panels** ([#162](https://github.com/full-bars/meso-miner/pull/162)): every baseline sample records which transport carried the traffic, and the DNS and DNS-pump modes count under their own names. Eight new Grafana panels chart the series.
- **Transport rows in the Internals panel** ([#163](https://github.com/full-bars/meso-miner/pull/163)): `top` shows what each transport mode carries, with the `h3/all` byte pair rendered in one unit. The rows appear only once the provider has reported a transport read, so a box with no H3 and no DNS modes draws exactly the panel it drew before.
- **ReportId on the close contract** ([#154](https://github.com/full-bars/meso-miner/pull/154)): each logical close report now carries a random 16-byte identifier. It is a backward and forward compatible proto3 optional field, so an older backend ignores it, and it gives the backend a durable dedup identity for retries and out-of-band reports. No client decodes it.
- **A STUN success aggregate in the log** ([#153](https://github.com/full-bars/meso-miner/pull/153)): a raw STUN binding probe per endpoint over IPv4 and IPv6, grouped by provider and aggregated as one line on an adaptive 1 or 5 minute interval. `stun.cloudflare.com` joins the ICE-gather endpoints. Log only; no NAT or ICE behaviour changed.
- **Live traffic, runtime internals and a reworked `top` view** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the live status block separates billable from total traffic, so bytes that were not billable, a direct socket for instance, are visible instead of folded into one number. Session totals follow proxies being removed or respawning rather than jumping. `top` gains a zoomable graph, a runtime panel (goroutines, heap, descriptors), a theme and graph-style menu, and a layout that adapts to a small terminal. The provider answers three new light control-socket commands, `traffic`, `internals` and `goroutines`, so the 100ms poll no longer rebuilds a full snapshot, and an older provider is detected and falls back to the snapshot's own rates.
- **A stated reason for the node's state** ([#131](https://github.com/full-bars/meso-miner/pull/131)): `starting` and `degraded` now say why. The reasons are still resolving proxies, a source that could not be read, a source that returned nothing, or how many proxies are dead against how many are configured.
- **An OOM-aware start cap, shadow by default, with a kill switch** ([#134](https://github.com/full-bars/meso-miner/pull/134)): after a kernel OOM kill of this provider's own cgroup subtree since the previous start (same boot, higher `oom_kill` counter; a reboot is never blamed), the next start runs 80% of the peak running proxies instead of the load that just died. The cap never goes below the larger of 50 and a quarter of the desired pool, takes at most three reductions per 24 hours and then freezes, relaxes 10% at a start that comes a full clean day after the last change, and clears at the desired size. It never writes your `proxy trim` file; the tighter of the two caps applies. The default is `shadow`, which decides and logs and enforces nothing. `urnet-tools set oom-cap on|off|shadow` or `URNETWORK_OOM_CAP` switches it, any source saying `off` wins, and `off` forgets a standing cap so it cannot rebind when the switch goes back on.
- **An action ledger and `urnet-tools autopilot log [limit]`** ([#134](https://github.com/full-bars/meso-miner/pull/134)): every OOM-cap decision and every trim result is appended to `~/.urnetwork/autopilot.jsonl` (time, actor, action, from, to, mode, reason; capped at 256 KiB, newest lines kept). `autopilot log` shows it as a timeline, marks shadow decisions `[shadow]`, and prints the current `oom-cap` value. A provider that predates the `ledger` control command answers with an explanatory error.
- **Trim commands leave a receipt in the log** ([#134](https://github.com/full-bars/meso-miner/pull/134)): `[proxy][trim] received: cap=N (was X); R running, D desired, applying` once per change, an `applied:` result line even when nothing needed shedding, and `received: cap cleared`. These capacity lines also reach the important buffer and the disk event log, so they survive a reboot.
- **A low memory headroom log and startup memory warnings** ([#134](https://github.com/full-bars/meso-miner/pull/134)): host or cgroup available memory below the larger of 150 MiB and 10% of RAM, capped at 400 MiB, for two consecutive 30 second samples logs `[proxy][resources] low memory headroom`, and `recovered` once it sits clearly above the line again. At startup the provider warns when the finite `GOMEMLIMIT` is short for the heap the pool needs or the box is short for the whole-process footprint, flags a `GOGC` pinned under `URNETWORK_PROFILE=auto`, and logs one `effective RAM ceiling` line naming the limit in force. All advisory: the provider never changes these values.
- **`urnet-tools smart-dialer [status|on|off]`** ([#133](https://github.com/full-bars/meso-miner/pull/133), [#134](https://github.com/full-bars/meso-miner/pull/134)): transport scoring carried a hardcoded 500 ms latency baseline that assumed what the network costs. With the smart dialer on, weights come from success ratio and error streak, then scale by measured connect cost relative to the fastest transport that works on this network, and serial attempts try measured-faster transports first. It never removes a transport, failures keep their authority, and below a sample floor the existing order stands. A background probe gives every transport connect-only attempts so the alternatives get measured, and a failed probe never demotes a transport with real successes. Off by default, live, persisted and re-applied at startup. With it off, scoring is exactly what it was.
- **Billable and total traffic in the node snapshot** ([#136](https://github.com/full-bars/meso-miner/pull/136)): the snapshot now carries session byte totals, the total-traffic rate and the persisted lifetime billable total next to the billable rate. Session totals accumulate the same per-tick deltas the rates use, so a proxy that is removed or respawns never adds or subtracts bytes.
- **A built-in baseline recorder** ([#137](https://github.com/full-bars/meso-miner/pull/137), [#138](https://github.com/full-bars/meso-miner/pull/138)): every provider keeps a small local record of how it behaves in `~/.urnetwork/baseline.jsonl` (1 MiB, newest whole lines kept), a start mark at launch and then one sample every 15 minutes. On by default with no setup, so an upgrade can be judged against what the box did before it. Counts and totals only, never proxy addresses, usernames or passwords, and a field that could not be measured is omitted rather than written as zero. `urnet-tools baseline show|mark|compare`; `compare` derives the rate from the lifetime counter's delta, excludes the post-start ramp from both sides, prints `insufficient data` for a short segment and warns when capacity changed. `urnet-tools set baseline off` stops recording at once and keeps the file, and `URNETWORK_BASELINE_INTERVAL` changes the interval.
- **The pressure monitor, pool controller and degraded-proxy reaper are supervised** ([#140](https://github.com/full-bars/meso-miner/pull/140)): a panic in any of them used to end that loop for good, leaving the last pressure score, a tightened GOGC and a shrunk memory budget in force. They now restart with exponential backoff, and the pressure monitor puts its score, memory budget and GOGC back to neutral whenever it exits and removes its `pressure_status` file. A shutdown during the backoff does not count a restart. `urnet_loop_restarts_total` and `urnet_loop_up` show it per loop.
- **GC state, conntrack fill and TCP socket states are recorded** ([#142](https://github.com/full-bars/meso-miner/pull/142)): `baseline.jsonl` and `/metrics` carry GOGC in force, whether the governor is tightening, the GC CPU share, conntrack fill, TIME_WAIT and orphaned sockets. Observation only, the socket readers are Linux only, and a value the box cannot supply (including one of the two socket counters on its own) is left out instead of written as zero.
- **Host memory is read on Windows and macOS** ([#143](https://github.com/full-bars/meso-miner/pull/143)): RAM detection and the available-memory reader only knew `/proc/meminfo`, so Windows and macOS fell back to a fixed 850 MiB. macOS now reads `hw.memsize` and approximates available memory from the page counters, Windows uses `GlobalMemoryStatusEx`, and other platforms keep the fallback. Linux is unchanged and cgroup ceilings still win. The macOS figure is an approximation that has not been run on real hardware.
- **A memory pressure signal on Windows and macOS** ([#144](https://github.com/full-bars/meso-miner/pull/144)): when `/proc/pressure/memory` cannot be read, macOS reads the kernel's memory-pressure level and Windows its low-memory notification, mapped onto the `psi_mem` percent scale. A failed Windows handle creation is retried on the next sample. Linux is unchanged. The level mappings are judgement calls not yet exercised on real hardware.

### Changed

- **A node that deliberately runs with no proxies now reads `active`, not `degraded`** ([#131](https://github.com/full-bars/meso-miner/pull/131)): serving on the direct transport with no proxy source configured is a valid, completed configuration, and was previously indistinguishable from a real outage. A configured source that came back empty still reads degraded, and so does a node whose direct transport is off with nothing to serve.
- **The idle hint blames auth only when it explains the idleness** ([#131](https://github.com/full-bars/meso-miner/pull/131)): a steady trickle of auth retries on a large healthy pool is no longer reported as an auth outage. Auth is blamed for a failure wave, or when most of the pool is not connected while failures are happening; otherwise the hint states what is true and shows the numbers behind it.
- **The reload summary says where additions came from** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the `reloaded: +N added` line breaks the additions down by source, and URL-sourced launches get their own line instead of being folded into a bare count.
- **A hotswap is declined when the box cannot hold two providers** ([#134](https://github.com/full-bars/meso-miner/pull/134)): the candidate runs while the old provider drains, so both are resident. Available memory must be at least 1.1 times the running provider's RSS, bounded by cgroup headroom, otherwise the update falls back to a plain restart and the `urnet_hotswap_outcomes_total` label is `low_memory`.
- **RAM detection honours the unit's own cgroup limit** ([#134](https://github.com/full-bars/meso-miner/pull/134)): it walks the process's cgroup and its ancestors and takes the tightest `memory.max` or `memory.high`, so `MemoryMax=` and `MemoryHigh=` on a systemd unit are seen, and reclaimable page cache is not counted as used. Under `URNETWORK_PROFILE=auto` a unit limited below the host's RAM now selects a smaller tier.
- **The goroutine pressure sensor judges per proxy** ([#134](https://github.com/full-bars/meso-miner/pull/134)): a fixed goroutine ramp held any large healthy pool at pressure 1.00. It now scores goroutines per proxy from 50 running proxies up, keeps the absolute ramp for smaller pools, and does not count the direct transport.
- **Proxy identity is the address plus the user, not the bare address** ([#130](https://github.com/full-bars/meso-miner/pull/130)): a shared gateway can serve several accounts at one host and port. The provider treated the bare address as the identity, so two accounts collided into one entry and flipped which one won on every reload, which looked like a perpetual credential change and rotated forever. The identity key is now the address plus the user for a shared-gateway proxy, with the password excluded so a password change stays the same identity. Health, bandwidth, earnings, the reaper, history pruning and the parked set all follow the new key, and legacy bare-address entries migrate to it. Address remains the dial target everywhere.
- **Trim and pool-shed rank by billable earnings, not total bytes** ([#150](https://github.com/full-bars/meso-miner/pull/150), [#151](https://github.com/full-bars/meso-miner/pull/151)): total bytes include the platform's non-billable traffic, which on a measured box is about seventeen times the billable figure. A proxy that moved a lot of non-billable traffic and earned nothing ranked as a protected earner, and a small real earner could be shed ahead of it. The ranking now reads what each running proxy earned, taking the larger of this run's billable bytes and the decayed earnings score, including a proxy that is still launching and has no bandwidth record yet.

### Fixed

- **A proxy auth baseline is now a deep copy** ([#129](https://github.com/full-bars/meso-miner/pull/129)): the recorded baseline shared a pointer with the live proxy settings, which the runtime mutates during its own auth flow. The baseline drifted within milliseconds of launch, so every reload compared a mutated baseline against a fresh parse, saw a change, and rotated. Only credentialed proxies showed it. A genuine config change still rotates exactly once.
- **H3 closes its transport and socket when a connection ends** ([#155](https://github.com/full-bars/meso-miner/pull/155)): `quic-go` left the transport and its packet socket open on a non-single-use transport, so every H3 reconnect leaked one UDP descriptor, its read goroutine and the transport state. The connection now closes the connection, the transport and the socket in order, and logs an `h3 closed` line with the connection age and frame counts.
- **H3 counts keepalive writes and keeps the drain on payload** ([#156](https://github.com/full-bars/meso-miner/pull/156)): the closed-line frame totals now include outbound keepalives, and the drain compares payload reads only, so a connection that pings itself still drains.
- **H3 keeps empty routed messages out of the payload counter** ([#157](https://github.com/full-bars/meso-miner/pull/157)): empty routed messages no longer count as payload writes, matching the receive-side ping classification.
- **The pool summary catches a slow steady leak and survives a counter rollback** ([#160](https://github.com/full-bars/meso-miner/pull/160)): the sustained-growth gate applied the whole-window floor to a half window, which doubled the rate a leak had to reach before it was reported. A counter rollback underflowed the interval and reported `0% reuse over 18 quintillion takes` forever. The gate now requires half the floor, and the interval re-seeds on a rollback and reports nothing for that dump.
- **The OOM marker, proxy cache, GOGC baseline, baseline start row and trim receipt fixes** ([#158](https://github.com/full-bars/meso-miner/pull/158)): a HotSwap parent and its candidate record starts in the same second, so the run marker now carries a random `start_id` and only its writer refreshes it; an unparseable `proxy_url.json` is moved aside to `proxy_url.json.corrupt` instead of skipping every merge cycle, and `proxy remove` on a file proxy applies again; the GC governor adopts the current GOGC while it is not tightening and never overrides an explicit `URNETWORK_BASELINE_GOGC`; the baseline start row records the version being replaced; and the trim applied line is written durably only when the cap changes or running proxies are shed.
- **The OOM start marker, systemd unit repair, baseline state reason and URL launch line** ([#145](https://github.com/full-bars/meso-miner/pull/145)): the OOM start marker is written in every mode, a no-op update self-heals a missing `Type=` on the systemd unit, a baseline sample records the state reason, and the URL launch line goes through the reload's deferred writer.
- **A source that goes empty stops the proxies it used to supply** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the reload returned before the removal pass, so the old proxies kept dialling and the stale count made the status line ignore the new empty-source state. A proxy with live clients is now drained rather than cut, and the state file is reconciled and written on that path.
- **A file source that cannot be read is reported as a failure** ([#131](https://github.com/full-bars/meso-miner/pull/131)): it left the resolution pending, so the status line read `starting: resolving proxies` and the snapshot later read it as a stuck startup. Running proxies are left alone.
- **A proxy source URL cannot leak its token into the logs** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the per-source log lines and the operator warning now use a redacted label that strips the query, the userinfo, and credential-bearing path segments. A source like `.../token/SECRET/list` no longer reaches the important log, and one source no longer produces two different warning keys.
- **A no-source node stops claiming it is retrying** ([#131](https://github.com/full-bars/meso-miner/pull/131)): that configuration has nothing to retry, and the reason now matches the systemd status line.
- **Pool reuse and growth are judged per interval** ([#131](https://github.com/full-bars/meso-miner/pull/131)): the take and create counters are cumulative, so a tag that churned heavily once stayed flagged as low-reuse forever, a warning nothing could clear. A leak is now sustained growth across the window rather than a single burst at the end of it.
- **Self-management correctness fixes ported with the stack** ([#134](https://github.com/full-bars/meso-miner/pull/134)): the OOM kill switch really turns the feature off instead of reverting to `shadow`, and takes effect at once; the marker heartbeat runs while it is off, so the first real kill after re-enabling is not refused as stale; an unreadable `proxy_url.json` is skipped instead of wiping the cache, grades and blacklist, and no longer cancels running URL proxies; the retention writer batches appends behind one handle and rotates before the write that would cross the limit; the action ledger keeps an oversized newest line instead of zeroing the file; trim keeps a shed proxy's state, the status denominator honours the trim cap, and the cap is honoured before launching at startup; the auto-profile GOGC is applied once per process.
- **A slow give-up could mislabel a later genuine failure** ([#133](https://github.com/full-bars/meso-miner/pull/133)): an attempt cut off by the connect deadline is reported as slow (`cut_short=true`) and no longer moves the shared auth rate limiter, each slow cycle restarts the retry ladder, and proxy-path symptoms (TLS handshake timeout, connection reset) are no longer blamed on API reachability.
- **The slow-retry semaphore was skipped after a genuine give-up, and source URL tokens could reach the log** ([#134](https://github.com/full-bars/meso-miner/pull/134)): the semaphore gate now also counts genuine retry cycles, so a box with many failing proxies cannot send them all at the auth API together. A source URL that failed to parse, a transport failure, two summary lines and the SSRF guard's errors no longer repeat the raw URL; they use the redacted source label. Refusal behaviour is unchanged.
- **A hotswap could leave the audit trail without the new process's entries** ([#135](https://github.com/full-bars/meso-miner/pull/135)): the old process persists its ring again at the end of its drain, and that write could land after the successor's merged persist. The successor now waits out the drain, merges again and persists. The reconcile builds a deduplicated, timestamp-ordered union and keeps the newest 1000, and every persist snapshots and writes under one lock so an older snapshot cannot land after a newer one. Audit trail only; traffic was never affected.
- **The pool controller sawtoothed on boxes with self-heal on** ([#139](https://github.com/full-bars/meso-miner/pull/139)): after a pressure cut it regrew by 25 every five minutes into the same wall. It now caps regrowth at 90% of the pool size where pressure first appeared, relaxing over 24 hours and then released, ranks pressure sheds by the persisted earnings score, and clamps the URL target to the running-proxy cap minus running paid proxies.
- **The GC governor chased its own tail** ([#139](https://github.com/full-bars/meso-miner/pull/139)): the sweep measured memory by a figure that follows the heap goal, which moves with the GOGC the governor writes. The sweep and the 10 second subtick now share one measure (live heap plus goroutine stacks), the subtick honours the CPU veto, and entering the critical level forces a full GC at most once per five minutes.
- **A draining proxy could recreate its own registry entry** ([#141](https://github.com/full-bars/meso-miner/pull/141)): a late log line or dial after unregister created an address-less entry nothing removes, inflating the proxy count and the figures derived from it. Those paths no longer create entries.

### Maintenance

- **The Linux installer no longer downloads from the wrong fork** ([#146](https://github.com/full-bars/meso-miner/pull/146)): `do_install` carried a second copy of its whole body, so after a successful first pass the stale copy looked up the latest tag on the 3.23-fix repo and its mirror, and a fresh install could end with "Failed to download from both primary and mirror". The duplicate is removed, and the test harness now asserts the release lookup appears once and no 3.23-fix URL remains.
- **`urnet-tools` updates come from the meso-miner repository** ([#147](https://github.com/full-bars/meso-miner/pull/147)): the release API and the download URLs were hardcoded to `full-bars/urnetwork-3.23-fix`, so the update timer resolved tags that never matched a `-meso` version and could replace the meso provider with a 3.23-fix build. Every URL now goes through one release repo constant set to `full-bars/meso-miner`.
- **Log emoji markers standardized** ([#152](https://github.com/full-bars/meso-miner/pull/152)): a log-only pass over the operator-facing provider lines (contract acquired and closed, expiry, provide ping, out-of-band, pressure and OOM, hotswap, the Ready banner, the lifetime ledger). No behaviour change.
- **CFAA blocklist syncs** ([#128](https://github.com/full-bars/meso-miner/pull/128), [#132](https://github.com/full-bars/meso-miner/pull/132)): refreshed the content-filtering blocklist from upstream. The set of blocked destinations changed, so a node relays a slightly different set of destinations than before.
- **TLS test helpers shared with the untagged tests** ([#148](https://github.com/full-bars/meso-miner/pull/148)): the untagged TTL tests used helpers defined only in the unix-tagged file, so `GOOS=windows go vet .` failed to type-check the package. The helpers move to an untagged file, unchanged. Linux and darwin behaviour is unchanged.
- **The H3 runtime gate is covered with the gate switched off mid-dial** ([#164](https://github.com/full-bars/meso-miner/pull/164)): the test parks a real dial, flips the gate off, and checks the connection is not counted as a connect or a drop and the transport waits instead of dialling again.
- **Docs** ([#149](https://github.com/full-bars/meso-miner/pull/149), [#166](https://github.com/full-bars/meso-miner/pull/166)): the capacity, dialer, baseline and memory work is documented in the CHANGELOG, FORK_CHANGES and the release notes, and `set help` now lists the h3, h3-datagram, baseline, proxy-audit and metrics-listen keys with their value domains, defaults and effects. The bare `set` listing now prints all documented keys this fork accepts.

### CI

- **Smoke runs serialize per repository** ([#161](https://github.com/full-bars/meso-miner/pull/161)): every smoke job signs in against the live API with the shared account, and the concurrency group was per branch, so six open PRs authenticated at once and tripped the sign-in limit. Every job that lost the race failed as if the code were broken. The group is now repo-wide, so one smoke run exists at a time and a queued run waits. A queued run is cheaper than a red board on unrelated PRs.

---

## [v2026.9.22-1052862940-meso] — 2026-09-22

### Fixed

- **Message-pool buffer leak on connect failure** (<https://github.com/full-bars/meso-miner/pull/126>): the upstream-connect failure path released the pooled packet without returning its buffer to the pool. It now returns the buffer exactly once, with a regression test. Restart a provider to reclaim memory the leak already took.

### Maintenance

- **Deterministic test suite** (<https://github.com/full-bars/meso-miner/pull/124>).
- **Security blocklist sync** (<https://github.com/full-bars/meso-miner/pull/125>).

---

## [v2026.09.21-1790052822-meso] — 2026-09-22 (tag belongs to `full-bars/sn`; the work below is meso's)

### Added

- **Audit ring now records lifecycle events** (<https://github.com/full-bars/meso-miner/pull/118>): `urnet-tools history` previously showed only `set` and `clear` config changes. It now also records process start (with the version), the hotswap handoff, and control-socket shutdown. After an update the sequence reads `hotswap` on the retired process, and the successor's ring shows `start` plus the merged `hotswap` (`start` then `hotswap` on spawned candidates, `hotswap` then `start` on the Docker execve path).
- **Audit entries survive hotswap** (<https://github.com/full-bars/meso-miner/pull/118>): the parent flushes the audit ring at the handoff commit point, before the takeover message, on every handover path (systemd, Windows, Docker). The successor merges the ring from disk after takeover, with timezone-safe deduplication, so the last control-socket commands before an update are not lost. The parent quiesces its control socket at the same commit point, so a command accepted after the flush cannot vanish in the parent's memory; it falls back to the pending-overrides queue, which the successor merges.
- **Live node snapshot and a live block in `urnet-tools status`** (<https://github.com/full-bars/meso-miner/pull/114>): the provider keeps a snapshot of the node, cached for about a second, and serves it over the control socket as `snapshot`. It carries the billable rate now and as 1 and 5 minute averages with a 10 minute history, active clients and sessions, the proxy pool by status, pressure, memory, descriptors and goroutines, the restart reason, and a flowing, idle, degraded or starting verdict with a hint when idle. `urnet-tools status` shows it as a live block, `urnet-tools status --json` prints it for scripts, and a host with several providers gets a one-line summary of each.
- **Restart reason** (<https://github.com/full-bars/meso-miner/pull/114>): `urnet-tools update`, `hotswap` and `restart` record why the provider is about to restart, and the provider reports it after it starts (`update`, `hotswap`, `manual`, `clean`, `unclean` or `first-start`) in the snapshot and as `urnet_restart_reason`.
- **Resource metrics and alert rules** (<https://github.com/full-bars/meso-miner/pull/114>): `urnet_mem_limit_bytes`, `urnet_rss_bytes`, `urnet_open_fds` and `urnet_fd_limit` (the last three on Linux); `UrnetworkNodeDown` and `UrnetworkRestartLoop` Prometheus alerts on by default in the Monitoring bundle, plus four commented-out fleet-specific ones. See `docs/Monitoring.md`.
- **`urnet-tools top`, a live full-screen view of a provider** (<https://github.com/full-bars/meso-miner/pull/115>): the last 10 minutes of throughput as a graph, current and average rate, clients, the proxy pool, memory and descriptors, and recent events such as restarts and state changes. It reads only the provider's control socket (the live snapshot above) and changes nothing. Also available as `urtop`, a link the installer and `urnet-tools update` now create. Keys: `q`, `Esc` or `Ctrl-C` quit; `Tab` and `Shift-Tab` switch provider; `+` and `-` change the refresh rate; `?` shows help. When the provider does not answer (stopped, or an older build) the screen stays up, shows `DISCONNECTED` with the reason and a countdown, and resumes by itself. It needs an interactive terminal; use `status` for scripts.
- **HotSwap how-to and measured costs** (<https://github.com/full-bars/meso-miner/pull/109>): new `docs/HotSwap.md` covering requirements, the update flow, what you will see, and measured costs (connection ramp, memory, drain).
- **Design proposal: per-client make-before-break HotSwap handover** (<https://github.com/full-bars/meso-miner/pull/111>): `docs/design/hotswap-make-before-break.md` plans a handover that keeps every client connected throughout. A proposal only, no code.
- **Bandwidth and heartbeat reporters ported, legacy ps1 scripts retired** (<https://github.com/full-bars/meso-miner/pull/103>).
- **Automated proxy audit and quality enforcement** (<https://github.com/full-bars/meso-miner/pull/116>): `urnet-tools proxy audit on|off|status|release`, parking proxies that grade as junk; status reports parked and paused honestly.
- **Control unitless providers** (<https://github.com/full-bars/meso-miner/pull/119>): lifecycle and settings commands work against bare or containerized providers without a systemd unit, state-dir rows are deduplicated, and `urnet-tools get` reads one setting.
- **File-backed proxy add and paste fixed** (<https://github.com/full-bars/meso-miner/pull/113>): comments and blank lines survive, lock-guarded deduplication, keyed credentials preserved.
- **Security blocklist sync** (<https://github.com/full-bars/meso-miner/pull/117>): refreshed the content-filtering blocklist from upstream — a net reduction of about 6,400 IPv4 ranges and 32 IPv6 prefixes after upstream pruning. The shipped file is the sync-time upstream snapshot; upstream has since moved.

### Security

- **Container-discovery ghost hardening, round 2** (<https://github.com/full-bars/meso-miner/pull/106>): containerized providers no longer appear as host providers (cgroup classification when the mount namespace is unreadable); provider state is read and written through descriptor-pinned handles that walk from the kernel-attributed owner home without following symlinks; state-dir arguments are validated against the owner captured in the same process scan; `docker cp` output is decoded from the tar stream and size-capped; Docker commands have deadlines and the exec fallback rejects option injection; session load and save, the pending-overrides lock, self-heal, hotswap counters, the direct toggle, the reload trigger and the unit backup and replace paths no longer re-resolve a user-controlled pathname as root; a recovered provider binary is chmod'ed and chown'ed on the open file descriptor.

### Fixed

- **HotSwap unit migration was a silent no-op** (<https://github.com/full-bars/meso-miner/pull/109>): the installer writes a unit with no `Type=` line and `update` only rewrote an explicit `Type=simple`. A unit with no `Type=` now gets `Type=notify` and `NotifyAccess=all`.
- **HotSwap declines up front when the running provider has no notify socket** (<https://github.com/full-bars/meso-miner/pull/109>): no SIGUSR2 abort and rollback; new decline label `needs_restart`.
- **pprof diagnostics reappear on every hotswap** (<https://github.com/full-bars/meso-miner/pull/109>): the candidate retries the diagnostics bind until its parent releases the port.
- **`urnet-tools` is on PATH** (<https://github.com/full-bars/meso-miner/pull/109>) for non-interactive shells, zsh and root: the installer links the binaries into `~/.local/bin` and `/usr/local/bin` and writes PATH blocks; `urnet-tools update` repairs older installs.
- **Proxy credential rotation on re-paste** (<https://github.com/full-bars/meso-miner/pull/107>): pasting an address with different credentials rotates the running proxy instead of silently keeping the old credentials; every duplicate entry for an address is scanned before an add is skipped.
- **Docker idle-update poll is bounded** (<https://github.com/full-bars/meso-miner/pull/106>): a hung Docker daemon can no longer stall the idle wait past its own timeout.
- **Message-pool leak attribution** (<https://github.com/full-bars/meso-miner/pull/107>): per-call-site leak tags are on by default, name the acquiring call site, and are race-detector safe.
- **Interactive delegated subcommands wire stdin** (<https://github.com/full-bars/meso-miner/pull/105>): `proxy remove-dead`, `remove`, and `trim` read your answer instead of timing out on piped runs.
- **Stale docs links and CI targets fixed** (<https://github.com/full-bars/meso-miner/pull/102>).

### Changed

- **Provider status uses a sliding severity scale** (<https://github.com/full-bars/meso-miner/pull/118>): `active` at 90% or more of configured proxies live, `partial` at 70-89%, `degraded` at 50-69%, `critical` below 50% (including zero), with the exact percentage always rendered and clamped at 100. A healthy node with a small dead tail of proxies no longer reads as `partial` forever.
- **Start entries persist immediately on normal boots** (<https://github.com/full-bars/meso-miner/pull/118>): only a hotswap successor defers its start entry until the takeover merge; a regular boot keeps writing to disk at once.
- **HotSwap is no longer described as zero-downtime.** Measured on a live node, a hotswap removes the 2 to 3 second window with no provider process, but proxy connections still ramp back over about 30 s, the same as a restart. See `docs/HotSwap.md`.

### Maintenance

- **CFAA blocklist syncs** (<https://github.com/full-bars/meso-miner/pull/101>, <https://github.com/full-bars/meso-miner/pull/104>, <https://github.com/full-bars/meso-miner/pull/108>).
- **Parity completion and release prep** (<https://github.com/full-bars/meso-miner/pull/94>, <https://github.com/full-bars/meso-miner/pull/112>).

### CI

- **Ship-release hands off to the tag-triggered pipeline** (<https://github.com/full-bars/meso-miner/pull/99>, <https://github.com/full-bars/meso-miner/pull/100>).

---

## [v2026.9.18-1049118720-meso] — 2026-09-18

### Added
- **Full parity port (PR #92, #93, #94)**: provider runtime (zero-downtime HotSwap, UNIX control socket with live overrides, Prometheus `/metrics` endpoint, dynamic state overrides, earnings-aware proxy prioritization, audit ring buffer, systemd `Type=notify`), the complete urnet-tools CLI (`config`, `hotswap`, `history`, `lifecycle`, `metrics`, `profile`, `proxy_ids`, `ramlogs`, `dashboard`), all fork test suites, and the Prometheus/Grafana monitoring bundle.
- **Transfer engine parity (PR #93)**: transfer flight tracking with forget-on-RTO congestion semantics, selective-ack ordering and provable-hole wake, WebRTC data channel hardening, route-manager concurrency fixes.
- **Shakedown parity (PR #96)**: `shakedown.sh`/`docker-shakedown.sh` aligned with the release state and extended with the fork's full Q-Z coverage (drop-in merge-order matrix, settings-survive-update, hotswap decline/engage, failure injection, MemoryMax sweep, 5000-proxy stress, multi-provider).
- **`VT_JSON_FILE` machine-readable export in `vt-scan.py` (PR #95)**: JSON write failures now fail the scan instead of silently skipping WDSI staging.

### Fixed
- **Docker publish GHCR-only (PR #97)**: the Docker Hub login leg had no credentials on this repo and failed every main push; the GHCR image (`ghcr.io/full-bars/meso-miner`) is the registry the fleet and installers pull from.
- JWT build mode autodetect: passing an auth code positionally (or `URNETWORK_AUTH_CODE`) selects jwt mode automatically; explicit `BUILD=` still overrides.

---

## [v2026.9.9-1788916951-meso] — 2026-09-09

Full parity with the 3.23-fix v30.9 line: warm proxy prioritization, `sn-status`, and security hardening. The same code was also published as `v2026.9.9-1788917009-meso-hub`, a variant that kept the old hub; the hub has since been removed.

---

## [v2026.8.28-1031582360-meso] — 2026-08-28

Fixes the endless retry loop on dead proxies, makes Docker self-update fetch from this repository, and adds Pelican egg support. Two earlier builds the same day, `v2026.8.28-1031544720-meso` and `v2026.8.28-1031579830-meso`, carry the same release notes. See `releases/v2026.8.28-1031582360-meso.md`.

---

## Inherited from urnetwork-3.23-fix (carried by the parity port)

## [v3.23.0-fix.31.4]

### Fixed
- **Flight window no longer grows back on a resend timeout (PR #640)**: a timeout halved the unreliable-flight window, but the old release path handed the timed-out item to the acknowledge path, which grew the window back on the same event. Timeouts now only shrink the window; delivery evidence is the only thing that grows it. On lossy routes the congestion reduction sticks and recovery is consistent.
- **The receiver no longer acknowledges items it can still evict (PR #641)**: held items were acknowledged on arrival and then quietly evicted when a full hold had to admit an earlier arrival. The sender learned of the withdrawal only after two acknowledgement-tail probes and the full selective-ack lease. The receive hold now commits a prefix that no later arrival can evict; tentative items above the boundary are held unacknowledged and evicted at zero cost, so a removal costs the sender a resend instead of a stalled lease.

### Changed
- **CFAA blocklist refreshed (PR #637)**: content-filtering ranges synced from upstream.
- **Hub deprecation notice (PR #635, #636)**: release notes carry a CAUTION callout. The hub is removed in the next release series.

### Other
- **H3 + miner migration plan (PR #639)**: added `MIGRATION_PLAN.md` and `scripts/h3-workspace.sh`. No behavior changes.

## [v3.23.0-fix.31.3]

### Fixed
- **Pooled buffers returned on every drop path (PR #633)**: `SendPacketWithTimeout` and the multi-client send paths dropped packets without returning the pooled byte buffer on channel-full, timeout, and cancellation paths. A busy relay leaked buffers into the message pool until it pinned hundreds of megabytes. All drop and backpressure paths now return the buffer exactly once, with regression tests covering the ownership contracts.

### Changed
- **CFAA blocklist refreshed**: content-filtering ranges synced from upstream.
- **Hub deprecation notice (PR #635)**: release notes now carry the initial deprecation callout.

## [v3.23.0-fix.31.2]

### Added
- **Zero-Downtime HotSwap on Windows (PR #613, #614)**: the in-place binary handover now works on Windows with SIGUSR2-equivalent process signaling and `execve` replacement; Docker containers can hot-swap the provider binary without restarting the container, with `/metrics` state carried across the handover.
- **Windows lifecycle management (PR #616)**: `urnet-tools` now supports `start`, `stop`, `restart`, and control-socket shutdown on Windows via Task Scheduler and named-pipe control, bringing feature parity with the Linux systemd lifecycle.
- **Go `urnet-tools` in Windows release tarball (PR #617)**: the Windows tarball now ships the compiled Go `urnet-tools` binary alongside the provider; legacy `urnet-tools.ps1` updater removed in favor of the Go-based self-update path.
- **Automated WDSI submission bundle (PR #618)**: every release now produces a ready-to-submit WDSI (Windows Driver Signing Infrastructure) bundle automatically, removing manual preparation steps.
- **Metrics reachable out of the box (PR #620)**: `/metrics` endpoint is now enabled by default and reachable without requiring manual `URNETWORK_PPROF` opt-in, so Prometheus scraping works immediately after install.
- **Prometheus and Grafana monitoring bundle (PR #621)**: a ready-to-deploy monitoring stack ships with the release, including a Prometheus configuration that scrapes the provider's `/metrics` endpoint and a Grafana dashboard with pre-built panels for provider health, transfer throughput, proxy tiers, and contract metrics.

### Fixed
- **`/metrics` Prometheus parse errors (PR #619)**: fixed metric name and label formatting so the `/metrics` endpoint parses cleanly under Prometheus's text exposition format without scrape errors.
- **Root allowed through control-socket peer check (PR #622)**: root callers are no longer rejected by the Unix socket peer credential check; non-root rejections now send a structured JSON error response instead of a bare message.
- **`config` command discovery path (PR #623)**: `urnet-tools config` now resolves providers using the same discovery logic as `status`, eliminating mismatches where `status` found a provider but `config` could not.
- **Docker nightly staging path and update settle delay (PR #624)**: corrected the tarball extraction path in the nightly Docker build and increased the post-update verification settle delay so slow-starting providers are not falsely flagged as failed.
- **Throttled errors counted in Prometheus counter (PR #626)**: errors suppressed by the rate limiter were not incrementing the Prometheus error counter, so dashboards under-reported error rates during sustained failure storms.
- **Three shakedown root-cause fixes (PR #627)**: addressed three distinct failure modes discovered during automated shakedown runs — incorrect script exit handling, missing prerequisite checks, and stale state between shakedown sections.
- **`urnet-tools` provider deduplication by state dir (PR #628)**: when multiple unit files pointed at the same provider state directory, `urnet-tools` listed the same provider multiple times; units are now deduped by canonical state directory path.
- **Docker nightly tarball extraction regression (PR #629)**: added a regression test and fixed the extraction path handling for nightly tarballs inside Docker containers.
- **Smarter update verification loop with PID tracking (PR #630)**: the post-update verification now tracks the new process by PID instead of relying on a fixed settle delay, so it detects both success and failure faster and avoids false negatives on slow starts.
- **HotSwap Docker execve arg sanitization (PR #614)**: Docker `execve` arguments for in-container HotSwap are now sanitized, preventing injection of untrusted values during the binary handover.
- **`gofmt` compliance for `connect.go` (PR #612)**: formatted `connect.go` to pass the `gofmt` CI gate.

### Changed
- **Release engineering hardening (PR #617, #618, #629)**: Windows packaging cleaned of legacy updater remnants; WDSI bundle automated; Docker nightly extraction regression-tested to prevent silent packaging regressions.

---

## [v3.23.0-fix.31.1]

### Added
- **`urnet-tools history` command**: exposes the provider's 1000-entry command audit ring via the control socket. Supports `--limit` and `--cursor` for pagination. Fixed wire-format mismatch (`int64` vs `time.Time` serialization) that broke every invocation.
- **HotSwap unit migration (PR #611)**: `urnet-tools update` rewrites a `Type=simple` unit to `Type=notify` when the binary it installs sends `READY=1` at startup (v31.0+), so the following update can hot swap; the migrating update itself uses a normal restart. The unit follows the installed binary: installing anything older, or a binary whose version cannot be read, demotes it back to `Type=simple`, as does a HotSwap rollback to such a binary. A `Type=notify` unit is therefore never left paired with a binary that cannot signal readiness, the skew #546 kept the installer on `Type=simple` to avoid. Unit, temp and backup files are written with `O_NOFOLLOW`. Non-fatal: a failed migration logs a note and falls back to restart.
- **HotSwap operator-facing decline messages (PR #611)**: every HotSwap decline point now tells the operator what went wrong in plain language and what to do about it, instead of technical jargon. Covers version too old, Type=simple, Windows, and missing NOTIFY_SOCKET.
- **HotSwap decline metrics (PR #611)**: new `urnet_hotswap_outcomes_total{reason="..."}` Prometheus counter tracking HotSwap success and decline outcomes by reason (version_old, unit_not_notify, windows, takeover_failed, etc.). Written atomically by `urnet-tools` and scraped by the provider's `/metrics` endpoint.
- **`ParseByteCount` human suffixes (PR #610)**: `urnet-tools set gomemlimit` now accepts common formats (`1536M`, `2G`, `512MB`, `1g`, `2tib`) in addition to the original lowercase iB variants. Spaces stripped, longest-suffix-first matching.
- **`urnet-tools set metrics on|off` (PR #611)**: when `URNETWORK_METRICS` is unset, the listener starts on the first free loopback port in `127.0.0.1:9100-9103`. Set `URNETWORK_METRICS` to serve it on another interface. A persisted `on` is applied at boot.
- **Proxy earnings priority ranking (PR #611, from #601)**: proxy launch order now considers earnings history — high-earning URL proxies are promoted into the trusted launch group alongside file proxies. Sort order: warmth first, then provenance, then earnings. Exploration quota interleaves 1 unproven proxy per 5 trusted cold proxies to prevent starvation.

### Fixed
- **`urnet-tools history` wire-format mismatch**: `AuditEntry.Timestamp` was `int64` on the client but the provider sends `time.Time` (RFC3339 string), causing JSON unmarshal failure on every invocation.
- **`urnet-tools set metrics on` no-op**: `applyMetricsLive` required `URNETWORK_METRICS` env var at boot — if unset, the server never started and `set metrics on` silently did nothing. Now auto-starts the metrics server on a free loopback port.
- **HotSwap declined on every systemd node**: installs run `Type=simple`, so HotSwap always declined. The unit is now migrated during update when the installed binary supports it. The migration runs in the `urnet-tools` performing the update, so on a 31.0 node run `urnet-tools self-update` before `urnet-tools update` to migrate on the upgrade to 31.1.
- **Proxy earnings priority inversion**: cold promoted URL proxies could jump ahead of warm unpromoted URL proxies because provenance was checked before warmth. Sort order corrected to warmth-first.
- **Proxy earnings starvation**: unproven URL proxies could be permanently starved behind cumulative cold-proxy ramp delays. Added exploration quota.
- **Contract retry wait**: the wait is recomputed from its baseline before every retry, so it follows denials that async `CreateContract` callbacks record between retries and drops back once a contract succeeds or the denial state expires.
- **Adaptive proxy `ParallelBlockSize`**: `getAdaptiveBlockSize()` now guards against zero/negative values.
- **`consecutiveErrors` overflow**: capped at 20 to prevent unbounded growth.
- **`writeStateFile` double close**: the fd was closed directly and again by the `*os.File` wrapping it, whose finalizer later closed whatever descriptor had reused the number. Surfaced as intermittent "bad file descriptor" test failures.
- **HotSwap counter file symlink write**: `urnet-tools` (root) wrote `.hotswap_declines.json.tmp` into the provider-owned state dir with `os.WriteFile`, which follows symlinks. Now written with `O_NOFOLLOW`.
- **Contract denial backoff cycled instead of climbing**: denial state expired 2 minutes after the last denial, shorter than the 120s+ tiers, so the count reset before reaching the cap. State now survives backoff + 2 minutes. A frame carrying several errors counts as one denial.
- **Adaptive probe batch shared across proxies**: the success window was process-wide, so one proxy's failing path resized every proxy's batch. Now one window per client strategy; the batch halves when healthy and never exceeds the configured `ParallelBlockSize`.
- **`gogc` "off" could disable GC**: a set carrying `OFF` (any casing the CLI did not rewrite to a clear) called `SetGCPercent(-1)`. Only `disabled` turns collection off; the CLI clear match is case-insensitive.
- **Persisted `metrics on` ignored at boot**: without `URNETWORK_METRICS`, a saved `on` was never re-applied at startup, so `set metrics on` did not survive a restart. The auto-selected listener is now held open from probe to serve.
- **Denial backoff overflow**: a very large denial count overflowed the shift and returned no backoff.
- **Serial dialer ordering race**: dialer health is snapshotted before sorting, with one comparator shared by both evaluation paths.
- **Unit migration hardening**: a symlinked unit file is refused, and files root writes into a user's unit directory are handed to that directory's owner.
- **Test harness global mutation**: `withGlobalEarningsStore` now uses `t.Cleanup` to restore state.

### Changed
- **CFAA blocklist sync (PR #609)**: refreshed Computer Fraud and Abuse Act IP blocklists from upstream definitions.
- **HotSwap decline messages**: all 5 decline points now include operator-facing guidance with actionable next steps instead of technical error strings.
- **Proxy warmth test expectations**: updated to reflect warmth-first sort order.

### Test Coverage
- 62 new test functions covering denial backoff, proxy selection and ordering, adaptive probe batching, HotSwap unit migration, counters and declines, the control socket (`gogc`, metrics at boot), earnings launch ranking, `ParseByteCount`, the audit wire format, and the `writeStateFile` descriptor fix.

---

## [v3.23.0-fix.31.0]

### Added
- **Provider Control Plane & Daemonization (Flagship)**: Decoupled provider service execution from interactive CLI sessions; added a background supervisor, a local Unix domain socket control plane (`~/.urnetwork/provider.sock`, `provider/control_socket.go`) and client protocol in `urnet-tools` for live runtime configuration management without restarts or brittle shared-file locks. Atomic set/clear, single-owner `provider_state.json`, and cross-process flocking.
- **Offline Pending Configuration Queue**: Seamless fallback queue (`~/.urnetwork/pending_overrides.json`) for `urnet-tools set` invocations when the provider is offline, atomically merged and applied one-shot on startup.
- **Zero-Downtime HotSwap (Docker only in practice)**: In-place binary handover with control-socket/listener transfer, mandatory TAKEOVER-ACK-then-yield, graceful drain, SIGUSR2/`urnet-tools hotswap` + Docker PID-1 in-place `execve` and systemd `Type=notify` handoff, and automatic rollback if the candidate fails to take over.
- **Public IP Autodetection & Dashboard Rename (PR #534)**: Automatic detection of public IPv4 address with caching, 60s TTL, concurrent request deduplication, and opt-out (`~/.urnetwork/disable_ip_autodetect` / `urnet-tools ip-detect off`); added `urnet-tools rename` to set custom dashboard display labels dynamically.
- **Systemd Environment Key Migration**: Unified `URNETWORK_PROFILE`, `URNETWORK_RAMLOGS`, `GOMEMLIMIT`, and `GOGC` under control-socket configuration, eliminating manual `override.conf` requirements.
- **urnet-tools Multi-Provider UX (one provider per OS user)**: implicit current-user scope on ordinary commands; `providers` / `providers --all` inventory; cross-user `--user`/`--unit` commands self-elevate under `sudo <full binary path>`; `-y`/`--yes` aliases `--force`; `optimize` self-elevates and applies atomically with rollback; actionable manage-time refusals; control-socket liveness in `status`.
- **JWT Refresh Transfer Stats Logging**: Step 3/3 of JWT auto-refresh parses `paid_bytes_provided` and `unpaid_bytes_provided` from `GET /transfer/stats` and logs account balances in human-readable units inline (`unpaid: X, paid: Y`).
- **CFAA Blocklist Synchronizations (PR #540, #542)**: Automated synchronization of Computer Fraud and Abuse Act IP blocklists with upstream definitions.
- **Unified configuration, v2 schema (PR #575)**: the provider becomes the single source of truth for every setting. A v2 envelope, backward compatible with v1, records where each setting came from (socket, env, pending, legacy, default); a `status` socket command returns all of it with that metadata, and `urnet-tools config` renders it as a table or JSON. Mutation goes through one API so values and their metadata cannot drift, and persistence syncs the file and its parent directory.
- **Prometheus `/metrics` endpoint (PR #565)**: eleven `urnet_*` metrics covering uptime, connections, proxy pool by status, per-proxy bytes and clients, errors by category, contracts by result, and Go runtime memory and goroutines. No new dependencies; the text format is generated directly. Mounts on the loopback diagnostics server, with `URNETWORK_METRICS` binding it on the Tailscale address for remote scraping.
- **`urnet-tools config`, `history`, `profile`, `metrics`, `dashboard` (PR #568)**: settings with their sources, the provider's 1000-entry command audit ring, the tuning profile, a live toggle for the metrics endpoint without a restart, and a terminal status panel. `set` now warns when a key needs a restart to take effect.
- **Control socket hardening (PR #573)**: `SO_PEERCRED` checks the connecting UID against the provider's on Linux, a five-second per-line read deadline closes the slowloris shape, requests are capped at 64 KiB and values at 4 KiB, and protocol version negotiation rejects anything newer than it understands rather than parsing it hopefully.
- **Server-side control validation and restart flagging (PR #566, #572)**: all fourteen keys are validated at the server, so a raw socket client cannot persist a value that breaks the next start. Each response carries a `needs_restart` flag computed from whether the key has a live side effect, so it cannot go stale. `v4` and `v8` are accepted as turbo aliases.
- **Phased startup banner and Ready summary (PR #571, #577)**: boot messages are collected into phases with per-phase results and timings over 100 ms, closed by a single `✅ Ready` line carrying version, profile and proxy count. Suppressed under systemd and by `URNETWORK_STARTUP_BANNER=0`.
- **Version stamp for `-trimpath` builds (PR #579)**: `-trimpath` strips `-ldflags` out of Go build info, so `main.Version` cannot be read back out of a release binary. The release workflow now embeds `URNET_VERSION_STAMP=<version>` as program data, which survives, and the tool scans for it without executing the file. Works on a stopped provider, a cross-architecture binary, and any release build, and is what lets a v30.9 tool verify an upgrade to v31.
- **Provider version over the control socket (PR #578, #580)**: a running provider answers a `version` request with its own build string, the only source reporting what the process is rather than inferring it from the filesystem. Falls back to `/proc/<pid>/exe` and then on-disk build info, which matters because `update` swaps the binary before the HotSwap preflight runs and a rename over a running binary leaves that path naming a deleted file.
- **Conntrack tuning in `optimize` (PR #569)**: both settings are host-wide and persist via /etc/sysctl.d. The one-hour established timeout is deliberate: a proxy node churns connections, and at the kernel default of five days the table fills with entries for peers that are long gone. Idle connections elsewhere on the box that do not send keepalives can be reaped after an hour. `nf_conntrack_max` was never set, leaving the 65,536-entry kernel default, which is far too low for a proxy box. It is now scaled to host RAM across six brackets, from 131,072 under 1 GB to 4,194,304 above 32 GB, with `nf_conntrack_tcp_timeout_established` cut to an hour. Skipped cleanly when the module is not loaded, persisted to `/etc/sysctl.d/99-urnetwork.conf` with rollback.
- **Heartbeat build line (PR #595)**: `[health][build]` leads every five-minute heartbeat tick with version, profile and node name, so a log tail can be tied to a build after the startup banner has scrolled out of the RAM log window.
- **Message-pool health verdict (PR #592, #595)**: an aggregated pool line in the heartbeat reporting `ok`, `warming`, `watch` or `leak` in plain language, keyed on the outstanding low-water mark and all-time allocation count rather than a return percentage, which cannot detect a leak proportional to throughput. Needs an hour of uptime before the floor tracking is meaningful, which `warming` states with a countdown. Documented with worked examples in `LOG_REFERENCE.md`.
- **`urnet-tools proxy ids` (PR #593)**: shows the `client_id` the platform assigned to each proxy, including the `direct` transport. Bearer tokens in the client-JWT store are never printed.
- **Proxy resolution state in systemd `STATUS=` (PR #591)**: reloads publish the configured proxy count and a resolution state; a failed or empty fetch publishes the failure with its reason and emits a rate-limited operator warning. Operator-facing only, and `READY` stays decoupled from health.

### Fixed
- **ICE IPv6 Host Candidate Guard**: Gated synthetic IPv6 host candidates behind `egressIPv6Usable()` send probes to prevent blackholed cellular routes on Android from stalling ICE connection negotiation.
- **Memory Target & GC Tuning at Startup**: Persisted `gomemlimit` and `gogc` applied at bootstrap and runtime via debug APIs.
- **systemd `Type=notify` start/restart wedge (PR #543, #546)**: the provider signalled `READY=1` only once a proxy had authenticated, so a `Type=notify` unit waited on a readiness signal that arrived late or never and `systemctl start`/`restart` hung. READY is now sent when the provider is self-managing.
- **Provider version on `-trimpath` release builds (PR #547)**: `urnet-tools` could not resolve the version from a stripped release binary, so `update --tag` reported the wrong version and refused to align an install to a requested release.
- **HotSwap drain misreporting a successful handoff (PR #552)**: the liveness monitor armed even when no child process backed the session, where `Wait` returns immediately; that case and `ctx.Done()` were both ready and Go chose at random, so a successful handoff could exit non-zero as a dead candidate.
- **Profile defaults clobbering persisted `gomemlimit`/`gogc`**: applying a profile overwrote values an operator had set explicitly.
- **Installer `json_escape` emitting unquoted JSON**: corrupted `pending_overrides.json`; queueing an override under `sudo` also left the file owned by root.
- **The weekly auto-update timer had never completed an update (PR #581, #583)**: `ExecStart` was a bare `urnet-tools update`, systemd hands a oneshot unit `/dev/null` on stdin, and the version prompt was gated only on `!force`, so every run exited 1 on the refused read. The two confirmation gates ignored the interactive decision the target pickers already used, so one run could skip the picker as non-interactive and then demand a prompt. A single `unattendedUpdate()` decision now drives all of them: force, a systemd `INVOCATION_ID`, or stdin on `/dev/null` skip prompts, while other non-interactive stdin still refuses, so a pipe or a pty-less SSH session cannot silently auto-confirm. Concurrent updates of one binary are serialised by an flock. Not a v31 regression; the install script has written that `ExecStart` since before v3.23.0-fix.30.9.
- **HotSwap had never engaged on a release build (PR #578)**: `update` swaps the binary before the preflight runs, and a rename over a running binary leaves `/proc/<pid>/exe` naming a deleted file, so the preflight read an empty version and declined with a reason naming a version that was never the problem, returning before the unit-type check ran.
- **A sentinel-update unit appeared as a provider (PR #576)** and **discovery now corroborates units against `ExecStart` (PR #562)**.
- **The HotSwap decline reason was lost on the update path (PR #560)**, so a fallback to restart reported the fallback without saying why.
- **Profile commands with no arguments print current status (PR #564)** rather than erroring on the empty argument.
- **HotSwap handed off to the binary it replaced (PR #588, #594)**: the handoff resolved its image with `os.Executable()`, which follows the inode rather than the path, so once the updater moved the running binary to a backup it spawned the backup. The old build came up, announced READY, the parent drained, and the operator saw a clean zero-downtime swap that changed nothing. `updateProvider` skips the unit restart when the trigger succeeds, so the fallback that would have worked was bypassed precisely because the handoff "worked". Not a regression. The install path is now captured at init and the candidate is validated with `syscall.Access(X_OK)` as a regular executable file. A provider on an earlier build cannot be HotSwap-upgraded onto this one; restart the unit once.
- **Version resolution defeated by `+dirty` pseudo-versions (PR #587)**: Go 1.27 with `-trimpath` emits `v0.0.0-<ts>-<hash>+dirty` in build info. The filter rejected `+incompatible` but not `+dirty`, so the pseudo-version passed as real, short-circuited the version-stamp scan, and `hotSwapVersionOK` declined a valid candidate.
- **Empty version in the batch confirmation gate (PR #584, #585)**: `confirmGateMulti` now records the version running before an unattended swap, and falls back to reading the binary when the in-memory provider struct carries no version.
- **RAM log trim replaced kept history with NUL padding (PR #596)**: the five-second trimmer read with `ReadAt` on a write-only descriptor, so the read always failed with `EBADF`; the error was discarded and the zero-filled buffer was written back as the preserved history. Both the main and the important buffer were affected, the latter 97% padding. Measured on a live node, 3.5 MB of NUL bytes preceded the first real line. Not a regression, and never covered by a test.
- **`urnet-tools logs` followed stored state, not the provider (PR #597)**: the RAM-log decision came from the control-state chain alone, while the provider decides from `URNETWORK_RAMLOGS` in its environment. The two disagreed whenever the variable arrived via a unit body, a drop-in, an `EnvironmentFile=`, or `docker -e`, streaming an almost-empty journal while the provider wrote to `/dev/shm` twice a second.
- **Two unthrottled error lines (PR #598)**: `completeHandshake failed` (487 lines in 6m25s on a node with 1160 proxies) is now throttled per reason class rather than per client, since a per-client throttle on a busy box is no throttle at all; `[c]audit send error` is throttled globally. Both carry an `(N suppressed)` count.
- **Lossy carriers stalling the reliable lane (PR #586)**: ports upstream's WebRTC lane-gating fix. A full unreliable resend queue stalled the whole send sequence while a reliable relay lane sat idle. Unreliable flight limits are decoupled from the global resend-queue gate, acknowledgement RTT samples from unreliable transports are excluded from the RTT window, and ICE UDP connections get a 4 MB socket buffer. Running the port live surfaced three further defects fixed here: a flight reserve stranded on zero-byte acknowledgements, a message-pool double return in the multi-route write helpers, and a nil-map panic from a shadowed declaration in route selection that this change made reachable.
- **Phantom providers from non-provider siblings (PR #599)**: discovery fell back to matching any `urnetwork-*` unit name when `ExecStart` could not be read, so an unrelated unit appeared as a second provider and made every command on a single-provider box ambiguous. A name-only match now also requires a `.urnetwork` state directory.

### Changed
- **`urnet-tools ip-detect` is now `urnet-tools show-ip`**: the old name described the mechanism rather than the effect and read as a diagnostic that would report the IP, and sat awkwardly beside `direct`, which is about the address the provider serves on. `ip-detect` and `ipdetect` remain as aliases.
- **Settings changes are logged at the provider**: `set`, `clear`, the startup merge of queued overrides, and dashboard-label changes now emit log lines, so an operator can confirm from the node's own log that a change registered with the daemon. `get` stays silent because `status` polls it on every invocation.
- **`urnet-tools help` lists `hotswap`**: registered since PR #533 but never shown in the usage text.
- **Tuning help text corrected**: `turbo`, `auto`, `eco` and `lowmode` described writing a systemd drop-in; they have written the control-socket key since the v31 migration.

### Release Engineering
- **Pre-release shakedown sections Q-Z (PR #544)** and a **Docker Shakedown workflow (PR #545)** covering the v31 daemon and container surfaces.
- **Shakedown runs to completion (PR #550)**: section L read an unassigned variable, which under `set -u` aborted the suite silently, so every section after it had never executed. Both shakedown workflows now surface the script's raw output on failure and warn on a report with no SUMMARY.
- **CI wall clock roughly halved (PR #551)**: race suite sharded across three runners, lint parallel to the tests, retry no longer masking genuine failures, and concurrency groups on the three workflows that lacked them.
- **Manual shakedown dispatch can target a release (PR #548)**.
- **Shakedown failures across four surfaces (PR #558)**: Docker architecture selection, `--tag`, restart verification and hotswap. Plus a Docker shakedown that raced image publication (PR #555) and review findings on both workflows (PR #557).
- **Two tests fixed (PR #559, #563)**: an exec race in the discovery fallback test, and an auto-update test that mutated the host it ran on.
- **A test that passed only in CI (PR #589)**: `TestCurrentProviderNetworkID` redirected `HOME` but not the package-level client-JWT store, which is initialised at load time from the real home directory, so it failed on any machine that had ever run a provider.
- **`sn-status` error contract and flake (PR #590)**: `FetchSnStatus` always returned a nil error, so a total API outage surfaced as zero-value assertion mismatches. It now errors when both independent calls fail while preserving partial results, and each call gets its own timeout budget instead of sharing one across three.
- **Update timer unit passes `-y` (PR #583)**: makes the non-interactive intent explicit in the file an operator reads.

---

## [v3.23.0-fix.30.9]

### Added
- **TCP-return retention across ack timeouts (PR #506, #521)**: in-flight packets on TCP return paths are retained past ack deadlines instead of being prematurely dropped, protecting relay session continuity and operator billing credit during transient network stalls. Retained items are bounded by `MaxRetainedBytes` and cleanly drained on flow resets (`rstFlow`).
- **Retention health telemetry (PR #506, #521)**: structured `retained_ack` and `retained_drop` events written to the persistent health log (`proxy_health_log.go`) via a buffered, thread-safe writer.
- **Bulk proxy ingestion via `proxy paste` (PR #502, #520)**: operator command accepting raw proxy lines from stdin, files, or HTTP(S) URLs. Auto-detects and normalizes `host:port`, `socks5://`, `user:pass@host:port`, and CSV formats.
- **Signal-safe proxy-paste temp cleanup (PR #520)**: unlinks plaintext temporary proxy-paste files on `SIGINT`/`SIGTERM` with cross-platform process signaling (`os.Process.Signal`) and restores default signal disposition.
- **SSRF guard on proxy sources (PR #522)**: dial-time and redirect-time verification blocking all operator-supplied URLs from reaching loopback, link-local metadata (`169.254.169.254`), private RFC1918, IPv6 ULA, and multicast addresses.
- **Dynamic direct transport toggle (`provider direct`) (PR #510, #514, #516, #517)**: CLI command (`provider direct on|off`) to enable or disable native direct IP transport on demand without restarting SOCKS5 proxies. State is persisted atomically to `~/.urnetwork/direct_override`.
- **Usage & traffic accounting (`urnet-tools usage`) (PR #507, #521)**: aggregate traffic accounting distinguishing billable relay bytes from control-plane protocol overhead, with rolling time-series summaries (`24h`, `7d`, `30d`, `lifetime`).
- **`choose_network` presets (PR #504)**: fast target switching using `main` and `beta` network presets.
- **Persistent DoH cache (PR #496)**: cached DNS resolutions and per-server scores persist across restarts, so a reboot no longer cold-starts the resolver.
- **Upstream production DoH optimizations P1-P3 (PR #495)**: server scoring, serve-stale (RFC 8767), staggered launch, single-flight coalescing, memory budget, TLS resumption, and warm-up on resolver failure.
- **Client JWT hot-restart v2 (PR #494)**: expired client JWTs are renewed on startup and identities are snapshotted before restart, gated on network compatibility. A provider restarts with a working identity instead of re-authenticating from scratch.
- **Warm proxy priority on startup (PR #526)**: evaluates local client JWT store on boot and prioritizes warm, previously-authenticated proxies during startup selection to minimize validator probe latency.
- **Accelerated startup stagger for warm proxies (PR #526)**: reduces connection launch stagger from 2s to 100ms for known-warm proxies, slashing cold-start ramp times by up to 95%.
- **Network ID conflict guard (PR #526)**: hardens `AnyNetworkID` against mismatched network identity state, ensuring strict network identity consistency.
- **Subnet 25 telemetry & status dashboard (`sn-status`) (PR #528)**: live Bittensor Subnet 25 miner and node telemetry displaying global ranking, top-200 tier eligibility, net bandwidth provided (MiB/GiB), registered coldkey (SS58 & Hex), subnet epoch lifecycle, and finalized epoch merkle pool payout share (basis points & percentage). Available across `urnet-tools sn-status`, `urnet-docker sn-status`, and `provider sn-status`.
- **JSON machine telemetry output (`--json`) (PR #528)**: machine-readable JSON output for `sn-status` across all CLI tools for automated monitoring, scraping, and alerting fleets.
- **Subnet 25 ranking API client bindings (PR #528)**: Go SDK methods `NetworkGetRankingSync` (`GET /network/ranking`) and `StatsLeaderboardSync` (`POST /stats/leaderboard`) on `BringYourApi`.
- **Bittensor operations guide (`docs/Bittensor-Operations.md`) (PR #528)**: operator guide covering Subnet 25 mining mechanics, coldkey registration, epoch lifecycle, top 200 cutoff requirements, reward claims, and CLI monitoring workflows.
- **Go-native `idle-update` (PR #525)**: polls billable throughput rate before updating in place, ensuring active client sessions aren't severed, with configurable `--timeout` ceiling and `--threshold`.
- **Docker CLI parity (PR #525)**: `urnet-docker direct`, `urnet-docker usage`, and `urnet-docker proxy paste` delegating to in-container commands.
- **Tilde home path expansion (PR #525)**: expands `~` in `--file` and positional arguments across CLI tools and shells.
- **CFAA blocklist sync (PR #527, #530, #532)**: refreshed Computer Fraud and Abuse Act IP blocklists from upstream definitions (IPv4: 48,427 entries, IPv6: 453 entries).
- **Automated CFAA blocklist sync CI/CD (PR #529, #531)**: daily automated GitHub Actions cron workflow and repository dispatch trigger that monitors upstream `urnetwork/connect` for blocklist updates, verifies lookup table integrity, and opens automated pull requests with Discord webhook notifications.
- **Provider version in `urnet-tools status` (PR #497)**: each provider's actual running version is shown.
- **Subprocess timeouts (PR #499)**: every discovery and systemctl call is bounded (5s/10s) so a hung process cannot wedge the tool.

### Changed
- **`pathTable` LRU eviction & bounds (PR #513)**: multi-hop route table in `ip.go` guarded with mutex locking, bounded size cap, and LRU pruning to eliminate unbounded memory growth under peer churn.
- **DNS negative-cache bounded pruning (PR #513)**: replaced unbounded negative DNS caching with periodic sort-based eviction.
- **Message pool metrics & refcount safety (PR #505, #521)**: converted pool size distribution mutex (`sizeDistMu`) to `sync.RWMutex` and added bounds checking against buffer counter underflows/overflows.
- **urnet-tools security audit remediation (PR #499, #505, #509)**: version strings are read from Go build info when recorded; discovered binaries are still executed as a 3s-timeout `--version` fallback when build info lacks a version (e.g. `-trimpath` builds); `set`/`fast-auth`/`self-heal` chown created state files to the provider user instead of leaving them root-owned; secure hub install verification via release digest and exclusive temp download; `runtime.GOARCH` arch detection; atomic drop-in writes with `%%` escaping.
- **Reliable provider updates (PR #497, #503)**: post-restart verification reads running process image via `/proc/<pid>/exe`; redundant reinstall for stale on-disk binaries.
- **Direct reloader lifecycle (PR #514, #517, #521)**: compare-and-delete CAS tracking and clean `cancelMap` cleanup prevent orphaned cancellation tokens or race conditions during rapid reload cycles.
- **Bounded DNS resolution (upstream)**: DNS lookups capped at 3s and abort on the winning server.
- **Source comments cleanup (PR #518, #519)**: removed stale review-process references and AI attributions for clean codebase hygiene.

### Fixed
- **Contract billing debit rounding (PR #521)**: fixed `unack()` debit rounding to ensure packets discarded at the retention backstop ceiling are never erroneously credited as acknowledged billable transfer.
- **Cross-platform build compatibility (PR #520)**: replaced POSIX-only `syscall.Kill` with standard `os.Process.Signal` for clean Windows (`GOOS=windows`), macOS (`GOOS=darwin`), and Linux compilation.
- **SSRF test fixture loopback isolation (PR #522)**: added unexported `ssrfAllowLoopback` toggle to allow local `httptest.NewServer` mock fixtures during test runs without relaxing strict production guards.
- **Direct toggle `<state>` argument parsing (PR #517)**: fixed docopt command parsing to properly recognize positional state arguments.
- **Root PATH sanitization (PR #517)**: preserved accessible executable directories in system search paths while preventing ancestor directory traversal.
- **Uninstall path guard (PR #499)**: `uninstall` refuses to delete well-known system directories; no longer removes arbitrary absolute paths.
- **Update confirm no longer hangs (PR #499)**: the non-TTY confirm path returns an error instead of blocking on stdin forever (cron/CI-safe).
- **Chown of state dir (PR #499)**: `chownLikeStateOwner` no longer a self-referential no-op; the provider's state dir is correctly owned.
- **DoH concurrent-map race (PR #499 / upstream)**: `stateLock` restored around the stale-cache read.
- **14 parity findings resolved (PR #523)**: fixed CodeRabbit-confirmed findings from meso-miner parity port audit across provider core, memory allocation, and connection management.

### Removed
- **Unreliable outage watcher**: the alert-webhook-based outage signaling feature was removed (parity with meso-miner). It produced false outage signals.

---

## [v3.23.0-fix.30.8]

### Added
- **Pelican panel egg support (PR #480)**: the hardened provider image is now importable into the Pelican game-server panel as a one-click egg (`pelican/egg-urnetwork-323fix.json`, PLCN_v3). `BUILD` (stable/nightly/jwt) and `USER_AUTH` are user-editable; `PASSWORD` and `AUTHCODE` are admin-only. `PELICAN=yes`, `ENABLE_VNSTAT=false`, and `ENABLE_IP_CHECKER=false` are hidden and non-editable in the egg, pinning the audit-preferred defaults.
- **Pelican boot smoke (PR #480)**: `docker/scripts/test_pelican_smoke.sh` drives `pelican_panel.sh` against a fake provider binary. Empty credentials fail fast, jwt mode routes to `auth-provide` with the code, stable mode routes through the auth loop to `provide`. Runs in `test-and-lint`.
- **Pelican behavioral gate tests (PR #480)**: `docker/scripts/test_pelican_gates.sh` adds 15 mutation-checked tests covering the PELICAN update gate in `start_nightly.sh`, the `urnet-tools update` refusal, the `$HOME`-based state-dir resolution in `proxy-health.sh` / `proxy-traffic.sh`, and the egg-JSON invariants (non-empty rules arrays, known validator tokens, unique env_variable and sort values). Wired into CI as its own step.

### Changed
- **Pelican-gated runtime update policy (PR #480)**: under `PELICAN=yes`, `start_nightly.sh` short-circuits the update check (bootstrap and daily watcher) and `urnet-tools update` exits 1 with a refusal message. The published image is the single source of truth under a panel; a runtime fetch would silently swap the audited fork binary for whatever the release API serves mid-flight. Non-Pelican Docker deployments keep the v3.23.0-fix.30.6 behavior unchanged.

### Fixed
- **Docker state-dir split-brain (PR #480)**: all `docker/scripts/*.sh` now resolve provider state at `$HOME/.urnetwork` instead of the hardcoded `/root/.urnetwork`. Update-pending markers already used `$HOME`; the auth path was the outlier. Any container run with `HOME != /root` (e.g. Pelican eggs) would have silently split auth and marker state across two directories. Default-Docker behavior is unchanged (the image still ships with HOME=/root); the fix is a no-op for `docker run` and a correctness fix for panel/egg deployments.
- **Pelican egg vnStat note (PR #480)**: reworded the vnStat description in `pelican/README.md` and the egg JSON. The off-by-default posture in the egg is an operator-preference default, not a current-exposure claim; vnStat was patched long ago.

## [v3.23.0-fix.30.7]

### Added
- **Paid and file-list proxy grading (PR #458)**: the provider now table-probes every tracked paid and file-list proxy and assigns an A-F reachability grade. Before this release those proxies were never graded. Trimming and the proxy summary could not see their health. A per-sweep summary line reports how many proxies were graded and how many are pending. Proxies are only demoted on positive evidence, never on silence.
- **Adaptive sample sampling (PR #458)**: a probe starts small (min_sample_width) and grows toward max_sample_width only while a proxy's score stays borderline (within pass_bar plus or minus border_line_band). Clearly-good and clearly-dead proxies settle at the small width and spend almost no probe bandwidth.
- **Honest pending status (PR #458)**: a proxy the probe reaches but cannot decide (too few of the sampled hosts resolvable from the box) is marked pending instead of being assigned a fabricated grade.
- **Per-tick probe budget (PR #458)**: one 5-minute scoring sweep probes at most max_paid_probes_per_tick proxies (default 200). Oldest-stale probes are scored first.
- **Stage-0 liveness gate (PR #458)**: paid proxies get a one-dial SOCKS5 and API reachability check before a sample block.
- **Restored emoji and sectioned help (PR #459)**: `urnet-tools` and `urnet-docker` restore the approved emoji and sectioned per-command help.

### Changed
- **Probe configuration JSON (PR #458)**: proxy probe settings now read from `~/.urnetwork/proxy_probe.json` with keys `enabled`, `sample_width`, `min_sample_width`, `max_sample_width`, `timeout_ms`, `pass_bar`, `preferred_bar`, `border_line_band`, `max_paid_probes_per_tick`, and `stage0_liveness`.
- **Restart targeting for user-owned units (PR #459)**: a fix corrects restart targeting when a provider unit is owned by another user. The restart runs against the right unit instead of the current user's scope.
- **Suite test hardening (PR #460)**: the provider test suite replaces a fixed-wait flake with an adaptive wait and makes a paid-grader test hermetic. No production behavior change.

---

## [v3.23.0-fix.30.6]

### Added
- **Real per-command help (PR #448, #453)**: `urnet-tools` and `urnet-docker` now route through the Cobra CLI framework. Every command prints real per-command help with `-h` or `--help`. Each help page has a usage line, a short description, and copy-paste examples. The `urnet-docker proxy` command is a parent with 12 subcommand help pages. `urnet-docker exec -h` renders the in-container command's own help. `--help` never executes an action.
- **In-place container update (PR #453, #455)**: `urnet-docker update <target>` updates a running provider container in place. A target flag (`--unit`, `--user`, `--network`, or `--state-dir`) or a bare container name selects the container, then runs the in-container `urnet-tools update` self-update without recreating the container, behind the confirm gate. In-place update now works on any container image. A host-side repair fixes an in-container update script that older images ship broken (a busybox `mktemp` bug and a `pkill` name-truncation issue), then swaps the provider binary. If the swap stops the container (older images stop instead of relaunching), the command auto-restarts the same container so the provider comes back on the new binary. Plain `urnet-docker update` still self-updates the host binary only. The `self-update` and `selfupdate` aliases always update the host binary only.
- **Post-quantum session visibility (PR #455)**: the provider detects whether an end-to-end session uses post-quantum or classical TLS from the negotiated curve in the TLS connection state. The post-quantum hybrid curves `X25519MLKEM768`, `SecP256r1MLKEM768`, `SecP384r1MLKEM1024`, and `MLKEM1024` tag session-up and session-close log lines. A periodic `[pqe]` provider log line reports live post-quantum and classical session counts plus opens over 1h, 24h, 7d, and lifetime.

### Changed
- **Go 1.27.0 toolchain bump (PR #449)**: Compiler bumped from Go 1.26.4 to 1.27.0. Updates the `go` directive in `go.mod` to `1.27.0`, floats the CI `go-version` pins to 1.27 across all workflows, and moves the Docker builder base to `golang:1.27-alpine`. Validated under 1.27.0 with a clean `go build`, `go vet`, the full `-short -race` test suite, the complete cross-compile matrix (including 386 and mips/mipsle/mips64/mips64le), and a functional smoke. `go mod tidy` required no dependency changes.
- **CI skips heavy jobs on docs-only PRs (PR #452)**: the build, dash-compat, tool-functional-smoke, unix-lifecycle, and windows-lifecycle workflows skip their heavy jobs on pull requests that touch only `**/*.md`, `docs/**`, or `releases/**`. The labeler always runs. VirusTotal upload, lookup, and analysis timeouts are now non-fatal. A timed-out artifact records UNKNOWN in the summary instead of failing the scan job. Only a genuine malicious hit over the threshold fails the job.

---

## [v3.23.0-fix.30.4] — 2026-08-22

### Added
- **Restored urnet-tools command suite (PRs #438, #439, #440, #441)**:
  - **`auth <code> [-f]`**: restored provider authentication delegation to provider binary `auth-provide`/`auth`. Bypasses global flag parsing so `-f` forces JWT overwrite; drops privileges to run as the target user when invoked by root. (PR #439)
  - **`choose-network <api> <connect>` (or `choose_network` / `--reset`)**: restored custom backend network endpoint switching. Persists to `~/.urnetwork/network.json`. (PR #439)
  - **`fast-auth [on|off|status]`**: restored management of the `~/.urnetwork/fast_auth` rate-limiter bypass marker with input validation and confirm gating. (PR #439)
  - **`set [help | <key> <val> | <key> off | <key>]`**: restored runtime tuning overrides in the provider state dir for the 7 keys read live (`node-name`, `report-interval`, `proxy-url-max`, `proxy-url-refresh`, `cleanup-scope`, `cleanup-interval`, `fast-auth`). Confirm-gated state writes with value validation. (PR #439)
  - **`session save <file>` & `session load <file>`**: restored encrypted provider identity export and import (AES-256-CBC bundle with PBKDF2/SHA-256). Features non-interactive password refusal/prompting, no-clobber protection, automated timestamped pre-load state backup (`~/.urnetwork.bak.<timestamp>`), same-account verification with `--allow-different-account` override, and state file chowning to the target owner. (PR #440)
  - **Hub command family (`hub init`, `link`, `unlink`, `test`, `onboard-cmd`, `show-password`, `open-port`, `update`)**: fully restored in pure Go. `hub init` sets up the hub service with password/salt validation; `hub link <url>` pairs providers with interactive CA verification and SHA-256 fingerprint pinning (TOFU with InsecureSkipVerify on initial fetch, verified against pinned CA/fingerprint) and confirm-gating on identity change; `hub test` verifies TLS chains against `hub_ca.pem` or pinned fingerprints; `hub onboard-cmd` generates URL-escaped one-liners; `hub open-port` manages firewall rules with confirm gates; `hub unlink` and `hub update` manage pairing and lifecycle. (PR #441)
  - **`self-heal [on|off|status]`**: restored CLI management for the `~/.urnetwork/proxy_self_heal` pressure-monitor marker. (PR #438)
  - **Linux `systemctl status` view**: Linux `status` command now renders live `systemctl status` (user or system unit) for systemd-managed providers, with graceful table fallback for bare processes. (PR #438)
- **Persistent proxy trim (`urnet-tools proxy trim <N>`) (PR #442)**: provider-side hard cap holding running proxies to `<N>`. Sorts all proxies by the A-F reachability grade (`score/graded/failed/tier/last_graded` across both file and URL cache sources) and sheds worst-graded proxies first (dead -> never-graded -> F -> D -> C -> B -> A), using billable traffic as a secondary tiebreaker to preserve earning proxies. Persists cap to `~/.urnetwork/proxy_trim` across restarts and reloads; clamps AIMD controller `TargetPoolSize` to prevent controller fighting; blocks over-budget additions during fetch/reload while preserving history. Supports `--preview` / `-n` / `--dry-run` and reset (`proxy trim off` or raising limit). (PR #442)
- **Persisted default provider target (`urnet-tools default`) (PR #436)**: added `default set <target>`, `default show`, and `default clear` commands to persist a preferred provider selector in `os.UserConfigDir()/urnet-tools/default`. Automatically resolves multi-provider invocations without repetitive targeting flags while logging a visible notice to stderr. Explicit flags (`--unit`, `--network`, etc.) and `--all` always take precedence. (PR #436)
- **Loopback-only diagnostics and profiling (`URNETWORK_PPROF`) (PR #423)**: opt-in diagnostics listener on loopback IP (e.g. `127.0.0.1:6060`) serving Go standard pprof profiles (`/debug/pprof/*`). Non-loopback IPs and hostnames are rejected. (PR #423)
- **Message pool metrics & categorized error tracking (PR #425, #426)**: loopback endpoints `/metrics/pool` and `/metrics/errors` exposed when `URNETWORK_PPROF` is active. Pool metrics track hits, misses, returns, active buffers, capacity, GC cycles, and per-size distribution. Error tracking captures rate-limited recent errors with truncated stack traces across transport, IP, proxy, and WebRTC layers. (PR #425, #426)
- **Adding Proxies documentation (PR #430)**: comprehensive per-OS guide covering proxy loading, formatting (`host:port` vs `host:port:user:pass`), Windows PowerShell traps (hidden `.txt.txt` extensions, quoting), and Docker host-side (`urnet-docker proxy add`) vs in-container (`--proxy_file`) usage. (PR #430)
- **urnet-docker command suite expansion & feature parity (PR #445)**: expanded host-side `urnet-docker` to full parity with `urnet-tools`. Adds first-class container lifecycle (`start`, `stop`, `restart`, `logs` with RAMLOGS `/dev/shm` streaming), authentication (`auth`, `fast-auth`, `choose-network`), runtime configuration (`set`, `report`, `self-heal`), observability (`summary`), session export/import (`session save/load`), hub management (`hub`), and proxy controls (`proxy add`, `clear`, `remove`, `refresh`, `add-source`, `remove-source`, `remove-dead`, `health`, `traffic`, `trim <N>`, `exclude`). Expands discovery to recognize upcoming `meso-miner` container naming. (PR #445)
- **Emoji and sectioned help menu styling (PR #437)**: restructured help output into clean, emoji-decorated sections: Core Commands, Session & defaults, Performance & Tuning, Proxy Management, Hub Management, Maintenance, Targeting rules, and Force options. (PR #437)
- **Windows and macOS status panel styling (PR #409)**: styled status UI rendering on Windows and macOS matching native terminal aesthetics. (PR #409)

### Changed
- **Adaptive GC governor consolidation (PR #428, #429)**: retired the separate eco runtime monitor loop and consolidated all dynamic GC tuning into a single `gcGovernor` in `resource_pressure.go` active across all profiles. The governor is the single writer to Go runtime `GOGC`, tightening GOGC below profile baseline based on the tighter of process heap fraction and host available RAM. Includes a 10s live-heap subtick to catch spikes fast. Controlled by `URNETWORK_ADAPTIVE_GC` (on by default); backs off if `GOGC` is explicitly set by operator. Pressure status reports `gc_state` and `heap_frac`. Eco profile retains static startup baseline (GOGC 50, 75% RAM GOMEMLIMIT). (PR #428, #429)
- **Universal finite memory limit (PR #427)**: guarantees a finite memory limit across every provider execution path and profile (tier 1/2 `--max-memory`, cgroup limits, host available RAM boundaries), eliminating unbounded memory paths on constrained hosts. (PR #427)
- **Flag targeting syntax (`--flag=value`) (PR #433)**: `urnet-tools` and `urnet-docker` now accept both `--flag=value` and `--flag value` across all targeting selectors (`--unit`, `--user`, `--network`, `--network-id`, `--state-dir`, `--match`). (PR #433)
- **Docker installer routing and PATH handling (PR #432)**: served standalone installer at `dl.fullbars.xyz/urnet-docker.sh` with GitHub raw fallback; automatically appends non-root install directory to `~/.bashrc` PATH; warns on unrecognized flags or extra positional arguments. (PR #432)
- **Docker build layer caching (PR #424)**: optimized Dockerfile builder stage by isolating `go.mod`/`go.sum` download into a dedicated cacheable layer and slimming the build context. (PR #424)

### Fixed
- **Proxy trim earning protection and F-tier eviction (PR #447)**: restructured proxy trim eviction hierarchy to protect active billable client traffic (`traffic > 0`) against idle proxies (`traffic == 0`). Confirmed failing F-tier nodes are evicted before untested/probationary ungraded proxies, and smaller earners shed before larger earners to preserve maximum operator revenue. (PR #447)
- **Same-user privilege rejection in journalctl, systemctl, and optimize (PR #446)**: resolved `Using the --machine= switch requires root privileges / Operation not permitted` on Linux non-root operations (`logs`, `restart`, `status`, `update`, `uninstall`). Centralized user-session dispatch in `systemctlUserArgs()` and `journalctlArgs()` to omit `-M` when targeting units owned by the current user. Decoupled `optimize` from provider discovery so host kernel sysctls can be applied on unconfigured nodes; auto-escalates via sudo when available. (PR #446)
- **Windows CI test pipe buffer deadlocks (PR #446)**: fixed 10-minute timeout hangs on Windows runners by draining `os.Pipe()` buffers concurrently across all test capture helpers. (PR #446)
- **Proxy remove data loss fix (PR #434)**: fixed a critical bug where `proxy remove` without `--all` previously forwarded an unconditional wipe; now correctly passes target addresses and `--match` patterns. `proxy add-source`, `remove-source`, and `remove-dead` now properly honor `--dry-run`. (PR #434)
- **urnet-tools reinstall recursion (PR #434)**: fixed `urnet-tools reinstall` self-recursion by delegating cleanly to `updateProvider`/latest. (PR #434)
- **Provider binary discovery on Windows and macOS (PR #435)**: fixed case-sensitive binary matching on Windows and enabled multi-path probing for `provider_beta` and `urnetwork` binaries across macOS and Windows; standardized `isPrivileged` execution seam. (PR #435)
- **Deleted binary handling on discovery and update (PR #433)**: stripped `(deleted)` suffix from `/proc/exe` discovery to prevent phantom binary paths; skipped backup staging when the running provider binary was already unlinked. (PR #433)
- **Windows installer architecture selection and uninstall (PR #408)**: corrected single per-arch asset selection on Windows; handled flat tarball extraction layout; fixed uninstaller path filter to match exact installed binary. (PR #408)
- **Connect NAT shard buffer test race (PR #443)**: fixed test-side data race in `ip_smtp_seam_test.go` by retaining pooled seam packets until asynchronous NAT shard processing completes. (PR #443)
- **Upstream monitor compact diff representation (PR #410, #431)**: replaced 60KB raw diff truncation with structured GitHub API deltas and introduced a fork-aware manifest to eliminate false-positive porting alerts. (PR #410, #431)
- **CI review workflow stability (PRs #411, #413-#417, #419, #420)**: repaired PR head SHA resolution, OAuth action triggers, and YAML indentation across review workflows before pruning non-functional triggers. (PRs #411, #413-#417, #419, #420)

### Security
- **CFAA IP security blocklist sync (PR #412)**: synchronized provider egress firewall tables with upstream feed (commit 6813e788), expanding IPv4 prefix coverage from 43,299 to 44,225 and IPv6 prefix coverage from 431 to 513. (PR #412)
- **Cross-user state and privilege isolation (PRs #439, #440, #441)**: delegated provider commands drop privileges to match target user; session save/load and hub link enforce strict file permissions (0700 state dir, 0644 trust files) and chown state assets to the unit owner; single-stdin password reader avoids TTY leakage. (PRs #439, #440, #441)

## [v3.23.0-fix.30.3] — 2026-08-19
### Added
- **Busybox-safe mktemp for in-container updates**: `urnet-tools update` now uses `mktemp -d /tmp/urnetwork-update-XXXXXX` (directory template, busybox-compatible) instead of the broken `.tar.gz` suffix template that failed with `Invalid argument` on Alpine images. One `rm -rf` cleans up. Test harness hardened with a busybox-enforcing `mktemp` stub that rejects any template not ending in `XXXXXX`. New coverage: zero-byte tarballs, partial downloads overwritten by mirror, update-pending marker lifecycle, architecture mapping, busybox stub regression guard. (PR #406)
- **Nightly self-update path hardened**: `start_nightly.sh` `func_check_update` stages the update in a busybox-safe temp dir, uses `curl -sfL` (fails on HTTP errors), cleans up on every failure path, and writes `update-pending` only after the binary swap + version write succeed. Previously it reused a shared `/tmp/urn_update`, downloaded with `curl -sL`, and set the marker before download/extraction completed; a failed update left the restart loop believing a new binary was installed. The metadata fetch is also guarded so a transient API or network failure cannot trip the script's `set -e` and kill the provider supervision loop; the check skips that attempt. The new binary is staged on the target filesystem and atomically renamed into place, and the version-file and restart-marker writes are checked so a partial install cannot falsely signal a restart.
- **Full semver docker tag on release pushes**: CI workflow preserves the full semver tag (e.g., `v3.23.0-fix.30.3`) instead of truncating to the minor segment. Operators can pull the exact release they deployed. (PR #405)

### Changed
- **Default-provider selection completed**: `urnet-tools` now detects the current OS user on Linux, Darwin, and Windows (`currentUserName` cross-platform), discovers the provider running under that user, and auto-selects it for read-only commands (`logs`, `status`, `summary`). Gated by a real privilege check: root callers never auto-default; unprivileged callers only auto-default when exactly one reachable provider belongs to them. Surfaces selection reason (e.g., `auto-selected: sole provider for user alice (pid 1234)`). Falls back to explicit `sudo <binary>` hint when an unprivileged user has `sudo` available but the target provider is owned by another account. Test seams cover privilege guard, fallback message, and cross-platform username resolution. (PR #404)

## [v3.23.0-fix.30.2] — 2026-08-18
### Added
- **Post-quantum encryption now works end to end**: the provider enables its encrypted session layer on the serving client (PR #400), and the control channel routes encrypted handshakes by generation id (PR #401). An app with post-quantum encryption turned on completes the handshake instead of stalling at the 60-second timeout. The provider stays Opportunistic, so plaintext apps are unaffected.

- **urnet-tools usage text**: the help output now lists the full command surface (performance, proxy, hub, and maintenance commands) instead of only the core commands. (PR #402)

## [v3.23.0-fix.30.1] — 2026-08-17
### Added
- **SMTP policy layer**: port 25 stays local-only; ports 465 and 587 require TLS. Owner-scoped eviction, one RST per rejected flow, and hostname-shaped EHLO/HELO arguments (PR #392).
- **CFAA IP blocklist sync**: tables updated from upstream's feed sources (PR #395).
- **Go tools fixes**: stopped-provider discovery restored, auto-update timers create/remove correctly, bare container-name targets accepted, cross-user provider discovery, and false-positive dashboard units excluded (PRs #393, #396, #399).
### Fixed
- **Smoke-test harness**: proxy refresh waits for started_at; gh API 5xx retries in the labeler (PRs #397, #398).
### Changed
- **Documentation restructure**: configuration tables, guide navigation, troubleshooting matrix (PR #394).

## [v3.23.0-fix.30] — 2026-08-16
### Added
- **urnet-docker host-side proxy commands**: `proxy add`, `clear`, `remove`, `add-source`, `remove-source`, `refresh`, and `remove-dead` now run from the host. The exec plumbing is hidden. Example: `urnet-docker proxy add ~/proxies.txt`.
- **macOS lifecycle support**: auto-start and auto-update use launchd agents. A headless fallback covers servers without a login session. Discovery uses pgrep because macOS has no /proc.
- **Runner-based functional testing**: smoke tests run real auth and real providers on free Linux runners, replacing the test droplet for tool testing.
- **Soak tests**: native and containerized providers each run a three-hour soak with a capped proxy pool, restart cycles, and client-ID reuse checks.
### Fixed
- **Windows uninstall could never delete anything**: the safety guard rejected all Windows paths. It now accepts absolute drive paths and rejects volume roots.
- **Windows auto-start/auto-update now use Task Scheduler** instead of the legacy startup-folder shortcut. Uninstall removes the scheduled tasks.
- **Windows state directory corrected**: now matches where the provider actually stores data.
- **Proxy clear worked only in the shell wrapper**: the tool now maps clear to remove-all.
- **Container wrapper supported only some proxy commands**: add-source and remove-source are now accepted.
- **Linux uninstall orphaned the auto-update timer**: the timer is now disabled on uninstall.
- **Build cache flake stalled image builds**: replaced with setup-go native cache and a retry.
### Changed
- **Docker image tag model is explicit**: `latest` means the last tagged release. `main` tracks the current code. CI tests pull `main`.
- **CI no longer false-passes**: tests verify provider authentication and fail loudly instead of swallowing errors.
## [v3.23.0-fix.29.1] — 2026-08-14

### Added

**RFC 7323 TCP timestamps, MSS negotiation, and initial-window warmup** (#380): the provider's TCP stack ports upstream perfvar's option handling. A unified option parser now reads MSS, window scale, and timestamp together, replacing the old window-scale-only parser. Timestamps negotiate from the peer's SYN and emit on every non-RST segment (SYN-ACK, data, pure ACK, FIN-ACK), backed by a monotonic clock and a reorder guard. DataPackets now clamp payload to the peer's advertised MSS (RFC 879/6691 data-only semantics, 536 floor). A new InitialWindowSize setting scales with the memory budget, is power-of-two, and clamps between 4 KiB and 128 KiB; the SYN advertises the literal unscaled window per RFC 7323, then the post-handshake ACK jumps to the warmup window. The fork keeps its existing 4 KiB/4 MiB low-RAM window profile rather than following upstream's move to 64 KiB/16 MiB. ReadBufferByteCount now accounts for the timestamp option so full IPv6 reads no longer split into a runt tail segment. Multiple independent reviews covered the port; new tests span the window-scale table (9 cases), MSS+timestamp extraction, malformed-tail parsing, SynAck layouts, timestamp bytes, PureAck TSopt, and DataPackets timestamp segmentation. Needs a fleet deploy. This is the only provider binary change in this release.

**VirusTotal scan of release binaries** (#381): release binaries are scanned before publishing with a two-tier gate. Zero to two detections pass (the known stripped-Go false-positive band), three to ten ship but are flagged for review, eleven or more block the release. The scan writes a report receipt with a per-artifact PASS/REVIEW/FAIL table and VirusTotal permalinks.

**ClamAV scan of release binaries** (#383): an EICAR self-test plus a clamscan pass over all 20 release binaries, run after the VirusTotal scan as a quota-free second opinion.

**Pre-release shakedown workflow** (#384-391): the gauntlet workflow is rebuilt and renamed to shakedown. It runs on a disposable 1 CPU/1 GB DigitalOcean droplet with guaranteed cleanup (trap plus job timeout), and covers a fresh install, auth, the Go tool, the full proxy lifecycle, URL sources against real free proxies, the admission pipeline, docker, the hub under systemd and docker, hot-restart identity, update --tag, self-update, a long observation phase with resource sampling, and clean shutdown. Tier-1 failures exit non-zero and fail the job; tag-triggered runs test the exact release being cut. The range along the way adds an apt-get update before the docker install, skips URL checks when auth failed, adds a report review step, posts the run verdict to a Discord webhook, adds a preflight connectivity gate, requires a public IPv4 before SSH, fixes the auth-check window to read the full journal, annotates Wacatac.C!ml as a confirmed false positive, and adds hub coverage for both systemd and docker. PR #391 bulletproofs the runner-to-droplet lifecycle: the test runs detached via systemd-run, the exit code survives through an atomic-rename sentinel, a deterministic gate maps 0/1/75/124 to PASS/RELEASE_BLOCKED/ENV_BLOCKER/HARNESS_CRASH, a separate 15-minute sweeper workflow destroys orphaned droplets older than 3 hours and reaps stale SSH keys, polling is bounded, the report streams each poll, a heartbeat detects a stalled script, and a concurrency group queues runs instead of cancelling. The lifecycle went through six rounds of independent review. A full run takes about 2 hours, bounded by a watchdog and job timeout.

### Fixed

**Provider panicked when HOME was unset**: the pre-release shakedown found a startup panic, "$HOME is not defined," on every invocation when the HOME environment variable was missing. This broke `--version`, one-shot commands, and the `update --tag` path in bare environments, such as a root shell with no HOME set. The client-JWT store now degrades to in-memory-only mode when HOME is unavailable; identity reuse is lost for that process, but the command still runs. The auth and proxy-config write paths now return errors instead of panicking. This fix blocked the first v3.23.0-fix.29.1 release attempt; the release was re-cut once the fix landed.

**DoH tests hung on live server lookups** (#382): TestDohQuery and TestDohCache queried live public DoH servers for a hostname that does not resolve, so the retry loop hung. A local httptest server now serves canned Google-style JSON DoH responses, and the tests fail fast instead of retrying forever.

## [v3.23.0-fix.29.0] — 2026-08-14

### Added

**Hub A-F grade surfacing** (#360): the provider's report payload now carries each proxy's grade (`score/graded/failed/tier/last_graded`, omitempty), the hub ingests it into a new `proxy_grades` store (latest-wins per node+proxy per hour, 7-day retention), and the dashboard renders a color-coded A-F **Grade** column — distinct from the existing traffic-composite **Score** column. URL-store grade timestamps are honest: a real `LastGraded` field stamped only on genuine stage-1 grades, never on liveness-only probes. Grade tiers are sanitized to exactly A-F at ingest plus HTML-escaping at both render sites (stored-XSS hardening). Needs a fleet deploy — provider binary + hub image both change.

**urnet-tools/urnet-docker tool distribution + self-update** (#362): The Go tool rewrite from #345 is now actually shippable. `release.yml` builds `cmd/urnet-tools` and `cmd/urnet-docker` for the full matrix and attaches them as release assets (`urnet-tools-<os>-<arch>` / `urnet-docker-<os>-<arch>`, bare — no `.exe` even on Windows); `Provider_Install_Linux.sh` and `Provider_Install_Mac.sh` install the Go binary (sha256-verified against the release API digest) instead of self-copying the shell script, with a legacy fallback for releases that predate the asset; docker-only users get a standalone host-side installer (`scripts/install-urnet-docker.sh`, `curl ... | sh`, digest-verified). `urnet-tools update` now also refreshes the tool binary itself; new `self-update`/`selfupdate` subcommands update only the tool (works on boxes with zero providers); `urnet-docker update` self-updates the tool. The structural check on downloaded binaries is now platform-aware (ELF/Mach-O/PE) so self-update works on macOS and Windows, not just Linux. Needs a fleet deploy for the installer change; the new assets ship with the next tagged release.

**Paid/free probe divergence + earn-skip** (#357): stage-1 table probing splits into two cadences. Paid/file proxies move to a wider stale window (6h calm / 3h hot vs the URL 3h/1h) and are skipped entirely when they show a positive billable delta within the last 15 minutes — earn-skip — with a hard 24h force-probe ceiling and never-graded proxies always probed, so fail-fast can never be starved. The first URL fetch + probe pass is deferred 20s after process start, so a crash-looping box can never re-fetch/re-probe on every restart. The grade summary's stale ratio picks its freshness window per entry source. Needs a fleet deploy — probe cadence and probe spend change on every box at redeploy time.

### Fixed

**Earn-skip dead in production** (#357): the earn tracker stored per-address earning state under `ProxyHealthSnapshot`'s formatted `proxy[N] (addr)` keys while the paid grader looked up raw addresses — keys never matched, so earn-skip never fired and every paid proxy was always probed. Keys are now normalized on ingest and lookup, the tests drive the tracker through the real snapshot API, and per-address state is pruned to the live proxy set (and cleared when the health set empties).

**Resource-leak hunt — six findings + one audit follow-up fixed** (#367, #369, #370, #371, #372, #373, #374): a full pass over the multi-client window and encryption layer closed every confirmed leak. Source-count refcount now increments once per unique (bucket, path), pruning phantom (destination, source) map entries that grew monotonically at thousands-of-proxies scale (#367). The handshake timeout watchdog cancels the epoch ctx when the timeout records a failure, freeing the parked `runHandshake` worker against silent peers (#369). `removeClient` cancels each affected update's ctx so teardown goroutines wake immediately instead of stranding on the 120s idle timer — with a follow-up supersede guard so a successor update registered after cancellation is never clobbered by the stale teardown delete (#370, #373). The PQE path derives a sane encryption IdleTimeout (max of send/receive buffer idle) instead of adopting the 0 default, arming the reap loop and eliminating zombie refs==0 sessions (#371). `SendEncryptedControl` returns its pooled buffer on both cancel-path exits instead of leaking one per teardown race (#372). `Cancel()` now unsubscribes the receive callback like `Close()`, releasing dead clients' callback chains immediately (#374). Every fix is mutation-proven and independently reviewed (Sonnet + Opus passes). Needs a fleet deploy — provider binary behavior changes.

**CI flake root causes closed** (#364, #368): the window-stats `-short` timeout is now derived from the bucket duration (was a fixed 300ms coin flip under `-race` load), and the paid-grade undecidable test pins the pass counter to a hostname-only probe block (literal-IP targets in the host table bypass the DNS-fail cache by design, so ~4.7% of passes always dialed one). CI is deterministic; test-only, no fleet deploy.

**Go-tool gauntlet — 8 bugs found + fixed by live field testing** (#376): the rewritten Go `urnet-tools`/`urnet-docker` was gauntlet-tested on a fresh droplet against the live beta network. Found and fixed: no version command (added `version`/`--version`/`-v`); `report`/`hot-restart` delegated to non-existent provider subcommands (now write `~/.urnetwork/report_url` / restart the unit, with a confirm gate + `--force` on hot-restart); systemctl argv omitted the unit name (start/stop/restart/hot-restart failed with "Too few arguments"); `summary` delegated to the wrong flat form (now the provider's nested `proxy summary`); `proxy add-source`/`remove-source` missing entirely (URL sources now manageable); `proxy refresh --force` didn't forward `--force` (warmup gate never bypassed); proxy `--help` at any position showed root help or hung (now proxy-specific help, never executes). The report_url override is written 0644 so a provider running as a different user can read it. Needs a fleet deploy — the tools ship as release assets with this release.

## [v3.23.0-fix.28.0] — 2026-08-12

### Added

**In-process per-proxy client-JWT renewal** (#356): the beta backend (and mainnet's new token format) mints 24-hour JWTs; previously each proxy's client JWT was minted once at startup and never renewed in-process, so after ~24h of uptime every proxy's token expired, all auths 401'd, and providers silently became black holes. The provider now renews each proxy's client JWT 12h before expiry (hourly retry, immediate on 401), re-signing the SAME client_id through `/network/auth-client` so server-side reputation is preserved; a process-wide mutex + shared rate limiter prevent API stampedes; a no-op on backends still issuing long-lived tokens.

**EncryptionMode tri-state port (scope 2)** (#350): Off/Opportunistic/Required mode with fail-closed gates; bounded per-peer TLS establishment (60s handshake timeout default, was unbounded) (#353).

**urnet-docker exec flag forwarding** (#349, #352): a `--` separator forwards inner flags verbatim; unknown leading flags error instead of being silently dropped; the ramlogs hint resolves the real container name; `urnet-docker logs` follows the right provider.

### Fixed

**window-stats test flake under -short** (#354): deterministic bucket span.

## [v3.23.0-fix.27.0] — 2026-08-09

### Added

**A-F proxy quality tiers + best-overall cache eviction** (#343): every URL-source proxy that survives the stage-1 gate is assigned a letter grade (A >= 0.9, B >= 0.8, C >= 0.7, D >= 0.6, F < 0.6); all sources' candidates pool into a single best-first admission funnel up to the cap; the cache evicts lowest-grade entries when full; the fetch cycle probes only new addresses while the reaper's stale sweep refreshes cached grades.

**Read-only grading for paid/file-list proxies** (#344): a background sweep grades every non-URL proxy the box serves with the same stage-1 table probe on the same 1-3h stale cadence, persisting Score/Graded/Failed/LastGraded into `proxy.state`. Read-only by construction — grades never gate admission, evict, or feed give-up/cleanup.

**Periodic A-F grade summary + grades.log history** (#346): every 5 minutes (configurable via `proxy_grades.json interval_sec`) the provider logs a running tier snapshot — per-source A-F breakdown, changes vs the previous round, median/p95/min score stats, next-probe countdown — and per-proxy tier changes emit delta lines into the important buffer and a durable per-day history at `~/.urnetwork/grades/` (retention default 7 days).

**Provider-aware `urnet-tools` rewritten in Go** (#345): the fleet ops tool is now a single Go codebase (`urnet-tools` for process/systemd, `urnet-docker` for containers) that discovers real running providers, identifies each by its JWT network identity, refuses ambiguous targets with an inventory table, and gates destructive ops behind a confirm prompt. All 25 legacy subcommands dispatch with verified parity.

### Breaking Changes

- **`urnet-tools` is now a Go binary** (#345): drop-in at the same path; operators on multi-provider boxes must now specify a target (`--unit` / `--user` / `--network` / `--network-id` / `--state-dir`) — the tool refuses to guess.


### Added

**A-F letter-grade proxy quality tiers** (#343): Every URL-source proxy that survives the stage-1 gate is assigned a letter grade (A >= 0.9, B >= 0.8, C >= 0.7, D >= 0.6, F < 0.6). Best-overall cache eviction keeps the highest-tier proxies when the cache is full; all sources' candidates pool into a single A-to-F admission funnel (best-first up to the cap); the fetch cycle probes only new addresses while the reaper's stale sweep refreshes cached grades; a per-cycle A-F grade breakdown is logged. Cross-source duplicates are probed once per cycle; the eviction tie-break uses the grade score. Seven review passes (two CodeRabbit rounds plus independent passes) closed the fetch-logging phantom lines, cross-source duplicate probing, reaper-refresh herd, and eviction-tie-break gaps. Needs a fleet deploy.

**Read-only grading for paid/file-list proxies** (#344): Proxies from `--proxy_file` / the internal config bypass the URL admission gate by construction and were previously invisible to the quality system. A background sweep now grades every non-URL proxy the box serves with the same stage-1 table probe on the same 1-3h stale cadence, persisting Score/Graded/Failed/LastGraded into `proxy.state` (omitempty — same field shape as the URL store). Read-only by construction: only the grade fields are written; admission, eviction, give-up, and cleanup never read them, so a graded F keeps serving exactly as it did before. `proxy_probe.json enabled=false` skips the sweep entirely. Needs a fleet deploy.

### Changed

**Provider-aware `urnet-tools` rewritten in Go** (#345): The fleet ops tool is now a single Go codebase (`urnet-tools` for process/systemd, `urnet-docker` for containers) instead of the ~4000-line POSIX shell script plus a separate PowerShell variant. Discovers actual running providers (process scan + systemd units), identifies each by its JWT network identity, refuses ambiguous targets with an inventory table, and gates destructive ops behind a confirm prompt (`-f` for machines, never for provider selection). All 25 legacy subcommands dispatch with verified parity. Update is interactive-first with mandatory SHA-256 digest verification; the staging dir is a private per-update `MkdirTemp`; the hub install sanity-checks ELF magic instead of executing the downloaded binary; `optimize` is platform-aware (Linux ephemeral-port pool + TIME_WAIT sysctls, Windows netsh/reg equivalents). **Breaking for multi-provider boxes**: the tool now refuses to guess — scripts must specify `--unit`/`--user`/`--network`/`--network-id`/`--state-dir` when the box is ambiguous. Needs a fleet deploy.

## [v3.23.0-fix.26.8] — 2026-08-09

### Added

**Stage-1 table-probe quality gate for URL-source proxy admission** (#342): A URL-source proxy only had to survive stage-0 (a SOCKS5 greeting plus an API `CONNECT`) to be admitted, which proves the proxy is alive but not that it can actually reach the destinations providers need to reach. Every surviving proxy is now dialed at `:443` against a sampled block of the backend's ~127 health hosts (default 12 per pass, 4s per-target timeout), and only proxies scoring `>= pass_bar` (default 0.6, with a 0.9 "preferred" tier) go on to the auth queue. Design follows upstream `ip_remote_multi_client_probe.go`: positive evidence only (a SynAck proves the proxy's own upstream dial; silence never convicts), resolution outside the probed channel (the box's own DNS, so a proxy with broken DNS never fails a TCP probe), deterministic disjoint-block rotation, and a viability abort so an aborted pass is always a decided verdict. A grade persists only for DECIDABLE passes (quorum of the sample answered, context not cancelled) — an empty, cancelled, or resolver-gutted pass leaves the prior grade untouched, so the box's own DNS can never convict a proxy. RFC 1929 auth (`host:port:user:pass`) now carries through both probe stages so credentialed, usually paid, URL entries are graded on the same footing as free ones. Proxies graded below the bar never spawn, and the auth gate reports them distinctly from proxies that are simply dead. A kill switch (`~/.urnetwork/proxy_probe.json` with `{"enabled": false}`) restores pre-feature behavior end-to-end; `sample_width`/`timeout_ms`/`pass_bar`/`preferred_bar`/`enabled` are all runtime-tunable. Score/Graded/Failed now persist in `proxy_url.json` cache entries, matching the backend's own data model. Two rounds of CodeRabbit review plus an independent Opus 5 pass closed a kill-switch accounting bug, an inert regression test (now mutation-verified), a cache-growth bound, and credentialed-proxy admission handling. Needs a fleet deploy — this changes provider binary behavior and introduces a new operator-facing config file.

### Fixed

**TTL fix for the inert reorder dialer** (#340): The reorder technique alternates a connection's socket TTL between 0 and its native value to force retransmits and out-of-order arrival at the TLS ClientHello. `IP_TTL` only accepts 1-255; Linux rejects 0 with `EINVAL`, and `SetSocketTtl` discarded that error, so on Linux the TTL never moved and the reorder/fragment+reorder dialers delivered plain fragmentation while reporting themselves as reorder dialers. Every fleet node runs Linux, so the technique has been inert in production for the fork's entire life. The low TTL is now set to 1 (in range, still dropped at the first L3 hop, the loss the technique needs), and `SetSocketTtl`'s error is now returned so a future out-of-range value fails loudly instead of silently. Two previously-vacuous test assertions are now real; 8 tests ported from upstream; 34 resilient tests pass, race clean. Activates a mechanism that has cost nothing on Linux for the fork's whole life and now costs something real — deliberately dropped segments in the ClientHello flight, quantified upstream at 12 dropped blocks of 24 on the reorder-only path. Needs a fleet deploy; worth staging on one node before rolling fleet-wide.

### CI

**Fixed truncated AI summaries, bound model calls, made the portability verdict fork-aware** (#341): Tier-1 summaries were piped through `tail -1`, keeping only the final chunk of the model's streamed JSON output — measured at 6.6% retention (143 of 2153 characters) on a representative run, so every summary that shipped was cut down to whatever fragment happened to land in the last chunk. The accept guard that was supposed to catch bad summaries couldn't distinguish a real summary from a fragment; it now requires at least 400 characters and a parseable verdict tag, applied at 12 sites. A wedged model call could hang the job for up to 6 hours with nothing timing it out; all four call sites are now wrapped in `timeout 120`. The portability rubric was a closed allowlist with no way to say "the fork shares this code"; `[MUST PORT]` now has to lead with a shared-code citation, and `[NO ACTION]` has to give a positive reason instead of defaulting to it. The truncation, guard, and timeout fixes are deterministic and verifiably correct; the rubric fix is a prompt change, measured at improving one diff's verdict accuracy from 2-of-4 to 5-of-5 correct on a single test case, and is deliberately biased toward false positives (flagging something as portable when it isn't) rather than false negatives. No deploy implications — this only touches the CI monitoring pipeline, not the provider binary.

## [v3.23.0-fix.26.7] — 2026-08-06

### Fixed

**Resilient TLS leaked fds, stranded the socket TTL, and could corrupt a stream** (#339): Both TCP reorder branches in `net_resilient.go` acquired a descriptor via `tcpConn.File()` and never closed it, so every failed write leaked one. The socket TTL those branches lower to force retransmits was restored only on the success path, leaving a failed connection at the lowered value for the rest of its life. A mid-fragment write failure returned `0, err` while the buffer still held the full record and earlier fragments were already on the wire, so a resend re-entered `Write`, re-fragmented from the start, and duplicated bytes into the TLS stream; `Off()` separately discarded any partially buffered record instead of draining it. The TTL is now read and restored through `SyscallConn` rather than a dup'd descriptor (no fd to leak at all), each restore is scoped to the branch that lowered it, an indeterminate write drops the buffer and disables the layer so a retry goes direct, `Off()` drains first, and the extender connection is closed on failures after the mode switch. Carries upstream `connect`'s fail-closed resilient TLS implementation into the fork. 1,138 lines of new tests including an fd-growth assertion over repeated failed writes.

**A canceled dial is no longer counted as a backend failure** (#332): Both auth sites counted every `connect()` error as a backend failure, including context cancellation from this process's own teardown. Closing a multi-client window cancels many transports mid-dial at once, and that burst tripped the degraded threshold with fresh timestamps, so the next session started already gated and skipped its first `CreateContract` per sequence. This fork keeps the window-expansion gate upstream removed, so a teardown-manufactured outage also blocked the next window from expanding. `RecordProxyAuthFailure` sat on the same error, marking healthy proxies as auth-failed and feeding them to the dead-proxy reaper. Both now sit behind a `ctx.Err()` carve-out. The open-coded bookkeeping is replaced with `noteBackendFailure`/`noteBackendSuccess` under a mutex, which also fixes two latent defects: the timestamp and counter were separate stores, so a concurrent success landing between them left a positive count with a zero timestamp that reads as healthy; and an aged-out failure streak was extended rather than discarded, so one new failure could read as a full outage on the strength of two events an hour old. Ports the review fixes from urnetwork/connect#192.

**Contract metrics: unbounded retention, corrupt windows, and a 160-minute "24h"** (#328): `provider/contract_metrics.go` had no test coverage and three compounding defects. Every proxy index kept a ~15.4 KB bucket ring forever with nothing to free it, and indices are monotonic per unique address, so churn-heavy boxes grew without bound. The ring advanced one slot per epoch change regardless of gap size, so an idle proxy's stale counts read as recent. And the `24h:` status row covered 960 × 10s, clamped, which is 160 minutes. Now: `retire()` on proxy teardown nils the rings while keeping the registry entry and lifetime atomics (deleting would zero a returning proxy's totals and make a proven-good proxy a reaper candidate) and invokes the previously discarded `AddContractStatusCallback` unsubscribe; buckets carry their own epoch so `window()` stops at the first out-of-range bucket; and two rings (fine 10s × 420, coarse 10min × 144) make the 24h row truthful while cutting per-live-proxy footprint from 15.4 KB to 6.7 KB.

**Hub rollup test flake** (direct to main): the hourly-rollup tests used timestamps that could straddle an epoch-hour boundary and failed at the top of the hour. Pinned mid-hour.

**Root package dependency surface reduced** (#315): `sn_deps.go` deleted. The file was four blank imports of `urfoundation/sn` packages whose own comment said to remove them once real callers existed — `provider/sn.go` now imports all four directly. While it lived, it forced the go-ethereum/cloud-SDK tree into the root `connect` library package: every consumer (extender, connectctl, bindings) compiled and linked the entire Ethereum + AWS/Azure + ZK toolchain. After deletion the root package drops from 471 to 370 transitive dependencies, and `go mod why go-ethereum` routes via `provider` instead of `connect`; library consumers no longer pay for what only the provider binary uses.

**`connecting` guard missing from two of three degraded predicates** (#316): `IsDegraded()` had the guard, `DegradedProxies()` and `ProxyHealthByAddress()` did not. `RegisterProxy` reuses the existing health struct on respawn, setting `connecting = true` without resetting the predecessor's `everUp`/`downSince`, so a proxy that was simply reconnecting read as a degraded tier — and the URL-source reaper's `isLive` check could demote or evict it mid-respawn. All three predicates now agree on the guard, and the dead `total`/`bwCount` counters in `ProxyHealthSnapshot()` were removed.

**Stale `connecting` state now bounded at one pulse cycle** (#320, #322): `connecting` is cleared only by `markProxyUp`/`markProxyDown`, neither of which fires on a failed dial, so a proxy that respawned and never reconnected reported `"connecting"` indefinitely. `RegisterProxy` now stamps `connectingSince`, and past the bound a previously connected proxy falls back to the degraded tier (a never-connected one to `dead`), so a hung respawn reads differently from a fresh one. The bound is 65 minutes, one hourly retry pulse plus margin, explicitly coupled to the provider's `deadConfirmDelay` and the ~1h staging window the docs promise operators — a never-connected proxy only reads as `dead` after a full pulse cycle, not during normal deployment ramp. The heartbeat report and snapshot now also evaluate the fresh `connecting` state before the inherited `everUp` (as `ProxyHealthByAddress()` already did), so a previously connected proxy mid-respawn no longer counts as degraded or dead while its new connection attempt is running. This also switched on a latently dead path: `NewlyDead` previously could never fire for never-up proxies (its `!connecting` clause was never false), and now logs one `DEAD` row to `proxy_health.log` per proxy that genuinely never connected within a pulse cycle; nothing alerts on that row.

**Status server timeouts** (#317): The provider's status HTTP server had no timeouts; a dribbled-header client could hold connections open indefinitely on the exposed port. `ReadHeaderTimeout: 10s` and `IdleTimeout: 120s` now match the hub's configuration, with `WriteTimeout` deliberately unset so the SSE stream is not killed.

**`hub-join` client hardening** (#317): Both PAKE join round-trips used `http.DefaultClient` — no timeout, no context — so a blackholed hub wedged the CLI forever with no output. They now share a 30s-timeout client with contexts, so Ctrl-C aborts a wedged join and a hung hub fails cleanly; the discarded KE2 response decode error is now checked and reported. A same-night hotfix (#321) repaired the test call sites after the PAKE signature churn.

**`go vet ./...` fully clean** (#318): 12 unreachable `return` statements after `panic(err)` removed from `connectctl/main.go` (connectctl is a developer CLI where panicking is intended; the panic calls themselves are unchanged). Vet now reports zero findings repo-wide, the first time in the fork's history, so genuinely new findings can no longer hide in the noise.

### Performance

**Hot-path verbose logging no longer allocates** (#329): The provider ships with `-v=0`, but an unguarded `V(n).Infof()` still constructs and heap-boxes its arguments on every call. Measured against the glog-backed logger at a disabled level, the per-inbound-message site in `Client.run` cost 128 B and 5 allocations per message; the per-packet sites in `ip.go` cost 32 B and 1 allocation. A central fix in `log.go` is impossible, since arguments escape at the call site and `Verbose` is an interface, so the only remedy is per-call-site guards. 35 Tier-1 sites now carry inline `if v := log.V(n); v.Enabled()` blocks (17 in `ip.go`, 16 in `transfer.go`, 2 in `ip_remote_multi_client.go`) and benchmark at 0 B, 0 allocations. Tier-2 per-connection sites were deliberately left alone. No log level, message text, or format string changed; output is byte-identical at any verbosity.

**Capped multiplicative resend backoff** (#331): The fork's resend path backed off only while `isBackendDegraded()` and shifted uncapped as `ScaledRtt() << min(sendCount, 6)`, reaching 64s at `sendCount=6` with a 1s RTT despite a defined-but-ignored `MaxResendInterval` of 8s. Adopts upstream's formula, which backs off on every repeated resend rather than only during outages, caps at `MaxResendInterval`, and offsets from the first resend so the first transmission stays at plain `ScaledRtt()`. The unconditional form matters when acks are delayed by queueing rather than lost: a flat timeout re-sends the whole in-flight window each interval, and the duplicates feed the congestion that delayed the acks. Extracted into a pure `resendBackoff()` helper so the formula is unit-testable; `isBackendDegraded()` and its other gating points are untouched. This is a backport, not a port; the fork's version would be a regression upstream.

### Documentation

**Contract docstrings across the transfer stack** (#333, #334, #335, #336, #338): Roughly 400 lines documenting invariants that were previously only discoverable by reading the implementation. Covers the transfer data plane (client, send sequence, receive sequence), the contract manager lifecycle, queue and provide-key contracts, the route manager, multi-route selector and transport weights, the RTT window, stream manager and control sync contracts, and the net/io message pool, dialing and framing contracts. No behavior change. The CodeRabbit review of #333 is what surfaced the three `net_resilient.go` defects fixed in #339.

**`LOG_REFERENCE` refresh** (#327): Documents the pulse `connecting` field, hub bootstrap lines, and `proxy_health.log` rows.

### CI

**PR auto-labeling** (#326, #337): Every PR is labeled along two axes, area from changed paths via `actions/labeler`, and type from the conventional-commit prefix already used in PR titles, plus `upstream-port` when the title mentions upstream. Porting from `urnetwork/connect` is a recurring category in this fork with no way to filter for it before now. `needs-fleet-deploy` and `security` remain manual because they are judgement calls not derivable from a diff. #337 orders the type labeler after the area labeler to close a race where the two could overwrite each other. The workflow triggers on `pull_request` rather than `pull_request_target`, so the write-scoped token is never exposed to an untrusted head ref, and the PR title reaches the script through `env:` rather than `${{ }}` interpolation, so an attacker-controlled title cannot be spliced into the shell.

**gofmt sweep and gate** (#324): 29 drifted files formatted, pure alignment across var blocks, struct literals and spacing, 92 insertions and 94 deletions with no semantic change. A new `Check gofmt` step in `test-and-lint` fails the build if `gofmt -l .` prints anything, so formatting cannot silently drift back.

**Verbose-log gate blind spots closed** (#330): The gate protecting #329 had two holes. A format string shared between a guarded site and an unguarded allowlisted site meant un-guarding the guarded one would pass CI silently; the gate now enforces disjointness between the allowlist and the guarded sites, making the whole class self-policing. And the extractor's `(?m)^[^/]*` matched across newlines, mis-attributing lines and leaving 15 sanctioned formats unchecked entirely, so the allowlist held 41 entries against a true 55. Both fixed, with the gate watched in the passing state and both failing states.
## [v3.23.0-fix.26.6] — 2026-08-03

### Fixed

**Fragmented IPv4 packets dropped in `parseIpv4`** (#311): A non-first fragment has no transport header and a first fragment has a truncated payload; both were previously misparsed as if a transport header were present, reading garbage bytes as ports/flags. Any packet with the MF bit set or a non-zero fragment offset is now rejected before transport parsing. DF-only and unfragmented packets are unaffected. Ported from upstream `e05ecee0`.

**SCTP/WebRTC reliability tuning** (#312): `ReceiveMtu` corrected from 4 KiB to 1500 (it's a per-packet demux buffer, not the SCTP receive window); `SctpCwndCAStep` set to 4 MTUs for faster recovery from independent loss on higher-latency paths; new lazy SCTP progress watchdog tears down blackholed associations where ICE consent stays healthy but the data plane is dead. Ported from upstream `aee94774` (settings only). No pion dependency bump needed (`SetSCTPCwndCAStep` verified at vendored v4.2.15).

**`SecurityPolicyStatsCollector` destination cardinality bound** (#314): `resultDestinationCounts` was unbounded — every unique (protocol, ip, port) tuple got a permanent map entry on long-running providers. Now capped at 1024 destinations per result with the final slot as an overflow bucket ("other destinations"); unrecognized result values share one unknown-result bucket; zero-count updates ignored. Ported from upstream `45357960`.

### Added

**`sender_generation_id` proto field** (#313): Added to `ExchangeSignals` to disambiguate a delayed initial `WaitingForSdpOffer` from a newly restarted passive association. Inert until the peer-connection generation-reset logic lands; additive and wire-compatible with peers that don't set it. Ported from upstream `45357960` (field only).

**Proto source drift repaired** (#313): Commit `ccada52` (Jul 11) regenerated `transfer.pb.go`/`frame.pb.go` from a different upstream `.proto` without landing matching source changes; a `make build` regen would have silently deleted `NetworkPeer`, `NetworkPeersReset`, `NetworkPeersUpdate`, and the `TransferNetworkPeers*` enum values (all live, used by `transfer_peer_manager.go`). Definitions reconstructed from the shipping generated code and restored to the `.proto` sources; `protocol/` `make build` is safe to run again.

### Out of scope (deferred)

- ICMP echo path (upstream `e05ecee0`): client-facing, provider relays TCP via SOCKS.
- Android/cellular P2P (`759a5a7d`, `aee94774` Android bits): mobile-gated; fork lacks `ProviderStreamPolicy`/`DegradedMode` prerequisites.
- Lifecycle/budget overhaul (`45357960` remainder, `fe8dee32` full perf split): needs a dedicated diff session against the fork's divergent `message_pool.go`/`memory_budget.go`.

## [v3.23.0-fix.26.5] — 2026-08-02

### Added

**`urnet-tools idle-update`** (#301): Waits for billable traffic to drop below a threshold (default 5 KiB/s, `--threshold`) and stay there for a sustained window (default 5 min, `--window`) — verified with a second, tighter polling pass — before applying a pending provider update, instead of updating immediately and cutting off active sessions. `--window 0` updates immediately. Fails closed if `billable_rate` isn't available yet, and rejects `--threshold`/`--window`/rate values outside Bash's signed-int64 range.

**Cloudflare Worker sources versioned in-repo** (#304): `dl`, `dl-fullbars`, `geo`, and `provider-redirect` — previously dashboard-only or partially untracked — now live under `workers/` with their `wrangler.jsonc` configs and a `workers/README.md`. Deploys remain manual (`wrangler deploy`); nothing is wired into CI.

**`URNETWORK_HUB_TRUSTED_PROXIES`** (#302): New hub env var. `X-Forwarded-For`/`X-Real-IP` are now only honored from configured trusted proxy IPs/CIDRs — previously trusted unconditionally from any client, letting an attacker spoof their apparent IP and evade PAKE join rate-limiting entirely. Without this env var set, a hub behind a reverse proxy now sees the proxy's own IP for every request (collapsing rate-limiting into one shared bucket) instead of trusting an unverified header — set it to the proxy's address to restore correct per-client behavior.

**Upstream a2b144c port** (#298): `WindowType`/`OverrideAllowDirect`/PQE/peer-identity support ported from upstream `urnetwork/connect`.

**Tiered setup guides** (#297): Beginner/intermediate/advanced installation documentation.

### Fixed

**`proxy.state` ghost-entry accumulation** (#305): `ProxyReloader.reload()` in `proxy_reload.go` only pruned `proxy.state` for addresses that were both currently running and no longer desired. A dead/offline proxy's goroutine has usually already exited by the time it's removed (e.g. via `proxy remove-dead`), so it was never in the "running" set and its state entry was never deleted — accumulating forever and getting re-reported as removable on every subsequent `remove-dead` run. Confirmed on a production node: 722 proxies correctly removed from config/live fleet, but `proxy.state` retained 857 entries, 711 of which existed nowhere else. `reload()` now reconciles `proxy.state` against the full desired set (config/file + URL cache) instead. Guards against a related edge case: if `proxy_url.json` fails to read mid-cycle, the prune pass is skipped rather than deleting state for still-desired URL proxies over a transient error.

**Pooled-buffer leaks on backpressure/error** (#303): `tcpSend` could double-return a pooled packet on certain nil-sequence paths in `ip.go`. UDP backpressure drops and RST-buffer sharing are now leak-free under load; GCM seal count is capped per key.

**Node-bound hub credentials** (#302): v2 node credentials are now bound to their owning node — a valid credential for one node can no longer act on another node's data via `/api/report`, `/api/heartbeat`, or `/api/nodes/remove`. `onboard.sh`'s Host header is validated before being interpolated into the generated install script.

**Docker in-place update process handling** (#299): The update path now kills the correct process and respawns it properly.

**Cloudflare Worker hardening** (#304): `dl-fullbars`'s GitHub proxy now allowlists outbound headers instead of forwarding everything from the client; the `curl | sh` install dispatcher fails the install if the script download fails instead of silently running an empty script; `geo` worker's RTT fields use explicit null checks and the response is marked `Cache-Control: no-store`.

**Ramlog redirect scoped to `provide`/`auth-provide`** (#306): `URNETWORK_RAMLOGS=1` redirected stdout/stderr into `/dev/shm/urnetwork.log` for every invocation of the binary, not just the long-running `provide` process — one-shot CLI subcommands run via `docker exec` (`proxy remove-dead --preview`, `proxy summary`, etc.) produced no visible output at all. Found while validating the `proxy.state` fix on a live test container. Now scoped to `provide`/`auth-provide` only.

**Hourly reload reconciler** (#309): `reload()` in `provider/proxy_reload.go` only ran on an explicit trigger (add-source, remove-dead, proxy refresh, URL fetch merge, reaper change). A mass-failure event could leave still-desired proxies stuck out of the running set with no future trigger to bring them back. Confirmed on a production node via `proxy_health.log`: a mass degrade event mostly self-recovered within an hour, but a persistent subset (~3300 proxies) sat idle for ~22 hours until an unrelated `add-source` call forced a reload. New `runReloadReconciler` fires a reload trigger every hour unconditionally as a safety net — cheap no-op when nothing is actually wrong, not gated behind `URNETWORK_SELF_HEAL`.

## [v3.23.0-fix.26.4] — 2026-07-18

### Added

**Degraded-proxy reaper** (#293): Background reaper runs every 3 minutes during sustained backend outages, ranks degraded proxies by lifetime contribution (traffic + contracts won), and cancels the worst-contributing half every cycle — while always keeping at least half alive so the fleet can detect recovery immediately. `direct`/native mode is permanently exempt. Re-verifies a proxy is still actually degraded immediately before cancelling it, since scoring/sorting thousands of candidates takes real time and the target could have reconnected (or been replaced by hot-reload) in that window.

**GOMEMLIMIT for turbo profiles** (#293): `turbo-v4`/`turbo-v8` now set a memory ceiling (80% of available RAM) when the operator hasn't set `--max-memory`/`GOMEMLIMIT` explicitly — previously `GOGC=200` with no ceiling at all, so nothing capped growth during an outage. Matches the safety net `eco` mode already had.

**`connect.IsDegraded(address)`**: New exported health check used by the reaper's stale-decision recheck; also usable standalone.

### Fixed

**TOCTOU tmpfile vulnerability** (#292): Provider update downloads now use `mktemp`-generated unpredictable paths instead of a hardcoded `/tmp/urnetwork-update.tar.gz`, closing a symlink-race (CWE-377) that could let a local user redirect the extraction to overwrite an arbitrary file. Provider binary installation is now staged and atomically replaced only on full success, with cleanup on any failure path.

**Download reliability** (#291): Provider updates route through `dl.fullbars.xyz` first with automatic GitHub fallback, plus two follow-up fixes in the same window — `curl` now fails on HTTP error responses (a missing `-f` flag meant the GitHub fallback never actually triggered on a bad primary response), and the Windows primary/mirror URL selection logic was corrected. Added `-y`/`-f` flags to skip the interactive confirmation on restart.

**Dashboard contract feed** (#294): Fixed a copy-paste bug where the "Recent Contracts" dashboard feed matched `"[contract] acquired"` twice instead of also matching `"[contract] denied"` — every contract denial was silently invisible in the dashboard. Removed a dead, never-displayed `activeProxies` capture left over from an unfinished feature.

**BusyBox httpd docroot**: Restricted to `/app/www` via symlink across all Docker startup scripts, closing unauthenticated access to the full `/app/` directory on port 8080.

**CI flake** (#290): Fixed a test still using a hardcoded 45s deadline that PR #286 had bumped everywhere else.

**JWT NetworkId claim key** (#295): `ParseByJwtUnverified` in `jwt.go` previously read `claims["network_name"]` for both `NetworkName` and `NetworkId` (copy-paste bug). `NetworkId` now correctly reads `claims["network_id"]`. 3 new tests cover valid, missing, and malformed JWT claims.

**authBytes message-pool leak** (#295): `runH3` in `transport.go` was missing the `defer MessagePoolReturn(authBytes)` that `runH1` has, leaking one pool buffer per H3 auth handshake.

**logThrottle consolidation** (#296): Replaced 6 hand-rolled atomic rate-limiters in `transfer.go` with 3 `logThrottle` instances, removing ~50 lines of duplicated `CompareAndSwap`/`Swap` boilerplate. All three call sites (`[c]ping`, `[c]ping err`, `[r]drop`) behave identically.

**Division-by-zero guard** (#296): `contractByteCount()` in `transfer_contract_manager.go` now guards against `ContractTransferByteSeqScale=0` (bad profile override), returning `max(StandardContractTransferByteCount, minByteCount)` instead of panicking.

**Explicit GOGC=100 for tier3** (#296): `applyTier3` in `tuning.go` now calls `debug.SetGCPercent(100)` to match the other three tiers, which already set it explicitly. No behavioral change (100 is Go's default).

**SequencePeerAudit error logging** (#296): `SequencePeerAudit.Complete()` no longer silently swallows `SendControl` errors — the empty `func(...){}` callback was replaced with an `Errorf` that logs the error. Test confirms the log message is emitted without panicking.

**.gitignore hardening** (#296): Added `hub/hub_bin`, `hub.db`, and `prs.json` to prevent accidental commits of build artifacts and local state.

### Known gaps not yet in this release

No open gaps currently tracked for the next release.

---

## [v3.23.0-fix.26.3] — 2026-07-16

### Added

**Hub CA cert auto-bootstrap** (#281): On startup, `hub init` now checks `URNETWORK_HUB_TOKEN`, `URNETWORK_HUB_TOKEN_FILE`, and `URNETWORK_HUB_TOKEN_STDIN` for a hub token. If present, it fetches the CA cert from `$HUB/ca-cert?token=...` using the token before doing anything else.

**PAKE-based hub join** (`hub -hub-join <url>`): New command that runs the OPAQUE handshake against a hub's join endpoints, reads password from stdin, and saves a per-node credential to `~/.urnetwork/hub.credential`. The credential is accepted by `requireAuth` alongside `URNETWORK_HUB_TOKEN`. Credentials are stored hashed (SHA-256), revoked on `/api/nodes/remove`, and re-registration follows `hub.password` rotation. Pending handshakes expire after 2 minutes to bound server memory. The CLI runs the same tested OPAQUE helpers as the unit test suite rather than a separate implementation.

**Auto Tier 4 Extreme** (#280): On hosts with >= 8 GiB RAM, the provider now auto-selects the Tier 4 (extreme) performance profile matching turbo-v8 settings. Manual `tier set 4` overrides remain.

**Docker-backed hub install/update** (#278): `urnet-tools hub install` and `hub update` on macOS and Windows now deploy the hub via Docker (`docker pull`/`run` against `ghcr.io/full-bars/urnetwork-3.23-fix-hub`). Linux supports `--docker` opt-in. All platforms share the same `urnetwork-hub` container name and `urnetwork-hubdata` named volume.

**HTTP Basic Auth for hub dashboard** (#282): The hub dashboard and read-only API endpoints now accept HTTP Basic Auth via `URNETWORK_HUB_DASHBOARD_PASS`. Separate from `URNETWORK_HUB_TOKEN` (used for write endpoints). Unset = unauthenticated.

**Persisted custom network selection** (#288): New `provider choose_network <api_url> <connect_url>` / `provider choose_network --reset` commands let operators running their own API/connect backend save it to `~/.urnetwork/network.json` instead of repeating `--api_url`/`--connect_url` on every invocation. Resolution order: flag > saved config > hardcoded default. Docker: `UR_API_URL`/`UR_CONNECT_URL` env vars (must be set together), also reachable via `urnet-tools choose_network` (Linux/Docker exec and Windows). Ported from `urfoundation/sn` PR #1.

### Fixed

**CA cert live reload** (#279): The hub now watches `hub_ca.pem` via file poll and reloads the CA certificate on change without restarting. Allows CA cert rotation on live hubs.

**Hot-restart warning fix** (#277): `urnet-tools restart` no longer always shows "cold restart required" — it now reflects actual hot-restart status. Deduped `hotRestartEnabled()` check into a cross-platform helper. Worker download fallback ported from Linux to macOS (`Provider_Install_Mac.sh`) and Windows (`urnet-tools.ps1`) so all three platforms have GitHub rate-limit resilience for binary downloads.

**`--tag` not honored on hub update** (#277): When a persisted config had a tag and `--tag` was passed, the persisted tag took precedence. Fixed so `--tag` always wins.

**Docker hub update pointed at wrong image** (#277): `hub update` was pulling the provider image instead of the hub image. Fixed to resolve the correct `-hub` image.

**Go vet fix** (#277): Escaped `%` in printf-style comment.

### Known remaining

- **Fingerprint pinning (`hub.pin`)**: Deprecated. To be removed in the next release.

---

## [v3.23.0-fix.26.2] — 2026-07-14

Release notes: `releases/v3.23.0-fix.26.2.md`

### Added

**Self-Healing Proxy Resource Management** (#259): Two-layer system — always-on fixes (default `proxy_url_max=500`, cleanup scope `"url"`, faster dead give-up, stale re-probe) plus opt-in closed-loop pressure system (`URNETWORK_SELF_HEAL=1` / `urnet-tools self-heal on`). AIMD pool controller grows under calm, sheds worst-first under pressure. Off by default.

**Hub Off/Set live reload** (#276): All four hub commands (`link`/`unlink`/`set`/`off`) now write the same override file the provider polls every report tick — `hub set`/`off` no longer require a restart.

**Subnet Integration Prep (Phases 1–2)** (#272): Pins `urfoundation/sn` crypto/chain packages; backports upstream `Sn*Sync` API methods. Inert until CLI wiring lands.

### Fixed

**ProbeOK Lost on Cache Merge** (#274): Fixed `mergeProxyURLEntries` discarding API-reachability results before caching, letting the reaper blacklist proven-live proxies.

**go-ethereum Dependency Bump** (#275): `github.com/ethereum/go-ethereum` v1.16.7 → v1.17.4, clearing 5 Dependabot alerts.

**Installer Links** (#271): Replaced `raw.githubusercontent.com` installer links with `dl.fullbars.xyz` shortcuts.

---

## [v3.23.0-fix.26.1] — 2026-07-13

Release notes: `releases/v3.23.0-fix.26.1.md`

### Added

**Docker: In-Place Updates** (#255): `urnet-tools update` inside the Docker container allows in-place binary replacements without pulling a new image.

**Official `dl.fullbars.xyz` URLs** (#258): All installation scripts and docs updated to use the new shortened domain.

**Go 1.26 Toolchain & Dependencies** (#267): Compiler bumped from 1.25.x to 1.26.4; core deps bumped (`pion/webrtc`, `quic-go`, `golang.org/x/*`).

**Standardized Repository Templates** (#268, #269): Structured PR and Release templates for consistent technical documentation.

### Fixed

**Upstream Infrastructure Port** (#261): Ported upstream commits `ee7a476` + `83dc999` — Egress interfaces, Memory Budgets, Contract Stats, Peer Management, `ip_assoc.go`.

**Peer API Refactor & Pause Fix** (#262): Ported upstream Peer API changes — `ReceiveFunction` type change and pause behavior fix.

**Upstream 532ee20c Fixes** (#265): Mode election fix, hot-spin CPU fix, connection eviction fix, `MonitorValue` constructs.

**Core Stability & Leak Cleanup** (#266): Fixed memory leaks in transfer pool and route manager (unbound map growth), resolved WebRTC stream ID panic, prevented infinite CPU spin loop during warmup, handled auth context cancellations.

**Hot-Reload Lock Race & Shutdown Diagnostics** (#260): Idempotent lock releases; cancel-time stack trace diagnostics for easier debugging of exit hangs.

---

## [v3.23.0-fix.26] — 2026-07-09

### Hot-Restart Enabled by Default

`hotRestartEnabled()` now returns `true` unless `URNETWORK_HOT_RESTART=0` is set. Client JWTs are reused across restarts without any opt-in flag — the write path was already un-gated (v25.15), now the read path follows. Set `=0` to disable.

Platform toggles updated:
- **Linux**: `hot-restart off` sets `URNETWORK_HOT_RESTART=0` in override.conf
- **macOS**: `hot-restart off` sets `=0` in launchd plist  
- **Windows**: `hot-restart off` sets `=0` in user env
- `hot-restart on` removes the flag (falls back to Go default = true)

### Added

**Session save/load** (#250): New `urnet-tools session save <file>` / `session load <file>` commands. Encrypted via `openssl aes-256-cbc -pbkdf2`, bundles all 7 identity and proxy-list files from `~/.urnetwork/`, with password-prompted passphrase. Network ID check on load prevents cross-account transfer.

**`provider print-network-id <file>`** (#250): New hidden CLI subcommand that extracts the `network_id` JWT claim using the existing `gojwt` parser, used by the session load safety gate.

**Atomic file writes** (#250): 6 file paths (`jwt`, `jwt_last_refresh`, `.provider.key`, `.provider.cert`, `proxy`) converted from `os.WriteFile` to `os.CreateTemp` + `os.Rename`. Enables `session save` against a live, traffic-serving provider without torn-file risk.

**`applyStagedSession()`** (#250): New Go function called early in `provide()`. Checks for `~/.urnetwork/.session-pending` marker and atomically swaps files from `.session-staging/` → live directory. This is the mechanism that lets `session load` run safely without stopping the provider.

**macOS native installer** (#252): New `Provider_Install_Mac.sh` — supports `install`, `start/stop/restart/status` (via launchctl), `hot-restart on|off`, `session save|load`, `proxy`, `hub`, `auth`, `logs`. Installs binary to `~/.local/share/urnetwork-provider/`, creates launchd plist with KeepAlive, strips quarantine xattr.

**macOS CI builds** (#252): `release.yml` expanded to build and publish `darwin/amd64` and `darwin/arm64` binaries alongside linux. Release tarball includes `darwin/` directory.

**Windows hot-restart toggle** (#251): `urnet-tools.ps1 hot-restart on|off`. Sets/removes `URNETWORK_HOT_RESTART` as user-level env var, with process-level `$env:URNETWORK_HOT_RESTART` propagation so immediate `Start-Process` restart inherits it. Help text reorganized matching Linux PR #245.

**Docker session save/load** (#253): `session save|load` commands added to `docker/scripts/urnet-tools.sh`. Interactive TTY guard prompts if `-it` is omitted. Restart via `pkill` triggers start script crash loop.

**Help text reorganization** (#245): `show_help()` in `Provider_Install_Linux.sh` rewritten with full descriptions, hub commands listed for the first time, `urnet-tools set help` sub-menu, header attribution updated.

**IPv6 STUN auto-detect** (#246): Provider probes IPv6 STUN reachability on startup via `sync.Once`-guarded 100ms UDP dial. If unreachable, restricts ICE candidates to `NetworkTypeUDP4`/`NetworkTypeTCP4` — eliminates "network is unreachable" STUN noise on IPv6-unreachable hosts.

**Transport self-wake loop fix** (#248): Restructured `run()` loop in `transport.go` — `activeMode()` notify channel captured AFTER `setActiveMode()` call, guarded with `lastMode` dedup to skip redundant calls. Eliminates the structural vulnerability that caused a 100% CPU self-wake loop.

**No-arg status toggles** (#247): `eco`/`ramlogs`/`lowmode`/`hot-restart` with no arguments now report current status instead of erroring. Space-separated aliases (`hot restart`) work correctly.

### Fixed

**Passphrase cmdline leak** (#254): All `openssl enc -pass "pass:$var"` invocations changed to `-pass "file:$_pf"` with temp file and immediate cleanup. Passphrase no longer visible in `ps` output.

**Mac installer `$0` fix** (#254): `cp "$0"` replaced with GitHub raw URL download — `cp "$0"` fails silently when invoked via `curl | sh`.

**Atomic writes safety** (#254): `atomicWriteFile` changed from fixed `path + ".tmp"` to `os.CreateTemp(dir, name+".*.tmp")` to avoid temp file collision under concurrent `.provider.key`/`.cert` writers.

**`--force` parsing** (#254): Session load now scans `$3+` for `--force/-f` — previously silently dropped when placed after the file path.

**Bundle JWT validation** (#254): Hard-fail if `print-network-id` returns empty (corrupt bundle JWT) — previously silently bypassed the network_id gate.

### Security

- Session bundles: `openssl aes-256-cbc -pbkdf2`, password-required
- Network ID safety gate on load prevents cross-account identity transfer
- Temp files `0600`, cleaned up on both success and error paths

### Added

**Critical event log** (#242): New `~/.urnetwork/events.log` on disk (not RAM) — 1MB capped, auto-rotating file that survives restarts. Captures STARTUP, SIGNAL, PROVIDER EXIT, PANIC, and FATAL events. Provides a permanent forensic trail even when `/dev/shm` logs are wiped by a restart.

**RAM logs survive restarts** (#242): `shmLogPath` and `shmImportantLogPath` now open with `O_APPEND` instead of `O_TRUNC`. Previous run logs are preserved with a `--- provider restarted at ...` separator.

**Panic hook in `connect` package** (#242): `connect.CritLogger` / `connect.LogCritical()` writes recovered panics from any networking goroutine to the disk-based events.log.

**Release notes backlog** (#243): Release notes for v3.23.0-fix.25.6 through 25.13 are now written and committed.

---

## [v3.23.0-fix.25.13]

### Fixed

**Best Proxies fresher last-seen**: UNION scan of `proxy_node_hourly` alongside `proxy_fleet_daily` for today-accurate timestamps.

### Changed

**x/crypto v0.51.0 → v0.52.0**: Resolves 7 critical CVEs.

---

## [v3.23.0-fix.25.12]

### Added

**Sortable dashboard columns**: Best Proxies and Proxies tables support click-to-sort on all columns.

---

## [v3.23.0-fix.25.11]

### Added

**`proxy remove-dead --auth-failures=N`**: Per-address auth failure threshold with 250/day rate limit. Self-healing skip of currently-up proxies.

---

## [v3.23.0-fix.25.10]

### Added

**Hub dashboard charts**: Added Mbps throughput, Billable %, and Contract Win Rate charts. Fixed chart overflow.

**AI.md operations guide**: Comprehensive guide for provider + hub operations.

---

## [v3.23.0-fix.25.9]

### Fixed

**Dash shell compatibility**: Removed `set -e` and added `|| true` guards on `curl` assignments in `onboard.sh`.

---

## [v3.23.0-fix.25.8]

### Added

**Hash-based URL routing**: Dashboard sections now bookmarkable (`#overview`, `#servers`, `#proxies`, `#contracts`, `#best`).

### Fixed

**Onboard script**: HTTP fallback double-port bug, added direct `/api/cert` fallback.

---

## [v3.23.0-fix.25.7]

### Fixed

**SQLite index hint**: Forced `INDEXED BY idx_pnh_hour_proxy` on live-tail scan to prevent query planner from picking the wrong index.

---

## [v3.23.0-fix.25.6]

### Fixed

**Hub dashboard**: SSE debounce to 5s, sort state survives table rebuilds, composite index on `proxy_node_hourly(hour, proxy_id)`, rate limiter removed (Caddy handles it).

### Changed

**CI**: Renamed `hub-v*` Docker tags to `hub-docker-v*` to clarify they are Docker-only.

---

## [v3.23.0-fix.25.2] — 2026-07-05

### Summary
Two more fixes found while responding to a live fleet incident: a self-healing repair for a rogue systemd drop-in that left the provider with no crash-restart protection, and a logging regression that's been misattributing every plain-level log line to `log.go` since mid-June.

### Fixed

**Self-heal invalid `Restart=` systemd drop-in**: a `restart-override.conf` drop-in with `Restart=yes` (not a valid systemd value — the directive only accepts `no`/`always`/`on-failure`/etc.) was found on a fleet node, silently rejected by systemd on every reload and falling back to the base unit's `Restart=no`. This left the node with zero auto-restart protection since at least July 1. Not something urnet-tools ever wrote — a full history search turned up no commit that ever generated this file or value — so origin is unknown (likely a manual edit). `install_systemd_units` now scans `urnetwork.service.d/*.conf` on every install/update/reinstall and repairs `Restart=yes|true|1` to `Restart=on-failure`.

**Restore correct file:line in log output**: the glog→Logger interface migration (PR #69, 2026-06-15) added a wrapper frame between call sites and the actual `glog` calls. Only the `V(n)` verbose path accounted for the extra frame; the plain `Info`/`Infof`/`Warningf`/`Errorf` methods called glog's non-depth-aware functions directly, so every plain-level log line in the codebase has reported `log.go`'s own line instead of the real caller since that PR merged. Switched to glog's depth-aware variants (`InfoDepth`/`InfoDepthf`/`WarningDepthf`/`ErrorDepthf`) to match the verbose path's existing convention.

---

## [v3.23.0-fix.25.1] — 2026-07-05

### Summary
Two correctness fixes found during a deep code audit, both introduced when the fork branched from stock v3.23 and never ported from upstream.

### Fixed

**P2P transport setup blocked forever, silently dropping one direction of every bidirectional relay stream**: `NewP2pTransport()` started its connection-negotiation loop synchronously (`HandleError(p2pTransport.run)` with no `go`). Since that loop only returns when the stream's context is cancelled, the first call never returned — so `StreamSequence.Run()`'s second `NewP2pTransport()` call (needed for the opposite direction) was unreachable. Every bidirectional P2P stream ended up with only one direction's WebRTC transport ever negotiated, forcing silent fallback to lower-priority transports. Fix: restore `go HandleError(p2pTransport.run, cancel)` to match upstream. Added INFO-level logging (`[p2p]` transport start/stop, `[sm]` both-transports-created) so this class of bug is visible in logs going forward instead of failing silently.

**Message pool buffer leak in DNS fragment reassembly**: `combineQueue.RemoveOlder` dropped timed-out fragment sets without returning their pooled buffers via `MessagePoolReturn`; `combineQueue.Combine` overwrote a fragment slot on a duplicate/retransmitted index without returning the buffer it replaced; and `packetTranslation.decodeDns`'s goroutine had no shutdown drain for its `dnsCombineQueue` or `readPipeline`, leaking any buffers still in flight at teardown. All three now return buffers to the pool.

---

## [v3.23.0-fix.25] — 2026-07-05

### Summary
Minor version bump covering ~50 PRs across core networking stability, performance optimization, proxy lifecycle management, a fully self-hosted hub dashboard with live SSE push, audited error propagation, and tightened security. Includes all sub-patch releases from v3.23.0-fix.24.1 through v3.23.0-fix.24.34 plus new fixes and tuning.

---

### Core Networking

**SendSequence no longer panics at resend cap** (PR #201): When a packet hit the 16-resend limit, it was removed from the resend queue but left orphaned in `sendItems`. The next cumulative ack would hit `panic("Missing item")`, tearing down the entire send sequence and flushing pending contracts. Fix: park the item at `AckTimeout` horizon instead of dropping it.

**Contract rejection statuses now visible to callbacks** (PR #202): `HandleControlFrame` constructed `ContractStatus` on Trust/Invalid errors but returned before dispatching. Locally-rejected and malformed contracts were invisible to `registerContractCallback` (provider metrics) and penalty logic. Fix: dispatch before the early return.

**Pool buffer leak on write timeout — v2** (PRs #203, #206): `WriteDetailed` timeout/ctx-done/done branches had `MessagePoolReturn` commented out, stranding one shared pool ref per timed-out write. PR #206 removes the double-return regression from the first fix and applies `gofmt -w`.

**SOCKS5 proxy DNS death spiral** (PR #189): Resolve target hostnames locally before passing to SOCKS5 dialer, converting FQDN→IPv4. Prevents proxy infrastructure DNS from being blacklisted by upstream resolvers.

**Proxy warmup pileup on 2000+ proxy pools** (PR #190): Three compounding fixes — DNS lookup cache (60s TTL), 30s SOCKS5 dial timeout, and `markProxyDown` now clears `h.connecting` state to unblock warmup progress tracking.

**Self-wake 100% CPU feedback loop** (PR #191): `modeMonitor.NotifyAll()` gated on actual mode changes. Affected v24.27–v24.32.

**Error propagation for reload/state writes** (PRs #163, #165): Silently discarded errors in reload trigger and proxy probe paths now log warnings. Split error check from sequence comparison to prevent transient FS failures from spuriously triggering reloads.

**Port bounds check** (PR #168): Fix CodeQL alert #11 — `strconv.Atoi` could produce values outside 0–65535 port range before `uint16` cast.

**Dead code cleanup** (PR #200): `rttHeap.MinRtt()` returned arbitrary heap leaf instead of actual min. `MultiRouteSelector.setActive()` ignored its parameter. Both fixed.

---

### Performance

**Message pool N-way mutex sharding** (PR #207): Each size-class freelist (2048–65536) split into 16 internal shards with independent mutexes, eliminating cross-proxy lock contention on the hottest allocation path. Rollback lever: `URNETWORK_MESSAGE_POOL_SHARD_COUNT=1`. ~60–80% contention reduction expected at fleet packet rates.

**Default resend queue 2→4 MiB** (PR #204): Raised `ResendQueueMaxByteCount` to match the 4 MiB send window. ~2× per-client throughput ceiling on default-profile nodes.

**Contract ramp-up scale 2→3** (PR #209): Adds intermediate ramp step (2 MiB → ~44 MiB → ~86 MiB → 128 MiB vs 2 MiB → ~65 MiB → 128 MiB). Reduces unused contract allocation on short-lived connections by ~30%.

**RTT fill-fraction floor 0.5→0.7** (PR #205): At ≥1s RTT, 90 MiB consumed per 128 MiB contract before renegotiation (was 64 MiB). Halves contract churn on high-latency paths.

**Hot-path allocation optimizations** (PRs #137, #138, #148–#150): Zero-alloc marshal via `frame_protobuf.go`, `ulid.Make()` replaced with allocation-free `NewId()`, closure hoisting in `ip_remote_multi_client.go`, `ip.go` IpPath caching + timer reuse, `transfer.go` safeAck by-value + timer reuse.

**Message pool optimized for write pipeline** (PRs #148–#150): Pooled buffers drained from write pipeline on cancellation. `SendBuffer.Ack` tries all candidate sequences instead of only the first.

---

### Hub Dashboard & Self-Hosting

**Self-hosted hub dashboard** — a full-featured fleet management UI:
- SQLite-backed historical storage with 5m report interval (PR #123)
- Multiple sortable data views: Overview, Servers, Proxies, Contracts (PRs #184, #185)
- uPlot charts: healthy proxies, traffic, billable bytes, clients (PR #158)
- Summary cards, lazy proxy loading, filter/search, slide-out drawer (PRs #155–#157)
- Per-proxy earning column (`earning=yes/no`) (PR #124)
- Per-proxy contract metrics with 15m/1h/24h rolling windows (PR #182)
- Fleet-wide per-proxy analytics: hourly 90d, daily 13mo, fleet_daily forever (PR #184)

**Live updates via SSE** (PR #188): Server-sent events push `data: refresh` to dashboard tabs the moment a heartbeat or report arrives. No polling.

**TLS hub reports with cert pinning** (PR #186): Auto-generated ECDSA P-256 cert, TOFU-style pinning, `hub init`/`hub link`/`hub unlink` commands, transactional `hub update` with rollback.

**Live heartbeat endpoint** (PR #187): `/api/heartbeat` at 15s cadence with connection reuse and exponential failure backoff (capped at 5m).

**Performance** (PR #186): Gzip compression, per-IP rate limiting (60 req/min), stale node eviction (15 min), nodeSummary API.

**Operational commands**: `urnet-tools report <url>` for runtime hub URL config (PR #154), `urnet-tools set report-interval 60s` for runtime report cadence (PR #159).

**Hub Docker & CI** (PRs #193–#195, #197): First-class Docker image with buildx caching, CI pipeline building on `hub/**` changes, reduced build context.

---

### Proxy URL Sources & Lifecycle

**Dual-stage SOCKS5 + API reachability probe** (PR #152): Tests SOCKS5 compliance AND API reachability through the proxy on a single TCP connection. Background reaper with 5-min retry, 3-failure eviction to 24h persistent blacklist.

**Escalating give-up backoff** (PRs #129, #133): 15m → 30m → 1h → 2h → 4h → 8h → 16h → 24h, permanent eviction after 10 cycles via persisted `proxy_url.json` blacklist.

**File-before-URL launch priority** (PRs #139, #140): File-based proxies launch before URL-sourced ones (sorted by source). URL fetcher waits for warmup completion.

**Pattern-based proxy management** (PR #183): `proxy remove --match=<substring>` with host-substring matching, persisted exclusion patterns, `proxy exclude` subcommand.

**Multi-tier degraded proxy cleanup** (PRs #145, #185): Runtime-configurable auto-cleanup with source filtering. Dead proxy reclassification after 7 days down.

**Operator-curated proxy resilience** (PRs #119, #120): Auth retry with slow capped backoff (5m→10m→15m). URL requeue decoupled from reload engine via `time.AfterFunc`.

---

### Observability

**Per-proxy contract metrics** (PRs #182, #184): Lock-free atomic counters for acquired/denied contracts. Contract Win Rate card, contracts/hr chart, sortable Contracts column in node table.

**Per-minute earning windows** (PR #121): `[earn] billable_1m/5m/15m/60m` every 60 seconds, decoupled from health heartbeat.

**`[profit]` heartbeat + contract utilization** (PR #126): 15s ticker showing `earning=yes/no clients=N rate=X`. Contract close logging with `acked=X allotted=Y util=Z%`.

**Important log buffer** (PR #135): 1 MiB `/dev/shm/urnetwork-important.log` — high-value lines only (`[profit]`, `[health]`, `[outage]`). Survives main ramlog flood.

**Log spam suppression** (PRs #131, #135): Unified `logThrottle` replaces 4 identical rate-limiters. `[c]ping err` suppressed to 1 line per 5 minutes with `(N suppressed)` count.

**Version awareness** (PR #132): `urnet-tools -v` shows running vs on-disk version separately. Provider version logged at startup. Goroutine count in health heartbeat.

---

### Security

**DoH certificate pinning** (PR #164): Uses `DefaultTlsConfig()` with ISRG Root X1/X2 pinning instead of insecure nil TLS config for all four DoH providers.

**System cert pool for DoH** (PR #167): Restored Go's system cert pool after LE-only pool broke Cloudflare, Google, Quad9, and OpenDNS.

**Hub bearer token authentication** (PR #170): `URNETWORK_HUB_TOKEN` required for `/api/report` and `/api/nodes/remove` write endpoints.

**Cap unbounded reads** (PRs #180, #181): `io.LimitReader` (8 MiB HTTP / 64 KiB DoH) on all response body reads. JWT write failure now triggers `log.Fatal`.

**IP Security DPI refactor** (PR #160): Replaced monolithic `ip_security.go` with layered DPI pipeline. Packed binary-search blocklist (64,131 IPv4 + 214 IPv6 ranges). BitTorrent signature detection (BEP 3/5/15/29).

---

### CI/CD & Operations

- **`urnet-tools update -f`** now stops, updates, and restarts non-interactively (PR #125)
- **`urnet-tools update`** fetches latest script from GitHub; bundled in tarball (PR #136)
- **Idempotent `override.conf` helpers**: `override_set_env`/`override_rm_env` eliminate duplicate systemd entries (PR #153)
- **Runtime tunables**: `urnet-tools set node-name`, `report-interval`, `proxy-url-max`, `proxy-url-refresh`, `fast-auth` (PR #159)
- **Provider version resolution** scoped to its own tag scheme (PR #199)
- **Remove dead config fields**: `LegacyCreateContract` and `TrackUsedContracts` (PR #166)
- **`urnet-tools` no-args** shows help instead of triggering install (PR #161)
- **Dash/bash compatibility**: `echo -e` replaced with `printf` across scripts; lint extended to deps/uninstall (PR #117)
- **Build release monitor**: Discord notifications on `urnetwork/build` release tags (PR #162)
- **CI**: Docker build depends on test success, module cache, skip registry logins on PR builds (PRs #112, #198)
- **Deps**: Bump `golang.org/x/net` (PR #196)

---

## [v3.23.0-fix.24.34] — 2026-07-03

### Added
- **Provider version startup log**: The binary now logs `[startup] provider version=...` once at process startup (inside `provide()` before proxy setup), matching the version line Docker already printed via startup scripts. Existing `client_id`/`instance_id` output is unchanged.
- **Goroutine count in health heartbeat**: The `[health]` log line now includes `goroutines=N` for spotting leaks and runaway goroutine growth.

### Changed
- **Removed legacy `WARP_VERSION` env var**: `RequireVersion()` now returns the linked-in `Version` directly. The Dockerfile no longer sets `WARP_VERSION`, so in-container `urnet-tools update` no longer pins the reported version to the original image version.

---

## [v3.23.0-fix.24.33] — 2026-07-03

### Fixed
- **100% CPU self-wake feedback loop** (PR #191): Commit `1f64686` (v24.27) added `modeMonitor.NotifyAll()` to `PlatformTransport.setActiveMode()` so mode-waiting goroutines would wake on changes. However `run()` calls `setActiveMode()` unconditionally — even when the selected mode is already active — causing `NotifyAll()` to close the notification channel `run()` literally just captured, forming a self-wake loop that spins ~8,000 Hz per CPU core. Fix: gate `NotifyAll()` on actual mode change. Same guard applied to `setModeAvailable()`. Regression tests added.

### Added
- **Self-wake diagnostic log**: `setActiveMode` now logs a rate-limited warning when called with the mode already active — surfaces the feedback loop pattern in logs before it silently consumes 100% CPU.
- **Regression tests**: `TestSetModeAvailableNoSpuriousWake` and `TestSetActiveModeNoSpuriousWake` verify that neither setter fires `NotifyAll` when the value hasn't changed, but both still fire on real transitions.

---

## [v3.23.0-fix.24.32] — 2026-07-03

### Fixed
- **Proxy warmup goroutine pileup** (PR #190): Three fixes for warmup on large proxy pools (2000+): DNS lookup cache (60s TTL) so 2000+ concurrent goroutines don't hammer the system resolver; 30s SOCKS5 dial timeout for the warmup path (no-op for paths with existing context deadlines); `markProxyDown` now clears `h.connecting` so proxies whose initial connection fails aren't stuck in "connecting" state forever.

---

## [v3.23.0-fix.24.31] — 2026-07-02

### Fixed
- **SOCKS5 proxy DNS death spiral** (PR #189): Broken DNS resolvers on paid SOCKS5 proxies caused `golang.org/x/net/proxy` to fail TLS handshakes. Fix resolves target hostnames locally before passing them to the SOCKS5 dialer, converting FQDN → IPv4. Downgrade-safe.

---

## [v3.23.0-fix.24.30] — 2026-07-02

### Added
- **Hub TLS with cert pinning (TOFU)** (PR #186): Hub binary accepts `-tls-addr` and auto-generates a self-signed ECDSA P-256 cert on first boot. `/api/cert` exposes the SHA-256 fingerprint for trust-on-first-use pinning; providers verify it via `~/.urnetwork/hub.pin` before reporting. New `urnet-tools hub` subcommands:

  | Command | What it does |
  |---|---|
  | `hub init` | Enables TLS via `URNETWORK_HUB_TLS_ADDR=:8443`, restarts, prints fingerprint + firewall hint |
  | `hub link https://host:8443` | Fetches and confirms the fingerprint, pins it, sets `report_url` |
  | `hub unlink` | Removes the pin, reverts to plain HTTP |
  | `hub test [url]` | Verifies the pinned fingerprint still matches (openssl, curl fallback) |
  | `hub open-port <port>` | Detects firewalld/ufw/iptables/nftables, prints the exact command to open it |
  | `hub update [-f] [-t tag]` | Transactional binary update with automatic rollback on failure |

- **Transactional hub update** (PR #186): `hub update` is atomic — stops the service, backs up `hub.db`, downloads and verifies the new binary, swaps it in, restarts, and verifies it came up. Any failure restores the old binary + DB and restarts the previous version. Idempotent (no-ops at the target version unless `--force`). 40 test cases cover tag resolution, rollback states, and systemd templating.
- **Live hub heartbeat** (PR #187, #188): New `/api/heartbeat` endpoint gives the dashboard a 15s-cadence liveness signal, separate from the full `/api/report` (5-15m) — Mbps rate, last-seen, uptime, and now per-proxy status/contract counts, all updated in-memory only (no DB writes). The provider only sends proxies whose status or contract counters actually changed since the last tick, so payload size scales with fleet activity, not fleet size. One `http.Client` is reused per reporter instance instead of a fresh TCP+TLS handshake every tick, and consecutive failures back off exponentially (capped at 5m) so a flaky link to the hub doesn't turn into a retry storm.
- **Live dashboard updates via SSE** (PR #188): New `GET /api/events` endpoint pushes a "something changed" signal to connected browser tabs the instant a heartbeat or report lands, instead of waiting on the dashboard's 30s poll. The poll stays as a backstop for links where SSE gets buffered or stripped.
- **Dashboard visual polish** (PR #186): Inline color-coded IP tags per node row (same-NAT boxes cluster visually), a green TLS padlock icon on nodes reporting over HTTPS, and sortable Servers-page columns (replacing the old grouped-header layout).

### Fixed
- **Dead proxy reclassification** (PR #185): Proxies down for ≥7 days are now classified `dead` instead of `degraded` in `ProxyHealthSnapshot()`, matching the existing `inactive` tier.
- **Fleet chart initial-spike glitch** (PR #185): `loadFleetChart()` now seeds cumulative deltas from the first hour's data instead of 0, preventing a flattened chart after the first spike.
- **Window pill ordering** (PR #185): Proxies/Contracts page time-window pills now read ascending (1h → 24h → 7d → 30d → 1y).
- **NULL contract columns crash** (PR #185): `COALESCE(contracts_acquired,0)` / `COALESCE(contracts_denied,0)` guards added to three queries that could hit a NULL scan error on older rows.
- **`/api/nodes` missing traffic fields** (PR #186): Response now includes `rx`/`tx`/`bill_rx`/`bill_tx`/`clients` aggregated from proxy data — these were missing and caused `undefined` errors in the dashboard JS.

---

## [v3.23.0-fix.24.28] — 2026-07-01

### Added
- **Per-proxy contract metrics** — Each proxy goroutine now tracks contract acquisition and denial counts with lock-free atomic counters. Rolling time windows (15m, 1h, 24h) let operators spot short-term performance changes. Two new visibility layers:

  **Provider CLI**: `urnet-tools proxy summary` now shows:
  
    --- Contract Stats ---
      Acquired: 262
      Denied:   1012
      Win rate: 20.5%
      15m:  42 acquired / 181 denied
      1h:   262 acquired / 1012 denied  
      24h:  262 acquired / 1012 denied

  **Hub dashboard**:
  - **Contract Win Rate card** — Fleet-wide view of raw acquired vs denied totals and computed win percentage
  - **Contracts/hr chart** — uPlot chart showing acquired (green) and denied (red) contracts per hour, letting you see patterns across the day
  - **Contracts column** in the fleet node table — Sortable column showing which nodes win contracts
  - **Won/Lost in proxy drawer** — Per-proxy breakdown of successful vs denied contracts, sortable by either column

  **SQLite storage**: `node_hourly.contracts_acquired` and `node_hourly.contracts_denied` store contract history for 1-year retention, queryable via `/api/history?hours=168` for weekly rollups.

---

## [v3.23.0-fix.24.27] — 2026-07-01

### Fixed
- **Hub bearer token authentication**: `/api/report` and `/api/nodes/remove` now require `URNETWORK_HUB_TOKEN` (PR #170). Constant-time comparison, backward-compatible (unset = unauthenticated). Report body capped at 1MB.
- **RouteManager concurrent map crash**: Added mutex lock in `DowngradeReceiverConnection` to prevent "concurrent map iteration and write" crash under transport churn (PR #171).
- **NACK accounting leak**: `removeEventBucket` now subtracts `sendNackCount`/`sendNackByteCount` on bucket eviction, preventing permanent bandwidth accounting drift (PR #172).
- **SendBuffer.Ack wrong sequence handling**: Moved `break` inside success branch so all candidate sequences are tried before giving up (PR #173).
- **Mode monitor waiters never wake**: Added `NotifyAll()` calls after `setModeAvailable`/`setActiveMode`, allowing H1/H3 mode-wait loops to wake on state change instead of timing out (PR #174).
- **Write-pipeline buffer leak on cancel**: UDP and TCP write goroutines now drain `writePayloads` on exit; additional drain in `Run()` catches items re-queued after the goroutine drain (PR #175).
- **MultiRouteSelector.Read ignored caller context**: Changed to use passed-in `ctx` instead of `self.ctx` (PR #176).
- **Checkpoint contracts closed incorrectly**: `CloseContractWithCheckpoint` now guards cleanup behind `!checkpoint` (PR #177).
- **Client replace cleanup bypassed removeClient**: `expand()` now calls `clientRemoveCallback` instead of bare `Cancel`, properly cleaning up `clientUpdates` entries (PR #178).
- **PeerConn data channel not closed on teardown**: Added `conn.Close()` to deferred cleanup in `peerConn.Run()` (PR #179).
- **Unbounded HTTP/DoH reads**: Added `io.LimitReader` caps (8 MiB / 64 KiB) on all response body reads (PR #180).
- **JWT write error silently ignored**: Added error check + fatal log for `os.WriteFile` in JWT persist (PR #181).

---

## [v3.23.0-fix.24.21] — 2026-06-27

### Added
- **`urnet-tools fast-auth [on|off]`**: Toggle the auth rate limiter bypass without restarting the provider. `fast-auth on` takes effect immediately (same as `URNETWORK_AUTH_UNLIMITED=true`). `fast-auth` with no args shows the current state.
- **`urnet-tools set [<key> [<value>|off]]`**: Unified interface for all runtime tuning overrides — no restart needed, changes take effect on the next provider tick. `urnet-tools set` with no args shows all active overrides. `urnet-tools set <key> off` clears the override and reverts to the startup default.

  | Key | Example | What it changes |
  |-----|---------|-----------------|
  | `node-name` | `urnet-tools set node-name ny1-box3` | Identity label shown in hub dashboard |
  | `report-interval` | `urnet-tools set report-interval 60s` | How often bandwidth stats are posted to the hub (min 10s) |
  | `proxy-url-max` | `urnet-tools set proxy-url-max 500` | Caps URL-sourced proxies loaded per fetch cycle |
  | `proxy-url-refresh` | `urnet-tools set proxy-url-refresh 15m` | How often the URL proxy list is re-fetched (min 10s) |
  | `cleanup-scope` | `urnet-tools set cleanup-scope degraded` | Which proxies the cleanup job targets (`dead`, `degraded`, `all`) |
  | `cleanup-interval` | `urnet-tools set cleanup-interval 6h` | How often the dead-proxy cleanup job runs (min 1m) |
  | `fast-auth` | `urnet-tools fast-auth on` | Auth rate limiter bypass (also settable via `set`) |

  Backed by `~/.urnetwork/<name>` files introduced in PR #159; `urnet-tools set` is the supported interface for all of them.

### Fixed
- **Hub fleet charts rewritten** (PR #158): The sparkline was plotting cumulative totals (monotonically increasing, unreadable). Now shows 4 charts in a 2×2 flex grid: healthy proxies + active clients over time; hourly traffic deltas (RX/TX per hour window); billable traffic per hour; peak clients + reporting nodes.
- **History chart aggregates correctly in multi-node view** (PR #158): Was interleaving every node's data as separate line segments at the same X positions. Now aggregates by hour and sums RX/TX across all nodes. Negative deltas from reporting gaps clamped to zero.
- **Earning metric seeds correctly on restart** (PR #158): `prevBillable` was initialized to zero on restart, so the first report cycle always showed 0 earnings. Now seeded from per-proxy IDs stored in the DB — earning is accurate from the first report after a restart.
- **Proxy drawer routing fixed** (PR #158): `/api/nodes/<id>/proxies` was unreachable due to a routing conflict — the drawer silently failed to load per-node proxy lists. Drawer now defaults to sort by active clients descending (then billable RX descending), with sortable columns and 90vw width.
- **`/api/nodes` response includes earning field** (PR #158): Summary cards now reflect live earning state without a separate request.

---

## [v3.23.0-fix.24.22] — 2026-06-28

### Added
- **IP Security DPI Refactor** (PR #160): Replaced the monolithic `ip_security.go` (~66K lines) with a layered deep-packet-inspection pipeline. Payload-level BitTorrent signature detection (BEP 3/5/15/29, entropy-based encrypted-flow heuristic) instead of port-only heuristics. 214 IPv6 prefix ranges now blocked (previously unchecked). 66K-line `map[[4]byte]bool` blocklist replaced by ~8K-line packed binary-search for zero-allocation lookups. Known-safe protocols (NTP, IKE, DNS/UDP) skip DPI entirely via three-way verdict (drop/allow/pass-to-DPI).

---

## [v3.23.0-fix.24.23] — 2026-06-28

### Fixed
- **`urnet-tools` no-args shows help instead of triggering install** (PR #161): Running the script with no arguments was triggering a full install instead of showing the help menu. Fixed via `$0` path check instead of the broken `URNETWORK_TOOLS_MODE` env var injection approach.

### Added
- **Build release monitor** (PR #162): New GitHub Actions job monitors `urnetwork/build` repository for release tags, posts commit summaries with file changes to Discord. Expands critical files list and shows actual filenames in notifications.

---

## [v3.23.0-fix.24.24] — 2026-06-29

### Fixed
- **Error propagation for reload/state writes** (PR #163): `writeReloadTrigger()` and `writeProxyState()` now log warnings on failure instead of silently discarding errors. Prevents hot-reloads from silently breaking and stale state files when disk writes fail.
- **DoH certificate pinning** (PR #164): DNS-over-HTTPS resolver now uses `DefaultTlsConfig()` with ISRG Root X1/X2 pinning instead of insecure `// FIXME` nil TLS config. Prevents potential MITM of DoH responses.
- **Reload watcher and proxy probe error handling** (PR #165): Split error check from sequence comparison in `readReloadSeq` to prevent transient FS read failures from spuriously triggering proxy reloads. Added proper timeout handling for `SetDeadline` errors in probe stages.
- **Removed dead config fields** (PR #166): Eliminated `LegacyCreateContract` and `TrackUsedContracts` fields from `ContractManagerSettings` that were always `false` and had no active code paths.

---

## [v3.23.0-fix.24.29] — 2026-07-02

### Added
- **Pattern-based proxy removal** (PR #183): `proxy remove --match=<substring>` removes all proxies whose host matches a case-insensitive substring — no provider restart needed. Matching operates on host only (never port/user/pass). Persists exclusion patterns to `proxy_url.json` so URL source refreshes don't re-add matching proxies. `proxy exclude <pat>` / `proxy exclude --remove <pat>` manages the pattern list; `proxy summary` shows active patterns.
- **Fleet-wide per-proxy analytics** (PR #184): Three-tier proxy history in the hub database with delta-based ingestion, automatic rollups, and time-based retention:
  - **Tier 1 — proxy_node_hourly (90d)**: Sparse per-node per-proxy hourly deltas from cumulative provider counters. A delta tracker suppresses the first report after provider restart (counter going backwards = new baseline, no spike). Only proxies with actual activity get rows.
  - **Tier 2 — proxy_node_daily (13mo)**: Rolled up from hourly by a background goroutine using a high-water mark to prevent double-counting.
  - **Tier 3 — proxy_fleet_daily (forever)**: Fleet-wide daily aggregates with per-proxy node count.
  - Retention windows configurable via `URNETWORK_HUB_RETAIN_HOURLY_DAYS` (default 90) and `URNETWORK_HUB_RETAIN_DAILY_MONTHS` (default 13).
- **New hub read APIs** (PR #184): Three GET endpoints consumed by the new UI:
  - `/api/proxies/top?window=&sort=&node=&limit=` — proxy leaderboard with traffic/contracts/denied sorting
  - `/api/proxies/history?addr=&window=&split=node` — per-proxy time series with optional per-node comparison
  - `/api/nodes/contracts?node=&window=` — per-server won/denied contract series from node_hourly
- **Hub UI overhaul** (PR #184): Sidebar navigation with four pages:
  - **Overview**: summary cards + 5 fleet mini-charts (traffic, billable, clients, nodes, contracts/hr)
  - **Servers**: sortable/filterable node table with per-node proxy drawer
  - **Proxies**: leaderboard from `/api/proxies/top` with window pills, sort dropdown, node filter, color-coded win%, and collapsible idle section
  - **Contracts**: fleet won-vs-denied uPlot chart + per-server table sorted by win rate with green/red split bars; click a server opens its proxy drawer

### Fixed
- **Hub test CI timeout**: `TestRollupAndPrune` was processing 17000+ empty rollup hours on every run, timing out at 600s in CI. Mocked `nowFunc` to limit the rollup window to only the 3 test hours (0.06s → 0.06s, was 15s without race).

---

## [v3.23.0-fix.24.26] — 2026-06-30

### Fixed
- **`isHttpRequest` actually shipped** (upstream b6ee955): The v3.23.0-fix.24.25 entry documented this port, but the code was never added to `ip_security_dmca.go`. This change delivers it for real — plaintext HTTP/1.x request lines are detected in the DPI flow and return `dmcaAllow` before the encrypted-traffic entropy heuristic fires, preventing false-positive drops of radio/media streaming over non-standard ports.
- **Privileged-port DPI skip** (upstream 2144e33): `dmcaDetector.classify()` now allows destination ports `<1024` without payload inspection or flow tracking. A privileged port can't host a peer-to-peer/BitTorrent hole (peers, DHT, uTP, plaintext trackers all run on ephemeral/high ports), so skipping it lets legitimate non-web-standard encrypted services on privileged ports (e.g. Telegram MTProto on 443) through. Peer traffic on high ports is still inspected.
- **DoH rewrite actually shipped** (upstream b6ee955): The v3.23.0-fix.24.25 entry documented this port too, but it was also lost in the same history rewrite. Restored the full DoH refactor: dnsmessage wire-format support for Quad9/OpenDNS, parallel queries to 4 providers (Cloudflare, Google, Quad9, OpenDNS), `MinCacheTtl`/`MaxConcurrentResolutions` settings, single-flight query coalescing, local DoH support, and `TlsConfig` field for cert pinning. Merge conflict resolved by applying LE pinning to remote DoH (tunneled) while preserving system pool for local DoH (host-dialed).
- **Miss flag lost in DoH merge timeout paths** (PR #169): `dohQueryWithClientResult`'s three early-return paths (deadline, ctx cancel, timeout) were creating a new result struct with only `AddrTtls`, silently dropping a merged `Miss=true` from an already-received NXDOMAIN response. This caused the cache layer to skip caching, doubling DoH requests on resolution timeout.

### Added
- **DPI test coverage**: Ported upstream `ip_security_dmca_test.go` wholesale (13 tests). The fork's earlier DPI refactor (`f79e3c5`) brought `ip_security_dmca.go` without its test file, leaving the detector untested; this restores full upstream parity so future audits diff cleanly.

---

## [v3.23.0-fix.24.25] — 2026-06-29

### Fixed
- **DoH certificate pinning** (PR #164): DNS-over-HTTPS resolver now uses `DefaultTlsConfig()` with ISRG Root X1/X2 pinning instead of insecure `// FIXME` nil TLS config. Prevents potential MITM of DoH responses. (This was already live; PR #167's DoH rewrite was lost.)
- **`isHttpRequest` detection** (PR #167): Ported from upstream b6ee955. Detects plaintext HTTP/1.x request lines in DPI flow and returns `dmcaAllow` before the encrypted-traffic entropy heuristic fires, preventing false positives on radio/media streaming over non-standard ports.
- **Port bounds check** (PR #168): Fixed CodeQL alert #11 where `strconv.Atoi` return values on 64-bit systems could hold values outside the 0–65535 port range. Added bounds guard so invalid ports fall back to `defaultAPIPort` instead of silently wrapping.

---

## [v3.23.0-fix.24.19] — 2026-06-26

### Performance
- **Proxy summary command** (PR #151): New `proxy summary` command shows a fleet-wide overview — total proxies, up/connecting/degraded/dead counts, source breakdown (file vs URL vs internal), URL source URLs with cached/blacklisted counts, provider start time, and file paths. Reads from on-disk `proxy_health.state` and `proxy_url.json` — no provider restart required. Usage: `urnet-tools proxy summary`.
- **Hub dashboard performance overhaul** (PR #155, #156, #157): The hub dashboard now serves gzip-compressed responses, lazy-loads proxy details on demand (instead of embedding 45k inline rows), and auto-refreshes via lightweight JSON API calls instead of full HTML page fetches. Fleet-wide traffic sparkline chart and historical time series charts via uPlot.
- **Hub server-side optimizations** (PR #155, #157): Added per-IP rate limiting (60 req/min), stale node auto-eviction (15 min), gzip compression middleware, and N+1 query fix on startup (2 queries instead of 1+N).

### Added
- **Idempotent override.conf helpers** (PR #153): New `override_set_env` and `override_rm_env` shell functions ensure systemd Environment= lines in `override.conf` never duplicate regardless of how many times a toggle command is run. Fixes the bug where `toggle_lowmode off` used `rm -rf` on the entire override directory, silently wiping unrelated settings (RAMLOGS, REPORT_URL, etc.).
- **`urnet-tools report` command** (PR #154): New `urnet-tools report <url>` sets the hub report URL at runtime via `~/.urnetwork/report_url` without restarting the provider. `urnet-tools report` shows the current URL. `urnet-tools report off` disables reporting. Works for both native binary and Docker (PowerShell wrapper).
- **Hub UI overhaul** (PR #156): Completely redesigned hub dashboard with summary cards (Total Proxies, Healthy, Degraded, Earning, Active Clients), tab navigation (Nodes/History), filter bar with text search and status dropdown, fleet-wide aggregate RX/TX sparkline chart, historical time series charts with 24h/3d/7d ranges, and a slide-out drawer for per-node proxy details.
- **Dual-stage SOCKS5 + API reachability probe for URL proxies** (PR #152): The URL proxy fetch pipeline now tests both SOCKS5 protocol compliance AND API reachability through each proxy — on a single TCP connection, with DNS resolved once and cached. Proxies that can't reach `api.bringyour.com` through the SOCKS5 tunnel are caught at fetch time (within seconds) instead of wasting auth-rate-limiter slots and generating log noise.
  - Stage 1 (3s): TCP connect + SOCKS5 greeting
  - Stage 2 (5s): SOCKS5 CONNECT to api.bringyour.com:443 through the proxy
  - 100ms random stagger before each probe dial spreads 50 concurrent probes across ~5s
- **Background URL proxy reaper** (PR #152): A background goroutine re-probes cached URL-sourced proxies that weren't fully verified every 5 minutes. After 3 consecutive API reachability failures, the proxy is moved to a persistent blacklist.
- **Blacklist pruner** (PR #152): Blacklisted addresses automatically expire after 24 hours and are removed, giving them a chance to re-enter on the next URL fetch cycle. Pruner runs every 30 minutes.

### Fixed
- **URL-sourced proxies that pass SOCKS5 but can't reach the API** are now filtered at fetch time or within 15 minutes by the reaper. Previously these proxies would consume auth rate-limiter slots indefinitely and accumulate retry attempts (up to 51+ seen in production) with "network error reaching API" errors.

---

## [v3.23.0-fix.24.16] — 2026-06-25

### Fixed
- **`proxy clear` now wipes URL cache, source URLs, and systemd PROXY_URL** (PR #139, #140): `proxy remove --all` was clearing the internal config and `proxy.state`, but leaving `proxy_url.json` and systemd `Environment=PROXY_URL` untouched. The cached URL proxies and env var would re-populate sources on restart — the clear had no lasting effect. Now `proxy_url.json` is wiped entirely (cache, blacklist, source URLs) and `PROXY_URL` is stripped from any systemd override drop-in in `~/.config/systemd/user/urnetwork.service.d/`, with a systemd daemon-reload.
- **File-sourced proxies launch before URL-sourced ones** (PR #139): The proxy launch order at startup was non-deterministic (Go random map iteration), so file-based and URL-sourced proxies were interleaved — no priority for paid proxies. The proxy list is now sorted so file-sourced proxies get lower indices in the launch sequence, giving them a head start via `backoffPacer` before any URL-sourced proxies begin connecting.
- **URL fetcher waits for file-proxy warmup before first fetch** (PR #140): The URL fetcher was triggering 5-15 minutes after startup, before file proxy warmup completed (~17 min for 1000 proxies at 1s stagger). URL-sourced proxies started authing mid-warmup, competing with file proxies for auth rate-limiter slots. The URL fetcher now waits for warmup to reach `>90% up with <5 connecting`, with a 60-minute timeout fallback so a few slow file proxies don't block URL proxies forever.

## [v3.23.0-fix.24.18] — 2026-06-26

### Added
- **Multi-tier degraded proxy removal** (PR #145): `proxy remove-dead` now supports `--degraded[=<duration>]` to remove degraded proxies offline past a threshold, `--source=<url|file|internal>` to filter by source, `--yes` for non-interactive use, and `--preview` for dry-run. Previously only `dead` (never authed) and `inactive` (7+ days) were removable — the `degraded` category (authed once, now offline) was a blind spot that let zombie proxies clog the pool indefinitely.
- **Proxy activity dashboard** (PR #145): `proxy activity` opens a real-time terminal dashboard showing active proxies carrying client traffic, per-proxy bandwidth rates, client counts, and recent contract acquisition events. Auto-refreshes every second. Press `q` or Ctrl+C to exit.

### Tests added (pending next release)
- **HMAC dual-format verification test** — regression test confirming `ContractManager.Verify()` accepts both legacy (pre-July 1) and standard (post-July 1) HMAC formats, ensuring the platform cutover doesn't break provider contract verification.
- **Startup sort order test** — verifies file/internal proxies sort before URL-sourced ones.
- **backoffPacer edge case tests** — zero stagger returns immediately, context cancellation aborts non-zero stagger waits.
- **Warmup gate lifecycle test** — URL proxies deferred during warmup are launched once warmup completes.

## [v3.23.0-fix.24.14] — 2026-06-25

### Fixed
- **`urnet-tools update -f` now reliably restarts the provider** (bootstrap + FORCE fix): Two bugs fixed: (1) The `-f` flag after a positional arg (`urnet-tools update -f`) was parsed by the subcommand handler which set `force_update=1` (lowercase) for version comparison bypass, but never set `FORCE=1` (uppercase) — so `stop_systemd_units` always took the non-force path and never stopped/restarted the service. (2) The bootstrap chicken-and-egg fix ensures the tarball-bundled `urnet-tools` is picked up even by old scripts via a check in the common script-writing section.

## [v3.23.0-fix.24.13] — 2026-06-25

### Fixed
- **`urnet-tools update` bootstrap self-update fix**: The self-update code previously had a chicken-and-egg problem — the fix for fetching the latest script was in the script determination code, but the old script (which runs the update) never reached that code path because it just did `cat "$0"` (read itself from disk). Now, **even the old script** will pick up the bundled `urnet-tools` from the freshly-extracted tarball immediately before writing, because the check lives in the common script-writing section after `cd "$workdir"`. This means: (1) the script self-update works on the very first update from any previous version, (2) `-f` restart-logic propagates correctly because the new script gets installed, and (3) future updates from v24.13+ work without any bootstrap issues.
- **`urnet-tools update -f` now reliably restarts the provider**: The `stop_systemd_units` function correctly checks `$FORCE` (set by `-f`) and stops the service, and `install_systemd_units` starts it back. This was already correct in the script logic but was blocked by the same chicken-and-egg — the old script's behavior was running, not the fixed one.

## [v3.23.0-fix.24.12] — 2026-06-25

### Fixed
- **`urnet-tools update` now fetches latest script from GitHub** (PR #136): Removed the `[ -n "$URNET_INSTALL_URL" ]` guard that blocked the GitHub fetch during normal `urnet-tools update` — that env var was only set for dev/testing overrides, so updates silently reinstalled the old script from disk. Added tarball-bundled script as highest-priority source. The script is now bundled in the provider tarball (`release.yml`), enabling offline-capable update.

**Note**: v3.23.0-fix.24.12 has a chicken-and-egg bootstrap issue — the fix can't propagate to existing installs because the old script running the update never reaches the new code paths. Use v3.23.0-fix.24.13+ which fixes the bootstrap by checking `$workdir/urnet-tools` in the common script-writing section.

## [v3.23.0-fix.24.11] — 2026-06-25

### Added
- **Important log buffer** (PR #135): New 1 MB `/dev/shm/urnetwork-important.log` secondary RAM buffer holding only high-value lines (`[profit]`, `[earn]`, `[health]`, `[outage]`, `client_id`, `instance_id`, `Permanently removed`, `[proxy][authrate]`) so the earnings/health status survives for hours even when the main 5 MB ramlog floods in ~84s. High-volume lines (per-proxy enumeration, per-attempt auth, `give-up`, `[net][s]select`) are deliberately excluded. `urnet-tools logs -i` / `--important` streams it. `isImportantLogLine` is a tested pure function.
- **Consolidated `[profit]` line** (PR #134): Single greppable line printed on each health heartbeat: `[profit] earning=yes|no reason=-|warmup|no_proxies|idle|no_traffic clients=N rate=X MB/s proxies_up=N serving=M idle=K`. Reason tells operators *why* when not earning without cross-referencing other lines. `[earn]` and `[traffic]` line formats left unchanged.

### Changed
- **Unified `logThrottle`** (PR #135): Four byte-identical `shouldLogX` rate-limiters (auth/select/write in `transport.go`, oob in `transfer_contract_manager.go`) collapsed into one reusable `logThrottle` type (one-line-per-interval + suppressed counter). `shouldLogX` stay as thin wrappers; call sites are unchanged.
- **Dropped per-proxy enumeration on reload** (PR #134): `reload()` no longer prints one line per added proxy address. The `+N added, -M removed` summary and the cold-start `Using N proxy servers:` roster are kept.

### Fixed
- **URL-sourced proxy give-up backoff is now enforced at launch** (PR #133): The escalating backoff (PR #129) computed a 15m→24h retry delay and scheduled a one-shot `time.AfterFunc` reload, but nothing gated relaunch — `reload()` re-added every desired-but-not-running proxy immediately (checking only `isDraining`), so dead proxies were relaunched every few minutes regardless of their schedule (proxies reached give-up 9 within 44 minutes of uptime; 70% of log volume was churn). `proxyFailureHistory` now records a per-address next-eligible time on each give-up, `reload()` skips addresses whose window has not elapsed and reports `N deferred (backoff)`, and `Reset`/`Prune` clear the window alongside existing counters. Frees auth-rate-limiter slots for live proxies in addition to cutting churn.
- **`urnet-tools -v` reports running vs on-disk version separately** (PR #132): `show_version` ran the on-disk binary's `--version`, so right after a plain (non-forced) `update` it reported the freshly-installed binary while the old image kept serving traffic, with no hint a restart was pending. It now resolves the running version through `/proc/<pid>/exe` (accurate even after the file is renamed to `.old` or deleted), prints separate **Running version** and **Installed on disk** lines, and warns with the restart command when they drift. Also catches pre-`--version` binaries via `(deleted)`/`.old` exe link detection.
- **`[c]ping err` log spam suppressed** (PR #131): Repeated identical `[c]ping err = Send sequence closed` messages during network outages now aggregate into one line per 5 minutes with `(N suppressed)` count. During the Detroit Hostodo transit outage this was producing 1,992+ identical lines over 2 days.

---

## [v3.23.0-fix.24.10] — 2026-06-24

### Added
- **Per-minute earning windows** (PR #121): New `runEarningWindows` goroutine emits `[earn] billable_1m=X billable_5m=Y billable_15m=Z billable_60m=W active=yes|no` every 60 seconds, decoupled from the ~5-minute health heartbeat. Operators see earning changes within 1 minute instead of waiting for the next heartbeat tick. Rolling 60-minute ring buffer with counter-reset guard for proxy restarts. Partial windows displayed during warmup.
- **URL-sourced proxy give-up backoff and eviction** (PR #129): Replaces the flat 15-minute give-up-to-retry delay with an escalating per-address backoff (15m→30m→1h→2h→4h→8h→16h→24h, +20% jitter). Permanently evicts addresses after 10 give-up cycles via a persisted blacklist in `proxy_url.json`, so a hopeless proxy can never re-enter the auth-rate-limiter lottery across restarts. Fixes a concurrent bug where `Prune` silently wiped give-up/failure counters during the wait window by using the full desired address set (file/internal + URL cache) instead of the live health registry.

### Fixed
- **`paceMonitor` goroutine now exits after warmup completes** (PR #122): The `✓ warmup … done` message was being re-emitted every 30 seconds indefinitely because the ticker loop had no terminal state. Added `return` after the done log line — the goroutine now exits as soon as `pct > 90 && connectingN < 5`, printing the completion line exactly once.
- **File-based proxies now start before URL-sourced proxies on boot** (PR #127): `runProxyURLFetcher` and `runProxyURLCleanup` goroutines were launched at line 1583, before the proxy-file reloader was ready — URL proxies would race ahead of operator-curated file proxies. Moved both goroutine launches to line 2056, after `reloader.StartWatcher(ctx)` ensures file proxies are loaded first.

---

## [v3.23.0-fix.24.7] — 2026-06-23

### Fixed
- **Operator-curated proxies never give up on auth** (PR #119): File, internal, and direct proxies that exhaust `maxAuthFailures` now switch to a slow capped retry (5m → 10m → 15m with jitter) instead of going permanently offline. URL-sourced proxies retain the short leash — they give up and the `time.AfterFunc` requeue brings them back after 15 minutes.
- **URL requeue decoupled from reload engine** (PR #120): Replaced the per-proxy `scheduleGiveUpRequeue` goroutine (15m sleep + cooldown map + trigger write + 30s recheck) with a single `time.AfterFunc` per proxy. Deleted the entire `proxyGiveUpCooldown` map/mutex/functions (~110 lines). `reload()` now acquires `proxy.lock` before proceeding, serializing hot-reloads with cross-process operations like `proxy remove-dead` and URL source fetches.

---

## [v3.23.0-fix.24.5] — 2026-06-22

### Fixed
- **SOCKS5 probe no longer runs on file/internal proxy lists**: The pre-auth SOCKS5 probe was running on every proxy, including operator-curated paid lists from proxy files. File-based and internal proxies now skip the probe and go straight to auth. URL-sourced proxies added via hot-reload are still probed.

---

### Performance
- **Raised default throughput ceilings** (`transport.go`, `ip.go`, `tuning.go`, `transfer_contract_manager.go`):
  - TransportBufferSize 1 → 16 (removes in-flight message bottleneck between framer and WebSocket writer)
  - TCP/UDP MaxWindowSize 1 MiB → 4 MiB (removes ~160 Mbps per-connection throughput ceiling at 50ms RTT)
  - applyTier3 now sets actual performance values for `URNETWORK_PROFILE=auto` on 4GiB+ nodes (4 MiB TCP window, 256-depth IP buffers, 4 MiB transfer queues)
  - Tier 4 Extreme auto profile added for >= 8 GiB nodes (8 MiB windows, 16 MiB queues, 512 seq buf, GOGC 200, contract ramp scale 3)
  - ContractTransferByteSeqScale 4 → 2 (reaches 128 MiB standard contract in 2 sequences instead of 4)
- **Relaxed client-side auth rate limiter** (`provider/auth_rate_limiter.go`):
  - Default min 1 → 20 req/s, max 10 → 200 req/s, burst 3 → 50
  - Added `URNETWORK_AUTH_UNLIMITED=true` env var to bypass the limiter entirely
  - Server-side ConnectionRateLimit already caps auth connections; the client limiter was serializing fleet warmup unnecessarily
- **CPU-scaled MultiRaceClientCount** (`ip_remote_multi_client.go`):
  - Races more providers per connection based on available CPU cores (4-12 instead of fixed 2)
  - 1-2 cores → 4, 3-4 cores → 6, 5-8 cores → 8, 9+ cores → 12
- **Dynamic ContractFillFraction based on RTT** (`transfer.go`, `transfer_rtt.go`):
  - Fill fraction adapts to observed RTT: 0.85 at low RTT (≤100ms) → 0.50 at high RTT (≥1000ms)
  - Prevents pipeline stalls on high-latency links while filling closer to capacity on fast links
- **Sharded packet dispatch** (`ip.go`):
  - Single-goroutine dispatch loop replaced with N shard goroutines (one per CPU, capped at 16)
  - Packets routed via deterministic FNV-1a flow hash of IP 4-tuple for per-flow affinity
  - Independent buffer instances per shard eliminate dispatch CPU bottleneck on multi-core nodes

### Added
- 11 new unit tests covering MeanRtt, computeFillFraction, flowHash, pickShard, auth unlimited mode, and CPU-scaled race count

### Changed
- Updated FORK_CHANGES.md with sections 43-47
- Updated progress.md with comprehensive codebase analysis findings

---

## [v3.23.0-fix.24.1] — 2026-06-21

### Fixed
- **MultiRaceClientCount set to 16 unconditionally**: Replaces the CPU-based tier system with a simple constant — 16 racers on all platforms. The race is double-bounded at runtime by actual healthy provider count and per-flow packet budgets, so the value acts only as a ceiling. Higher values on single-core nodes were previously held back by a conservative CPU heuristic that had no basis in the actual resource cost (goroutines are I/O-bound, not CPU-bound).

### CI
- **Docker build depends on test success**: `build-and-push` now `needs: test-and-lint`, saving ~3 min of Docker build compute on every failing PR.
- **Go module cache restored**: Re-added `~/go/pkg/mod` cache alongside existing build cache. Saves ~30s per run.
- **Test order optimized**: Go tests (with `-race`) now run before shell installer tests for faster failure feedback.

---

## [v3.23.0-fix.23] — 2026-06-20

### Added
- **SOCKS5 handshake probe**: The per-proxy TCP connect probe now performs a full SOCKS5 handshake (greeting + response) instead of a bare TCP connect, eliminating false positives from hosts that accept TCP but aren't functioning SOCKS5 proxies.
- **Bounded auth concurrency**: In-flight auth attempts are now limited to 5 via a concurrency semaphore, preventing resource exhaustion on large proxy lists.
- **Rate limiter heartbeat**: When the adaptive auth rate limiter is pinned at its floor (1 req/s), a `[proxy][authrate]` heartbeat line is logged every 60 seconds so operators can see the limiter is actively engaged.
- **Contract logs no longer rate-limited**: Contract lifecycle events (`[contract] acquired`, `[contract] denied`, `[contract] oob`) are now logged unconditionally for complete auditability.
- **`[traffic]` total with earning signal**: The `[traffic] total` line now includes `earning=yes|no` and `billable_today=X` fields for immediate visibility into whether a proxy is generating earnings.
- **Failure count resets on successful auth**: Per-proxy failure counters are reset to zero when a proxy successfully authenticates, preventing historical failures from skewing health tracking.
- **Non-429 retry delay scales with attempt**: Transient errors (non-429) now also scale their retry delay with attempt number (capped at 60s), instead of using a flat jitter.
- **Unproven overload streak decays gracefully**: The overload streak counter for unproven proxies now decays over time rather than clearing fully, preventing rapid re-triggering of backoff.
- **Give-up requeue on reload**: Proxies that gave up due to exhaustion of auth retries are re-queued for retry if a hot-reload cycle doesn't pick up their address (via file or URL source change).
- **RAMLOG ring buffer**: The in-memory log buffer now uses a ring-buffer design that keeps the newest ~3.3 MB and discards the oldest ~1.7 MB when full, rather than a simple truncation.
- **ControlPingTimeout enabled (30s keepalive)**: Active keepalive pings on control-plane connections detect silent drops within 30 seconds.
- **Stale proxy.lock detection**: On startup, the provider detects and cleans up stale `proxy.lock` files left behind after a crash.
- **Overlapping fetch cycles prevented**: Concurrent `PROXY_URL` fetch cycles for the same URL are now skipped with a log warning.
- **CLI commands now lock properly**: All CLI management commands (`proxy refresh`, `proxy remove-dead`, etc.) acquire the proxy lock to prevent concurrent operations from corrupting state.
- **Memory leak fix (failure history + proven set pruning)**: Internal failure history and proven-set data structures are periodically pruned of stale entries, preventing unbounded heap growth.
- **Custom HTTP client for URL fetches**: URL fetch subsystem uses a dedicated HTTP client with 30s connect / 60s response timeouts, isolated from the provider's control-plane transport.
- **Admission gate slot leak fixed**: All error return paths in the admission gate now properly release acquired slots, preventing gradual exhaustion of the admission budget.
- **Proxy URL Sources**: Point the provider at a live proxy list URL (`--proxy_url` / `PROXY_URL`, comma-separated for multiple sources) instead of, or alongside, a static `proxy.txt`. Fetches on an interval (default 15m), merges new entries into the existing hot-reload pipeline without disturbing already-warmed-up proxies, and supports scoped automatic dead-proxy cleanup (`--proxy_dead_cleanup_scope=url|all|none`) so a noisy public list can self-prune without touching hand-curated entries. See [Proxy URL Sources](docs/Proxy-URL-Sources.md).
- **Hot-reload added-proxy visibility**: When a hot-reload adds proxies, the provider now prints the same `Using N proxy servers:` + per-proxy listing used at startup, instead of only logging removals. Makes it trivial to confirm a `--proxy_file` or `--proxy_url` reload actually picked up new entries.
- **Global adaptive auth rate limiter**: All proxy auth attempts (first tries and retries alike) now funnel through a single shared, self-tuning rate limiter instead of relying on uncoordinated per-proxy backoff. The limiter starts at the believed safe ceiling, halves on a 429 from the auth API, and creeps back up after a sustained run of clean attempts — bounding aggregate load on a fragile upstream API without serializing startup of large proxy fleets.

### Resilience
- **Proactive JWT refresh**: JWT tokens now refresh automatically every 7 days (regardless of expiry), ensuring tokens never expire under normal operation. Includes 48-hour expiry fallback as safety net if periodic refresh fails. Startup jitter (0-9 minutes) desynchronizes fleet refresh attempts. Works across all auth modes (`BUILD=jwt`, `BUILD=stable`, `BUILD=nightly`) via JWT-to-JWT renewal. Last refresh timestamp persisted to disk to survive provider restarts.

### Security
- **QUIC Memory Exhaustion vulnerability**: Bumped `quic-go` to `v0.59.1` to resolve a vulnerability where an unauthenticated remote attacker could cause excessive memory allocation during the handshake.

### Telemetry
- **Increased RAMLOGS size**: Capacity expanded from 1MB to 5MB for larger diagnostic windows on high-volume nodes.
- **Enhanced `logs` command**: `urnet-tools logs` now supports `all`/`full` (stream entire buffer) and `dump` (save current buffer to `~/urlogs.txt`).
- **Docker CLI parity**: `urnet-tools` is now natively available inside the container, allowing operators to use the same management commands across all deployment types.

### Fixed
- **Proxy refresh status check failure**: Fixed a bug where `urnet-tools proxy refresh` failed with `FATAL [exit 51]: provider does not appear to be running` on first startup with 0 proxies. The provider now unconditionally writes the `proxy.state` file at startup and heals zero timestamps during heartbeat execution.
- **HTML error pages logged verbatim**: Auth failures that returned an HTML error body (e.g. a 429 from a rate-limiting proxy in front of the API) used to dump the entire page into the log. Now collapsed to `<html error page, N bytes>`.
- **429 auth retries used the same flat backoff as ordinary errors**: A rate-limited auth attempt now waits proportionally longer on each subsequent retry (capped at 60s) instead of the same flat 0.5–10.5s jitter used for transient network errors — so a batch of proxies hitting 429s together backs off instead of immediately re-hammering the API.
- **Sub-hour retry durations logged as `0h Nm`**: Duration formatting now omits the redundant hours segment when there are none, e.g. `15m` instead of `0h 15m`.
- **URL-sourced proxies stuck after exhausting auth retries**: A proxy whose addresses came from `--proxy_url` now automatically retries 15 minutes after giving up, instead of waiting for an hourly pulse that only file/manually-added proxies receive.
- **Installer/update JSON parsing broke under `dash`**: `Provider_Install_Linux.sh` passed raw GitHub API responses to `jq`/`python3` via `echo`, which interprets backslashes natively under `dash` (Debian/Ubuntu default `/bin/sh`) and silently corrupted JSON containing escape sequences. Switched to `printf "%s"`, added a silent-failure fallback to the script's web-scraping path, and removed bashisms (`[[ ... ]]`) from the test suite so it runs cleanly under strict POSIX `sh`.

### Documentation
- **Production-ready Docker guide**: Added recommended deployment patterns to README for persistent telemetry and auto-tuning.

---

## [v3.23.0-fix.21.2] — 2026-06-16

### Fixed
- **Message pool Share/Return race**: When a buffer was returned to the pool while another goroutine was concurrently sharing it, there was a narrow window where the buffer could be handed out to a third goroutine before the share completed — silently corrupting in-flight packet data. Metadata reset is now performed under the lock.
- **Orphaned buffer leak in proto serialization**: `ProtoMarshalWithTag` grabbed a pool buffer based on an estimated size. If protobuf's actual output exceeded the estimate, the library allocated a fresh backing slice and abandoned the pool buffer without returning it. The call site now detects the reallocation and explicitly returns the orphaned buffer.
- **Hub report failures were silent**: Non-2xx responses from the hub now log `[report] hub rejected report: <status>` instead of silently succeeding.
- **`TestMessagePoolShare` was failing**: The assertion was checking against the old maximum bucket size, predating the pool's larger bucket additions (16 KiB, 32 KiB, 64 KiB). Fixed to reflect current pool structure.

### Added
- **CI full test suite**: Replaced hardcoded `-run` allowlist with `go test -short -race ./...`. New tests are discovered automatically and Go's race detector runs on every build.
- **Sibling-fork drift monitor**: Daily CI job checks `full-bars/connect` for new commits to critical files and posts a Discord alert.
- **Bandwidth reporter startup jitter**: Reporter now waits a random duration (up to one full interval) before first post, preventing thundering-herd on fleet restart.

---

## [v3.23.0-fix.21.1] — 2026-06-16

### Performance
- **O(1) proxy health lookup**: Rewrote `proxy_health.go` tracker to use an address-based pointer index map, eliminating global mutex contention and `O(N)` array scans on every bandwidth update.
- **Dial latency metrics**: Injected `dur=Xms` field into all `[net][s]select` logs for real-time per-strategy latency visibility.
- **`[earn]` utilization log**: Added `[earn] proxies_up=N serving=M idle=K clients=C` summary to the periodic heartbeat.

### Fixed
- **TLS session deadlock**: Resolved a lock-ordering inversion in `EncryptionSessionManager` where idle-reaping and concurrent TLS handshakes could deadlock the provider permanently. Refactored to a push-model architecture.
- **`[traffic]` unit suffix**: Fixed formatting bug where `[traffic] total` printed `MB/s/s` instead of `MB/s`.
- **Docker tag consistency**: CI now preserves the full git release tag string; `docker pull` with exact semver no longer triggers `manifest unknown`.
- **Atomic binary replacement**: `urnet-tools update` now uses an atomic `mv` to bypass "Text file busy" on live binary replacement.
- **[contract] acquired/denied log**: Added log signal for contract lifecycle visibility.

---

## [v3.23.0-fix.21] — 2026-06-16

### Added
- **`urnet-tools hub` commands**: `hub set <url>`, `hub off`, `hub install` — configure bandwidth hub reporting without manual systemd edits.
- **Hub dashboard polish**: Fixed UI state tracking on auto-refresh, added natural sort for all column types, persistent sort directions, billable traffic highlighting, delta-time division guard.
- **Per-proxy failure diagnostics**: Dead/degraded proxies in `[health][proxies]` now include inline failure breakdown (`auth:N`, `timeout:N`, `drops:N`, `last_err`).

### Fixed
- **Auth error message clarity**: Proxy timeouts now log `"network error reaching API"` instead of suggesting JWT expiry.

### Internal
- **Logger interface migration**: All 357 `glog.*` call sites across 21 core files migrated to the `Logger` interface, eliminating merge conflicts on upstream rebases.

---

## [v3.23.0-fix.20.1] — 2026-06-14

### Fixed
- **`urnet-tools update` resilience**: Gracefully falls back to version string comparison when API response parsing fails; no longer aborts with a misleading error.
- **Parser optimization** (upstream PR#185): Fast-path protocol buffer parser and streamlined 120-second contract expiry cleanup adopted from upstream.

---

## [v3.23.0-fix.20] — 2026-06-12

### Fixed
- **HMAC Contract Verification Format Migration**: Implemented dual-format HMAC verification to support the upstream platform's contract signing format migration on July 1, 2026. Providers now verify both legacy (pre-July 1) and standard (post-July 1) HMAC formats seamlessly, ensuring continuous operation through the platform cutover with zero performance impact.
- **Write Error Log Suppression (QUIC)**: Fixed write error log flooding in the QUIC (runH3) transport path. Both WebSocket (runH1) and QUIC (runH3) write errors are now rate-limited to one log message per minute globally, with suppression counts reported to reduce noise during backend outages.

---

## [v3.23.0-fix.18.4] — unreleased

### Added
- **Proactive JWT Renewal**: The provider now checks the JWT expiry once per hour. When the token is within 48 hours of its `exp` claim, it proactively calls the auth API for a replacement and writes it to disk — no restart, no exit-78 blip. The check runs immediately at startup and every hour after. If the API is temporarily unreachable, it retries on the next cycle.
- **shmLogFatal**: All fatal error paths now write a `FATAL [exit <code>]: ...` line directly to the ramlog file before terminating (bypasses the pipe goroutine, so the message is never lost to a race on exit). Also writes to stderr for Docker logs. Works regardless of whether ramlogs are enabled.
- **Unique Exit Codes**: Every failure path now has a documented exit code so operators can triage from the exit code alone. See `FORK_CHANGES.md#exit-code-reference` for the full table.

### Fixed
- **Ramlog Race on Fatal Exit**: When the provider exited with code 78 (expired JWT), the error message was written to the stdout/stderr pipe but the process often died before the ramlog goroutine could flush it to `/dev/shm/urnetwork.log`. `shmLogFatal` writes directly to the file, sidestepping the race entirely.

---

## [v3.23.0-fix.18] — 2026-06-07

### Added
- **Unified Proxy Telemetry**: Completely overhauled the proxy tracking system. "Total Tx/Rx" now reflects the exact raw bytes on the wire for ALL traffic types (H1, H3, and NAT), providing 100% accurate billing vs wire-usage transparency.
- **Dialer Session Tracking**: The `CLIENTS` counter and `clients=N` log field now track active connections at the dialer level. This ensures that internal provider heartbeats and platform transports are correctly reflected in the load metrics, resolving the "clients=0" confusion on idle/health-check nodes.
- **Proxy Session Timers**: Added tracking for connection longevity. A new "MAX AGE" column in the traffic report and an `age=...` field in logs help identify zombie or stuck connections across the fleet.
- **Docker Built-in Aliases**: Added `proxy-traffic` and `logs` commands directly to the container. Operators can now use `docker exec -it <name> logs` to instantly tail RAMLOGS (resolving the empty `docker logs` issue) and `proxy-traffic` for quick load checks.
- **JWT Smart Refresh**: Implemented local `exp` claim validation and self-healing shell logic. Providers now detect expired tokens before network calls, exiting with code 78 to trigger automatic re-authentication in entrypoint scripts.

### Fixed
- **NAT Bandwidth Visibility**: Fixed a bug where NAT session bandwidth was not being included in the "TOTAL (TX/RX)" columns of the traffic report.
- **Counter Double-Counting**: Removed redundant atomic increments across `ip.go` and `transport.go`, consolidating all session and wire-traffic tracking into a single atomic source of truth in the dialer layer.
- **Auth Panic Guard**: Replaced multiple `panic` calls in `provideAuth` with structured error returns. Added nil-guards for `authClientResult` to prevent crashes on malformed API responses.

---

## [v3.23.0-fix.17] — 2026-06-05

### Added
- **Proxy Hot-Reload**: Live add/remove proxies via `urnet-tools proxy refresh` and `urnet-tools proxy remove-dead` without restart. Proxy slots are now stable across reloads.
- **Provider Logs Command**: Added `urnet-tools logs` to stream current RAMLOGS buffer and tail live logs automatically.
- **Per-Proxy Bandwidth Tracking**: Tracks cumulative billable and total bandwidth per proxy, visible via `urnet-tools proxy traffic`. Survives RAMLOGS rotation.
- **Active NAT Session Tracking**: The `[net][s]select` logs and traffic report now include a `clients=N` field to track active NAT sessions multiplexed through each proxy.
- **E2E Post-Quantum Encryption**: Ported upstream PR #183 adding ML-KEM/Kyber hybrid encryption and hardened `CloseContract` delivery (disabled by default in this release).
- **Global Tool Versioning**: `urnet-tools` subcommands now accept a `-t <tag>` flag for pinning to specific versions.

### Fixed
- **[net][s]select Error Spam**: Implemented rate-limiting for `[net][s]select:` error logs during backend outages. Errors are now suppressed to one log line per minute with a suppression count.
- **Clients Counter Bug**: Fixed a regression from the rc.1 pre-release where the clients counter was not being copied into bandwidth snapshots, causing it to display as 0.

---

## [v3.23.0-fix.16] — 2026-06-02

### Added
- **Dead-Proxy Health Report**: The `[health]` heartbeat now emits `[health][proxies]` lines listing `dead` (never authenticated) and `degraded` (worked before, down now) proxies, plus `recovered`/`lost` and `lifetime_recovered`/`lifetime_lost` counters that make the hourly retry pulse's effectiveness visible. A `[pulse]` marker logs each retry sweep. Full dead/degraded lists and a transition history are mirrored to `proxy_health.state` and `proxy_health.log` on the config volume (survives RAMLOGS), readable via `urnet-tools proxy health` (host) or `proxy-health` (Docker).
- **Per-Proxy Health Tracking**: The `[net][s]select` log now includes the proxy index and IP address when running a proxy list (e.g., `proxy[42] (1.2.3.4:1081) [fragment] success=100 error=2`). This allows operators to easily identify and remove failing or "black hole" proxies from their deployment.
- **Active Connection Counter**: Added `connections=N` to the `[health]` heartbeat log. This provides real-time visibility into the number of active TCP and UDP proxy sessions directly from the standard output.
- **Active Proxy Counter**: Added `proxies=N` to the `[health]` heartbeat log, counting authenticated proxy transports currently live on the platform. Unlike `connections` (end-user NAT sessions), this reflects how many proxies from your list are actually working, so a node with no users still reports a non-zero value (e.g. `connections=0 proxies=1188`).

---

## [v3.23.0-fix.15.4] — 2026-05-29

### Added
- **Force Update Flag**: `urnet-tools update -f` (or `--force`) now bypasses the version check and re-downloads/reinstalls even if the installed version matches the available version. Useful when a release tag is re-tagged with updated binaries or for manual recovery.

---

## [v3.23.0-fix.15.3] — 2026-05-29

### Fixed
- **Log Spam**: Removed redundant "Reporting to dashboard" log that was emitted on every auth retry, causing noise during startup or API errors. The `client_id` and `instance_id` logs already signal successful provider startup.
- **[r]drop Rate-Limiting**: Implemented rate-limiting for `[r]drop` errors, now suppressed to 1 per minute globally with suppression count. Prevents log flood during backend timeouts (similar to existing `[t]auth` and `[contract]oob` suppression).

### Documentation
- **README Restructure**: Major overhaul of the README for better clarity and organization. Moved detailed technical guides (Installation, Docker, Scaling, Tuning, Configuration) into a dedicated `/docs` directory to keep the main page focused on essential information.

---

## [v3.23.0-fix.15.2] — 2026-05-28

### Documentation
- **README Standardization**: Overhauled all `docker run` and `docker compose` examples to include optimized sysctls and automatic hostname detection by default.
- **Improved Clarity**: Standardized container names to `urfix` and volumes to `${NAME:-urfix}` for safer copy-pasting. Corrected the environment variables table to reflect refined identity logic.

### Fixed
- **Dashboard Reporting**: Optimized identity logic to avoid redundant `IP @ IP` strings. If no name is provided, only the redacted public IP is reported.

---

## [v3.23.0-fix.15] — 2026-05-28

### Added
- **Installer Root Guard**: The Linux installer now detects if it is being run as root and offers an interactive menu to create a dedicated service user (`urnet`) with the correct permissions. This prevents "Failed to connect to bus" errors caused by root's lack of a user session bus.
- **Assisted User Setup**: Automatically handles user creation, admin group detection (`wheel` or `sudo`), and systemd lingering enablement across diverse Linux distributions.
- **Hardened User Hand-off**: Implemented a robust `runuser` mechanism that handles SELinux-enforcing environments (like openSUSE and AlmaLinux) by ensuring correct environment propagation (`XDG_RUNTIME_DIR`, `DBUS_SESSION_BUS_ADDRESS`) and directory transitions.
- **Automatic Server Hostname Reporting**: Containers can now automatically report the host's actual server name via the `HOST_HOSTNAME` environment variable (e.g. `-e HOST_HOSTNAME=$(hostname)`).

### Fixed
- **Dashboard Identity Format**: Refined the identity reporting format to strictly follow `Name @ IP [Version]`. Version strings are now consistently enclosed in brackets, and random 12-char hex container IDs are automatically hidden and replaced with "provider" to keep the dashboard clean.
- **Timezone Consistency**: Standardized the default timezone to `America/Tijuana` across all entrypoint scripts (`start_jwt.sh`, `start_stable.sh`, `start_nightly.sh`) for consistent log timestamps and update watcher timings.
- **Proxy Command Syntax**: Fixed a bug in `urnet-tools proxy add` where an extra argument shift caused the file path to be misidentified as a command.
- **Auto-Tune Log Spam**: `[tune] auto-profile` was logged once per proxy server on startup instead of once per process. With large proxy lists this produced thousands of identical lines. The log is now emitted exactly once; per-proxy settings application is unchanged.
- **Eco Monitor Duplication**: The eco memory monitor goroutine was started inside the per-proxy loop, spawning one monitor per proxy. Under memory pressure, all copies would log the same `[eco]` line and call `runtime.GC()` simultaneously. The monitor now starts exactly once per process regardless of proxy count.

### Documentation
- **User-Level Service Guide**: Updated README with instructions on the new recommended non-privileged deployment path.
- **Generic Server Naming**: Standardized documentation to use generic node references for privacy.

---

## [v3.23.0-fix.14.4] — 2026-05-28

### Documentation
- **Streamlined Multi-Container Scaling**: Documented the "Shared JWT" method for running three nodes in a single `docker-compose.yml` with one auth code and shared storage.
- **Improved RAM Logging Guide**: Added a comprehensive `docker run` sample command with all common flags.
- **Expanded Outage Alerting Guide**: Added a detailed `docker run` example for setting up Discord/Slack/ntfy webhooks.

### Added
- **Environment Variable Authentication**: Added support for `URNETWORK_AUTH_CODE`. This allows providing auth tokens (especially those starting with dashes) without command-line parsing issues in Docker.
- **Dashboard Identity Reporting**: All provider builds (JWT, Stable, Nightly, and Pelican) now automatically detect their public IP (via `ip.me`, with a 5s timeout) and report `NodeName @ redacted-IP [Version]` to the backend for easier identification. The IP is redacted to `first.x.x.last`. This is always on and requires no configuration; it is distinct from the opt-in `ENABLE_IP_CHECKER` diagnostic, which logs the full IP locally.

### Fixed
- **Shared-volume crash safety (jwt build)**: The provider restart loop no longer deletes the JWT after repeated crashes. In the shared-config multi-node model that would have deauthenticated the entire stack with no automatic recovery (the auth code is single-use). After repeated crashes the container now exits cleanly for Docker's restart policy to cycle it, leaving the session intact.
- **Restart loop reliability (jwt build)**: Fixed a `provide || true` pattern that made the crash counter always read success, so the in-script restart/backoff never engaged on a real crash.
- **Multi-node startup race**: The 3-in-1 scaling guide now uses a healthcheck on the first node plus `depends_on` so secondary nodes wait for the shared JWT instead of crash-looping on first boot.
- **Graceful ZRAM handling**: Systems with kernels that don't include zram support (e.g., Oracle Linux UEK) now complete `optimize` successfully. ZRAM is skipped with a simple warning; other OS optimizations (sysctl, ulimits) continue normally. Users on Ubuntu can optionally install Zabbly kernel to gain zram support.

## [v3.23.0-fix.14.2] — 2026-05-28

### Fixed
- **Update command reliability**: Fixed silent update failures on systems missing `jq` or `python3`. Update now fails loudly with clear instructions to install missing JSON parsing tools instead of reporting "up-to-date" when it couldn't verify version dates.

## [v3.23.0-fix.14.1] — 2026-05-28

### Fixed
- **Ubuntu 20.04→24.04 upgrade compatibility**: Hardened `setup_zram_manual()` fallback for systems where the zramswap service fails after distro upgrades. Now tries dynamic module allocation first, implements module reload recovery on disksize config failure, and adds lz4 compression fallback if zstd is unavailable. Adds sysfs permission checks and timing delays to prevent race conditions.

## [v3.23.0-fix.14] — 2026-05-27

### Added
- **Auto-Tune Performance Profile**: New `URNETWORK_PROFILE=auto` dynamically selects buffer sizes and contract floors based on available RAM (Low/Balanced/Performance tiers). Automatically enables Eco Mode on RAM-constrained systems and enables RAM Logging if slow disk I/O is detected. Managed via `urnet-tools auto on/off`.
- **System Optimizer**: New `urnet-tools optimize` command (requires root) to apply "Golden Fleet" network tuning to the host:
  - **Auto-Installation**: Automatically installs `conntrack-tools` and `zram` on supported distros.
  - **Interactive Protection**: Detects pre-optimized states and asks for confirmation before overriding (skip with `-f`).
  - **Boot Persistence**: Configures `/etc/modules-load.d` to ensure `nf_conntrack` loads early.
  - Ulimit bumped to 1,048,576.
  - Conntrack max raised to 2,097,152.
  - TCP established timeout reduced to 1 hour (from 5 days).
  - Enabled **BBR** congestion control and **Fair Queuing (fq)** for improved network throughput.
  - Expanded local port range and enabled TCP port reuse.
- **System Auditor**: Provider now checks OS limits (ulimit, conntrack) and performs a dynamic Disk I/O test on startup. Logs high-signal warnings for suboptimal host limits or low disk space.
- **Message pool auto-sizing**: `InitialMessagePoolByteCount` now scales to RAM/32 at startup (floor 8 MiB, cap 256 MiB) instead of the hardcoded 1 MiB default. The 1 MiB default is far too small for large proxy list deployments — almost every packet above the pool cap fell back to a fresh GC allocation, adding unnecessary GC pressure. Skipped for `lowmem` profile and when `--max-memory` is set explicitly. Logs `[pool] message pool NMiB (RAM=NMiB)` once at startup.
- **Health heartbeat**: logs `[health] uptime=X profile=Y heap=ZMiB sys=WMiB` every 5 minutes. Passive liveness confirmation and heap trend visibility without external tooling. Interval configurable via `URNETWORK_HEALTH_INTERVAL`.

### Changed
- **Outage detection and alerting**: a background watcher polls `IsBackendDegraded()` every 30 seconds and logs `[outage] backend degraded` / `[outage] backend recovered` on state transitions. Runs always, no configuration required.
  - Requires 10 consecutive failures (5 minutes) before firing `outage_start` to eliminate false positives from brief network blips.
  - Requires two consecutive clean polls before firing `outage_clear` to avoid premature all-clears during brief mid-outage lulls.
  - Per-event 5-minute cooldown prevents webhook spam when the backend flickers at the recovery boundary.
- **RAM Log Tail Depth**: `urnet-tools logs` now displays the last 250 lines of history (up from 10) when in RAM logging mode.

### Fixed
- Fixed a potential panic in the health monitor when reading metrics on certain Go versions.
- `[turbo]` startup log now fires once at provider startup instead of once per proxy goroutine.
- `provide()` was missing a `ResizeMessagePools` call when `--max-memory` was set.
- **Watchtower Persistence**: `start_jwt.sh` now correctly reuses existing sessions, preventing "Invalid auth code" panics after image updates.
- **Installer date parsing**: Fixed Python 3.10 fromisoformat() error when parsing GitHub release dates with ISO 8601 `Z` timezone suffix.
- **Root installation handling**: Installer no longer exits on systemd enable failure when running as root in Docker containers (no user session bus). Warns gracefully instead.
- **Systemd lingering**: `urnet-tools optimize` now auto-enables lingering (`loginctl enable-linger`) for the detected user, ensuring systemd --user services persist after logout. The installer defers this to optimize (which already prompts for sudo if needed) to keep the install step zero-privilege.
- **Installer Robustness**: Fixed "No download URL" errors by implementing a robust `latest` tag resolution fallback (using GitHub redirects) and direct URL construction. This bypasses issues where the GitHub API returns malformed JSON.
- **Proxy Management**: Added `urnet-tools proxy add <file>` and `urnet-tools proxy clear` commands to simplify bulk proxy operations.
- **Robust ZRAM Deployment**: Implemented a "Universal Manual Fallback" for ZRAM that uses direct kernel commands (`zramctl`, `swapon`) if the distro-specific systemd service fails. Ensures ZRAM works reliably across all environments.

---

## [v3.23.0-fix.13] — 2026-05-27

### Fixed
- JWT is no longer deleted on every container start in `start_stable.sh`. The startup script now checks for an existing JWT at `/root/.urnetwork/jwt` and skips authentication entirely if one is found. This makes container restarts and Watchtower image updates seamless — the provider starts immediately without re-hitting the auth API. A persistent volume at `/root/.urnetwork` is required for this to survive container recreation.
- Auth failures in the provider binary no longer `panic` (which produced unreadable stack traces in Docker logs). They now print a clean error message to stderr and exit with code 1, allowing the shell restart loop to handle retries. Auth code failures include a hint about volume persistence.
- Provider binary now exits cleanly after 10 consecutive auth API rejections (expired or revoked JWT) so the shell restart loop can delete the JWT and re-authenticate. Previously the binary looped internally forever, making recovery impossible without manual intervention.
- Crash loop in `start_stable.sh` now calls `func_do_login` (not `func_check_credentials`) after clearing a bad JWT, so re-authentication actually runs.
- `urnet-tools logs` now correctly routes to `/dev/shm` when eco mode is active. Previously it checked for `lowmem` only, so eco users were tailed against journald (empty) instead of the RAM log.
- Auth error paths in `provider/main.go` (`os.UserHomeDir`, `os.MkdirAll`) converted from `panic` to clean `os.Exit(1)` with descriptive stderr messages.
- Auto-update timer no longer silently dead after install or reinstall. The install script was using `systemctl --user enable` instead of `enable --now`, so the timer was registered but never started. On long-running servers that hadn't rebooted, it never fired.

### Added
- Turbo mode (`URNETWORK_PROFILE=turbo-v4` / `turbo-v8`): raises the TCP Accordion window ceiling from 1 MiB to 4 MiB (V4) or 8 MiB (V8), removing the mathematical per-connection limit that existed because throughput is bounded by window/RTT.
  - Significantly higher theoretical ceilings for low-latency paths.
  - Transfer-layer resend and receive queues scale with the window (8 MiB for V4, 16 MiB for V8) so they don't become the new bottleneck.
  - IP and transfer goroutine buffer depths doubled (512 and 64 respectively).
  - WebRTC DataChannel buffer set to 2× window size per peer.
  - Contract ramp accelerated: `ContractTransferByteSeqScale` 4 → 2 (full speed in 2 contracts instead of 4).
  - GOGC raised to 200 with no GOMEMLIMIT — lets the heap breathe on RAM-rich boxes.
- `urnet-tools turbo <v4|v8|off>`: toggles turbo mode on the systemd provider service. Bare `urnet-tools turbo` prints current state.
- Docker `TURBO=v4` / `TURBO=v8` environment variable: single env var support for containers. The entrypoint translates it to `URNETWORK_PROFILE` before exec; GOGC is handled internally by the binary.

---

## [v3.23.0-fix.12] — 2026-05-26

### Fixed
- Raised `lowmem` mode initial contract size from 16 KiB to 256 KiB. The 16 KiB floor forced constant contract renegotiation that hurt throughput and earnings without meaningfully reducing RAM usage.

### Added
- Eco mode (`URNETWORK_PROFILE=eco`): GC-tuned memory profile for providers on RAM-constrained systems. Sets GOMEMLIMIT to 75% of detected RAM (cgroup-aware; reads cgroup v2/v1 limits so Docker `--memory` containers get the correct ceiling), enables `GOGC=50`, and leaves all buffers and contract sizes untouched so throughput and earnings are unaffected.
- `runEcoMemoryMonitor` goroutine: watches available memory every 30 seconds and dynamically tightens GC pressure when RAM is low — `GOGC=25` under pressure (<300 MiB available), `GOGC=10` at critical (<150 MiB) — then relaxes when it recovers. Hysteresis prevents oscillation. Inside Docker containers, uses cgroup headroom rather than host `MemAvailable` so pressure detection fires correctly.
- `urnet-tools eco <on|off>`: toggles eco mode on the systemd provider service. Merges eco-specific env vars into the existing override.conf rather than overwriting it, preserving other settings such as ramlogs.

---

## [v3.23.0-fix.11] — 2026-05-22

Productionizes the experimental work from the `v3.23.0-beta.1` pre-release (automated proxy recovery pulse and smart exponential backoff), folding it into main.

### Added
- Autonomous proxy recovery via an hourly global pulse (`pulse.go`). A pulse fired every 60 minutes wakes all goroutines blocked on `Pulse()`, so proxies stuck in exponential backoff recover without a provider restart.
- Exponential backoff (5s up to 1h cap) for parallel route selection; on pulse, failure counts and dialer health reset so blacklisted routes get a fair retry.
- Pulse integration and matching backoff for the P2P reconnect loop, which fix.10 did not cover.

---

## [v3.23.0-fix.10] — 2026-05-22

### Fixed
- Prevented bandwidth leak during backend API outages by gating retries when the backend is degraded:
  - Contract retry storm: `CreateContract` no longer launches API goroutines every 5s when degraded.
  - Transport reconnect storm: H1/H3 auth-error loops use exponential backoff (5s to 60s cap) instead of a near-instant retry timer.
  - Added `lastBackendFailNano` for accurate degraded-state detection without log-rate-limit flicker.
  - Degraded state clears immediately on successful reconnect rather than after a 60s timeout.

---

## [v3.23.0-fix.9] — 2026-05-21

### Fixed
- Limited resend amplification during backend outages (`MaxResendCount=16`, `MultiRaceClientCount=2`) so the resend queue can't grow unbounded when the API is unreachable.

### Added
- `urnet-tools update` now self-updates from GitHub instead of reinstalling the current version.
- Auto-update default interval changed from daily to weekly (Sunday midnight UTC).
- `urnet-tools update` no longer stops a running provider; the binary is updated on disk and you're prompted to restart when convenient.
- Documented `URNETWORK_RAMLOGS` and `URNETWORK_PROFILE=lowmem` in the Docker README, plus log rotation defaults and a RAM Logging section.

---

## [v3.23.0-fix.8] — 2026-05-20

### Fixed
- Rate-limited `[t]auth error` and `[contract]oob err` log spam during backend outages to one line per minute globally across all proxy instances
- A suppressed-count suffix (e.g. `(3,952 suppressed)`) is appended when the outage clears so no errors are silently dropped

---

## [v3.23.0-fix.7] — 2026-05-08

### Added
- Lowmode and RAM logging documentation added to README

---

## [v3.23.0-fix.6] — 2026-05-08

### Added
- `URNETWORK_RAMLOGS=1` environment variable — redirects provider logs to `/dev/shm/urnetwork.log` (RAM disk, 1MB cap) to eliminate disk I/O overhead
- `URNETWORK_PROFILE=lowmem` environment variable — enables RAM logging plus reduced buffer sizes and dynamic `GOMEMLIMIT` (85% of system RAM) for memory-constrained nodes
- `urnet-tools ramlogs on/off` command — toggles RAM logging independently of lowmode
- `urnet-tools lowmode on/off` command — toggles the full lowmem profile
- Dynamic `GOMEMLIMIT` calculation in lowmode (was previously a fixed value)

### Fixed
- ARM64 build failure caused by missing architecture-specific `dup2` implementation
- Release workflow and Dockerfile updated to build the full provider package correctly

---

## [v3.23.0-fix.5] — 2026-05-07

### Added
- One-liner install/uninstall commands in README
- Installation summary now shows technical improvements on first install
- Guidance for enabling systemd lingering so provider survives logout
- Auto-update timer cleanup on uninstall

### Fixed
- Auth error logging improved — errors are now visible without requiring verbose flags
- Panic on `proxy add` command prevented
- Various installer output and formatting improvements

---

## [v3.23.0-fix.4] — 2026-05-07

### Fixed
- Context cancellation return type corrected in provider
- Installer lingering instructions cleaned up

---

## [v3.23.0-fix.3] — 2026-05-04

### Added
- `CreateContractTimeout` increased from 30s to 60s to prevent stream drops during signaling spikes
- `ContractFillFraction` tuned from 0.8 to 0.7 for more headroom before contract exhaustion
- Custom installation script (`Provider_Install_Linux.sh`) with `urnet-tools` management suite
- Release workflow to build and publish provider binaries as GitHub release assets

### Fixed
- Docker image version now correctly reflects the build tag (was hardcoded to fix.1 in all images)
- Git tags are now fetched during CI builds so version extraction works correctly

---

## [v3.23.0-fix.2] — 2026-05-04

### Added
- Dynamic TCP window scaling (Accordion logic) — windows start at 4KB on idle connections and double up to 1MB under active throughput, then shrink back after 30s of inactivity

---

## [v3.23.0-fix.1] — 2026-04-30

### Added
- `InitialContractTransferByteCount` increased from 16 KiB to 256 KiB, eliminating the 13,107-byte effective capacity ceiling that caused excessive contract renegotiation and reduced earnings
- Expanded internal message pools (16KB, 32KB, 64KB) to reduce garbage collector pressure during high-throughput transfers
- IP buffer depth increased from 64 to 256 to absorb burst traffic without packet drops
- `[net][s]select` serial-select log promoted from Debug level 2 to INFO — one line per successful connection, visible without `-v`
- Multi-architecture Docker image (AMD64 + ARM64) with vnStat traffic monitoring integration
- CI/CD pipeline via GitHub Actions pushing to GHCR

---

## [v3.23-stock] — 2026-04-30

Baseline snapshot of the upstream URnetwork v3.23 provider before any modifications.
