# Run a provider as an extender

An extender is a provider that also accepts connections on ports the internet can reach, so people in censored countries can reach URnetwork through it. It is the same idea as a Tor bridge: it does not earn, it helps clients get through. The role is **on by default** on the provider's direct slot, and it asks the platform to list the host so clients are sent to it.

You normally do nothing. This page covers how to see whether it is working, the few things the host must allow, how to turn it off, Docker, and what to do when it is not working.

## See how much traffic the extender relays

`urnet-tools extender status` prints a `Traffic` table, one row per carrier, in human units (B, KiB, MiB, GiB, TiB):

```text
  Traffic:
    carrier           in        out      rate in     rate out  connections
    tcp-443:     1.2 GiB    4.0 GiB   45.0 KiB/s  310.0 KiB/s  3 live, 1280 total
    udp-443:    12.0 MiB    3.0 MiB      0.0 B/s      0.0 B/s  0 live, 40 total
```

`in` is the bytes from clients and `out` is the bytes to them. The rates are measured between two samples of the counters. `top` shows the same totals in its Extender box, one row per carrier that has carried traffic, and `/metrics` exports `urnet_extender_carrier_bytes_total{carrier,direction}` and `urnet_extender_carrier_connections{carrier}`.

These are the bytes this extender relayed. What the network credits depends on the contracts it counts, so these figures measure relayed traffic and are not a statement of earnings. The counts start at zero when the provider starts.

## Discovery

Discovery serves the platform's extender directory to clients. When the extender role is enabled, it defaults to serving the feed and joining gossip. Explicit saved `off` or `feed-only` settings take precedence. Older versions of `urnet-tools set extender-discovery off` cleared the override rather than saving it, so that earlier CLI choice cannot be detected: after upgrading, run the command again if discovery should stay off.

- `off` refuses both discovery services.
- `feed-only` serves the directory feed and refuses gossip.
- `on` serves the feed and joins the gossip mesh.

Use `urnet-tools set extender-discovery off` or `urnet-tools set extender-discovery feed-only` to change modes; `on` restores feed and gossip. The setting applies live and survives restarts. Check `urnet-tools extender status` or `urnet-tools status` for its state, peer ID, peer count, subscribers and startup failures. `urnet-tools top` shows the discovery state and counts in the Extender box and the `v` details panel. A running state means the local role started, not that peers have connected; zero peers means mesh connectivity is not yet demonstrated. On updated providers, the active directory record count includes all sources and is not proof of gossip. The valid-message count and last-received time show messages received from peers that passed validation and were accepted for directory application (including duplicates), since the gossip role started. A zero count does not prove failure: gossip carries live updates and there may not have been a new message yet. Apply errors count directory application failures, not messages rejected by the network validator. Older providers omit these counters.

A host waiting for platform root keys or retrying a failed discovery start keeps working as an extender. It refuses both discovery services until discovery starts. A feed-only role costs little. A gossip role keeps up to 16 mesh peers and 256 feed subscribers.

### Gossip diagnostics and events

The provider maintains an in-memory event ring of recent mesh, dial, reject and cap events. Read recent events with `urnet-tools extender events`:

```sh
urnet-tools extender events                  # list recent gossip events
urnet-tools extender events --since 10m      # filter by age
urnet-tools extender events --kind dial      # filter by kind: mesh, dial, reject, cap
urnet-tools extender events --json           # output as JSON
```

## Quick start

```sh
urnet-tools extender status     # is it working?
urnet-tools extender setup      # check port 443, the low-port route and firewall; ask before changing
urnet-tools extender about      # a plain-language explanation of the ports and how it works
urnet-tools extender disable    # off, and it stays off across restarts
urnet-tools extender enable     # back on
```

For it to work the host needs:

1. **Inbound TCP 443 that reaches the provider.** This is the one required port. If a web server (caddy, nginx, apache) holds it, the extender cannot be listed until you free or move that service.
2. **Permission to use TCP 443.** On Linux, a non-root provider can use the host-wide sysctl route or `cap_net_bind_service` on the binary. The sysctl route lets any local user bind ports 443 and above. The installer may offer the file capability on an interactive terminal. When the sysctl route is not active, `urnet-tools update` restores a previously verified capability and warns if it cannot.
3. **A public IPv4 address**, and for IPv6 a public IPv6 address with a working route out.

UDP 443, UDP 4053 and UDP 53 are optional. Without them the host is still listed over TCP. Setup does not add a UDP 53 firewall rule. On Linux, `--udp53` can lower the sysctl bind threshold after confirmation.

## Is it working?

```sh
urnet-tools extender status
```

shows whether the role is on, which ports are up, what each address family is doing and when it tries next, how many clients are connected right now, how many requests it has served over the last 24 hours, 7 days and its lifetime, and the cumulative served and refused request counts for each carrier (`tcp-443`, `udp-443`, `udp-4053`, `udp-53`) with refusal reasons. The same block appears in `urnet-tools status`, and `urnet-tools top` has an Extender box. The provider also writes one `[health][extender]` line with the same information every few minutes, so you do not have to search the log.

A family reads **ACTIVE** when the platform has listed this host on that address family. Until then the status says why: the platform said no (with its reason), the request failed, or the extender is waiting because TCP 443 is not bound.

"Requests served" counts requests the extender handled, once a request has passed its checks and been handed a stream. It includes the platform's own activation check: when the platform lists the host it forwards one real request through it, so an idle host reads a few above zero (about one per activation per address family). Latency probes are not counted. It does not count distinct people; the provider does not record who asked. Use "clients now" for who is connected.

## How activation works

1. The extender binds TCP 443 first. If that fails, nothing else opens (see below).
2. The provider asks the platform to list this host, once for IPv4 and once for IPv6. Each family is tried separately.
3. The platform dials back to this host on TCP 443, asks for a signed answer, and forwards a real request through it to its own API. It also tries UDP 443 and the DNS port. If those do not answer in its 10 second budget it leaves them out of the listing and still lists the host.
4. The platform returns a signed record that says when the listing expires. Today a listing lasts 24 hours. The provider checks the record against the platform's root keys (read from the platform's `/hello`) and renews it before it expires.
5. Most refused or failed attempts are retried after 10 minutes at first, doubling up to 6 hours, or when the platform says it has room again. If a family has not activated and the platform cannot reach TCP 443, the provider can wait 12 hours before retrying. A family that is refused does not delay the other.

The platform limits how often a host may ask: 6 activations per client, per address family, per hour, counted when the platform starts probing, whether it then succeeds or not. Restarting repeatedly and failing can use that up. A refusal that says "try again in N minutes" is the platform telling the provider to wait, and the provider waits.

### What survives a restart

The role keeps four small files in `~/.urnetwork/` (in Docker, `/root/.urnetwork`, which must be a persistent volume):

| File | What it holds |
|---|---|
| `.provider.extender.key` | The extender's identity (mode 0600, created on first use). Its public half names the extender, so the file must survive restarts. Never copy it to another box or account: the platform refuses a key that belongs to another network (`key_other_network`) and the log tells you to delete the file so a new one is made. |
| `extender_pacing.json` | When each address family may next ask the platform, and whether its listing is valid. A restart resumes the schedule instead of asking everything at once. |
| `extender_directory.json` | The platform's signed records, so a restart can recognise that a listing is still valid. |
| `extender_tally.json` | The request counts for 24 hours, 7 days and lifetime. |

After a restart a family whose listing is still valid shows ACTIVE straight away. The provider then asks the platform once more at a random moment in the next 30 minutes, so a fleet that restarts together does not ask together. A family with no valid listing asks as soon as it is allowed.

## Turn it on or off

```sh
urnet-tools extender disable
urnet-tools extender enable
```

`on` and `off` work too. Both apply to the running provider at once, with no restart and no unit file or drop-in. The setting is stored by the provider's control socket, so it survives restarts and updates.

`URNETWORK_EXTENDER=off` in the environment still works as a startup default. The control setting beats it, and an unrecognised value is ignored (the default, on, applies) and logged. In Docker, where `urnet-tools extender` is not available, use the environment variable and restart the container.

The role runs from the provider's direct slot, because only that slot's egress is this host's own address. Configured proxies do not prevent it, and a proxy can never be an extender: the platform would be dialing the proxy's address, where no part of this provider listens. Turning direct providing off (`urnet-tools direct off`) does stop it: the provider logs `[extender] off: direct providing is disabled` and carries on without the role. A proxied provider marks its platform connection, and the platform may count it at no head unit, capture-the-flag unit or extender unit. A bound proxy slot earns no pool share.

## Ports

| Port | Role | Notes |
|---|---|---|
| TCP 443 | TLS carrier | **Required.** The platform proves the host over this port. |
| UDP 443 | QUIC carrier | Optional. Control it with `extender-udp443`. |
| UDP 4053 | DNS carrier | Optional. Control it with `extender-udp4053`. See the note on DNS below. |
| UDP 53 | Second DNS port | Optional. Control it with `extender-udp53`. Setup can lower the bind threshold with `--udp53`. Manage firewall access separately. `URNETWORK_EXTENDER_DNS53=off` also disables it. |

TCP 443 gates everything. The extender binds it first, and **while it fails no other port is opened**: the platform could not list the host anyway, so nothing could use them. It retries every three minutes and opens the rest once it has the port. It says so plainly, naming what holds the port when the host will tell it:

```text
🌉❌ [extender] CANNOT USE tcp 443: listen tcp :443: bind: address already in use. Port 443 is held by caddy (pid 894). The platform proves an extender by dialing back to it on tcp 443, so while that port is taken this host will NOT be listed and no clients will be sent to it, so every other extender port stays closed until tcp 443 can be bound. Free the port (stop or move whatever holds it). The bind is retried every 3m0s; next attempt at 17:42.
```

A reminder repeats every 30 minutes while the port stays blocked. When it comes free, the log says `tcp 443 is bound again` and the platform is asked within a few minutes. (A listing the platform made earlier stays valid until it expires.)

**About the DNS ports.** The DNS carrier also answers ordinary DNS questions from anyone who can reach the port, resolved over DoH and rate limited per source and in total, so that from outside it looks like a small rate-limited resolver. That is part of its camouflage; it is upstream's design. It is not limited to the platform's hosts. Port 53 is probed by scanners much more than 4053. Firewall UDP 4053 and 53 like any resolver you do not want to expose, or turn the role off. Without them the host is still listed over TCP.

TCP 443 needs root, an unprivileged-port sysctl threshold of 443 or lower, or the file capability `cap_net_bind_service`. The Docker image runs as root. The sysctl route is for single-purpose hosts: it writes `net.ipv4.ip_unprivileged_port_start=443` to `/etc/sysctl.d/99-urnetwork-extender.conf` and applies it with `sysctl --system`. It avoids a file capability, but any local user can then bind ports 443 and above. The file capability route can leave the running provider unreadable to an unprivileged updater, because a process with file capabilities is non-dumpable. Replacing the provider binary can remove the file capability; the installer restores it after a swap and warns if it cannot verify it.

## Choosing carriers

UDP 443 carries QUIC traffic. UDP 4053 and UDP 53 carry DNS traffic. Each setting defaults to `on` and works independently. Choose which optional carriers the host should offer:

```sh
urnet-tools set extender-udp443 off
urnet-tools set extender-udp4053 off
urnet-tools set extender-udp53 off
```

Use `on` to enable a carrier again. The provider saves each choice and automatically restarts an active extender role when the choice changes. An `off` carrier is not bound or listed. Status shows it as `disabled by setting`. TCP 443 remains required.

UDP 53 is tried by default. A port already in use cannot bind. On Linux, a non-root provider needs `cap_net_bind_service` or an `ip_unprivileged_port_start` limit of 53 or lower. The default setup sysctl limit of 443 does not permit binding port 53. To choose 53 instead, run `urnet-tools extender setup --route=sysctl --udp53`. The plan shows the current value, the persistent drop-in and the undo command. Confirm by typing `yes` or use `--yes` for non-interactive setup. This change permits every local user to bind ports 53 to 1023. Use `--dry-run` to inspect the plan without changing the host. Manage the UDP 53 firewall rule separately. FreeBSD and other platforms do not support this flag. `URNETWORK_EXTENDER_DNS53=off` also disables UDP 53 even when its setting is `on`.

`urnet-tools extender status` and `urnet-tools top` always show a `dns 53` line, so you can tell why UDP 53 is not serving:

| Shown | Meaning |
|---|---|
| `bound` | UDP 53 is open. |
| `not bound (address already in use)` | Another service holds UDP 53. |
| `not bound (permission denied)` | The provider may not bind a port below 1024. See the capability note above. |
| `not bound (bind failed, cause unknown)` | The bind failed for another reason. The provider log has the error. |
| `not requested` | `URNETWORK_EXTENDER_DNS53=off` turned UDP 53 off. |
| `disabled by setting` | `extender-udp53 off` turned it off. |

Providers that predate this show `not reported` in `top` when UDP 53 is not bound.

## Prepare the host with `urnet-tools extender setup`

```sh
urnet-tools extender setup
urnet-tools extender setup --dry-run
urnet-tools extender setup --yes --route=sysctl
urnet-tools extender setup --route=sysctl
```

`urnet-tools extender setup` inspects TCP 443 occupancy, the selected low-port route and the host firewall. It prints the exact commands it proposes and asks before changing anything. In a non-interactive run, only explicit `--yes` or `--force` approves changes; `--dry-run` prints the plan without applying it.

1. **Port 443 occupancy.** Checks whether another process already holds TCP 443 and stops with the holder's name and PID if one does.
2. **Low-port bind privilege.** On Linux, setup selects the route first. Interactive setup offers sysctl (recommended on a single-purpose host), a binary file capability, or skip. Unattended setup must name one with `--route=sysctl|capability|skip`, even with `--yes`. The sysctl route writes `net.ipv4.ip_unprivileged_port_start=443` to `/etc/sysctl.d/99-urnetwork-extender.conf`. If that file exists, setup first keeps its contents and mode in `99-urnetwork-extender.conf.pre-urnetwork`, a name sysctl.d ignores, and the printed recovery restores it. A drop-in that setup created is removed instead. A live value of 443 or less counts as configured only when a sysctl file also persists it. Setup reads the sysctl.d directories in systemd order, where the last assignment wins, then `/etc/sysctl.conf`. If the binary still has a bind capability, setup warns that the running provider stays non-dumpable and may be unreadable to the updater. Existing file capabilities are preserved. On FreeBSD, setup is print-only and untested this release. It makes no changes. It prints the sysctl commands and undo instructions. Regular providing on FreeBSD is unaffected.
3. **Host firewall rules.** Inspects the active local firewall (`ufw`, `firewalld`, `iptables`/`ip6tables`, `nftables`, `pf`/`ipfw`, or Windows Firewall) for inbound TCP 443 (required TLS carrier), UDP 443 (optional QUIC carrier) and UDP 4053 (optional DNS carrier), and prints only the missing commands. For firewalld, it plans runtime and permanent rules separately and does not reload the firewall. For example, if `ufw` is active and already allows `443/tcp` while `443/udp` and `4053/udp` are missing, it proposes only `ufw allow 443/udp` and `ufw allow 4053/udp`. UDP 53 is optional and user-managed; setup does not open it.
4. **Confirmation gate.** Prints the port summary and every command it will run under `Commands to run:`, then asks `Execute these commands? [y/N]:` before changing anything. In a non-interactive session without `--yes` (or `--force`) it executes nothing and exits with an error; `--dry-run` (`-n`) prints the plan and exits without prompting. When elevation is needed, the elevated run is pinned to the provider you inspected, and root refuses to configure a different one. If the choice cannot be expressed uniquely, setup refuses and prints the inventory command to run first.
5. **Bind verification.** On Linux, after capability checks, runs `<provider-binary> --extender-bind-probe` as the provider's user (`sudo -n -u <user>`) when TCP 443 is free to confirm a live low-port bind. When the running provider already holds TCP 443 or the probe cannot run without a password prompt, `setup` states plainly that the live TCP 443 bind was not tested.

Docker containers are not modified by `extender setup`; publish the ports on `docker run` or use `--network host` as shown below, and allow the ports in the host firewall.

## The firewall

Opening a port in the provider does not open it on the host. If the host has a firewall, allow what you want:

Use whichever matches your host. Only TCP 443 is required; the UDP lines are optional.

```sh
# ufw (Ubuntu, Debian)
sudo ufw allow 443/tcp
sudo ufw allow 443/udp
sudo ufw allow 4053/udp

# firewalld (RHEL, Alma, Rocky, Fedora)
# Setup plans missing runtime and permanent rules separately. It does not reload.
sudo firewall-cmd --add-port=443/tcp
sudo firewall-cmd --permanent --add-port=443/tcp

# iptables (plain)
sudo iptables -I INPUT -p tcp --dport 443 -j ACCEPT
sudo iptables -I INPUT -p udp --dport 443 -j ACCEPT
sudo iptables -I INPUT -p udp --dport 4053 -j ACCEPT
sudo ip6tables -I INPUT -p tcp --dport 443 -j ACCEPT   # for IPv6, repeat the rules with ip6tables

# nftables (adjust the table and chain names to your ruleset)
sudo nft add rule inet filter input tcp dport 443 accept
sudo nft add rule inet filter input udp dport '{ 443, 4053 }' accept
```

`iptables` and `nft` rules added this way are lost at reboot. Save them with your distribution's tool (`sudo netfilter-persistent save` on Debian and Ubuntu, `sudo service iptables save` on RHEL-style hosts, or write them into `/etc/nftables.conf`). ufw and firewalld (with `--permanent`) already persist. If you also have IPv6 enabled, make sure the rule covers it: ufw and firewalld cover both families by default, plain `iptables` does not. Other firewalls (a Windows host, a cloud provider's panel) need the equivalent inbound TCP 443 rule.

### Cloud providers block inbound ports by default

Most clouds put a second firewall in front of your VM, and it blocks inbound traffic until you allow it. Opening the port inside the VM is not enough; you must also allow inbound **TCP 443** (and optionally UDP 443 and UDP 4053) there:

- **Oracle Cloud (OCI):** the subnet's security list or the instance's network security group. Oracle's Ubuntu images also ship with their own `iptables` rules that reject inbound ports, so you usually need the host rule as well.
- **Google Cloud (GCP):** a VPC firewall rule allowing the ports to the instance (by network tag or service account). Ingress is denied by default.
- **AWS:** the instance's security group (and the subnet's network ACL if you changed the default).
- **Azure:** the network security group on the NIC or subnet.
- **Other hosts:** look for a "firewall", "security group" or "network rules" panel in the provider's dashboard.

Allow IPv6 separately if the provider's rules are per address family (GCP and AWS rules are; add an IPv6 source range `::/0` alongside IPv4 `0.0.0.0/0`). If activation works for IPv4 but not IPv6, or the log says the UDP ports did not answer, this is the first place to look.

A router in front of a home or office host needs the ports forwarded to it. A host behind carrier-grade NAT cannot receive inbound connections at all and cannot be an extender.

You can tell what the platform could reach from the activation warning. For example, `the platform left out udp 443 (quic)` means that carrier failed the platform's probe even though the family activated. Check the named port in the host firewall and any cloud security group. The health line and status continue to show it as `bound, left out by the platform (probe failed)`. The next renewal retries it; a ✅ line says when the platform accepts it.

## IPv6

IPv6 activates separately. It needs a **public IPv6 address and a working IPv6 route out**. A host with an IPv6 address but no default route cannot reach the platform over IPv6, and the attempt times out after about a minute:

```text
🌉❌ [extender] IPv6 activation try 1 failed after 60.2s: the platform did not answer in time
```

Check on the host: `ip -6 route show default` should print a route, and `curl -6 https://api.bringyour.com/hello` should answer. If either fails, fix the host's IPv6 networking (your provider's gateway setting); the extender cannot work around it. IPv4 is unaffected.

## What the extender will forward

An extender forwards client traffic only to hosts on its allow list (the DNS carrier above is separate). The platform's own hosts are allowed by default: for `api.bringyour.com` that is `bringyour.com` and `*.bringyour.com`. This is required, because the platform's proof forwards a request to its own API host through the extender, and an extender that refuses it can never be listed.

`URNETWORK_EXTENDER_ALLOWED_HOSTS` adds more. It is a comma-separated list of exact hostnames and `*.domain` wildcards, and it is added to the platform's hosts, never in place of them:

```sh
URNETWORK_EXTENDER_ALLOWED_HOSTS='example.com,*.example.org'
```

## Docker

The default run command in [Docker Deployment](/full-bars/meso-miner/wiki/Docker-Deployment) publishes no ports, so the extender could not be reached. There are three ways to run it.

**1. Host networking (simplest, IPv4 and IPv6).** The container uses the host's addresses and ports directly.

```sh
docker run -d --name=meso-extender --pull=always --restart=unless-stopped \
  --network host --cap-add=NET_ADMIN --cap-add=NET_RAW \
  -e AUTH=authcode -e URNETWORK_AUTH_CODE='YOUR_AUTH_CODE_HERE' -e HOST_HOSTNAME=$(hostname) \
  -v urnetwork_data:/root/.urnetwork \
  ghcr.io/full-bars/meso-miner:latest
```

**2. Published ports on the default bridge (IPv4 only).** The default Docker bridge has no IPv6, so only IPv4 activates.

```sh
docker run -d --name=meso-extender --pull=always --restart=unless-stopped \
  --cap-add=NET_ADMIN --cap-add=NET_RAW --sysctl net.ipv4.ip_forward=1 \
  -p 443:443 -p 443:443/udp -p 4053:4053/udp \
  -e AUTH=authcode -e URNETWORK_AUTH_CODE='YOUR_AUTH_CODE_HERE' -e HOST_HOSTNAME=$(hostname) \
  -v urnetwork_data:/root/.urnetwork \
  ghcr.io/full-bars/meso-miner:latest
```

**3. Published ports with IPv6 enabled on the bridge.** Turn on IPv6 in Docker's daemon config (`/etc/docker/daemon.json`), restart Docker, and run the same command as in (2). The container gets a private IPv6 address that Docker translates.

```json
{
  "ipv6": true,
  "fixed-cidr-v6": "fd00:d0c:1::/64",
  "ip6tables": true,
  "experimental": true
}
```

**Compose.** The same three modes as a Compose file. With published ports (use this with the IPv6 daemon setting from (3) to get both families, or as is for IPv4 only):

```yaml
services:
  urnetwork:
    image: ghcr.io/full-bars/meso-miner:latest
    restart: unless-stopped
    cap_add: [NET_ADMIN, NET_RAW]
    sysctls:
      net.ipv4.ip_forward: "1"
    ports:
      - "443:443"
      - "443:443/udp"
      - "4053:4053/udp"
    environment:
      AUTH: authcode
      AUTHCODE: "YOUR_AUTH_CODE_HERE"
      HOST_HOSTNAME: my-host
    volumes:
      - urnetwork_data:/root/.urnetwork
volumes:
  urnetwork_data:
```

With host networking, drop `ports` and `sysctls` and add `network_mode: host`:

```yaml
services:
  urnetwork:
    image: ghcr.io/full-bars/meso-miner:latest
    restart: unless-stopped
    network_mode: host
    cap_add: [NET_ADMIN, NET_RAW]
    environment:
      AUTH: authcode
      AUTHCODE: "YOUR_AUTH_CODE_HERE"
      HOST_HOSTNAME: my-host
    volumes:
      - urnetwork_data:/root/.urnetwork
volumes:
  urnetwork_data:
```

In every case:

- The auth code is single-use. Keep `/root/.urnetwork` on a persistent volume: after the first start the saved login is used (`-e AUTH=authcode` with no code works), and the volume also keeps the extender identity and listing.
- No extender variable is needed. To keep the role off in a container, set `-e URNETWORK_EXTENDER=off`.
- `urnet-tools extender` is not available inside the container. Read the state with `docker logs <container> | grep 🌉` (the `[health][extender]` line repeats every few minutes) and control the role with the environment variable. The Docker log follower can replay retained lines; this limitation is not fixed in this release.
- The port also has to be open on the host's firewall and forwarded by whatever sits in front of the host.

## Reading the log

Every extender line starts with a 🌉. A line that reports an outcome adds a mark: ✅ worked, ⛔ the platform refused, ❌ failed, ⚠️ a warning. `grep 🌉` finds all of them and `grep '🌉❌'` every failure. For the full list see the [Log Message Reference](/full-bars/meso-miner/blob/main/LOG_REFERENCE.md).

When the role starts:

```text
🌉✅ [extender] started (startup; setting from default). It listens for censored clients on tcp 443 (required), then udp 443 and dns 4053, then asks the platform to list this host.
🌉✅ [extender] ready — serving on tcp, quic and dns; forwards allowed for 2 hosts
🌉✅ [extender] activation started: this host now asks the platform to list it for IPv4 and IPv6. Each family is tried separately; results are logged as they happen.
🌉✅ [extender] IPv4 activation try 1 worked in 10.2s
🌉✅ [extender] IPv4 is now activated on try 1. The platform can reach this host over IPv4.
```

When it is off, it says why once at startup and how to turn it back on:

```text
🌉 [extender] off: turned off by control socket (urnet-tools extender off). Turn it back on with: urnet-tools extender on
```

The periodic `[health][extender]` line summarises everything:

```text
🌉❤️ [health][extender] serving | tcp 443 bound, udp 443 bound, dns 4053 bound | IPv4 ACTIVE 203.0.113.5; tcp 443 bound, accepted by the platform; udp 443 bound, left out by the platform (probe failed); dns 4053 bound, left out by the platform (probe failed) | IPv6 not active (failed): no public IPv6 address, next try 17:23; tcp 443 bound, platform status unknown; udp 443 bound, platform status unknown; dns 4053 bound, platform status unknown | clients now 2, requests served 24h 120, 7d 800, lifetime 5000
```

The role also exports its state to Prometheus. Turn metrics on with `urnet-tools metrics on` and read the `urnet_extender_*` series: `urnet_extender_enabled`, `urnet_extender_state{state=...}` with one series per state (`off`, `starting`, `serving`, `tcp_unavailable`, `unavailable`, `error`, `closed`), `urnet_extender_carrier_listening{carrier=...}`, `urnet_extender_carrier_listen_error{carrier=...}`, `urnet_extender_dns53_state{state=...}` with one series per state (`bound`, `disabled`, `address_in_use`, `permission_denied`, `unknown`, `not_requested`, `pending`), `urnet_extender_requests_total{service=...}`, `urnet_extender_refused_total{stage=...}`, `urnet_extender_carrier_requests_total{carrier=...,outcome=...}`, `urnet_extender_carrier_refused_total{carrier=...,stage=...}`, `urnet_extender_tcp_retry_total` and `urnet_extender_connections_active`.

`serving on tcp` means the process bound the port inside itself. **ACTIVE** means the platform proved the host from the outside and listed it. Neither tells you that a particular client is using it; the live client count and the request totals do.

## When it is not working

| What you see | What it means | What to do |
|---|---|---|
| `CANNOT USE tcp 443 … address already in use` | Something else holds TCP 443 (often caddy or nginx). | Stop or move it. The extender retries every 3 minutes by itself. |
| `CANNOT USE tcp 443 … permission denied` | The provider may not bind below 1024. | `urnet-tools extender setup --route=sysctl` (sysctl route), `sudo setcap cap_net_bind_service=+ep <provider binary>`, or run as root. |
| IPv4 ACTIVE, `bound, left out by the platform (probe failed)` | The named carrier was left out after its probe failed. | Check that port in the host firewall and any cloud security group. The next renewal retries it. |
| `dns 53 unavailable … address already in use; keeping 4053` | Another resolver holds UDP 53. | Nothing to do, 4053 serves. `URNETWORK_EXTENDER_DNS53=off` silences it. |
| IPv6 `failed … did not answer in time` | No IPv6 route out, or inbound IPv6 blocked. | `ip -6 route show default` and `curl -6 https://api.bringyour.com/hello` on the host. |
| `the platform says this extender key belongs to ANOTHER network` | The identity file was copied from another box or account. | Stop the provider, delete `~/.urnetwork/.provider.extender.key`, start it again. |
| A family stays `waiting` | TCP 443 is not bound, the platform asked it to wait, or direct providing is off. | Read the status line; it says which. |
| `elevated extender setup target changed after discovery; refusing to configure any provider` | Root resolved a different provider than the one you inspected, so setup refused before it changed anything. | Run the inventory command that setup prints, then run setup again with an explicit `--unit` or `--state-dir`. |
| `off: direct providing is disabled` | `urnet-tools direct off` is set. | `urnet-tools direct on`. |
| `FAILED to start: <reason>` | The role failed for a reason other than a port. | The provider keeps providing and retries every 15 seconds; fix the reason shown. |

### Known limitations

The Docker log follower can replay retained lines. A hot-swap candidate can race to bind a socket still held by the current provider. These limitations are not fixed in this release.

## FAQ

**Does it earn?** The role itself does not change what the provider earns. It is a public service like a Tor bridge. The network operator announced on 2026-11-12 an extender pool for SN25 that starts with epoch 8, around 2026-11-27. An extender in that pool can earn alpha, whose value changes and can fall. Eligibility at the end of an epoch needs the extender to be in the active pool, to have an address that passes the five-minute challenge, to have a payout wallet, to stay outside a top-level miner's fleet and to stay off the operator's own networks. From 2026-11-12 an extender also counts as a contract hop only when it can have relayed that contract. Read the notices on `ur.xyz/mainnet` before you rely on this, because the operator sets the rules and can change them. This release adds no support for the pool.

**Can a proxy be an extender?** No. The platform dials the address it sees; for a proxy that is the proxy's server, where nothing of this provider listens. Only the host the provider runs on can be an extender.

**Can I run more than one on a box?** Only one process can hold TCP 443 on an address, so one extender per address.

**Does restarting cost anything?** Each family asks the platform once more after a restart, spread over the next 30 minutes. That counts toward the platform's 6 per hour per family. A still-valid listing keeps showing ACTIVE meanwhile.
