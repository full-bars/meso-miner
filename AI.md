# AI Guide: URnetwork Provider (meso-miner)

This file is for an AI assistant that helps a user install, configure and manage the provider. It points at the project docs instead of repeating them. Give it to your AI together with the linked pages.

Always check a command against `urnet-tools help <command>` or the linked doc before running anything that changes a live system.

## Project layout

See [PROJECT_STRUCTURE.md](PROJECT_STRUCTURE.md) for the full map. In short:

- `provider/` is the relay binary that serves proxies and earns.
- `cmd/urnet-tools`, `cmd/urnet-docker` and `internal/urnettools/` are the operator tool. The shell installer `scripts/Provider_Install_Linux.sh` only installs; it is not the CLI.
- `docs/` holds the user documentation.
- `Dockerfile` and `docker/scripts/` build the container image.
- `monitoring/` is the Prometheus and Grafana bundle.

## Install the provider

```bash
curl -fsSL https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Install_Linux.sh | sh
```

This installs the provider and the `urnet-tools` command. macOS, Windows, FreeBSD and Alpine (OpenRC) have their own installers. Full details: [docs/Installation.md](docs/Installation.md).

### Authenticate

The user needs an auth code from [app.ur.network](https://app.ur.network), [ur.io](https://ur.io/) or the mobile app.

```bash
urnetwork auth                  # prompts for the code
urnet-tools auth                # same; paste the code at the prompt, never on the command line
```

Docker deployments can use `URNETWORK_AUTH_CODE` or an email and password instead. See [Docker Deployment](docs/Docker-Deployment.md).

### Add proxies

```bash
urnet-tools proxy add proxies.txt           # one proxy per line
urnet-tools proxy add-source https://...    # live URL source that refreshes itself
urnet-tools summary                         # sources, health and counts
urnet-tools proxy refresh                   # reload live, no restart
```

Docs: [Proxy Management](docs/Proxy-Management.md), [Proxy URL Sources](docs/Proxy-URL-Sources.md), [Adding Proxies](docs/Adding-Proxies.md).

## Docker

Full guide: [docs/Docker-Deployment.md](docs/Docker-Deployment.md).

```bash
docker pull ghcr.io/full-bars/meso-miner:latest
```

Manage containers from the host with `urnet-docker` (install with `scripts/install-urnet-docker.sh`). It takes the same commands as `urnet-tools`.

## Performance tuning

Full guide: [docs/High-Volume-Performance-Tuning.md](docs/High-Volume-Performance-Tuning.md).

```bash
urnet-tools auto on           # pick a profile from detected RAM
urnet-tools turbo v8          # high-throughput profile
urnet-tools eco on            # low-memory GC tuning
urnet-tools optimize          # raise OS limits (ulimit, conntrack)
urnet-tools ramlogs on        # logs in RAM (/dev/shm)
```

## Common urnet-tools commands

```bash
# Service
urnet-tools status | start | stop | restart
urnet-tools logs [N]               # last N lines, then follow
urnet-tools top                    # live full-screen view
urnet-tools update                 # upgrade the provider
urnet-tools hotswap                # swap to a new binary without dropping the provider process
urnet-tools self-update            # update urnet-tools itself
urnet-tools uninstall

# Proxies
urnet-tools proxy add <file> | paste | clear | remove | refresh
urnet-tools proxy health           # up, degraded and dead proxies
urnet-tools proxy traffic          # live bandwidth per proxy
urnet-tools proxy trim <N>         # hold at N proxies, shed the worst first
urnet-tools proxy remove-dead      # interactive cleanup

# Settings
urnet-tools set [<key> [<value>|off]]   # show or change a runtime setting
urnet-tools config [--json]             # every setting and where it came from
urnet-tools profile [<name>]            # memory and GC profile
urnet-tools metrics on|off              # Prometheus /metrics
urnet-tools usage [graphs]              # billable vs control traffic
urnet-tools choose-network <api_url> <connect_url>   # custom backend
urnet-tools choose-network --reset

# Identity
urnet-tools rename <name>               # dashboard display label
urnet-tools session save|load <file>    # move a node between machines
urnet-tools providers [--all]           # list providers on this box
urnet-tools sn-status                   # Bittensor subnet 25 status
```

On a box with more than one provider, add a target flag such as `--unit`, `--user`, `--network`, `--network-id` or `--state-dir`. Full reference: [docs/urnet-tools-go.md](docs/urnet-tools-go.md).

## Environment variables

Full reference: [docs/Configuration.md](docs/Configuration.md).

| Variable | Purpose |
|----------|---------|
| `BUILD` | Docker start mode: `stable`, `nightly` or `jwt` |
| `USER_AUTH` / `PASSWORD` | Email and password login |
| `URNETWORK_AUTH_CODE` | Auth code for first run |
| `UR_API_URL` / `UR_CONNECT_URL` | Custom API and connect backend (set both, Docker) |
| `URNETWORK_PROFILE` | `auto`, `lowmem`, `eco`, `turbo-v4`, `turbo-v8` |
| `PROXY_URL` | Live proxy list URL, comma-separated for several |
| `URNETWORK_RAMLOGS` | `1` logs to `/dev/shm` |
| `URNETWORK_HEALTH_INTERVAL` | How often the `[health]` line is logged |
| `URNETWORK_SKIP_AUDIT` | `1` skips the startup system audit |
| `URNETWORK_HOT_RESTART` | Reuse the client login across restarts. On by default; `0` turns it off |

## Monitoring

The old hub dashboard no longer exists. For many machines, run `urnet-tools metrics on` on each and use the bundle in `monitoring/`. See [docs/Monitoring.md](docs/Monitoring.md).

## Releases

- Release tags look like `v<YYYY.M.D>-<unix>-meso`, for example `v2026.10.20-1076956260-meso`.
- The Docker image is `ghcr.io/full-bars/meso-miner`; `:latest` follows the newest release.
- `urnet-tools update` resolves "latest" from this repository's releases.
- Notes: [CHANGELOG.md](CHANGELOG.md) and `releases/`.

## Key files

| Path | Purpose |
|------|---------|
| `~/.local/share/urnetwork-provider/bin/urnetwork` | Provider binary |
| `~/.local/share/urnetwork-provider/bin/urnet-tools` | Operator tool |
| `~/.urnetwork/provider.sock` | Control socket used by `urnet-tools` |
| `~/.urnetwork/provider_state.json` | Saved runtime settings |
| `~/.urnetwork/pending_overrides.json` | Changes queued while the provider is stopped |
| `~/.config/systemd/user/urnetwork.service` | Provider systemd unit (user install) |
| `~/.config/systemd/user/urnetwork.service.d/override.conf` | Environment overrides |
| `/dev/shm/urnetwork.log` | RAM log when ramlogs is on |

## Troubleshooting

1. Run `urnet-tools status` and `urnet-tools logs`.
2. Check proxies with `urnet-tools proxy health`.
3. Look the log line up in [LOG_REFERENCE.md](LOG_REFERENCE.md).
4. See [docs/Troubleshooting.md](docs/Troubleshooting.md).

## Documentation index

| Doc | Covers |
|-----|--------|
| [Installation](docs/Installation.md) | Install and post-install setup |
| [Docker Deployment](docs/Docker-Deployment.md) | Docker run, Compose, scaling |
| [Configuration](docs/Configuration.md) | Environment variables and profiles |
| [Proxy Management](docs/Proxy-Management.md) | Hot reload, health, cleanup |
| [Proxy URL Sources](docs/Proxy-URL-Sources.md) | Live proxy feeds |
| [Performance Tuning](docs/High-Volume-Performance-Tuning.md) | Profiles and the optimizer |
| [Monitoring](docs/Monitoring.md) | Prometheus and Grafana |
| [Troubleshooting](docs/Troubleshooting.md) | Errors and resource problems |
| [Bittensor Operations](docs/Bittensor-Operations.md) | Subnet 25 tools |
| [Project Structure](PROJECT_STRUCTURE.md) | Directory layout |
| [Log Reference](LOG_REFERENCE.md) | Every log line |
| [Fork Changes](FORK_CHANGES.md) | Changes from upstream |
