# Dual-Stack and IPv6 Support

Plain-language answer to "are we dual stack, and what does that mean per proxy?"

## The short version

The provider supports both IPv4 and IPv6. Since `v2026.10.20-1076956260-meso` the direct identity is a dual-stack transport group: one transport pinned to IPv4, one pinned to IPv6, and a family-agnostic standby. The remaining gap is the provider's egress **through a proxy**: by default it still resolves the target itself with an IPv4-only query and hands the proxy an IPv4 literal. `proxy_target_family=proxy` hands the hostname to the proxy instead.

The defaults need no configuration and do not affect earnings.

## What works today

- **The provider's own platform connection** runs both families by default through the family group described below. Only the native direct identity builds the group; a proxied identity reaches the platform through its proxy and stays on one family-agnostic transport.
- **The proxy data plane** understands both families. The SOCKS5 UDP relay handles IPv4 and IPv6 destinations (`socks5_udp.go` accepts both ATYP 1 and 4).
- **Clients reaching a proxy over IPv6** works when that proxy's endpoint is registered or advertised that way; the provider does not block it.
- **The direct identity** is dual stack by default: `ip_version=dual` runs the v4 pin, the v6 pin and the standby. `ip_version=ipv4` and `ip_version=ipv6` pin it to one family, and `legacy` restores the pre-dual-stack single transport.

## The direct identity's family group

`provider/family_transport.go` decides once per identity launch whether the direct identity gets the group. It does when the identity is native, has no proxy settings, both family platform URLs exist (`connect-v4.bringyour.com` and `connect-v6.bringyour.com` for the shipped backend), and the connect host is the one whose `-v4` and `-v6` names were verified. A proxied identity never gets it: it reaches the platform through its proxy, and a pinned transport dials from the host.

The group runs three transports on the same route manager:

- **The v4 pin** dials the IPv4 family URL, resolves only A records, uses direct dialers only, and declares family 4 to the platform.
- **The v6 pin** does the same over IPv6 and declares family 6.
- **The family-agnostic standby** dials the ordinary platform URL with the ordinary strategy. It starts only after 15 seconds with neither pin connected (or at once if no pin can connect at all), and stands down as soon as a pin connects.

A pin never feeds the process-wide backend-degraded gate, and a control-plane family policy that contradicts a pin makes it idle rather than re-dial the other family. Pinned transports bypass the control-plane demotion ledger by construction; that ledger steers only family-agnostic control dials. The launch line is `[ipv6] direct identity platform transports: dualstack group` or `legacy single (reason=...)`, and `[ipv6] family transports v4=... v6=... standby=... active=...` follows whenever the tuple changes.

**A host with no usable global IPv6 still builds the group.** The launch logs `[ipv6] dual-stack group at launch; the v6 pin holds until a global v6 address appears (probe: ...)`, and the v6 pin sits in the `sleeping` state rather than spinning through failed dials. It wakes on a network change, so a box that boots before SLAAC finishes joins dual stack later without a relaunch.

### Choosing the families with `ip_version`

`urnet-tools set ip-version <mode>` (control key `ip_version`) chooses the stack:

| Mode | What runs |
|---|---|
| `dual` (default) | The v4 pin, the v6 pin and the family-agnostic standby. |
| `ipv4` | The v4 pin only. The standby never dials, so the provider never appears on IPv6. |
| `ipv6` | The v6 pin only. The standby never dials, so the provider never appears on IPv4. A host without working IPv6 stays disconnected. |
| `legacy` | The family-agnostic transport alone, the behaviour before the group existed. The OS picks the family and no family is declared to the platform. |

The key is live. `SetStack` re-pins the running group break-before-make: a connected pin the new stack does not want keeps carrying the identity until a transport the stack wants is connected. The provider's transport budget is sized for the launch stack's transport count and does not resize live; a stack change applies its new sizing at the next identity launch. Clearing the key restores `dual`. Only the direct identity builds a group, so the key has nothing to switch on a proxied identity.

## The one gap: the provider's egress *through* a proxy

By default (`proxy_target_family=v4`, the behaviour of every release before this setting), `ProxySettings.NewDialContext` resolves a hostname target itself, with an **A (IPv4) query only**, and rewrites the SOCKS5 CONNECT target to an IPv4 literal (`net.go`, `lookupProxyTarget`). The proxy therefore never chooses the family for that connection.

`proxy_target_family=proxy` is the opt-in alternative: the hostname goes to the proxy untouched, so the SOCKS5 CONNECT carries the name and the **proxy's** resolver picks A or AAAA. It is read inside every proxied dial, so it changes live with no relaunch and no dropped session; connections already open keep the address they connected to. SOCKS5 never reports which family it used, so proxy mode adds no per-proxy family evidence (a capability probe through the proxy would be a separate change).

Consequences of the default:

- A box with 4,000 proxies runs one client per proxy, but every proxied platform session is IPv4, regardless of what the proxies support.
- IPv6 still works for the provider's own connection and for clients reaching a proxy over v6; it is the provider's egress through the proxy that is pinned.
- This is **not** part of the platform's modern-provider gate, which looks for post-quantum encryption and resizable transfer windows (both shipped).

The local resolution was deliberate: it shares a DNS cache across burst dials and bounds a bad resolver with a short DoH timeout, so thousands of warmup dials cannot stall behind DNS. Proxy mode gives that up in exchange for the proxy's own family choice. When DoH has no answer, the plain-DNS fallback rotates its configured server **per lookup, not per process**: each lookup seeds its own attempt counter from the shared one and the shared counter advances once per lookup, so one lookup's attempts walk consecutive servers and another concurrent lookup cannot move the rotation between them. DoH stays primary; only the plain-DNS fallback changes.

## What proxy mode changes

1. `urnet-tools set proxy-target-family proxy` switches every new proxied dial to name-carrying CONNECT.
2. The proxy's resolver becomes authoritative: a proxy with broken DNS now fails where local IPv4 resolution previously saved it.
3. There is no per-proxy family evidence: SOCKS5 never reports the upstream family, so proving a proxy is v6-capable needs a probe through it.
4. Family accounting stays honest: a dial failure through a proxy must never be counted as an IPv4 family strike, and it is not (`recordControlFamilyStrike` refuses proxied evidence).
5. Canary first: proxy mode changes DNS behavior for every proxied dial, and `urnet-tools set proxy-target-family v4` flips it back completely.

## What you need to do

- **Default:** nothing. No flag, no setting, no fleet change.
- **Per-proxy IPv6 now:** set `proxy_target_family=proxy` on a canary and compare it with a box on the default `v4`.
- **Choose the provider's own stack:** `urnet-tools set ip-version dual|ipv4|ipv6|legacy`; `dual` is the default and `legacy` is the pre-dual-stack behaviour.

## Status

| Line | Provider platform connection | Proxy egress family | Proxy data plane |
|---|---|---|---|
| `v2026.10.20-1076956260-meso` | Direct identity: v4 pin + v6 pin + family-agnostic standby, chosen by `ip_version` | IPv4 pinned by default; `proxy_target_family=proxy` lets the proxy choose | v4 and v6 |
| Before `v2026.10.20-1076956260-meso` | One family-agnostic transport (legacy) | IPv4 pinned by the provider | v4 and v6 |

IPv4 remains the default in every shipped configuration: `ip_version` defaults to `dual`, and `ipv4` is available when an operator wants a v4-only provider. The v4 pin is not demoted by the control-plane family policy; that policy only steers family-agnostic control dials (the API client, the platform websocket and the h3/quic name path).
