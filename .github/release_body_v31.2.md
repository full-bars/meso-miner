# v3.23.0-fix.31.2 — Windows provider support, monitoring bundle, and reliability fixes

## 📦 Quick Start / Install

**🐧 Linux:**
```sh
curl -fSsL https://dl.fullbars.xyz/install.sh | sh
```

**🍎 macOS:**
```sh
curl -fSsL https://dl.fullbars.xyz/install-mac.sh | sh
```

**🪟 Windows (PowerShell):**
```powershell
irm https://dl.fullbars.xyz/install-win.ps1 | iex
```

**🐳 Docker:**
```sh
docker pull ghcr.io/full-bars/urnetwork-3.23-fix:v3.23.0-fix.31.2
```

**🔄 Updating an existing install:**
```sh
urnet-tools update
```

---

## 🗂️ What's New

**Windows HotSwap support** — Live binary replacement on Windows providers without downtime, matching the Linux HotSwap experience.

**Windows lifecycle management** — Proper Windows service install, start, stop, and uninstall lifecycle via `urnet-tools`, including NTSM integration.

**Windows packaging & distribution** — Signed MSI/EXE installers with auto-update support, delivered via `dl.fullbars.xyz/install-win.ps1`.

**Monitoring bundle** — Ships with Prometheus metrics exporter and Grafana dashboard templates out of the box for fleet-wide observability.

**WDSI automation** — Automated Windows Driver Signing and installer build pipeline for repeatable, secure releases.

**Metrics default-on** — Provider metrics collection and export enabled by default on all platforms; no manual config needed for basic fleet monitoring.

---

## ⚠️ Breaking Changes

**(None. Drop-in upgrade.)**

---

## What's Changed
<!-- GitHub will automatically append the generated list of merged PRs and commits here. -->
