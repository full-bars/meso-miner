<!-- DRAFT. At tag time: rename this file to releases/<tag>.md, put the tag in the title and today's date in Released, then delete this comment. -->

# NEXT_TAG

**Released:** NEXT_DATE.

Previous release: [v2026.11.9-1094055530-meso](https://github.com/full-bars/meso-miner/releases/tag/v2026.11.9-1094055530-meso).

This release adds OpenBSD provider support and fixes a handshake bug where one forged id could wedge a peer session. It also moves the build to Go 1.27.4 and ports the client side of the network operator's FP2 provider selection.

The provider binary does not run the multi client window, so the FP2 port changes nothing for a provider fleet by itself. The fleet gains from the operator's server change. The server now offers a provider for the traffic it carries, even when the provider has no UDP or IPv6. See the [Dual-Stack guide](https://github.com/full-bars/meso-miner/blob/main/docs/Dual-Stack.md).

Update and restart, because the handshake fix changes the binary.

Legend: **[redeploy]** needs the new provider, tool or container image. **[installer]** needs the updated installer or script. **[no redeploy]** covers documentation, CI and tests.

## Quick start

Already running a provider? Update it in place:

```sh
urnet-tools update
```

Install on Linux:

```sh
curl -fSsL https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Install_Linux.sh | sh
```

Install on macOS:

```sh
curl -fSsL https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Install_Mac.sh | sh -s -- install
```

Install on Windows in PowerShell:

```powershell
irm https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Install_Win32.ps1 | iex
```

Install on FreeBSD from a non-root service account with `sudo`:

```sh
fetch -qo urnet-install.sh https://raw.githubusercontent.com/full-bars/meso-miner/refs/heads/main/scripts/Provider_Install_FreeBSD.sh
sudo sh urnet-install.sh install
```

Install on OpenBSD. Use an existing non-root service account and replace `urnet` with its name:

```sh
ftp -o urnet-install.sh https://raw.githubusercontent.com/full-bars/meso-miner/main/scripts/Provider_Install_OpenBSD.sh
doas env SERVICE_USER_OVERRIDE=urnet sh urnet-install.sh install
```

After the install, sign in as the service account with a single-use auth code from https://ur.io and restart the service. The [OpenBSD guide](https://github.com/full-bars/meso-miner/blob/main/docs/OpenBSD.md) has the commands.

For Docker, pull `ghcr.io/full-bars/meso-miner:latest`. The [Docker deployment guide](https://github.com/full-bars/meso-miner/blob/main/docs/Docker-Deployment.md) covers setup and persistent state.

## What's changed

### Added

- **OpenBSD provider support. [redeploy] [installer]** The provider and `urnet-tools` build for OpenBSD on amd64 and arm64. There is an installer, `rcctl` service setup, cron auto-update and release assets. `urnet-tools` guards `rcctl` actions against the wrong provider. The JWT read closes a symlink race. The BSD cron entry is hardened.
- **IPv6 admission and a TCP-only window in the multi client. [no redeploy]** SDK and proxy clients built on this tree keep an IPv4 hard minimum. They ask for IPv6-capable exits when the IPv6 share falls short. They hold a limited window of TCP-only providers. The provider binary does not run this code.

### Fixed

- **A forged handshake generation id cannot wedge a peer session. [redeploy]** A peer could send a ClientHello with a high id. The responder then dropped every later ClientHello that carried a lower id. The responder now takes an id only from a ClientHello and ignores foreign proofs and responses.
- **The BSD cron log path uses forward slashes on every build host. [no redeploy]**
- **A shutdown during the URL proxy source cycle no longer fails every source. [redeploy]** Before, a cancel marked every remaining source failed, set the resolution status to failed and warned once per source. The cycle now stops and logs one line with the cause. The results of finished sources are kept.

### Changed

- **The build is Go 1.27.4. [redeploy]**
- **The blocklist sync runs the blocker, CFAA, Telegram and security tests against the upstream tables before it opens a PR. [no redeploy]** If a test fails, the sync stops and posts the failing tests.

## Known limits

- The blocklist tables stay at the refresh before 2026-11-13. Upstream's tables from that day use a new format. They list a Telegram call reflector that upstream's own test rejects. The sync check blocks them until upstream fixes the table.
- Recent exit memory and the sticky redial of a fixed exit are idle in this line, because it lacks the transport-loss signal they need.
- The OpenBSD runtime was not run on a real OpenBSD host.
