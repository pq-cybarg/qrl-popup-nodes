#!/bin/bash
# Full reset of a local popup-node instance.
#
# One-command wipe of everything the popup-node substrate created on
# this host. After running, you're back to a state identical to a
# fresh `git clone` — no compose stack, no chaindata, no keystore
# master keys, no local images. `./configure.sh` + `docker compose up`
# will build the whole thing from scratch.
#
# What gets wiped:
#   * Running compose stack: docker compose down -v (or podman)
#   * Bind-mount data: rm -rf docker/data (chaindata, config, keystores
#     the compose stack created — NOT wallets under $HOME)
#   * Compose secrets: rm -f docker/.env
#   * qrlenv master key: purged from the OS keychain
#   * Local test-only container images tagged qrl-popup-* or
#     qrl-{gqrl,beacon,validator,dashboard}:*
#   * TLS certs under docker/tls/ (self-signed dev certs; regenerate
#     with docker/gen-tls-cert.sh)
#
# What is PRESERVED (deliberately):
#   * Wallets under $HOME/.qrl-dashboard/ (may hold real funds)
#   * Snapshots under docker/data/.snapshots/ (if wanted, invoke with
#     --wipe-snapshots to remove those too)
#   * Bare-metal chain state under qrl-2.0/chains/localnet/ (this
#     script is compose-only; use qrl-2.0/tools/reset-bare-metal.sh
#     for bare-metal reset)
#   * The signed release output under docker/out/
#
# Usage:
#   ./docker/reset-popup.sh                       # interactive confirm
#   ./docker/reset-popup.sh --yes                 # skip confirmation
#   ./docker/reset-popup.sh --wipe-snapshots      # ALSO remove snapshots
#   ./docker/reset-popup.sh --wipe-releases       # ALSO remove docker/out/
#   ./docker/reset-popup.sh --wipe-images         # ALSO remove local
#                                                   built images (frees
#                                                   1-2GB of docker cache)
#   CONTAINER_ENGINE=podman ./docker/reset-popup.sh
#
# Idempotent: safe to run when there's nothing to wipe.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
QRLENV="$REPO_ROOT/bin/qrlenv"
ENV_FILE="$SCRIPT_DIR/.env"
DATA_DIR="$SCRIPT_DIR/data"
OUT_DIR="$SCRIPT_DIR/out"
TLS_DIR="$SCRIPT_DIR/tls"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yaml"
ENGINE="${CONTAINER_ENGINE:-docker}"

WIPE_SNAPSHOTS=0
WIPE_RELEASES=0
WIPE_IMAGES=0
YES=0
while [ $# -gt 0 ]; do
    case "$1" in
        --yes|-y)          YES=1; shift ;;
        --wipe-snapshots)  WIPE_SNAPSHOTS=1; shift ;;
        --wipe-releases)   WIPE_RELEASES=1; shift ;;
        --wipe-images)     WIPE_IMAGES=1; shift ;;
        -h|--help)         sed -n '3,40p' "$0" | sed 's|^# \{0,1\}||'; exit 0 ;;
        *)                 echo "unknown flag: $1" >&2; exit 2 ;;
    esac
done

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

# ---------- preview ----------

info "planned wipe:"
echo "  * running compose stack (if any)"
echo "  * $DATA_DIR (chaindata, per-node keystores, jwt secrets)"
echo "  * $ENV_FILE (compose secrets)"
echo "  * qrlenv master key in OS keychain (profile: qrlenv)"
[ $WIPE_SNAPSHOTS -eq 1 ] && echo "  * $DATA_DIR/.snapshots (--wipe-snapshots)"
[ $WIPE_RELEASES -eq 1 ]  && echo "  * $OUT_DIR (--wipe-releases)"
[ $WIPE_IMAGES -eq 1 ]    && echo "  * local qrl-* container images (--wipe-images)"
echo
info "PRESERVED:"
echo "  * wallets under \$HOME/.qrl-dashboard/"
echo "  * bare-metal chain state under qrl-2.0/chains/"
[ $WIPE_SNAPSHOTS -eq 0 ] && echo "  * snapshots under $DATA_DIR/.snapshots"
[ $WIPE_RELEASES -eq 0 ]  && echo "  * releases under $OUT_DIR"
[ $WIPE_IMAGES -eq 0 ]    && echo "  * local qrl-* container images"
echo

if [ $YES -eq 0 ]; then
    warn "this is destructive — chaindata + keystores + secrets will be gone"
    printf "type YES to proceed: "
    read -r reply
    [ "$reply" = "YES" ] || { info "aborted"; exit 0; }
fi

# ---------- 1. stop compose stack ----------

info "stopping compose stack"
if [ -f "$COMPOSE_FILE" ]; then
    # `down -v` removes the named qrl-bootstrap volume; bind-mounts we
    # wipe separately below.
    "$ENGINE" compose -f "$COMPOSE_FILE" down -v --remove-orphans 2>&1 \
        | grep -vE "^time=|WARN" | tail -5 || true
    ok "compose stack stopped"
else
    info "  no compose file — skipping"
fi

# Preserve snapshots by moving them aside before wiping DATA_DIR.
SNAPS_SAVED=""
if [ $WIPE_SNAPSHOTS -eq 0 ] && [ -d "$DATA_DIR/.snapshots" ]; then
    SNAPS_SAVED="$(mktemp -d "${TMPDIR:-/tmp}/qrl-snapshots-XXXXXXXX")"
    mv "$DATA_DIR/.snapshots" "$SNAPS_SAVED/"
    ok "snapshots preserved: $SNAPS_SAVED (will be restored)"
fi

# ---------- 2. wipe data directory ----------

if [ -d "$DATA_DIR" ]; then
    info "wiping $DATA_DIR"
    rm -rf "$DATA_DIR"
    ok "  wiped"
else
    info "  $DATA_DIR absent — skipping"
fi

# Restore preserved snapshots.
if [ -n "$SNAPS_SAVED" ] && [ -d "$SNAPS_SAVED/.snapshots" ]; then
    mkdir -p "$DATA_DIR"
    mv "$SNAPS_SAVED/.snapshots" "$DATA_DIR/"
    rmdir "$SNAPS_SAVED" 2>/dev/null || true
    ok "  snapshots restored"
fi

# ---------- 3. wipe compose secrets ----------

if [ -f "$ENV_FILE" ]; then
    info "wiping $ENV_FILE"
    rm -f "$ENV_FILE"
    ok "  wiped"
else
    info "  $ENV_FILE absent — skipping"
fi

# ---------- 4. purge qrlenv master key ----------

info "purging qrlenv master key from OS keychain"
if [ -x "$QRLENV" ]; then
    "$QRLENV" --profile qrlenv delete-master 2>&1 | tail -1 || true
    ok "  purged"
else
    warn "  qrlenv not found at $QRLENV — skipped"
fi

# ---------- 5. optional wipes ----------

if [ $WIPE_RELEASES -eq 1 ] && [ -d "$OUT_DIR" ]; then
    info "wiping $OUT_DIR (--wipe-releases)"
    rm -rf "$OUT_DIR"
    ok "  wiped"
fi

if [ -d "$TLS_DIR" ]; then
    info "wiping $TLS_DIR (self-signed dev TLS certs)"
    rm -rf "$TLS_DIR"
    ok "  wiped"
fi

if [ $WIPE_IMAGES -eq 1 ]; then
    info "removing local qrl-* container images (--wipe-images)"
    IMG_IDS=$("$ENGINE" images --format '{{.Repository}}:{{.Tag}}' 2>/dev/null \
             | grep -E '^(qrl-|ghcr\.io/theqrl/qrl-)' || true)
    if [ -n "$IMG_IDS" ]; then
        # Ignore removal errors on images still referenced by other tags.
        echo "$IMG_IDS" | xargs -r "$ENGINE" rmi 2>&1 | tail -5 || true
        ok "  removed $(echo "$IMG_IDS" | wc -l | tr -d ' ') image tag(s)"
    else
        info "  no qrl-* images to remove"
    fi
fi

# ---------- done ----------

echo
ok "popup-node local state reset — repository is in a fresh-clone-equivalent state"
info "next: ./docker/configure.sh"
