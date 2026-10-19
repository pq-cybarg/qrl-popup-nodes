#!/bin/sh
# Universal beacon-chain entrypoint wrapper for popup-node containers.
#
# Handles two container-specific problems that bare-metal doesn't have:
#
# 1. QNR advertisement. beacon-chain encodes --p2p-host-ip into its QNR
#    so peers know how to dial it. Passing 0.0.0.0 produces a QNR that
#    followers can't dial. We detect the container's bridge IP at boot
#    and inject it as --p2p-host-ip.
#
# 2. Follower bootstrap. libp2p discv5 UDP + mDNS don't traverse
#    Docker's user bridge cleanly. If /opt/qrl/bootstrap/leader.qnr
#    exists (populated by the bootstrap-fetcher init container), we
#    inject it as --bootstrap-node so followers immediately dial the
#    leader instead of hoping to discover it.
#
# Both behaviors are opt-in by mount: only followers mount the
# bootstrap volume, so the leader skips branch 2 and only auto-injects
# its own container IP.

set -e

SELF_IP=$(hostname -i 2>/dev/null | awk '{print $1}')
if [ -z "$SELF_IP" ]; then
    SELF_IP=$(ip -4 -o addr show 2>/dev/null | awk '$2 != "lo" {sub("/.*","",$4); print $4; exit}')
fi
if [ -z "$SELF_IP" ]; then
    echo "beacon-entrypoint: ERROR — could not detect container IP for QNR advertisement" >&2
    exit 1
fi

set -- --p2p-host-ip="$SELF_IP" "$@"

LEADER_QNR_FILE="${LEADER_QNR_FILE:-/opt/qrl/bootstrap/leader.qnr}"
if [ -f "$LEADER_QNR_FILE" ]; then
    QNR=$(cat "$LEADER_QNR_FILE" 2>/dev/null || true)
    if [ -n "$QNR" ]; then
        QNR_PREFIX=$(printf '%s' "$QNR" | cut -c1-60)
        echo "beacon-entrypoint: follower mode — bootstrapping from ${QNR_PREFIX}..."
        set -- --bootstrap-node="$QNR" "$@"
    else
        echo "beacon-entrypoint: WARN — $LEADER_QNR_FILE exists but is empty; starting without bootstrap peer" >&2
    fi
fi

echo "beacon-entrypoint: self IP $SELF_IP; exec beacon-chain $*"
exec /opt/qrl/bin/beacon-chain "$@"
