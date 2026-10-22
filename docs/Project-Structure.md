# Project Structure

A map of the meso-miner repository, grouped by what each part is for. You do not need any of this to run a node. It is here for people who want to read or change the code.

meso-miner is the production-oriented fork of the URnetwork provider. The Go module is still named `github.com/urnetwork/connect`, so import paths look like upstream. Go 1.27.3.

## The three programs you actually run

| Program | Source | What it is |
|---|---|---|
| `provider` | `provider/` | The relay node. Logs in, connects your proxies, earns. |
| `urnet-tools` | `cmd/urnet-tools/` + `internal/urnettools/` | The operator tool: status, logs, update, proxies, tuning. Used on Linux, macOS, Windows and FreeBSD hosts. |
| `urnet-docker` | `cmd/urnet-docker/` | The same tool for Docker hosts. It runs on the host and reaches into your containers. |

`cmd/fake-provider/` is a stand-in provider used only by CI lifecycle tests. `connectctl/` is an upstream developer tool and is not used in production.

## Provider (`provider/`)

Everything the provider binary does that is not the shared network stack.

- Entry point and runtime: `main.go`, `startup_banner.go`, `control_socket.go`, `control_state.go`, `pending_overrides.go`, `hotswap*.go`, `thrash_watchdog.go`, `liveness_watchdog.go`, `loop_supervisor.go`.
- Proxy handling: `proxy_url*.go` (URL sources), `proxy_reload.go` (hot reload), `proxy_state.go`, `proxy_id.go`, `proxy_health_log.go`, `proxy_slow_retry.go`, `proxy_probe.go`, `proxy_benchmark.go`, `proxy_grade_*.go`, `proxy_trim*.go`, `proxy_udp_check.go`, `socks5_udp.go`, `ssrf_guard.go`.
- Login and identity: `auth_rate_limiter.go`, `proxy_admission_gate.go`, `client_jwt_*.go`, `network_refresh.go`, `renewal_watcher.go`, `wallet_*.go`.
- Transport choices: `family_transport.go` (dual-stack for the direct identity), `h3_*.go` (HTTP/3), `direct.go`.
- Bittensor and claim commands: `sn*.go`, `claim_*.go`, `bt_*.go`, `fleet*.go`, `hotkey_wallet.go`.
- Logging and diagnostics: `tlog.go`, `shmlog_*.go`, `important_log.go`, `audit_ring.go`, `node_snapshot*.go`, `metrics_*.go` (Prometheus), `incident_capture.go`.

## Operator tool (`cmd/urnet-tools/`, `cmd/urnet-docker/`, `internal/`)

- `internal/urnettools/` holds all the logic: command tree (`cobra.go`), provider discovery (`discover*.go`), update with checksum verification (`update.go`, `release.go`), start/stop/restart (`lifecycle*.go`), proxy commands (`proxy.go`), session save/load, self-heal, log following, and Docker support (`cli_docker.go`, `docker.go`).
- `internal/tui/` is the terminal drawing kit behind `urnet-tools top`.
- `internal/verdictfeed/` reads the signed backend-health feed (off by default).
- `internal/promtext/`, `internal/durable*/` are small support packages for metrics checks and durable state.

## Network stack (repository root)

The Go files in the root directory are the shared engine. By prefix:

| Prefix | What it covers |
|---|---|
| `transfer*.go` | Contracts, encryption, routing, queues, retries |
| `transport*.go`, `http_upgrade*.go`, `message_framer*.go` | Connections to the platform: WebSocket (H1), H1+, QUIC/H3, WebRTC, pluggable tunnel |
| `net*.go`, `egress*.go`, `control_family*.go` | Dialing, DNS-over-HTTPS, resilient connections, dual-stack |
| `ip*.go` | IP packet relay, NAT, fragments, ICMP, UDP |
| `ip_security*.go` | Traffic policy, including the CFAA, DMCA and messaging rules |
| `proxy_*.go` (root) | Proxy scoring and target-family logic shared with the provider |
| `reconnect_*.go`, `transport_flap_guard.go` | Protection against reconnect storms |
| `message_pool*.go`, `memory_budget.go`, `sysmem_*.go`, `tuning.go` | Memory use and auto-tuning |
| `jwt.go`, `api*.go` | Platform API client and tokens |
| `log*.go`, `metrics_prometheus.go`, `trace.go` | Logging and Prometheus metrics |

Test files sit beside the code they test (`*_test.go`).

## Supporting code

- `protocol/`: protocol buffer definitions (`*.proto`) and generated Go. Do not edit the generated files; run `make` in that folder.
- `api/`: platform API definitions.
- `extender/`: extender transport.
- `blocker/`: tool that builds the default block lists. `security/`: source list URLs for the IP blocklist.
- `hotkeywallet/`, `durablesys/`, `durablevolume/`: Bittensor wallet storage and durable-disk support.
- `emoji/`, `res/`: small assets.
- `third_party/`: vendored Go packages (Substrate RPC client, Windows named pipes). Licenses are in `THIRD_PARTY_LICENSES.md`.

## Install, packaging and deployment

- `scripts/`: installers and uninstallers (`Provider_Install_Linux.sh`, `_Mac.sh`, `_Win32.ps1`, `_FreeBSD.sh`), the Docker host installer (`install-urnet-docker.sh`), and release helpers (`pre-tag-check.sh`, `compute-meso-tag.sh`).
- `Dockerfile` and `docker/scripts/`: the container image. `entrypoint.sh` picks the start script from the `BUILD` setting.
- `pelican/`: egg for the Pelican game-server panel.
- `monitoring/`: ready-made Prometheus and Grafana stack. See [Monitoring](/full-bars/meso-miner/wiki/Monitoring).
- `.github/workflows/`: CI. `build.yml` builds and tests, `release.yml` publishes releases, plus installer, OpenRC, FreeBSD, CodeQL and soak checks.

## Documentation

- [README.md](/full-bars/meso-miner/blob/main/README.md): start here.
- `docs/`: one page per topic. `docs/guides/` has beginner, intermediate and advanced walkthroughs. `docs/design/` is design history for maintainers.
- [CHANGELOG.md](/full-bars/meso-miner/blob/main/CHANGELOG.md) and `releases/`: what changed in each release. Release notes before the `v2026...` ones are inherited from the 3.23-fix line.
- [FORK_CHANGES.md](/full-bars/meso-miner/blob/main/FORK_CHANGES.md): everything this fork changed compared with upstream.
- [LOG_REFERENCE.md](/full-bars/meso-miner/blob/main/LOG_REFERENCE.md): what each log line means.
- [CODESTYLE.md](/full-bars/meso-miner/blob/main/CODESTYLE.md), [AI.md](/full-bars/meso-miner/blob/main/AI.md): conventions and a guide for AI assistants.

## What was removed

The Hub (fleet dashboard server) and its commands were removed in v32.0. Fleet visibility now comes from Prometheus `/metrics` (`urnet-tools metrics on`) with the monitoring bundle and Grafana.
