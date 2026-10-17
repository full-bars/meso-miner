# Project Structure

This document outlines the high-level architecture and directory structure of the **URNetwork 3.23-fix** fork. It highlights the major subsystems and the custom components introduced in this fork (the Go-based urnet-tools management utility, proxy health tracking, tuning, and the DNS-over-HTTPS resolver).

## Directory Layout

```
urnetwork-3.23-fix/
├── provider/                         # Provider binary (the relay node)
│   ├── main.go                       # Provider entrypoint, settings parsing, graceful shutdown
│   ├── auth_rate_limiter.go          # Global adaptive auth rate limiter (AIMD: 20-200 req/s)
│   ├── proxy_admission_gate.go       # Weighted-lottery admission gate for auth slots
│   ├── proxy_failure_history.go      # Persistent per-proxy failure count (across requeues)
│   ├── proxy_auth_history.go         # Proven-proxy set for rate limiter gating
│   ├── proxy_probe.go                # Dual-stage SOCKS5 probe (TCP + API CONNECT)
│   ├── proxy_spread_sampler.go       # Content-keyed spread sampler for the stage-1 table probe
│   ├── proxy_url.go                  # Proxy URL state persistence (proxy_url.json)
│   ├── proxy_url_source.go           # URL fetcher, merge, periodic refresh, reaper, blacklist
│   ├── proxy_reload.go               # Hot-reload engine via .reload trigger files + give-up cooldown
│   ├── proxy_state.go                # On-disk proxy state management (proxy.state)
│   ├── proxy_id.go                   # Stable monotonic proxy ID assignment (e.g., proxy[0])
│   ├── proxy_health_log.go           # Durable state persistence for proxy health (disk writer)
│   ├── proxy_slow_retry.go           # Slow-retry state: 24h daily gate, 14-day drop (file proxies)
│   ├── proxy_benchmark.go            # Opt-in staggered latency probing (TCP and SOCKS5)
│   ├── proxy_direct_grade.go         # Read-only direct-path health grade (~/.urnetwork/direct_grade.json)
│   ├── proxy_match.go                # Pattern-based proxy removal (proxy remove --match)
│   ├── contract_metrics.go           # Fleet-wide per-proxy contract history tracking
│   ├── client_jwt_hotrestart.go      # Client JWT renew + identity snapshot across hot restarts
│   ├── claim_credential.go           # `provider claim` credential choice (store client, client jwt file, legacy coldkey)
│   ├── sn.go                         # Subnet commands (wallet set + unsigned-request gate, claim, bind-head)
│   ├── thrash_watchdog.go            # Swap-thrash watchdog, supervised exit-75 restart
│   ├── doh_cache.go                  # Persistent DNS-over-HTTPS cache with server-score persistence
│   ├── net_http_doh.go               # DNS-over-HTTPS resolver (server scoring, serve-stale)
│   ├── bandwidth_reporter.go         # Pushes periodic JSON telemetry to a configurable report URL
│   ├── important_log.go              # Important-event log (~/.urnetwork/important.log, disk)
│   ├── oom_cap.go                    # OOM-aware start cap (shadow by default) + kill switch (oom-cap on|off|shadow)
│   ├── action_ledger.go              # Audit ledger of capacity decisions (~/.urnetwork/autopilot.jsonl)
│   ├── baseline.go                   # Local behaviour recorder (~/.urnetwork/baseline.jsonl) for upgrade comparison
│   ├── h3_direct.go                  # H3 settings and eligibility for the direct identity
│   ├── family_transport.go           # Dual-stack platform transport group for the direct identity (ip_version)
│   ├── h3_mode.go                    # The `h3` control key: off, direct, on, a proxy count, or all
│   ├── h3_mode_apply.go              # Applies a changed `h3` setting and relaunches the proxies it adds
│   ├── h3_proxy_set.go               # Which proxied identities get the fallback transports
│   ├── h3_packet_conn.go             # UDP packet conn factory for H3 (SOCKS5 relay for proxied identities)
│   ├── socks5_udp.go                 # SOCKS5 UDP relay (RFC 1928 section 7) so QUIC can use a proxy
│   ├── proxy_udp_check.go            # Per-proxy UDP path check (relay answers, exit matches the TCP path)
│   ├── h1plus_metrics.go             # H1+ counters and the `urnet_h1plus_*` metrics
│   ├── wallet_consent.go             # Signed subnet wallet consent (`provider wallet challenge`, `wallet set`)
│   ├── wallet_consent_owner_*.go     # Seed-file ownership check per OS
│   ├── node_internals.go             # Internals snapshot (goroutines, heap, transport split) served to `top`
│   ├── memory_headroom.go            # Low free-memory watcher (host or cgroup headroom log)
│   ├── resource_config_warn.go       # Startup warning when GOMEMLIMIT or RAM is short for the pool
│   ├── smart_dialer_probe.go         # Background connect probes so the smart dialer can compare transports
│   ├── tlog.go                       # Thread-safe timestamped logging helpers
│   ├── shmlog.go                     # Rolling ring-buffer RAM log (/dev/shm/urnetwork.log)
│   └── ...                           # (network stack, transport, IP layer — see root)
│
├── cmd/                              # Go command entrypoints
│   ├── urnet-tools/                  # Manager/CLI binary (main.go)
│   └── urnet-docker/                 # Docker wrapper binary (main.go)
│
├── internal/
│   └── urnettools/                   # urnet-tools implementation (Go)
│       ├── cli.go                    # CLI dispatch, help, and target-flag parsing
│       ├── cobra.go                  # Cobra command tree (all subcommands)
│       ├── discover.go               # Provider discovery (systemd, docker, /proc)
│       ├── discover_unix.go          # Linux systemd + user-unit discovery
│       ├── update.go                 # Provider + tool update, digest verify, backup/prune
│       ├── lifecycle_cmds.go         # start/stop/restart/uninstall/reinstall, safe deletes
│       ├── lifecycle_unix.go         # systemd unit + timer management, linger check
│       ├── legacy_cmds.go            # logs, optimize, set, fast-auth, report
│       ├── proxy.go                  # proxy add/clear/summary, provider-user read checks
│       ├── session_cmds.go           # encrypted identity session save/load (AES-256-GCM)
│       ├── self_heal.go              # self-heal marker toggle (provider-scoped)
│       ├── provider.go               # Provider struct, version-from-buildinfo
│       ├── target.go                 # Target resolution / selection
│       ├── select_multi.go           # Batch selection (--all/--include/--exclude)
│       ├── release.go                # GitHub release + digest resolution
│       ├── exec_timeout.go           # bounded subprocess helper
│       ├── fsync_unix.go / _other.go # cross-platform fsync + file-ownership helpers
│       ├── restart_escalation.go     # staged-tool restart routing
│       ├── log_stream.go             # native log follower (tail + follow, rotation and truncation aware)
│       ├── log_follow_open_*.go      # per-OS open for the follower (Windows: delete sharing, long paths)
│       ├── restore_delegate.go       # set/fast-auth state-dir chown
│       ├── autopilot.go              # autopilot log: timeline of capacity decisions (OOM cap, trim)
│       ├── internals.go              # Internals snapshot for the `top` panel (goroutines, heap, transport split)
│       ├── provider_recover_*.go     # per-platform user/UID recovery
│       ├── docker.go                 # container discovery + docker CLI seam
│       └── ...                       # platform-specific lifecycle + test files
│
├── docker/
│   └── scripts/                      # Docker helper scripts (entrypoint, start_*, urnet-tools, proxy-*)
│
├── Dockerfile                        # Alpine-based, multi-stage, multi-arch build with vnStat
├── CHANGELOG.md                      # Human-readable release changelog
├── FORK_CHANGES.md                   # Comprehensive reference of all fork modifications
├── PROJECT_STRUCTURE.md              # This document
├── LOG_REFERENCE.md                  # Log-line format reference
│
# Core Library Components (Root)
├── connect.go                        # Core types: TransferPath, Id (16-byte ULID), ByteCount
├── net.go                            # TCP/TLS dialing with SOCKS5 proxy support (trackedConn)
├── net_http.go                       # Control-plane dialing & ClientStrategy
├── net_http_doh.go                   # DNS-over-HTTPS resolver with caching
├── net_http_smart_dialer.go          # Smart dialer: measured-cost transport preference + connect probes (off by default)
├── emoji/                            # Network tag validation and suggestion (copied from upstream connect)
├── transport.go                      # PlatformTransport: WebSocket (H1) + QUIC/H3 + DNS PT
├── transport_h3_gate.go              # Runtime on/off gate for H3, live-switchable
├── transport_h3_counted.go           # Byte-counting wrapper for the H3 UDP socket
├── transport_h3_datagram.go          # QUIC DATAGRAM message layer, receive route and guard
├── transport_h3_datagram_lane.go     # Bounded send lane for outgoing datagrams
├── transport_h3_datagram_state.go    # DATAGRAM counters shown on the [health] line
├── transport_h3_memory.go            # H3 memory bound (matches upstream)
├── transport_family.go                               # Family-pinned platform transports and the group that runs them
├── transport_family_stats.go                         # Per-family counters and logs for the dual-stack transports
├── transport_h1plus.go                               # H1+ authenticated custom carrier upgrade
├── transport_h1_stats.go / transport_h1_progress.go  # Live H1 connection stats and opt-in progress metadata
├── transport_authorization.go                        # Authorization close reasons shared by the transports
├── transport_credential_refusal.go                   # Hold on dials after the platform refuses a credential
├── framed_message_conn.go                            # Binary-message surface shared by H1 and H1+
├── http_upgrade*.go                                  # H1+ upgrade policy, error causes and handshake close
├── stage3_adapter.go / net_http_stage3.go            # Adapter surface the swapped-in upstream transport expects
├── net_http_leaf.go / net_http_internal_doh.go / net_http_doh_control.go# Upstream HTTP and DoH leaves, ported
├── net_http_exhaustion.go                            # HTTP operation owners that retain failure causes
├── net_doh_family_leaf.go                            # DoH family pairing
├── net_dial_race.go                                  # Shared dual-stack dial race
├── net_dial_udp_candidates.go                        # Progressive UDP dial candidates
├── net_extender_memory.go / net_extender_quic_send.go# Extender carrier memory and QUIC send lease
├── net_strategy_delivery.go                          # Delivery observation hook on dialed connections
├── control_family.go                                 # Address-family policy for control-plane dials
├── egress_dial.go / egress_resolver_*.go             # Control-dial escape and egress-bound resolver
├── platform_h3_gate.go                               # One H3-family socket generation per platform gate
├── tun_dial_observation.go                           # Request-local dial progress observation
├── transfer_key_type.go / transfer_progress_trace.go # Transfer lane identity and opt-in progress trace
├── message_pool_counts.go                            # Cumulative message pool counts
├── ip_ipv6_ext.go                                    # Shared IPv6 extension-header walker
├── ip_nat_fragment.go / ip_fragment.go / ip_ipv6_fragment.go# NAT fragment reassembly and fragmentation (IPv6 identification from crypto/rand)
├── ip_dial_failure.go / ip_dial_errno*.go            # Answer a source whose upstream dial failed (RST, ICMP unreachable)
├── ip_protocol_number.go                             # IANA IP protocol number type (upstream's)
├── api_wallet_consent.go                             # API client for the wallet sign-in challenge
├── transport_mode_stats.go           # Payload frames and bytes per transport mode
├── transport_p2p.go / _webrtc.go     # P2P WebRTC transport
├── transport_pt.go                   # Pluggable transport: DNS packet translation
├── transfer*.go                      # Client state machine, contracts, encryption, routing
├── ip.go / ip_security*.go           # IP-layer NAT, security policy, DPI classification
├── tuning.go                         # System auto-profiling (Tier1/Tier2/Tier3) by cgroup RAM
├── audit.go                          # Passive host kernel setting validator
├── message_pool.go                   # Dynamic allocation pool for relay payloads
├── jwt.go                            # JWT auth token management
└── api.go                            # Platform API client (auth, OOB control)
```

## Architectural Concepts

### The Provider Node (`provider/`)
The main worker. Binds to `api.bringyour.com` to authenticate and fetch a list of proxies. The fork extends it with client-JWT hot-restart identity reuse, a production-grade DoH resolver with a persistent scored cache, proxy health tracking, auto-tuning, and hot-reload of proxies without a full restart.

### The Hub — removed in v32.0
The standalone bandwidth hub dashboard (`hub/` package) was removed in v3.23.0-fix.32.0 (see FORK_CHANGES.md section 165). Fleet observability is served by the Prometheus `/metrics` endpoint (`urnet-tools metrics on`) and the built-in Grafana monitoring bundle. The generic `report` command still posts telemetry to any configured `report_url`.

### urnet-tools (`cmd/urnet-tools` + `internal/urnettools/`)
The operator's management utility, rewritten from a shell script into a Go binary. It discovers providers across systemd units, user sessions, and containers; manages their lifecycle and drop-ins; updates provider and tool binaries with digest verification; and exposes proxy, session, self-heal, metrics, and reporting commands. It is the fork's control plane and the primary subject of the 30.9 security audit remediation.
