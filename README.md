# meso-miner

[![CodeRabbit Pull Request Reviews](https://img.shields.io/coderabbit/prs/github/full-bars/meso-miner?utm_source=oss&utm_medium=github&utm_campaign=full-bars%2Fmeso-miner&labelColor=171717&color=FF570A&link=https%3A%2F%2Fcoderabbit.ai&label=CodeRabbit+Reviews)](https://coderabbit.ai)
[![CI](https://github.com/full-bars/meso-miner/actions/workflows/build.yml/badge.svg)](https://github.com/full-bars/meso-miner/actions)
![Go Version](https://img.shields.io/github/go-mod/go-version/full-bars/meso-miner?labelColor=171717&color=FF570A)
![Release](https://img.shields.io/github/v/release/full-bars/meso-miner?labelColor=171717&color=FF570A)
![Language](https://img.shields.io/github/languages/top/full-bars/meso-miner?labelColor=171717&color=FF570A)
![Activity](https://img.shields.io/github/commit-activity/m/full-bars/meso-miner?labelColor=171717&color=FF570A)

meso-miner runs a URnetwork provider: a small program on your server or home computer that shares your bandwidth with the network and earns for it. This is the meso-miner build, a production-focused fork made for people who run many proxies and many machines. It installs as a normal background service and updates itself with one command. It also supports Bittensor Subnet 25: sign one wallet message and your earnings can be paid there (see the [Bittensor Quick Start](docs/Bittensor-Quick-Start.md)).

## 🚦 Start here

New to this? Three steps to run a provider, then one optional step to get paid on Bittensor:

1. **Get an auth code** from [app.ur.network](https://app.ur.network), [ur.io](https://ur.io/), or the URnetwork mobile app.
2. **Install** with the one-liner for your system (see [Install](#install) below), then run `urnetwork auth` and paste the code.
3. **Check it works** with `urnet-tools status`.
4. **Get paid on Bittensor (optional).** Sign one wallet message, once, from your own computer. Your nodes do not change. See the [Bittensor Quick Start](docs/Bittensor-Quick-Start.md).

Steps 1 to 3 are all you need to start. Nothing has to be opened on your firewall, because the provider only makes outbound connections.

Not sure which guide to read? Pick your level:

| If you are... | Read |
| :--- | :--- |
| Brand new, want the shortest path | [🐣 Beginner](docs/guides/beginner.md) |
| Choosing between Docker and a normal install, adding your own proxies | [🧭 Intermediate](docs/guides/intermediate.md) |
| Running several servers and tuning them | [🚀 Advanced](docs/guides/advanced.md) |

## ✨ What meso-miner adds

- **Proxy handling made easy.** Load proxies from a file or a live URL, add or remove them while the provider runs, and see which ones are healthy (`urnet-tools proxy health`) with an A to F grade.
- **Updates without a process gap.** On an eligible node `urnet-tools update` hands over to the new version with automatic rollback, so there is no window without a provider process. Proxy connections rebuild over about 30 seconds after the handover, and the update falls back to a restart when HotSwap cannot run. See [HotSwap](docs/HotSwap.md).
- **Change settings live.** `urnet-tools set` changes tuning without editing service files or restarting.
- **Built-in traffic numbers.** `urnet-tools usage` shows billable traffic against control traffic, with history graphs.
- **Monitoring.** Turn on Prometheus metrics with `urnet-tools metrics on` and use the ready-made Grafana bundle. See [Monitoring](docs/Monitoring.md).
- **Safer under pressure.** Memory profiles (`auto`, `turbo`, `eco`, `lowmem`), a swap-thrash watchdog that restarts a stuck provider, and protection against reconnect storms during platform outages.
- **Many providers per machine.** One provider per OS user, with `urnet-tools providers` to list them.
- **Move a node.** `urnet-tools session save` and `session load` carry your identity and proxies to another machine.
- **Runs almost anywhere.** Linux (systemd and Alpine/OpenRC), macOS, Windows, FreeBSD and Docker.
- **Bittensor subnet 25 tools.** Sign one wallet message to get paid, then check with `provider wallet status`. See the [Bittensor Quick Start](docs/Bittensor-Quick-Start.md).

The complete list of differences from upstream is in [FORK_CHANGES.md](FORK_CHANGES.md).

## ₿ Bittensor Subnet 25

Most operators need to do one thing: sign a single wallet message, once, from their own computer. Your nodes do not change. Everything else here (hotkeys, claim daemons, fleets, multiple operators) is optional or advanced. Nothing here runs unless you start it, though leaving early settlement-v1 claims unclaimed can still cost you them.

- [Bittensor Quick Start](docs/Bittensor-Quick-Start.md): start here. Check where you stand, sign once, and see which commands you can ignore.
- [Bittensor operations](docs/Bittensor-Operations.md): the overview, from the two tiers and wallet consent through claims and the FAQ.
- [Bittensor head tier](docs/Bittensor-Head-Tier.md): the optional, advanced tier. Most operators should skip it.
- [Bittensor glossary](docs/Bittensor-Glossary.md): plain meanings of the words.
- [Bittensor claim daemons](docs/Bittensor-Claim-Daemons.md), [fleet](docs/Bittensor-Fleet.md), [hotkey delegation](docs/Bittensor-Hotkey-Delegation.md) and [multi-operator](docs/Multi-Operator.md): optional or advanced.

---

## 🗺 Find a topic

| If you want to... | Go here |
| :--- | :--- |
| **Get paid on Bittensor SN25 (do I need to do anything?)** | [Bittensor Quick Start](docs/Bittensor-Quick-Start.md) |
| Install on a Linux host as a user-level service | [Installation Guide](docs/Installation.md) |
| Run one Docker container | [Docker Deployment](docs/Docker-Deployment.md) |
| Run multiple containers on one host | [Multi-Container Scaling](docs/Multi-Container-Scaling.md) |
| Choose profiles, turbo mode, or host tuning | [Performance Tuning](docs/High-Volume-Performance-Tuning.md) |
| Understand environment variables | [Configuration Reference](docs/Configuration.md) |
| Understand what a log line means | [Log Message Reference](LOG_REFERENCE.md) |
| Track traffic usage (billable vs control overhead) | [Docker Deployment](docs/Docker-Deployment.md) · [CLI Reference](docs/urnet-tools-go.md) |
| Load a proxy file into the provider (per-OS) | [Adding Proxies](docs/Adding-Proxies.md) |
| Feed the provider a live proxy list URL | [Proxy URL Sources](docs/Proxy-URL-Sources.md) |
| Fix a problem | [Troubleshooting](docs/Troubleshooting.md) |
| Import into a Pelican game-server panel | [Pelican Panel](docs/Docker-Deployment.md#-pelican-panel) · [Egg README](pelican/README.md) |

---

## ⚡ Quick Start

### Install

**🐧 Linux (systemd)**

```sh
curl -fSsL https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Install_Linux.sh | sh
```

**🍎 macOS (launchd)**

```sh
curl -fSsL https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Install_Mac.sh | sh
```


**🐋 Docker**

```sh
docker pull ghcr.io/full-bars/meso-miner:latest
```

**🐋 Docker (management wrapper)**

```sh
curl -fSsL https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/install-urnet-docker.sh | sh
```

**🪟 Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Install_Win32.ps1 | iex
```

Windows Defender may flag this one-liner. See the note below.

> [!NOTE]
> Windows Defender may flag the Windows install one-liner, and it may flag the downloaded
> binaries. What we see are machine-learning heuristics (the `!ml` suffix), not signatures;
> for the binaries we publish they are false positives. Recent release pages record the scan
> results for the published binaries. If Defender blocks the one-liner, download the script,
> review it, and run it from disk instead. If Defender quarantines an extracted binary,
> allow it from Windows Security > Virus & threat protection > Protection history. Both lines
> go in PowerShell:
>
> ```powershell
> irm https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Install_Win32.ps1 -OutFile "$env:TEMP\install-win.ps1"
> powershell -NoProfile -ExecutionPolicy Bypass -File "$env:TEMP\install-win.ps1"
> ```

<details>
<summary>The detections you may see, and what each one means</summary>

- `Trojan:Script/Wacatac.B!ml`, `Trojan:Script/Wacatac.C!ml`: Defender's machine-learning label for the PowerShell installer script fetching and extracting a remote payload.
- `Trojan:Win32/Wacatac.B!ml`, `Trojan:Win32/Wacatac.C!ml`: Defender's machine-learning label for files whose shape looks like a packed trojan. Our Go binaries are stripped, statically linked and unsigned, which reads as a packed payload. The B and C variants are different model generations, so one binary can be flagged under more than one name. `Trojan:Win32/Execution.A!ml` is another label from the same family on some builds.
- `Trojan:Win32/Commando.A!ml`: fires on the download-and-run command line itself (the `irm ... | iex` one-liner), not on the installed files. Fetching a remote script and piping it into execution reads as a trojan-downloader pattern to the model.
- `Trojan:Win32/Bearfoos.A!ml`: a behavioural label for scheduled-task activity. The installer registers Task Scheduler tasks - a weekly update task (on `latest` installs) and, if you accept auto-start, a logon task so the provider starts at login - and a behavioural model cannot tell that apart from persistence malware.

</details>

### After you install

After installation, authenticate and start providing:

```bash
# Linux / macOS: one Go binary on every platform
urnetwork auth
urnet-tools proxy add ~/proxies.txt
urnet-tools proxy refresh
urnet-tools auto on
```
On Windows, run the same commands in PowerShell but use a Windows path, e.g.
`urnet-tools proxy add "$env:USERPROFILE\Downloads\proxies.txt"`. Full per-OS
walkthrough, including the `.txt.txt` extension trap: [Adding Proxies](docs/Adding-Proxies.md).

> [!NOTE]
> `urnet-tools` is a provider-aware Go binary. It discovers every provider on the box and **refuses to act on an ambiguous target** - on multi-provider machines, pass `--unit` / `--user` / `--network` / `--network-id` / `--state-dir`. See [docs/urnet-tools-go.md](docs/urnet-tools-go.md).
>
> Docker-only deployments: the provider runs in a container, but the management tool (`urnet-docker`) runs **on the docker host, outside the container**. Install it with the one-liner above (use `curl -fSsL https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/install-urnet-docker.sh | sh -s -- urnet-tools` for the systemd variant). The tool self-updates afterward (`urnet-docker update`).

### Uninstall

**🐧 Linux**

```sh
curl -fSsL https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Uninstall_Linux.sh | sh
```

**🍎 macOS**

Manual - see [docs/Installation.md](docs/Installation.md).

**🪟 Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Uninstall_Win32.ps1 | iex
```

**🐋 Docker**

```sh
docker rm -f <container> && docker rmi ghcr.io/full-bars/meso-miner:latest
```

**🐋 Docker (management wrapper)**

```sh
rm /usr/local/bin/urnet-docker   # root install
rm ~/.local/bin/urnet-docker     # non-root install
```

### 🐋 Docker (Production-Ready)

Recommended for real deployments - includes auto-tuning, in-memory logs, and persistent config with zero listening ports:

```bash
docker run -d \
  --name=urnetwork-provider \
  --pull=always \
  --restart=unless-stopped \
  --cap-add=NET_ADMIN \
  --cap-add=NET_RAW \
  --sysctl net.ipv4.ip_forward=1 \
  -e BUILD=jwt \
  -e URNETWORK_PROFILE=auto \
  -e URNETWORK_RAMLOGS=1 \
  -e HOST_HOSTNAME=$(hostname) \
  -e PROXY_URL='https://example.com/your-proxy-list.txt' \
  -v urnetwork_config:/root/.urnetwork \
  -v /path/to/proxy.txt:/app/proxy.txt \
  -e URNETWORK_AUTH_CODE='YOUR_AUTH_CODE_HERE' \
  ghcr.io/full-bars/meso-miner:latest
```

**Key env vars:**
- `URNETWORK_PROFILE=auto` - Auto-tunes based on available RAM (balanced, lowmem, etc.)
- `URNETWORK_RAMLOGS=1` - In-memory logging for fast diagnostics (view with `urnet-docker logs`)
- `URNETWORK_AUTH_CODE` - Auth code is exchanged for your JWT token, obtained from https://ur.io (single-use on first run; saved to volume)
- `PROXY_URL` - Optional live proxy list URL (comma-separated for multiple), additive with the mounted `proxy.txt`. See [Proxy URL Sources](docs/Proxy-URL-Sources.md).
- `UR_API_URL` / `UR_CONNECT_URL` - Point at a custom API + connect backend instead of `bringyour.com`. Must be set together; saved to the `~/.urnetwork` volume so it survives restarts. See [Configuration Reference](docs/Configuration.md).

> [!TIP]
> Providers do not require any listening ports. Outbound connections handle all traffic. If you wish to enable the optional vnstat web monitor, see [Running with vnstat](docs/Docker-Deployment.md#-optional-running-with-vnstat-bandwidth-monitor).

See [Docker Deployment](docs/Docker-Deployment.md) for Docker Compose, email/password auth, Watchtower, multi-container, and advanced options.

> [!NOTE]
> **Docker Management** - Use `urnet-docker` directly on the host (no `docker exec` wrapping needed):
> - `urnet-docker status`
> - `urnet-docker direct status` (or `urnet-docker direct off` for proxy-only mode)
> - `urnet-docker usage` (or `urnet-docker usage graphs` for time-series history)
> - `urnet-docker proxy add ~/proxies.txt`
> - `cat proxies.txt | urnet-docker proxy paste`
> - `urnet-docker proxy traffic`

---

## 🔧 Common Operations

| Command | Use this when... |
| :--- | :--- |
| `urnetwork auth` | You need to log in or refresh your identity manually |
| `urnet-tools direct [on\|off\|status]` | You want to check or toggle native direct IP providing (`off` = proxy-only stealth) |
| `urnet-tools usage [graphs]` | You want to view persistent traffic accounting (billable relay vs control plane split) |
| `urnet-tools proxy add <path\|url>` | You want to add proxies from a file (straight path or `--file=`) or URL source |
| `urnet-tools proxy paste < file` | You want to stream raw proxies from stdin or a pipe into the provider |
| `urnet-tools proxy trim <count>` | You want to enforce a hard cap, shedding worst A-F reachability graded proxies first |
| `urnet-tools proxy traffic` | You want to see active clients, bandwidth, and **Max Age** per proxy |
| `urnet-tools proxy health` | You need to see which proxies are `DEAD` vs `DEGRADED` vs `UP` |
| `urnet-tools logs` | You want to stream the current RAMLOGS buffer |
| `urnet-tools optimize` | You just added many proxies and need to tune kernel `ulimits` |
| `urnet-tools summary` | You want a single-pane fleet overview -- sources, health, URL cache status |
| `urnet-tools proxy refresh` | You updated your proxy list and want the node to reload live |
| `urnet-tools set hot-restart on/off` | Toggle client JWT reuse across restarts (on by default; there is no standalone `hot-restart` command). See [docs/Hot-Restart.md](docs/Hot-Restart.md) |
| `urnet-tools set [<key> [<value>]]` | Show or change a runtime tuning override live, without editing a drop-in or restarting |
| `urnet-tools hotswap` | Swap to an updated binary without a gap where no process is running (needs a `Type=notify` unit; otherwise `update` falls back to a restart). Proxy connections still ramp back over about 30 s. Procedure and measured costs: [docs/HotSwap.md](docs/HotSwap.md) |
| `urnet-tools config [--json]` | Show every provider setting with the source it came from, so you can see which writer won |
| `urnet-tools history [limit]` | Read the provider's command audit trail, now including lifecycle events (start, hotswap, shutdown) alongside `set`/`clear` changes |
| `urnet-tools dashboard` | Terminal status panel: state, active settings, proxy sources, restart warnings |
| `urnet-tools metrics on/off` | Toggle the Prometheus `/metrics` endpoint live, no restart |
| `urnet-tools profile [name]` | Show or set the memory and GC tuning profile |
| `urnet-tools proxy ids` | Show the `client_id` the platform assigned to each proxy, including `direct` |
| `urnet-tools rename <name>` | Set the dashboard display label without touching the hostname |
| `urnet-tools show-ip [on\|off\|status]` | Control whether the public IP is appended to that dashboard label (was `ip-detect`) |
| `urnet-tools providers [--all]` | List the providers on this box; `--all` (as root) covers every OS user |
| `urnet-tools session save <file>` | Export identity+proxy state as encrypted bundle (cross-machine transfer) |
| `urnet-tools session load <file>` | Import identity+proxy state, then restart |
| `urnetwork choose_network <api_url> <connect_url>` | You run your own API/connect backend and want the provider to default to it |
| `urnetwork choose_network --reset` | You want to clear a saved custom network and revert to the main network |

> [!TIP]
> `~/proxies.txt` and `/home/user/proxies.txt` are both valid straight path formats across all tools.

---

## 📡 Telemetry and monitoring

Traffic numbers are built in: `urnet-tools usage` and `urnet-docker usage` (billable vs control plane accounting with hour/day/month historical graphs).

Prometheus metrics are built in. `urnet-tools metrics on` turns them on without a
restart and prints the address to scrape: loopback plus the machine's Tailscale
address by default, or any address you choose with `urnet-tools metrics listen`.
The `monitoring/` bundle runs Prometheus and Grafana with a ready-made fleet
dashboard. See [Monitoring](docs/Monitoring.md).

> [!NOTE]
> **No fleet dashboard in the app.** The old hub dashboard was removed in v32.0. To watch many machines at once, turn on Prometheus metrics (`urnet-tools metrics on`) and use the [Monitoring bundle](docs/Monitoring.md) or your own Grafana.

---

## 💡 Recommended Defaults

- Use the **Linux installer** for a host-managed systemd service
- Use **Docker** if you prefer containers - both are fully documented
- Leave profile on **`auto`** unless you have a specific reason to override
- Mount `/root/.urnetwork` as a persistent volume in Docker deployments
- Run `urnet-tools optimize` after adding a large proxy list, or when the System Auditor flags kernel limits

---

## 📚 Documentation

**In-repo:**

- [Installation](docs/Installation.md)
- [Docker Deployment](docs/Docker-Deployment.md)
- [Multi-Container Scaling](docs/Multi-Container-Scaling.md)
- [Configuration Reference](docs/Configuration.md)
- [Node Identity & Dashboard Label](docs/Node-Identity.md)
- [Proxy Management & Hot-Reload](docs/Proxy-Management.md)
- [High-Volume Performance Tuning](docs/High-Volume-Performance-Tuning.md)
- [Monitoring](docs/Monitoring.md)
- [Troubleshooting](docs/Troubleshooting.md)
- [Bittensor Quick Start](docs/Bittensor-Quick-Start.md) · [Bittensor Operations](docs/Bittensor-Operations.md) · [Head Tier](docs/Bittensor-Head-Tier.md) · [Glossary](docs/Bittensor-Glossary.md)
- [Bittensor Claim Daemons](docs/Bittensor-Claim-Daemons.md) · [Bittensor Fleet](docs/Bittensor-Fleet.md) · [Bittensor Hotkey Delegation](docs/Bittensor-Hotkey-Delegation.md) · [Multi-Operator](docs/Multi-Operator.md)
- [Project Structure](docs/Project-Structure.md)
- [Log Message Reference](LOG_REFERENCE.md)
- [Go urnet-tools Reference](docs/urnet-tools-go.md)
- [Changelog](CHANGELOG.md)

**Wiki:**

- [Online GitHub Wiki](https://github.com/full-bars/meso-miner/wiki)
- [CI and Release Process](https://github.com/full-bars/meso-miner/wiki/CI-and-Release-Process)

---

## 🏗 Build Info

- **Base engine:** the URnetwork connect engine, brought up to the 33.1/34 generation (see [CHANGELOG.md](CHANGELOG.md)). This line began as a fork of the 3.23-fix line and has its own release numbers, such as `v2026.10.20-1076956260-meso`
- **Language:** Go 1.27.3, compiled on Alpine
- **Platforms:** Linux (systemd, and Alpine/OpenRC with a native boot-start service), macOS, Windows and FreeBSD. Alpine setup: [docs/OpenRC.md](docs/OpenRC.md); FreeBSD setup: [docs/FreeBSD.md](docs/FreeBSD.md)
- **Images:** Multi-arch `linux/amd64` + `linux/arm64`, `darwin/amd64` + `darwin/arm64` via GitHub Actions → GHCR
- **Bridge-friendly:** runs on standard Docker bridge networks, no `--network host` required

---

> [!WARNING]
> This is a private, custom modification for professional provider use. Not affiliated with the official UrNetwork project.
