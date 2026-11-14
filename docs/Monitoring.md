# Monitoring with Prometheus and Grafana

Every provider can serve Prometheus metrics at `/metrics`. The `monitoring/` bundle runs Prometheus and Grafana with a ready-made dashboard, so you can watch a whole fleet from one page.

It takes two steps: turn metrics on at each provider, then start the bundle on any machine that can reach them.

## Before you set anything up: the built-in baseline

Prometheus is the fleet view: it answers "how is this node doing right now", across every box, on a live dashboard. It is also the heavier option, and it says nothing about how a box behaved *before* an upgrade unless you were already recording.

Every provider therefore also keeps a small local record of its own behaviour in `~/.urnetwork/baseline.jsonl`, on by default, with no setup. It holds counts and totals only, never proxy addresses, usernames or passwords. The same file also records the GC state (`gc`: GOGC in force, whether the governor is tightening, and the GC CPU share) and the kernel's socket-table pressure (`net`: conntrack fill, TIME_WAIT and orphaned TCP sockets). These are observation only and feed no decision; a field the box cannot read is left out rather than written as zero. `urnet-tools baseline show` prints the newest rows and `urnet-tools baseline compare` reports a rate, proxy count, memory and restart difference across an upgrade, warning you when the capacity changed so a trimmed box is not misread as a regression.

This is the no-setup alternative for the one question Prometheus does not answer: did that upgrade make this box worse? It is not a replacement for the fleet view, and it is not a time-series database. For a fleet, use the bundle below.

## 1. Turn on metrics on each provider

```bash
urnet-tools metrics on
```

The command prints where the endpoint listens and what to give Prometheus:

```
Metrics: on
  listening  http://127.0.0.1:9100/metrics
  listening  http://100.64.0.10:9100/metrics
Prometheus target: 100.64.0.10:9100
```

The setting survives restarts and updates. Run `urnet-tools metrics status` at any time to see it again. `urnet-tools metrics off` stops the listener now. After a restart or HotSwap, an address or `auto` in `URNETWORK_METRICS` starts it again. The values `off`, `false`, `no`, `0` and `none` keep the listener disabled, and `metrics on` does not override them. To keep metrics off, set one of those values or remove the variable.

### Where the endpoint listens

| Situation | Listens on |
|---|---|
| Bare metal or VM (default) | `127.0.0.1` plus this machine's Tailscale address, if it has one |
| Docker container (default) | Every IPv4 interface inside the container, unless its network has a public address; then loopback and Tailscale only |
| `urnet-tools metrics listen <ip:port>` | Exactly that address |
| `URNETWORK_METRICS=<ip:port>` set in the environment | Exactly that address (overrides everything above) |

The default port is 9100. If another program already uses it (Prometheus's `node_exporter` does), the provider takes the next free port up to 9103. Use whatever `metrics status` prints.

> [!TIP]
> Tailscale is the easiest way to connect Prometheus to providers on different networks. If Tailscale starts after the provider, the provider starts serving on the Tailscale address within 30 seconds. No restart is needed.

**Without Tailscale**, pick an address Prometheus can reach, such as a private LAN or VPN address:

```bash
urnet-tools metrics listen 10.0.0.5:9100
urnet-tools metrics listen auto   # back to the default
```

> [!WARNING]
> The metrics include every proxy's address and traffic. Never expose the endpoint on a public address. `metrics listen 0.0.0.0:9100` on an internet-facing server is readable by anyone who can reach that port, unless a firewall blocks it.

### Docker

Inside a container `/metrics` is on by default and listens on port 9100 on every IPv4 interface of the container, so there is nothing to turn on. Other containers and hosts with a route to the container can reach it even without a published port. To scrape from outside, publish the port on an address Prometheus can reach:

```bash
docker run -d --name urnetwork -p 100.64.0.10:9100:9100 ... ghcr.io/full-bars/meso-miner:latest
```

The `urnet-tools` inside the image does not handle `metrics`, so `docker exec urnetwork urnet-tools metrics on` fails. A container with a public address on one of its interfaces stays on loopback and Tailscale instead (see the warning below).

Here `100.64.0.10` is the host's Tailscale address, and the Prometheus target is `100.64.0.10:9100`. For several containers on one host, publish each on its own host port (`-p 100.64.0.10:9101:9100`) and use that port as the target.

> [!WARNING]
> With `--network host`, the container shares the host's interfaces. If the host has no public address, the default binds every host interface, including the LAN. If it has one, the default stays on loopback and Tailscale. To choose the address, set `URNETWORK_METRICS=<tailscale-ip>:9100` in the container's environment. `urnet-tools metrics listen` does not run inside the image.

## 2. Run the monitoring stack

You need Docker with Compose v2 on the monitoring machine. Download `urnetwork-monitoring-<version>.tar.gz` from the [latest release](https://github.com/full-bars/meso-miner/releases/latest), then:

```bash
tar xzf urnetwork-monitoring-*.tar.gz
cd monitoring
./setup.sh 100.64.0.10:9100=node-1 100.64.0.11:9101=node-2
docker compose up -d
```

Each `address=name` argument adds one provider. Use the address from `metrics status`; the name is what the dashboard shows. On first run, `setup.sh` also creates `.env` with a random Grafana admin password.

Open `http://<monitoring machine>:3000` and sign in as `admin` with the password from `.env`. The **URnetwork Providers** dashboard is the home page.

To add providers later, run `./setup.sh` again with the new ones. Prometheus picks them up within a minute, with no restart.

> [!IMPORTANT]
> Grafana listens on port 3000 on every interface, protected by the admin password. To keep it private, set `GRAFANA_BIND` in `.env` to `127.0.0.1` or the machine's Tailscale address, then run `docker compose up -d` again.

### Settings

Add any of these to `.env`, then run `docker compose up -d`:

| Variable | Default | Meaning |
|---|---|---|
| `GRAFANA_BIND` | `0.0.0.0` | Address Grafana listens on |
| `GRAFANA_PORT` | `3000` | Grafana port |
| `PROMETHEUS_PORT` | `9090` | Prometheus port, always on `127.0.0.1` |
| `PROMETHEUS_RETENTION` | `30d` | How long Prometheus keeps data |

## What the dashboard shows

- **Fleet:** nodes up and down, billable throughput, active clients, and proxies up or failing. A table lists each node's version, uptime, clients, proxies, memory, and resource pressure.
- **Traffic:** billable and total bytes per second, clients per node, and the top 15 proxies by billable traffic.
- **Proxies:** pool status, grades, recoveries and losses, and contract outcomes.
- **Node health:** errors by category, resource pressure, memory, goroutines, sessions, and DNS-over-HTTPS failures. Two panels plot memory and file descriptors against their limits (the limit is a dashed line).
- **Transport (H1 and H3):** payload bytes per second by transport mode and direction, the share of outbound frames carried by H3, H3 connections up, H3 connect attempts, failures and drops per hour, how many connections offered and had QUIC DATAGRAM accepted, datagram messages received and dropped per second, and the WhoDis DNS and DNS-pump transports (connections up, attempts and failures). Auto mode does not start the DNS modes, so that panel stays at zero unless a transport was built with one of those target modes.
- **Lifecycle:** restarts by reason, uptime per node, HotSwap outcomes per hour, and version skew (each version in the fleet and how many nodes run it).

The transport panels need a provider that exports the `urnet_transport_*` and `urnet_h3_*` series. On older providers they stay empty, and the H3 and DATAGRAM series only move once `h3` and `h3_datagram` are switched on.

Use the **Node** selector at the top to narrow the view to specific providers.

The memory, descriptor and restart-reason panels need a provider that exports `urnet_mem_limit_bytes`, `urnet_rss_bytes`, `urnet_open_fds`, `urnet_fd_limit` and `urnet_restart_reason`. On older providers those panels stay empty and the rest of the dashboard works as before. Descriptor metrics are Linux only.

## Alerts

The bundle ships alert rules in `monitoring/prometheus/rules/urnetwork.yml`. Prometheus loads them at startup, and `docker compose up -d` mounts the `rules` directory for you. Firing alerts show at `http://127.0.0.1:9090/alerts`.

> [!NOTE]
> The bundle has no Alertmanager. Alerts are visible in Prometheus and as the `ALERTS` series, but nothing is emailed or posted until you add an Alertmanager and point Prometheus at it.

### Enabled by default

| Alert | Fires when | Does not detect |
|---|---|---|
| `UrnetworkNodeDown` | A node's `/metrics` endpoint has not answered for 5 minutes (`up == 0`). | A stopped provider looks the same as metrics switched off, a blocked port or a Tailscale outage. A node removed from `targets/providers.yml` never fires. It says nothing about whether a reachable node is earning. |
| `UrnetworkRestartLoop` | More than 3 restarts of one node in an hour. | A restart that hides between two scrapes, and the cause of a restart. Check `urnet_restart_reason` and the node's logs. |

`UrnetworkRestartLoop` counts drops of `urnet_uptime_seconds` with `resets()`. It does not use `urnet_startup_restarted`, because that gauge keeps one fixed value for the life of a process, so it cannot be counted with `increase()`.

### Optional rules

These are in the same file, commented out, because they need thresholds that suit your fleet.

| Alert | Fires when | Does not detect |
|---|---|---|
| `UrnetworkOldVersion` | A node runs a different version from most of the fleet for 7 days. | Which version is newer. During a slow rollout it can flag the early upgraders. It does not fire on a fleet that is uniformly out of date. |
| `UrnetworkMemoryNearLimit` | `urnet_mem_sys_bytes` is above 90% of `urnet_mem_limit_bytes` for 15 minutes. | Resident memory, other processes on the machine, and a container limit lower than the Go limit. Nodes with no limit set are skipped. |
| `UrnetworkFileDescriptorsNearLimit` | `urnet_open_fds` is above 85% of `urnet_fd_limit` for 10 minutes. | Which kind of descriptor leaks, and a spike between two scrapes. |
| `UrnetworkNoBillableTraffic` | A node is up, has been up for over 30 minutes, and billed no traffic for 30 minutes. | A broken node versus a quiet one with no demand. It can be noisy on idle nodes. |

To enable one, open `monitoring/prometheus/rules/urnetwork.yml`, read the note above the rule, adjust the threshold, and remove the leading `# ` from every line of that block (the block ends at the next blank line). Then reload Prometheus:

```bash
curl -X POST http://127.0.0.1:9090/-/reload
```

Nodes whose provider does not export a metric never match its rule, so a fleet with mixed versions is safe.

### Checking your changes

If you have `promtool` (it ships with Prometheus), run these from the `monitoring/` directory after editing the rules:

```bash
promtool check rules prometheus/rules/urnetwork.yml
promtool test rules prometheus/rules/urnetwork.test.yml
```

The test file covers the two enabled rules. `prometheus.yml` lists `urnetwork.yml` by name, so the test file is never loaded as rules.

## Troubleshooting

**A node shows as down.** On that node, run `urnet-tools metrics status` and check that the address matches the target in `prometheus/targets/providers.yml`. From the monitoring machine, `curl http://<target>/metrics | head` should print metrics. `connection refused` means nothing listens on that address. A timeout usually means a firewall or Tailscale ACL is blocking the port.

**The target in Prometheus shows an error.** Open `http://127.0.0.1:9090/targets` on the monitoring machine. The error column says why the last scrape failed.

**Panels are empty right after starting.** Rates need two scrapes, so allow about a minute.

Cumulative counters survive a provider restart. Traffic, transport, contract, error, DoH, control-command and loop-restart totals are written to `~/.urnetwork/metrics_counters.json` every 45 seconds and on a clean shutdown, and a starting provider adds the stored totals before it serves its first scrape. A restart (upgrade, reboot, crash) therefore continues every series instead of resetting it to zero, and a 24-hour delta computed from `/metrics` alone stays correct across one. A clean shutdown flushes the final totals; a crash can lose the increments since the last successful flush, which bounds one write interval of accuracy rather than resetting the series. A HotSwap candidate restores the store but does not write to it while the parent is still serving; on takeover it catches up to the parent's last write and becomes the writer.

Because a series continues instead of resetting, `rate()` or `increase()` computed across a restart would read the restored total as one step of traffic. The shipped dashboards therefore drop a node's series for the five minutes after `urnet_process_start_time_seconds` changes, so the restart is not shown as a spike; hand-written queries that rate these counters should do the same. Gauges are process-scoped and are not restored. `urnet_process_start_time_seconds` marks when the current process started, and `urnet_uptime_seconds` remains the current process's uptime.

## Metrics reference

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `urnet_info` | gauge | `version` | Always 1; carries the provider version |
| `urnet_uptime_seconds` | gauge | | Provider uptime |
| `urnet_process_start_time_seconds` | gauge | | Unix time the current provider process started |
| `urnet_billable_bytes_total` | counter | `direction` | Billable bytes, all proxies, persisted across restarts |
| `urnet_bytes_total` | counter | `direction` | All bytes, all proxies, persisted across restarts |
| `urnet_clients_active` | gauge | | Active clients, all proxies |
| `urnet_connections_active` | gauge | | Active connections |
| `urnet_transport_frames_total` | counter | `mode`, `dir` | Payload frames carried by each platform transport mode (`h1`, `h3`, `h3dns`, `h3dnspump`) per direction. Keepalives and speed or latency echoes are excluded, persisted across restarts |
| `urnet_transport_payload_bytes_total` | counter | `mode`, `dir` | Payload bytes carried by each platform transport mode, before framing, persisted across restarts |
| `urnet_transport_direct_h1_frames_total` | counter | `dir` | Payload frames carried by H1 for the direct identity only. This is the fair comparison for H3, which runs for that identity alone; `urnet_transport_frames_total{mode="h1"}` is every proxy identity together, persisted across restarts |
| `urnet_transport_direct_h1_payload_bytes_total` | counter | `dir` | Payload bytes carried by H1 for the direct identity only, persisted across restarts |
| `urnet_h3_up` | gauge | | H3 connections up now |
| `urnet_h3_connect_attempts_total` | counter | | H3 connect attempts, persisted across restarts |
| `urnet_h3_connects_total` | counter | | H3 connections that authenticated, persisted across restarts |
| `urnet_h3_connect_failures_total` | counter | | H3 connect attempts that failed, persisted across restarts |
| `urnet_h3_drops_total` | counter | | H3 connections that ended while still wanted. Switching `h3` or `h3_datagram` is not one, persisted across restarts |
| `urnet_h3_datagram_offered_total` | counter | | H3 connections that offered QUIC DATAGRAM, persisted across restarts |
| `urnet_h3_datagram_accepted_total` | counter | | H3 connections where the server accepted DATAGRAM. Offered without accepted means an old server, persisted across restarts |
| `urnet_h3_datagram_rx_messages_total` | counter | | Messages received over DATAGRAM, persisted across restarts |
| `urnet_h3_datagram_rx_bytes_total` | counter | | Bytes of messages received over DATAGRAM, persisted across restarts |
| `urnet_h3_datagram_rx_dropped_total` | counter | | Received DATAGRAM messages dropped because the receive route was full, persisted across restarts |
| `urnet_h3_datagram_rx_rejected_total` | counter | `reason` | DATAGRAMs refused by the datagram layer: `malformed`, `duplicate`, `checksum`, persisted across restarts |
| `urnet_h3_datagram_tx_messages_total` | counter | | Messages sent over DATAGRAM, persisted across restarts |
| `urnet_h3_datagram_tx_bytes_total` | counter | | Bytes of messages sent over DATAGRAM, persisted across restarts |
| `urnet_h3_datagram_tx_stream_messages_total` | counter | | Messages sent on the reliable stream of a connection that negotiated DATAGRAM, so the lane split is tx_messages against this, persisted across restarts |
| `urnet_h3_datagram_tx_errors_total` | counter | | DATAGRAM send errors, persisted across restarts |
| `urnet_h3_datagram_blackholes_total` | counter | | Connections whose datagram send was switched off because datagrams went out and none came back, persisted across restarts |
| `urnet_transport_pt_up` | gauge | `mode` | Packet-translation (`h3dns`, `h3dnspump`) transport connections up now |
| `urnet_transport_pt_connect_attempts_total` | counter | `mode` | Packet-translation transport connect attempts, persisted across restarts |
| `urnet_transport_pt_connects_total` | counter | `mode` | Packet-translation transport connections that authenticated, persisted across restarts |
| `urnet_transport_pt_connect_failures_total` | counter | `mode` | Packet-translation transport connect attempts that failed, persisted across restarts |
| `urnet_transport_pt_drops_total` | counter | `mode` | Packet-translation transport connections that ended while still wanted, persisted across restarts |
| `urnet_dualstack_mode` | gauge | `mode`, `reason` | Direct-identity platform transport mode, value always 1: `group` (v4 pin, v6 pin and standby) or `legacy` (single), with the decision reason |
| `urnet_dualstack_transport_state` | gauge | `transport`, `state` | Per-transport state: 1 for the state the transport is in (`ipv4`, `ipv6`, `standby`) |
| `urnet_dualstack_connect_attempts_total` | counter | `family`, `mode` | Platform connect attempts of the native identity's transports |
| `urnet_dualstack_connects_total` | counter | `family`, `mode` | Platform connects that authenticated |
| `urnet_dualstack_connect_failures_total` | counter | `family`, `mode` | Platform connect attempts that failed |
| `urnet_dualstack_drops_total` | counter | `family`, `mode`, `cause` | Sessions that ended on their own, by close cause |
| `urnet_dualstack_sessions_up` | gauge | `family` | Platform sessions up right now |
| `urnet_dualstack_connected_seconds_total` | counter | `family` | Seconds with at least one session up (overlapping carriers count once) |
| `urnet_dualstack_last_connect_timestamp_seconds` | gauge | `family` | Unix time of the last connect, 0 if none yet |
| `urnet_dualstack_last_drop_timestamp_seconds` | gauge | `family` | Unix time of the last session drop, 0 if none yet |
| `urnet_dualstack_family_mismatch_total` | counter | `family` | Pinned transports that connected over the other family; anything above 0 is a bug |
| `urnet_dualstack_session_lifetime_seconds` | histogram | `family` | Lifetime of ended sessions |
| `urnet_dualstack_pinned_down` | gauge | | 1 while no pinned transport is connected, after the first connect |
| `urnet_dualstack_pinned_down_events_total` | counter | | Times every pinned transport was down at once |
| `urnet_dualstack_pinned_down_seconds_total` | counter | | Seconds with no pinned transport connected |
| `urnet_dualstack_pinned_down_longest_seconds` | gauge | | Longest single stretch with no pinned transport connected |
| `urnet_dualstack_standby_active` | gauge | | 1 while the family-agnostic standby is dialing because no pinned transport is connected |
| `urnet_dualstack_standby_activations_total` | counter | | Times the standby started dialing |
| `urnet_dualstack_standby_active_seconds_total` | counter | | Seconds the standby has been dialing |
| `urnet_h1_max_message_bytes` | gauge | | Largest complete message read on any H1 transport (websocket or H1+); the read limit is 65535 |
| `urnet_h1plus_enabled` | gauge | | 1 while H1+ upgrades are allowed (`h1_plus`, default on) |
| `urnet_h1plus_attempts_total` | counter | | H1 dials that attempted the H1+ upgrade |
| `urnet_h1plus_accepted_total` | counter | | H1 dials the server upgraded to the custom H1+ carrier |
| `urnet_h1plus_websocket_selected_total` | counter | | H1 dials that ended on a plain websocket (a fallback, or H1+ off) |
| `urnet_h1plus_fallbacks_total` | counter | `reason` | H1+ upgrades that fell back to a websocket: `rejected`, `invalid`, `io`, `proxy` |
| `urnet_h1plus_handshake_failures_total` | counter | | H1+ upgrades that failed without falling back (TLS, authentication) |
| `urnet_h1plus_auth_failures_total` | counter | | H1+ upgrades answered 401 or 403 |
| `urnet_h1plus_handshake_seconds_total` | counter | | Time spent in H1+ handshakes, seconds |
| `urnet_h1plus_messages_total` | counter | | Messages carried on H1+ connections |
| `urnet_h1plus_bytes_total` | counter | | Message bytes carried on H1+ connections |
| `urnet_h1plus_writes_total` | counter | | Physical writes on H1+ connections |
| `urnet_h1plus_flushes_total` | counter | | Batched flushes on H1+ connections |
| `urnet_h1plus_read_errors_total` | counter | | Read errors on H1+ connections |
| `urnet_h1plus_write_errors_total` | counter | | Write errors on H1+ connections |
| `urnet_proxy_pool_size` | gauge | `status` | Proxies by status: up, connecting, degraded, dead |
| `urnet_proxy_bytes_total` | counter | `proxy`, `direction` | Bytes per proxy, persisted across restarts |
| `urnet_proxy_billable_bytes_total` | counter | `proxy`, `direction` | Billable bytes per proxy, persisted across restarts |
| `urnet_proxy_clients` | gauge | `proxy` | Active clients per proxy |
| `urnet_proxy_session_age_seconds` | gauge | `proxy` | Length of the current client presence window per proxy |
| `urnet_proxies_known` | gauge | | Proxies in the provider's proxy state |
| `urnet_proxy_grades` | gauge | `tier` | Proxies by grade |
| `urnet_proxy_health` | gauge | `status` | Proxies by recorded health |
| `urnet_proxy_graded_recent` | gauge | | Proxies graded in the last hour |
| `urnet_proxy_graded_stale` | gauge | | Proxies with an older grade |
| `urnet_proxy_auth_failures` | gauge | | Auth failures summed over current proxies |
| `urnet_url_proxy_grades` | gauge | `tier` | URL-sourced proxies by grade |
| `urnet_url_proxy_ungraded` | gauge | | URL-sourced proxies not yet graded |
| `urnet_contracts_total` | counter | `result` | Contract outcomes since start, persisted across restarts |
| `urnet_errors_total` | counter | `category` | Errors since start, persisted across restarts |
| `urnet_doh_failures_total` | counter | | DNS-over-HTTPS failures, persisted across restarts |
| `urnet_sessions_pqe` | gauge | | Active post-quantum sessions |
| `urnet_sessions_classical` | gauge | | Active classical sessions |
| `urnet_sessions_opened_total` | counter | `encryption` | Sessions opened, lifetime |
| `urnet_sessions_opened_recent` | gauge | `window`, `encryption` | Sessions opened in the last hour, day, or week |
| `urnet_lifetime_sessions_total` | counter | `encryption` | Sessions, persisted across restarts |
| `urnet_lifetime_contracts_total` | counter | `result` | Contract outcomes, persisted across restarts |
| `urnet_lifetime_proxies_total` | counter | `event` | Proxy recoveries and losses, persisted across restarts |
| `urnet_lifetime_billable_bytes_total` | counter | | Billable bytes, persisted across restarts |
| `urnet_lifetime_errors_total` | counter | `category` | Errors, persisted across restarts |
| `urnet_hotswap_outcomes_total` | counter | `reason` | HotSwap attempt outcomes |
| `urnet_control_commands_total` | counter | `cmd` | Control socket commands, persisted across restarts |
| `urnet_startup_clean_shutdown` | gauge | | 1 if the previous run shut down cleanly |
| `urnet_startup_restarted` | gauge | | 1 if the previous run did not shut down cleanly |
| `urnet_startup_upgraded` | gauge | | 1 if the version changed since the previous run |
| `urnet_startup_previous_version` | gauge | `version` | Always 1; the version before the last upgrade |
| `urnet_pressure_score` | gauge | | Resource pressure, 0 (fine) to 1 (emergency) |
| `urnet_mem_heap_bytes` | gauge | | Go heap in use |
| `urnet_mem_sys_bytes` | gauge | | Memory obtained from the OS |
| `urnet_mem_limit_bytes` | gauge | | The Go memory limit in effect |
| `urnet_rss_bytes` | gauge | | Resident set size (Linux) |
| `urnet_open_fds` | gauge | | Open file descriptors (Linux) |
| `urnet_fd_limit` | gauge | | File descriptor limit (Linux) |
| `urnet_restart_reason` | gauge | `reason` | Value 1 for the reason the current process started; absent for other reasons |
| `urnet_gc_cycles_total` | counter | | Garbage collection cycles |
| `urnet_gc_gogc` | gauge | | GOGC value currently in force |
| `urnet_gc_tightening` | gauge | | 1 while the GC governor holds GOGC below its baseline |
| `urnet_gc_cpu_fraction` | gauge | | Share of CPU spent in GC over the last window; absent until a window completes |
| `urnet_conntrack_used_ratio` | gauge | | `nf_conntrack_count` over `nf_conntrack_max` (Linux, when conntrack is loaded). Overflow drops packets silently |
| `urnet_tcp_time_wait` | gauge | | TCP sockets in TIME_WAIT (Linux) |
| `urnet_tcp_orphans` | gauge | | Orphaned TCP sockets (Linux) |
| `urnet_goroutines` | gauge | | Goroutines |
| `urnet_pool_latency_ms` | gauge | | Message pool average latency |
| `urnet_loop_restarts_total` | counter | `loop` | Times a supervised background loop ended unexpectedly and was restarted, persisted across restarts |
| `urnet_loop_up` | gauge | `loop` | 1 while a supervised loop is running, 0 while it is stopped or backing off |
