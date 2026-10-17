# Signing the network wallet consent with a browser wallet

Release settlement pays only a wallet that its coldkey has consented to. The consent is one signature that covers every provider client of your network, current and future. `provider wallet sign` serves a small local page where a browser wallet extension (Talisman, SubWallet, or any extension that exposes the Polkadot `signRaw` API) signs it, so the seed phrase never leaves the extension.

Run it on your own computer, never on a fleet node. A fleet node needs nothing.

## Steps

1. Save a network login on this computer: `provider auth` (an auth code from the app, or your email and password).
2. Check what is set today: `provider wallet status`.
3. Run `provider wallet sign <your coldkey ss58 address>` (or `urnet-tools wallet sign <address>`). It prints an address like `http://127.0.0.1:8765/` and tries to open it.
4. In the browser that has your wallet extension, unlock the extension, click **1. Connect wallet**, approve, then click **2. Sign** and approve. The extension shows the message; it starts with `Approve URnetwork network wallet mapping`.
5. The page shows the stored epochs and the server's receipt. The command checks that receipt against the hash it computed itself.
6. Check: `provider wallet status` shows `network consent ... in effect now` (or `starts in N epoch(s)`).

## The page

- Dark page in the URnetwork style, with a countdown for the message (valid 5 minutes) and for the page (closes after 15 minutes). **New message** fetches a fresh statement without restarting.
- Served on `127.0.0.1` only, with a per-run session token, a Host and Origin check, and a content security policy. It stores only a signature over the statement this program verified before showing it; the page cannot send a different message.
- The logo is the URnetwork wordmark from the URnetwork web repository (MPL-2.0). The site fonts are licensed and are used only if installed.

## Options

- `--wallet-from-epoch` / `--wallet-through-epoch`: an explicit interval. The default is the next epoch through the full 65,536-epoch span. A mistaken interval cannot be undone, so the tool refuses odd intervals unless `URNETWORK_WALLET_ALLOW_ODD_INTERVAL=1` is set.
- `--port`: the local port (default 8765; another free port is used if it is taken).
- `--no-open`: do not try to open the browser, only print the address.

A later consent must start at a later epoch than the one before it. For a headless computer use `provider wallet challenge` and `provider wallet set --message --signature` instead.
