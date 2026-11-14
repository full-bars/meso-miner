<!-- DRAFT. At tag time: rename this file to releases/<tag>.md, put the tag in the title and today's date in Released, then delete this comment. -->

# NEXT_TAG

**Released:** NEXT_DATE.

Previous release: `v2026.11.9-1094055530-meso`.

This release adds OpenBSD provider support, ports the client side of the network operator's FP2 provider selection, closes a handshake bug where one forged id could wedge a peer session, and moves the build to Go 1.27.4. The provider binary does not run the multi client window, so the FP2 port changes nothing for a provider fleet by itself. The fleet gains from the operator's server change, which offers a provider for the traffic it carries even when it has no UDP or IPv6. See the Dual-Stack guide. Update and restart, because the handshake fix changes the binary.

Legend: **[redeploy]** needs the new provider, tool or container image. **[installer]** needs the updated installer or script. **[no redeploy]** covers documentation, CI and tests.

## What's changed

- **OpenBSD provider support. [redeploy] [installer]** The provider and `urnet-tools` build for OpenBSD on amd64 and arm64. There is an installer, `rcctl` service setup, cron auto-update and release assets. `urnet-tools` guards `rcctl` actions against the wrong provider. The JWT read closes a symlink race, and the BSD cron entry is hardened.
- **A forged handshake generation id cannot wedge a peer session. [redeploy]** A peer could send a ClientHello with a high id and make the responder drop every later ClientHello that carried a lower id. The responder now takes an id only from a ClientHello and ignores foreign proofs and responses.
- **IPv6 admission and a TCP-only window in the multi client. [no redeploy]** SDK and proxy clients built on this tree keep an IPv4 hard minimum, ask for IPv6-capable exits when the IPv6 share falls short, and hold a limited window of TCP-only providers. The provider binary does not run this code.
- **The BSD cron log path uses forward slashes on every build host. [no redeploy]**
- **The build is Go 1.27.4. [redeploy]**
- **The blocklist sync runs the blocker, CFAA, Telegram and security tests against the upstream tables before it opens a PR. [no redeploy]**

## Known limits

- The blocklist tables stay at the refresh before 2026-11-13. Upstream's tables from that day use a new format and list a Telegram call reflector that upstream's own test rejects. The sync check blocks them until upstream fixes the table.
- Recent exit memory and the sticky redial of a fixed exit are idle in this line, because it lacks the transport-loss signal they need.
- The OpenBSD runtime was not run on a real OpenBSD host.
