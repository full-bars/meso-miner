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

# Next meso release — v34 parity + 34.1 follow-ups (2026-10-20)

> [!NOTE]
> This is the meso-miner release that picks up everything main has carried since `v2026.10.19-1075696970-meso`, and it is the one that moves meso onto the modern 33.1/34 connect generation. The v34 parity port is the headline: family-pinned platform transports with a dual-stack direct identity, a control-plane family policy with a demotion ledger, the platform transport budget and egress resolver, ICE resolution and the light routing classifier, the shared UDP socket lifecycle, the transfer memory budget core, H1+ framing, transport sessions and authorization, and the provider batch that brings `h3 auto`, the network-refresh route and the consolidated Docker entrypoint. The four 34.1 review-fix commits ride on top of it. `v2026.10.19-1075696970-meso` is the baseline for every comparison here; nothing in this note repeats work that shipped before it.

> [!IMPORTANT]
> Two changes are worth reading before you update. A native provider that reaches `connect.bringyour.com` directly now builds a dual-stack transport group for its direct identity, so the platform sees a v4-pinned and a v6-pinned transport instead of one family-agnostic one; a host with no usable global IPv6 still builds the group and parks the v6 pin, so it stays on IPv4 without a relaunch. And a node that settles as direct-only (no proxies configured, direct transport on) now runs H3 by default, while `h3_datagram` and `h3_datagram_send` default on everywhere a box already runs H3. An explicit control key wins in every case, and pooled nodes on the default `h3` setting are unchanged.

## 1. The story of this release

The last release closed runtime parity with the fork. This one closes engine parity with the 34 line. Upstream's modern transport was never a drop-in module: file-by-file copying kept pulling the next layer, so it landed as a three-way merge of `release/34.0` into the meso line at the shared merge-base, with the Go side taken from 3.23-fix and meso's branding, docs, workflows and scripts preserved. The result is not a parallel fork: meso now runs the same connect generation as 3.23-fix 34, and the provider-side batch that had been sitting on the mirror branches (`h3 auto`, the network refresh, the Docker entrypoint consolidation) comes with it. The four 34.1 commits then apply the same review follow-ups the fork shipped in `v3.23.0-fix.34.1`.

## 2. The modern transport generation (PR #231)

The parity merge brings the pinned transport and everything built on it:

- `transport_family.go` is family-pinned platform transports and the group that runs them. The direct identity runs a v4 pin, a v6 pin and a family-agnostic standby; the standby dials only after a delay with neither pin connected and stands down as soon as one connects. A pin dials its own family url, resolves only the matching record type, uses direct dialers only, declares its family in the auth header or frame, never feeds the process-wide backend-degraded gate, is held rather than spun when the device has no path of its family, and obeys a contradicting Force by idling.
- `transport_family_stats.go` and `stream_metrics.go` add the operator accounting: per-family attempts, connects, failures, drops by close cause, session lifetimes, pinned-down and standby timers, plus stream-sequence counts, lifetimes, open-request outcomes, control frames and client TLS handshakes.
- `transport_sessions.go` gives platform transport sessions a lifetime and a close cause, so "why did this connection end" is a metric instead of a log hunt.
- `transport_authorization.go` and `transport_credential_refusal.go` add authorization and a credential-refusal hold, so a refused credential parks the transport instead of re-dialing into the same refusal.
- `http_upgrade.go`, `framed_message_conn.go`, `message_framer_xl.go`, `transport_h1plus.go` and the `h1_plus` control key add H1+: an H1 connection can upgrade to a framed custom carrier that keeps H1's lanes, limits and batching but encodes ready payloads once through the shared framer. It falls back to a plain websocket on any failure and is on by default; `urnet-tools set h1-plus off` turns it off, and `[h1plus] last 10m:` summarizes how every H1 dial ended.
- Transfer-side ownership arrives with it: attempts are owned, budget reservations are released by their owners on every path, and retained-byte accounting is shared, which is what makes budgets composable in the first place.
- DoH internals (`net_http_doh_control.go`, `net_doh_family_leaf.go`, `net_http_internal_doh.go`) and progressive UDP dial candidates (`net_dial_udp_candidates.go`) are the name half of the same generation: a protected name resolves through the in-process resolver, and a UDP dial consumes the first usable address while the other family's query is still running instead of waiting for both.
- The merge also brings the 34 provider command surface: `provider claim-daemon`, `provider claim-daemon-v2`, `provider fleet` and the wallet hotkey upkeep commands, with their docs. They are subcommands only; nothing runs unless an operator invokes one.

## 3. Dual stack: the direct identity becomes a transport group (PR #231, provider side)

`provider/family_transport.go` decides, once per identity launch, whether the direct identity gets the family group. It does when the identity is native, has no proxy settings, both family platform urls exist, and the connect host is the one whose `-v4` and `-v6` names were verified live. A proxied identity never gets the group: it reaches the platform through its proxy, and a pinned transport dials from the host. The launcher logs the decision as `[ipv6] direct identity platform transports: dualstack group` or `legacy single (reason=...)`, and the state reporter logs `[ipv6] family transports v4=... v6=... standby=... active=...` only when the tuple changes.

The operator chooses the stack with the new `ip_version` control key (`dual`, `ipv4`, `ipv6`, `legacy`; `urnet-tools set ip-version`). It is live: `SetStack` re-pins the running group, breaking before making, so a switch never drops the working transport before its replacement connects. `legacy` is the pre-dual-stack behaviour, one family-agnostic transport. The transport budget is sized at launch for the stack's transport count and does not resize live.

On a host without a usable global IPv6 address the group is still built and the v6 pin sleeps rather than spins; it wakes on a network change, so a box that boots before SLAAC finishes joins dual stack later without a relaunch. Nothing about IPv4 changes: it stays primary and is never demoted.

## 4. The control-plane family policy and the family-aware dial (PR #222)

`control_family.go` ports the policy that steers the provider's own control-plane dials — the api https client, the platform websocket and the h3/quic name path. The tunnelled data plane is untouched: it carries both families and picks an exit by the packet's own version.

The policy itself is `Auto` in every shipped configuration, because nothing in the provider sets `Force4` or `Force6`; the part that is live is the demotion ledger. A dial that connects and then times out in its handshake is the signature of a path that carries small packets and drops large ones, so that family is demoted for a bounded window: 5 minutes for the first strike, doubling to a 6-hour cap, cleared on any network change, refused when the other family is not usable on this device, and never recorded for a dial that went through a proxy. `control_family_dial.go` adds the matching retry: after a connect-then-handshake-timeout it retries once over the other family, which is the failure Happy Eyeballs cannot see because it races only the TCP handshake.

Both halves only narrow a name dial. A dial to an IP literal is left alone, which keeps the extender dialers and the plain-DNS resolvers out of a demotion that their own failures could never undo.

## 5. Egress, routing and the platform transport budget (PR #223)

- `egress_dial.go` closes the name-resolution half of self-capture: a control dial to a hostname now resolves through an in-process resolver whose queries ride egress-bound sockets, so a provider that pins its sockets to the physical interface does not deadlock behind the tunnel it is building. Everything there is inert unless a platform supplies an egress resolver; Windows supplies one whose dial hook is inert until an egress interface is set, Linux preserves the live system resolver and refreshes only a server made stale by a resolv.conf transition, and other platforms keep the resolver they had.
- `egress_darwin.go` adds macOS interface pinning for sockets, so a provider can stay outside a packet tunnel on that platform too.
- `platform_transport_budget.go` adds the aggregate budget the transport group admits against, with exact claim accounting, background priority for pinned transports and a make-before-break pairing so a policy migration cannot put two full working sets on the wire at once. The provider sizes it from the memory target for the launch stack.
- The route-manager change in this PR is a test seam only: an exactly published route-state generation that integration tests use as a barrier. No production path installs an observer.

## 6. ICE resolution and the light routing classifier (PR #228)

`ice_resolve_net.go` binds native WebRTC address resolution to the peer generation's context, so a STUN or TURN hostname lookup is cancelled the moment that generation is retired instead of sitting parked on a socket read while `PeerConnection.Close` joins the ICE gatherer. The resolver is unchanged otherwise.

`routing_class.go` and `routing_classifier_light.go` add the traffic-class vocabulary and a light classifier: a flow can be classed as latency, throughput or bulk. It is inert by default — no consumer is installed and the newer generation's scoring and placement paths are not ported — so classification changes nothing about placement until something reads it.

## 7. The shared UDP socket lifecycle (PR #229)

`ip_udp_socket_poller.go` with the `epoll`, `kqueue` and `unsupported` backends replaces one blocking reader, one send loop and one idle timer per UDP flow with a small fixed set of readiness workers per family. A flow is permanently assigned to one shard, so it keeps a single drain producer and its datagrams keep arrival order, and a bounded delivery queue drained by a second worker keeps a slow receive callback off the descriptor drain.

It is inert: the poller is only built when `SocketReadShardCount` is set, no shipped setting sets it, and every platform without a backend keeps the established per-flow reader. The port also documents the tradeoff that has to be measured before anyone enables it: the blocking enqueue runs on the shard's single drain loop, so one slow callback couples every flow hashed to that shard, which the per-flow reader did not do.

## 8. The transfer memory budget core (PR #230)

`transfer_memory_budget.go` adds a byte budget that transfer queues share across sequences — typically every client of one device — so aggregate queue memory stays flat as peers grow while a single fast peer can still borrow a deep queue. A queue keeps a guaranteed floor that it never borrows against, reserves only the bytes above it, releases on every removal path, and a parented budget composes every admitted byte through its ancestors with one admission lock for the tree, so children with overlapping ceilings can never overdraw the root.

Like the poller it is inert on this release: `SendBufferSettings.ResendQueueBudget` and `ReceiveBufferSettings.ReceiveQueueBudget` are honored where they are set, and the defaults set neither, so no shipped configuration builds a budget. It is the core the wiring decision needs, not the wiring.

## 9. The provider batch: `h3 auto`, UDP relay checking, network refresh, Docker consolidation (PR #224)

- **`h3 auto`** is a new value for the existing `h3` control key. It chooses how many proxies get the fallback transports from the effective memory limit and the existing pressure signal — one slot per 64 MiB, floor 2 GiB, ceiling 64, shed at 0.75 smoothed memory pressure, held between 0.30 and 0.50, grown 4 at a time below 0.30 — and logs `[h3] auto: <previous> -> <next> proxies get the fallback transports (...)` only when the ranked set actually changes. It adds no sensor, no goroutine and no metric series, and it is the only mode that runs the controller.
- **The `h3` key is now a set of modes**, not a switch: `off`, `direct` (the default), `on`, a positive proxy count, `all`, and `auto`. Counts and `all` give proxies the automatic fallback transports through their own SOCKS5 UDP relay, never from the host.
- **H3 through a proxy is checked before it is used.** `provider/proxy_udp_check.go` proves a proxy's UDP relay works and, where it can be learned, that the UDP exit address matches the TCP exit; a proxy that fails stays on the TCP modes with a `[proxy][udp]` line saying why. `provider/socks5_udp.go` is the relay implementation.
- **The network refresh is new on this line.** `provider/network_refresh.go` renews the network JWT at `POST /auth/network-refresh` in one call, keeping the token's create time, roles, principal and guest mode, and falls back to the legacy code-create/code-login flow only for a server that predates the route or refuses the request. A 401 is terminal: the legacy call would present the same dead token and fail the same way, so the refresh ends with an error, the current JWT stays in place and the next hourly pass retries. A 403 and the other 4xx refusals except 408, 425 and 429 still fall back; 408, 425, 429, a 5xx and a transport error stay errors.
- **The Docker entrypoint is consolidated.** `entrypoint.sh`, `provider_common.sh` and `provider_env.sh` now own the shared startup logic, and `start_stable.sh`, `start_nightly.sh` and `start_jwt.sh` are thin wrappers that select a mode and a binary flavour. The invocation, the `BUILD` values and the volume layout are unchanged, and the Pelican gate is read from the shared script rather than duplicated.
- **The direct-only H3 default.** A node whose proxy resolution settles as direct-only now resolves `h3` to on, because the resource argument that keeps H3 off on a pooled node (one socket set per proxy identity) does not apply when there is exactly one identity. `h3_datagram` and `h3_datagram_send` now default on everywhere, as upstream offers and sends datagrams by default. An explicit key always wins, clearing a key returns to the new default, and a node that never settles direct-only and never sets `h3` is unchanged.

## 10. What the rest of the release window carries (PR #219, #220, #225, #226, #227)

- **The receive window is advertised** (#219): `Ack` gains `receive_window_byte_count` (proto field 8, an additive and backward-compatible field), and the provider sizes it from its receive queue, which is what the platform's modern-provider gate looks for beside post-quantum encryption. An older peer ignores the field.
- **Contract lifecycle logs carry the proxy identity** (#226): `ProxyDescription` appears in the contract create and close lines, so one contract can be traced to the proxy that served it.
- **The network-switch notice is truthful** (#227): the notice only prints when the effective network actually changes, `--reset` is decided before the write, and an unreadable config counts as changed.
- **The build log no longer doubles the version prefix** (#220), and both shakedowns run together with a jittered mint (#225, CI only).

## 11. Blocklist refresh (PR #221, #232)

The CFAA egress blocklist is refreshed twice in this window and now holds 41,425 IPv4 ranges and 548 IPv6 ranges. Data only: port policy and verdict semantics are unchanged, and no operator action is required.

## 12. The 34.1 review follow-ups (four commits)

- **Plain-DNS resolver rotation is per lookup, not per process**: when DoH has no answer the resolver falls back to the configured plain-DNS servers and the server it dials rotates one entry per resolver attempt. That counter was shared by the whole cache and advanced once per attempt, so a concurrent lookup could move it between another lookup's two attempts and both could land on the same address family; on a node that lacks that family each such lookup then pays a wasted or timed-out attempt. Each lookup now seeds its own attempt counter from the shared one, and the shared counter advances once per lookup instead of once per attempt. Lookups still spread across the configured servers, and the attempts of one lookup walk consecutive entries. DoH stays primary, so only the plain-DNS fallback changes.
- **The UDP shared-socket poller returns the rest of the batch when a drain is canceled**: a batch read from one socket holds one pooled buffer per fragment of an oversized datagram, and a canceled drain used to return only the packet it refused, leaving the rest of the slice counted as taken. It returns the remainder now. The poller stays inert, as above.
- **The reliable handoff timeout default is 0, not -1**: `DefaultReceiveBufferSettings().ReliablePackHandoffTimeout` was -1, which reads as an unbounded wait, in a field no production caller reads. The default is 0 and the comment states the field is carried for a later wiring decision with no effect yet. Nothing about a running box changes.
- **A network-refresh 401 is terminal and no longer falls back to code-create**: the rule described in section 9, which ships here together with the route itself, so meso never has a release in which a 401 retries the legacy flow.
- **Windows egress fail-closed behaviour is documented** (`egress_windows.go`, comment only): a host with a v4 egress index and `index6 == 0` fails dials of IPv6 destinations, because the only attempted option is the v4 one and an `AF_INET6` socket rejects it. That is intentional, and no code path changes.
- **The normal strategy dial is pinned to the same egress control** (`egress_dial_sites_test.go`, test only): dropping `NetDialer`'s `Control` field now fails the build.
- **Docker public-IP detection no longer masks curl's exit status** (`docker/scripts/provider_common.sh`): the declaration and the assignment are separate, so the exit status the script can see is curl's. The detected address and the log line are unchanged.

## 14. The open PRs that merge before the cut (PR #294, #297, #298, #299, #300, #301, #302)

- **A silent platform edge no longer drains egress through proxied H3** ([#302](https://github.com/full-bars/meso-miner/pull/302)). With `h3` set to `all`, a count or `auto`, every proxy identity runs the H3, H3-over-dns and dns-pump carriers beside its H1. When the edge answers nothing, each carrier redials on its own 30 to 60 second timer and every dial runs the whole QUIC retransmit ladder, about 33 KB. One node on `h3 all` spent a flat 15.5 to 16.4 Mbit/s for about 15 hours of the 2026-10-23 outage, roughly 30 times more tx than rx, while the governor read `verdict=ok rate=200.0/s`. Three things hid it, and each is fixed: QUIC bytes were never charged to the connect attempt, so the failed-byte breaker saw zero; a canceled dial (two of every three, because the per-transport H3 gate cancels a lower-priority carrier when a better one waits) was dropped with its bytes; and H3 dials never waited on the attempt budget. The handshake's wire bytes are now charged once, a canceled attempt's bytes count toward the breaker without counting as an attempt, and a proxied identity's H3 waits on the shared budget like its H1. The direct identity, proxy index 0 and family-pinned transports never wait. One conflict was resolved on the way in: meso-miner lacked the carve-out that local teardown is not a backend outcome at the H3 `recordAttempt` call, and this release takes the superset.
- **A failed OOM-cap state write is reported, not discarded** ([#301](https://github.com/full-bars/meso-miner/pull/301)). The startup OOM-cap decision wrote its state with the result discarded. That state carries the cap and the clean-window clock, so a lost write could let the next start read a stale window and relax the cap straight after a fresh OOM kill, and the applied path could ledger a cap that nothing enforces. Both branches now report the failure in the warnings, and the applied path leaves the timeline to the last cap that really was persisted.
- **The memory-headroom warning fires at the cap on a host whose RAM is unreadable** ([#298](https://github.com/full-bars/meso-miner/pull/298)). `EffectiveRAMLimit()` returns 850 MiB with the source `fallback default, host memory unreadable` when it cannot read host memory, which the headroom threshold turned into its 150 MiB floor, so the warning fired far too late on such a host. The limit is now classified by its source, never its value: an unreadable host gets the 400 MiB cap, a real 850 MiB host keeps the floor, and the note explaining the threshold goes to the persisted log once.
- **The degraded reaper holds off when the scores cannot rank the pool** ([#294](https://github.com/full-bars/meso-miner/pull/294)). `scoreDegradedProxies` ranks by lifetime bytes and contracts, and when the platform is unreachable neither moves, so every candidate tied and the reaper still dropped half the pool (249 of 498 proxies on one node in the 2026-10-23 outage). A round where every candidate scored the same, or there are too few to rank, now holds off instead of culling.
- **The smart dialer costs nothing while it is off** ([#297](https://github.com/full-bars/meso-miner/pull/297)). The idle preference's off state still cost a goroutine, a context and a timer per proxy, so a node with thousands of proxies carried thousands of idle goroutines. The start function is gated, so while the preference is off there is none. Because the loop now only exists when the preference was on at start, `smart_dialer` is reclassified restart-required: `set smart_dialer on` is persisted and takes effect at the next start, the CLI says so, and `set` now prints the same "takes effect at the next start" wording for `profile` and `ramlogs`, which never applied live. The default is unchanged: off.
- **The disk benchmark samples by time, not a fixed 16 MiB** ([#300](https://github.com/full-bars/meso-miner/pull/300)). The benchmark stopped at a fixed 16 MiB, which an NVMe writes in about 20 ms, so the rate reported at startup and in `urnet_disk_*` was a burst figure that moved between runs. The 3 s budget now governs, up to 256 MiB: microSD is unchanged at about 20 MiB, eMMC samples about 240 MiB, NVMe the 256 MiB ceiling in about 85 ms. The result carries its duration, so the log reads `900.0 MB/s (256 MB written in 85 ms)`. The container guard, IO-pressure skip, space cap, per-device cache and hung-write watchdog are unchanged.
- **The installer PATH-hint test runs in CI** ([#299](https://github.com/full-bars/meso-miner/pull/299)). `scripts/test_provider_install_shellrc_symlink.sh` proves the installer's PATH hint never writes through a name the service user controls, and it never ran in CI. It now runs with full history, and the script fails, not skips, when its falsification pin is missing under `REQUIRE_FALSIFICATION`, so a shallow checkout cannot turn it into a silent pass.

## 13. What is behaviour, what is inert, what is test only

| Item | Status |
| --- | --- |
| Dual-stack group for the direct native identity (`ip_version`) | Behaviour, automatic; nothing to configure |
| Control-plane family policy demotion ledger | Behaviour, automatic; bounded and self-clearing |
| Egress resolver on control dials | Behaviour where a platform resolver exists; others unchanged |
| H1+ upgrade of H1 connections (`h1_plus`) | Behaviour, on by default, falls back silently |
| `h3 auto`, `h3` counts and `all` | Behaviour only when the `h3` key selects them |
| Direct-only H3 default, `h3_datagram` / `h3_datagram_send` defaults | Behaviour on direct-only nodes and on any box running H3 |
| Network refresh with terminal 401 | Behaviour, hourly |
| QUIC dial accounting and proxied-H3 admission (#302) | Behaviour, automatic; changes only nodes with `h3` set to `all`, a count or `auto` |
| OOM-cap write failure reporting (#301), headroom classification (#298), reaper tie hold (#294) | Behaviour, automatic |
| Smart dialer gate (#297) | Behaviour: `smart_dialer` is now restart-required; default still off |
| Disk benchmark sampled by time (#300) | Behaviour: a representative rate on fast media |
| Installer shellrc symlink test in CI (#299) | Test and CI only |
| Receive window advertisement (#219) | Behaviour, wire-compatible |
| Shared UDP socket lifecycle (`SocketReadShardCount`) | Inert: no shipped setting sets it |
| Transfer memory budget core | Inert: no default builds a budget |
| Light routing classifier | Inert: no consumer installed |
| Route-manager observer (#223), `egress_dial_sites_test.go` pin | Test only |
| Windows egress comment, `provider_common.sh` SC2155 | Comment and exit-status only |

## Deploy Notes

1. **Rebuild or pull the new image; native installs re-deploy the binary.** `urnet-tools update` picks it up. There is no config change, no new required setting and no migration.
2. **Go 1.27.2 is required to build from source.** The merged tree needs the `third_party` module replacements, and the image now copies them, so a local build script that only graded the root module will fail until it accounts for them.
3. **Expect the dual-stack lines on a native node.** `[ipv6] direct identity platform transports: dualstack group` and then `[ipv6] family transports ...` on a change. A box with no global IPv6 logs the launch-time probe reason and parks the v6 pin; that is the designed steady state, not a failure. `urnet-tools set ip-version legacy` returns that identity to the single transport.
4. **Watch the control-plane demotions if the api path misbehaves.** A demotion is bounded (5 minutes doubling to 6 hours) and cleared by any network change; a demotion is refused when the other family cannot work. There is no operator switch for the ledger on this release.
5. **The H3 defaults moved.** A settled direct-only node runs H3 without setting anything, and any box already running H3 now offers and sends DATAGRAM. `urnet-tools set h3 off` and `urnet-tools set h3-datagram off` restore the old posture, and they win over the defaults.
6. **The network refresh is the new hourly path.** A dead token shows `network-refresh` followed by `refresh FAILED: ... — keeping existing JWT` with no `step 1/3` lines. Check the provider's stored network JWT rather than waiting for a code-create fallback that will not come.
7. **The CFAA refresh is data only.** Policy and verdicts are unchanged. No action.
8. **Rollback is the usual one.** Keep the previous provider binary and a state tarball, stop the unit, restore the binary, start. A rollback also drops the dual-stack group and the H1+ carrier, both of which the older binary never asks for.
9. **Docs.** The dual-stack story and the resolver rotation sentence are in [Dual-Stack](../docs/Dual-Stack.md). The `h3 auto`, `h3` and `h3_datagram` rows are in [Configuration](../docs/Configuration.md) and the sizing detail is in [H3 Auto](../docs/H3-Auto.md). The new log lines are in [LOG_REFERENCE.md](../LOG_REFERENCE.md) and the new series in [Monitoring](../docs/Monitoring.md). The upgrade row and version markers are in [advanced](../docs/guides/advanced.md), [Installation](../docs/Installation.md), [Docker Deployment](../docs/Docker-Deployment.md) and [urnet-tools (Go)](../docs/urnet-tools-go.md).
10. **`smart_dialer` is now restart-required.** `urnet-tools set smart_dialer on` is persisted and takes effect at the next start, and the CLI says so. The default is still off.
11. **Only nodes with `h3` set to `all`, a count or `auto` change for the H3 fix.** In an outage they log `[govern] budget cut ... reason=bytes`, which was silent before. Not yet run against a live outage, and the budget floor still spends about 0.5 Mbit/s.
12. **The first disk benchmark after the update reports the new sample.** On fast media the rate is a representative figure and no longer a 20 ms burst that moved between runs.

## What's Changed

- feat: mirror the v34 control-plane family policy and family-aware dial (#222)
- feat(meso): mirror the egress, route manager and budget surfaces (#223)
- feat(meso): port the 33.1 batch (h3 auto, network refresh, docker consolidation) (#224)
- feat(meso): mirror the ICE resolver and routing classifier (#228)
- feat(meso): mirror the shared UDP socket lifecycle and read poller (#229)
- feat(meso): mirror the transfer memory budget core (#230)
- feat: bring the modern 33.1/34 connect generation into meso — the parity merge (#231)
- fix(meso): act on the independent review of the parity merge (part of #231)
- chore(security): sync the CFAA blocklist from upstream (#221, #232)
- feat(transfer): advertise `receive_window_byte_count` for the modern provider gate (#219)
- fix(docker): do not double the version prefix in the build log (#220)
- ci: run both shakedowns together with a jittered mint (#225)
- feat(contract): include proxy identity in contract lifecycle logs (#226)
- fix(provider): only warn about the jwt when the network changes (#227)
- fix: rotate the plain-DNS resolver per lookup, not per process
- fix: v34 review follow-ups for handoff default, refresh 401, windows egress
- fix(udp): return the rest of the datagram batch when the poller drain is canceled
- test(net): pin the normal strategy dial to the egress control

**Full Changelog**: <https://github.com/full-bars/meso-miner/compare/v2026.10.19-1075696970-meso...<computed-tag>>
- fix(governor): price QUIC dials in bytes and admit proxied H3 under the attempt budget (#302)
- fix(provider): report an oom cap state write that fails (#301)
- perf(provider): size the disk sample by time instead of 16 MiB (#300)
- ci: run the installer shellrc symlink test, and fail if its pin is missing (#299)
- Classify unreadable host RAM by source so the headroom warning fires at the cap (#298)
- Stop the smart dialer spawning an idle goroutine per proxy (#297)
- fix(provider): hold the reaper when the scores cannot rank the pool (#294)
