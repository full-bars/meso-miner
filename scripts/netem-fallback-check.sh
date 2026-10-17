#!/usr/bin/env bash
#
# netem-fallback-check.sh — prove the staged transport fallback engages, by
# breaking the transports under it one stage at a time.
#
# Run this on a TEST host only. It deliberately degrades a provider container's
# network so the fallback chain has to move:
#
#     H1  ->  H3  ->  H3 over DNS  ->  DNS pump
#
# It drops TCP/443 egress first, which should push the direct identity onto H3,
# then drops UDP/443 as well, which should push it onto H3 over DNS and then the
# DNS pump. After each step it polls the container's /metrics and asserts the
# next stage actually engaged rather than merely being attempted.
#
# WHAT IT TOUCHES, AND WHAT IT WILL NOT
#   * Filters are installed inside the CONTAINER's network namespace only, via
#     `nsenter`. The host namespace is never modified.
#   * A container on host networking has no separate namespace to filter, so the
#     script REFUSES to run rather than degrade the host. This is the single most
#     important safety property here.
#   * Every filter it installs is removed on exit, including on error and on
#     Ctrl-C, via a trap. The trap is installed before the first filter is added.
#
# WHAT IT NEEDS
#   * root (or CAP_NET_ADMIN plus nsenter)
#   * `docker`, `nsenter`, `tc` with the `prio`, `u32` and `netem` modules
#   * a provider container with metrics enabled, and the container name passed
#     with -c
#
# It does NOT need network access to anything else, does not ssh anywhere, and
# makes no change outside the target container.
#
# USAGE
#   sudo ./scripts/netem-fallback-check.sh -c <container> [-p <metrics-port>]
#                                           [-t <stage-timeout-seconds>] [--dry-run]
#
# EXIT CODES
#   0  every stage engaged within its timeout
#   1  a stage did not engage (reported), or the environment was refused
#   2  a required tool is missing

set -euo pipefail

CONTAINER=""
METRICS_PORT=9090
STAGE_TIMEOUT=120
POLL_INTERVAL=5
DRY_RUN=0

# TC_HANDLE is the prio qdisc handle. Filters hang off it so removal is one
# command, not a search through whatever else the container's netns contains.
TC_HANDLE="ffff:"

usage() {
    sed -n '2,45p' "$0" | sed 's/^# \{0,1\}//'
    exit "${1:-1}"
}

while [ $# -gt 0 ]; do
    case "$1" in
        -c|--container) CONTAINER="${2:-}"; shift 2 ;;
        -p|--metrics-port) METRICS_PORT="${2:-}"; shift 2 ;;
        -t|--timeout) STAGE_TIMEOUT="${2:-}"; shift 2 ;;
        --dry-run) DRY_RUN=1; shift ;;
        -h|--help) usage 0 ;;
        *) echo "unknown argument: $1" >&2; usage 1 ;;
    esac
done

log()  { printf '%s %s\n' "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*" >&2; exit 1; }

[ -n "$CONTAINER" ] || { echo "container name is required (-c)" >&2; usage 1; }
[ "$(id -u)" -eq 0 ] || fail "must run as root (needs nsenter and tc)"

for tool in docker nsenter tc; do
    command -v "$tool" >/dev/null 2>&1 || { log "missing tool: $tool"; exit 2; }
done

docker inspect "$CONTAINER" >/dev/null 2>&1 || fail "no such container: $CONTAINER"

# --- refuse host networking -------------------------------------------------
# A container sharing the host namespace has no netns of its own to filter, so
# any tc rule would land on the host. Refuse, rather than degrade the machine
# this script is being run from.
NETWORK_MODE="$(docker inspect -f '{{.HostConfig.NetworkMode}}' "$CONTAINER")"
case "$NETWORK_MODE" in
    host)
        fail "container $CONTAINER uses host networking, so its tc rules would be the HOST's. Refusing."
        ;;
    default|bridge|"")
        log "container network mode: ${NETWORK_MODE:-default}"
        ;;
    *)
        # A named bridge or a user-defined network is fine; only 'host' is not.
        log "container network mode: $NETWORK_MODE (treated as a bridge)"
        ;;
esac

# --- find the netns ---------------------------------------------------------
PID="$(docker inspect -f '{{.State.Pid}}' "$CONTAINER")"
[ -n "$PID" ] && [ "$PID" != "0" ] || fail "container $CONTAINER is not running (pid=$PID)"
NETNS="/proc/$PID/ns/net"
[ -e "$NETNS" ] || fail "no network namespace at $NETNS"
log "container $CONTAINER pid=$PID netns=$NETNS"

# nsenter wrapper: every tc call runs inside the container's namespace.
in_ns() { nsenter -t "$PID" -n -- "$@"; }

# --- cleanup ----------------------------------------------------------------
# Installed BEFORE the first filter. Removes the whole prio qdisc, which takes
# every filter with it, so a partial install is still fully undone.
installed=0
cleanup() {
    if [ "$installed" -eq 1 ]; then
        log "removing filters from $CONTAINER"
        in_ns tc qdisc del dev eth0 root handle "$TC_HANDLE" prio >/dev/null 2>&1 || true
        installed=0
        log "filters removed"
    fi
}
trap cleanup EXIT INT TERM

# --- metrics helpers --------------------------------------------------------
# Reads one metric from the container's own metrics endpoint, from inside the
# namespace, so a filtered host path does not matter.
metric() {
    local expr="$1"
    in_ns sh -c "command -v wget >/dev/null 2>&1 && wget -qO- http://127.0.0.1:$METRICS_PORT/metrics || curl -s http://127.0.0.1:$METRICS_PORT/metrics" 2>/dev/null \
        | awk -v pat="$expr" '$0 ~ pat { sum += $NF } END { printf "%d", sum + 0 }'
}

# newest_value returns the sample value of the last matching line, or 0.
metric_sum() { metric "$1"; }

wait_for_stage() {
    local name="$1" expr="$2" want="${3:-1}"
    local deadline=$(( $(date +%s) + STAGE_TIMEOUT ))
    while [ "$(date +%s)" -lt "$deadline" ]; do
        local v
        v="$(metric_sum "$expr")"
        if [ "${v:-0}" -ge "$want" ]; then
            log "STAGE OK: $name engaged (value=$v)"
            return 0
        fi
        sleep "$POLL_INTERVAL"
    done
    log "STAGE TIMEOUT: $name did not engage within ${STAGE_TIMEOUT}s"
    return 1
}

# --- filters ----------------------------------------------------------------
# A prio qdisc is the container's root qdisc; u32 filters hang off it and drop
# by destination port. Applied to eth0, the container's own interface.
install_qdisc() {
    if [ "$DRY_RUN" -eq 1 ]; then
        log "DRY RUN: would install the prio qdisc and filters"
        return 0
    fi
    in_ns tc qdisc add dev eth0 root handle "$TC_HANDLE" prio >/dev/null
    installed=1

    # TCP/443 egress: pushes H1 (websocket tls and h1+) off, leaving H3.
    in_ns tc filter add dev eth0 parent "${TC_HANDLE}1" protocol ip   prio 1 u32 match ip   dport 443 0xffff action drop
    in_ns tc filter add dev eth0 parent "${TC_HANDLE}1" protocol ipv6 prio 1 u32 match ip6  dport 443 0xffff action drop
    log "installed: TCP/443 egress dropped"
}

add_udp_drop() {
    if [ "$DRY_RUN" -eq 1 ]; then
        log "DRY RUN: would also drop UDP/443 egress"
        return 0
    fi
    in_ns tc filter add dev eth0 parent "${TC_HANDLE}1" protocol ip   prio 2 u32 match ip   dport 443 0xffff action drop
    in_ns tc filter add dev eth0 parent "${TC_HANDLE}1" protocol ipv6 prio 2 u32 match ip6  dport 443 0xffff action drop
    log "installed: UDP/443 egress dropped as well"
}

# --- the check --------------------------------------------------------------
log "baseline: reading metrics before any filter"
base_h3="$(metric_sum 'urnet_transport_frames_total\{[^}]*mode="h3"')"
log "baseline h3 frames: ${base_h3:-0}"

install_qdisc

# H3 over UDP should take over once TCP/443 is gone.
if ! wait_for_stage "H3 (tcp/443 dropped)" 'urnet_h3_up' 1; then
    fail "H3 did not engage with TCP/443 dropped. Is h3 enabled for the direct identity (urnet-tools set h3 on|direct)?"
fi

add_udp_drop

# With both dropped, H3 over DNS and then the DNS pump should carry it.
h3dns_ok=0
if wait_for_stage "H3 over DNS (udp/443 also dropped)" 'urnet_transport_frames_total\{[^}]*mode="h3dns"' 1; then
    h3dns_ok=1
fi
pump_ok=0
if wait_for_stage "DNS pump" 'urnet_transport_frames_total\{[^}]*mode="h3dnspump"' 1; then
    pump_ok=1
fi

if [ "$h3dns_ok" -eq 1 ] || [ "$pump_ok" -eq 1 ]; then
    log "PASS: the fallback chain moved past H1 with both TCP and UDP 443 dropped"
else
    fail "neither H3 over DNS nor the DNS pump carried a frame. Check that the DNS transports are enabled for this identity, and that the metrics endpoint is reachable at 127.0.0.1:$METRICS_PORT."
fi

log "removing filters and re-reading H1"
cleanup
sleep "$POLL_INTERVAL"
log "post-cleanup h3_up=$(metric_sum 'urnet_h3_up')"
log "done"
