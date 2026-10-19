#!/bin/sh
# Init-container script that polls beacon-node1's REST gateway for its
# QNR and writes it to a shared volume so follower beacons can bootstrap.
#
# Bare-metal equivalent: qrl-2.0/configs/localnet/start-follower-beacons.sh
# — that script curls localhost:3600/qrl/v1/node/identity and passes the
# QNR as --bootstrap-node when it forks each follower. In compose we
# split it into (a) this one-shot fetcher, and (b) an entrypoint wrapper
# on each follower beacon that reads the file at start.

set -e

GATEWAY="${LEADER_BEACON_GATEWAY:-http://beacon-node1:3600}"
OUT="${QNR_OUTPUT:-/shared/leader.qnr}"
MAX_TRIES="${MAX_TRIES:-90}"
SLEEP_SECS="${SLEEP_SECS:-2}"

OUT_DIR="$(dirname "$OUT")"
mkdir -p "$OUT_DIR" 2>/dev/null || true

echo "fetch-leader-qnr: waiting on $GATEWAY/qrl/v1/node/identity (max ${MAX_TRIES}x${SLEEP_SECS}s)..."

i=1
while [ "$i" -le "$MAX_TRIES" ]; do
    QNR=$(curl -sf --max-time 5 "$GATEWAY/qrl/v1/node/identity" 2>/dev/null \
          | jq -r '.data.qnr // empty' 2>/dev/null || true)
    if [ -n "$QNR" ] && [ "$QNR" != "null" ]; then
        printf '%s' "$QNR" > "$OUT"
        QNR_PREFIX=$(printf '%s' "$QNR" | cut -c1-60)
        echo "fetch-leader-qnr: wrote QNR to $OUT (${QNR_PREFIX}...)"
        exit 0
    fi
    echo "fetch-leader-qnr: attempt $i/$MAX_TRIES — leader not ready, sleeping ${SLEEP_SECS}s"
    i=$((i + 1))
    sleep "$SLEEP_SECS"
done

echo "fetch-leader-qnr: ERROR — could not fetch leader QNR after $MAX_TRIES attempts" >&2
exit 1
