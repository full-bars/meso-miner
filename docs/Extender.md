# Run a provider as an extender

The extender role lets the provider accept incoming carrier connections and forward requests to approved hostnames. It is opt-in and off by default.

## Before you start

The host must be reachable on the ports you publish. Open the ports in the host firewall and forward them through any router or NAT in front of the host.

Set `URNETWORK_EXTENDER=on` to enable the role. The provider also accepts `1` and `true`; other values leave it off. The role uses the direct slot, whose egress address belongs to this host. Configured proxy entries do not block the role, but a proxied slot cannot host it because that slot exits through the proxy's address.

Direct providing must be enabled at startup. The provider checks a saved `~/.urnetwork/direct` setting before `DISABLE_DIRECT_IP=1`; with neither set, direct providing is on. If it is disabled, the provider logs:

```text
[extender] role not started: direct providing is disabled
```

The extender identity is stored at `~/.urnetwork/.provider.extender.key` with mode `0600`. In Docker, keep `/root/.urnetwork` on a persistent volume so the identity survives container recreation.

## Forward allow list

`URNETWORK_EXTENDER_ALLOWED_HOSTS` is a comma-separated list of exact hostnames and `*.domain` patterns. A wildcard matches subdomains, including deeper names, but does not match the base domain. An empty list refuses every forward request.

```sh
URNETWORK_EXTENDER_ALLOWED_HOSTS='example.com,*.example.org'
```

When the list is empty, the provider logs:

```text
[extender] no forward hosts configured (URNETWORK_EXTENDER_ALLOWED_HOSTS); every forward request will be refused
```

## Ports

The provider attempts these carriers:

- TCP 443 carries the TLS carrier.
- UDP 443 carries QUIC.
- UDP 4053 carries the DNS carrier.

Set `URNETWORK_EXTENDER_DNS53=on` to also bind the DNS carrier on UDP 53. It is additive, so DNS 4053 remains enabled. Port 53 is commonly already held by a local resolver.

If TCP 443 cannot be bound, the role continues with the other carriers that bind and retries TCP 443 no faster than every three minutes. Binding a port below 1024 requires root or `CAP_NET_BIND_SERVICE` on a bare-metal host. A carrier bind failure does not stop the provider.

## Docker

The default Docker examples publish no inbound ports. To run the extender, enable the role and publish its carrier ports:

```sh
docker run -d \
  --name=meso-extender \
  --pull=always \
  --restart=unless-stopped \
  --cap-add=NET_ADMIN \
  --cap-add=NET_RAW \
  --sysctl net.ipv4.ip_forward=1 \
  -p 443:443/tcp \
  -p 443:443/udp \
  -p 4053:4053/udp \
  -e BUILD=jwt \
  -e URNETWORK_EXTENDER=on \
  -e URNETWORK_EXTENDER_ALLOWED_HOSTS='example.com,*.example.org' \
  -e HOST_HOSTNAME="$(hostname)" \
  -v meso_extender_state:/root/.urnetwork \
  ghcr.io/full-bars/meso-miner:latest AUTH_CODE_HERE
```

To add the optional DNS port, set `URNETWORK_EXTENDER_DNS53=on` and add `-p 53:53/udp` to the `docker run` command. Do not publish UDP 53 if another service on the host already owns it. If the host is behind a router, forward the published ports to it as well.

## Bare-metal install

Set the same variables in the provider service environment and restart the service:

```ini
Environment="URNETWORK_EXTENDER=on"
Environment="URNETWORK_EXTENDER_ALLOWED_HOSTS=example.com,*.example.org"
```

## Confirm the role started

The first `[extender] ready` log line reports the carriers that are listening and the forwarding policy. A `tcp_unavailable` state means TCP 443 is being retried while other carriers remain available. A ready line confirms the listeners bound inside the provider, not that a firewall, router or upstream network can reach them. Test from another network.

If the role cannot start for another reason, the provider logs `[extender] role disabled:` followed by the error and continues without the extender role.

When the provider metrics endpoint is enabled, the extender exports `urnet_extender_enabled`, `urnet_extender_state`, `urnet_extender_carrier_listening`, `urnet_extender_carrier_listen_error`, `urnet_extender_requests_total`, `urnet_extender_refused_total`, `urnet_extender_connections_active`, `urnet_extender_relay_bytes_total` and `urnet_extender_tcp_retry_total`.

For the surrounding container startup behavior, see [Docker Deployment](/full-bars/meso-miner/wiki/Docker-Deployment) and [Log Message Reference](/full-bars/meso-miner/blob/main/LOG_REFERENCE.md).
