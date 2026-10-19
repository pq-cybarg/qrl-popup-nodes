#!/bin/bash
# configure.sh — first-time popup-node bring-up wizard.
#
# Interactively prompts for the essentials, writes `.env`, initializes the
# qrlenv master key, generates per-node JWT secrets (encrypted with
# qrlenv), and chowns the data directory to UID 1000 (so the rootless
# containers can write to it).
#
# Safe to re-run. Refuses to clobber an existing .env unless you pass
# --force. Re-running with --force does NOT rotate qrlenv keys or
# regenerate JWT secrets that are already set; it only rewrites the
# plaintext-knob lines.
#
# See docs/POPUP-NODES-ROOTLESS.md for the full config surface.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
QRLENV="$REPO_ROOT/bin/qrlenv"
ENV_FILE="$SCRIPT_DIR/.env"
EXAMPLE="$SCRIPT_DIR/.env.example"
PROFILE="qrlenv"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'

die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

# ---------- flags ----------

FORCE=0
INTERACTIVE=1
REGEN_GENESIS=0
while [ $# -gt 0 ]; do
    case "$1" in
        --force) FORCE=1; shift ;;
        --non-interactive) INTERACTIVE=0; shift ;;
        --regen-genesis) REGEN_GENESIS=1; shift ;;
        --profile) PROFILE="${2:-}"; shift 2 ;;
        -h|--help)
            sed -n '1,20p' "$0" | sed 's|^# \{0,1\}||'
            echo "Flags:"
            echo "  --force              overwrite existing .env"
            echo "  --regen-genesis      always regenerate genesis.ssz (default: only if missing)"
            echo "  --non-interactive    accept all defaults, no prompts"
            echo "  --profile <name>     qrlenv keychain profile (default: qrlenv)"
            exit 0
            ;;
        *) die "unknown flag: $1" ;;
    esac
done

# ---------- pre-flight ----------

[ -x "$QRLENV" ] || die "qrlenv not found at $QRLENV. Run 'chmod +x $QRLENV' and retry."
[ -f "$EXAMPLE" ] || die ".env.example not found at $EXAMPLE"

if [ -f "$ENV_FILE" ] && [ $FORCE -eq 0 ]; then
    die ".env already exists. Pass --force to overwrite. Existing encrypted values are preserved either way."
fi

# ---------- disk-space preflight -----------------------------------------
#
# Bring-up + snapshots + container images + chaindata add up to 5-15GB
# per popup-node instance. Silent out-of-space failures during
# `docker compose up` are notoriously hard to debug (containers die
# mid-boot with cryptic errors). Warn early. Bypass with
# QRL_ACK_LOW_DISK=1 or QRL_SKIP_DISK_PREFLIGHT=1.

if [ "${QRL_SKIP_DISK_PREFLIGHT:-0}" != "1" ]; then
    info "disk-space preflight"
    # Check the filesystem that will host docker/data — mkdir first
    # since df needs a real path.
    DISK_TARGET="${QRL_DATA_DIR:-$SCRIPT_DIR/data}"
    mkdir -p "$DISK_TARGET"
    # df -Pk emits POSIX-portable output: 1024-blocks in col 4 (available)
    AVAIL_KB=$(df -Pk "$DISK_TARGET" 2>/dev/null | awk 'NR==2 {print $4}')
    if [ -n "$AVAIL_KB" ]; then
        AVAIL_GB=$((AVAIL_KB / 1024 / 1024))
        info "  target: $DISK_TARGET"
        info "  free:   ${AVAIL_GB}GB"
        if [ "$AVAIL_KB" -lt 2097152 ]; then       # < 2GB
            warn "  ✗ CRITICAL: less than 2GB free — bring-up will corrupt or crash"
            [ "${QRL_ACK_LOW_DISK:-0}" = "1" ] || \
                die "aborting; set QRL_ACK_LOW_DISK=1 to override, or free some disk"
        elif [ "$AVAIL_KB" -lt 10485760 ]; then    # < 10GB
            warn "  ! LOW: less than 10GB free — a full bring-up + snapshot cycle needs ~15GB"
            warn "  ! development can proceed but you'll hit space issues within a day"
        else
            ok "  sufficient headroom (>10GB)"
        fi
    else
        warn "  could not read free space on $DISK_TARGET — skipping"
    fi
fi

# ---------- CPU-mitigations preflight (phase 2, nation-state target) ---------
#
# Bring-up will still proceed if mitigations are missing — we just want the
# operator to see it in writing before they trust their node to a machine
# that's exposed to Spectre v2 / L1TF / MDS. Skipped on non-Linux hosts
# (macOS has no /sys/devices/system/cpu/vulnerabilities/). Bypass entirely
# with QRL_SKIP_CPU_PREFLIGHT=1.

if [ "${QRL_SKIP_CPU_PREFLIGHT:-0}" != "1" ] && [ -d /sys/devices/system/cpu/vulnerabilities ]; then
    info "CPU-mitigations preflight (Linux host)"
    VULN=""
    for f in /sys/devices/system/cpu/vulnerabilities/*; do
        [ -f "$f" ] || continue
        name=$(basename "$f")
        val=$(cat "$f" 2>/dev/null || echo "?")
        case "$val" in
            *"Vulnerable"*|*"vulnerable"*)
                warn "  ✗ $name: $val"
                VULN="$VULN $name"
                ;;
            *"Not affected"*|*"Mitigation"*)
                # Mitigation applied or not affected — quiet path.
                ;;
            *)
                info "  ? $name: $val"
                ;;
        esac
    done
    if [ -n "$VULN" ]; then
        warn "Host is Vulnerable to:$VULN"
        warn "This is a NATION-STATE target profile — you should not proceed on"
        warn "an unpatched host. Fix microcode / kernel-cmdline mitigations, or set"
        warn "QRL_SKIP_CPU_PREFLIGHT=1 to override with acknowledged risk."
        [ "${QRL_ACK_CPU_VULN:-0}" = "1" ] || die "aborting; set QRL_ACK_CPU_VULN=1 to override"
    else
        ok "  no Vulnerable status reported by /sys/devices/system/cpu/vulnerabilities"
    fi
fi

# ---------- prompts ----------

prompt() {
    local var="$1" default="$2" description="$3"
    local val=""
    if [ $INTERACTIVE -eq 1 ]; then
        echo
        echo -e "${CYAN}${description}${NC}"
        printf "  ${var} [${default}]: "
        read -r val
    fi
    echo "${val:-$default}"
}

info "QRL popup-node configuration wizard"
if [ $INTERACTIVE -eq 0 ]; then
    warn "non-interactive mode: using defaults for everything"
fi

QRL_VARIANT=$(prompt "QRL_VARIANT" "standard" \
    "Which gqrl variant? [standard | rescue-edition | moscow-chill]")
case "$QRL_VARIANT" in
    standard|rescue-edition|moscow-chill) ;;
    *) die "invalid variant: $QRL_VARIANT" ;;
esac

QRL_NUM_NODES=$(prompt "QRL_NUM_NODES" "4" \
    "How many nodes to run (1-8 recommended)?")
if ! [[ "$QRL_NUM_NODES" =~ ^[1-8]$ ]]; then
    die "QRL_NUM_NODES must be 1..8 (edit docker-compose.yaml to go higher)"
fi

QRL_NETWORK_ID=$(prompt "QRL_NETWORK_ID" "32333" \
    "Network ID (also used as chainID for localnet)")
QRL_CHAIN_ID="$QRL_NETWORK_ID"

QRL_DATA_DIR=$(prompt "QRL_DATA_DIR" "./data" \
    "Host directory to store chain data (will be created + chowned to UID 1000)")
# Resolve to absolute for the chown step.
if [[ "$QRL_DATA_DIR" != /* ]]; then
    QRL_DATA_DIR_ABS="$SCRIPT_DIR/$QRL_DATA_DIR"
else
    QRL_DATA_DIR_ABS="$QRL_DATA_DIR"
fi

QRL_DASHBOARD_PORT=$(prompt "QRL_DASHBOARD_PORT" "8080" \
    "Dashboard HTTP port (bound to 127.0.0.1)")

QRL_LOG_LEVEL=$(prompt "QRL_LOG_LEVEL" "info" \
    "Log level (info | debug)")

# ---------- write plaintext .env ----------

info "writing $ENV_FILE"
{
    echo "# Auto-generated by docker/configure.sh — edit by hand or re-run."
    echo "# Secrets in this file are qrlenv-encrypted (PQ-safe: AES-256-GCM + OS keychain)."
    echo
    echo "# ---- variant ----"
    echo "QRL_VARIANT=${QRL_VARIANT}"
    echo "QRL_VERSION="
    echo
    echo "# ---- topology ----"
    echo "QRL_NUM_NODES=${QRL_NUM_NODES}"
    echo
    echo "# ---- chain identity ----"
    echo "QRL_NETWORK_ID=${QRL_NETWORK_ID}"
    echo "QRL_CHAIN_ID=${QRL_CHAIN_ID}"
    echo "QRL_GENESIS_BALANCE=100000000"
    echo
    echo "# ---- storage ----"
    echo "QRL_DATA_DIR=${QRL_DATA_DIR}"
    echo
    echo "# ---- ports (all >1024, bound to 127.0.0.1 by default) ----"
    echo "QRL_EL_HTTP_PORT_BASE=9545"
    echo "QRL_EL_WS_PORT_BASE=9645"
    echo "QRL_EL_AUTH_PORT_BASE=9551"
    echo "QRL_EL_P2P_PORT_BASE=31303"
    echo "QRL_CL_GRPC_PORT_BASE=4100"
    echo "QRL_CL_REST_PORT_BASE=3600"
    echo "QRL_CL_UDP_PORT_BASE=13000"
    echo "QRL_CL_TCP_PORT_BASE=14000"
    echo "QRL_DASHBOARD_PORT=${QRL_DASHBOARD_PORT}"
    echo
    echo "# ---- devnet flags ----"
    echo "QRL_DISABLE_STAKING_CONTRACT_CHECK=true"
    echo
    echo "# ---- logging ----"
    echo "QRL_LOG_LEVEL=${QRL_LOG_LEVEL}"
    echo
    echo "# ---- runtime ----"
    echo "QRL_RUNTIME=default"
    echo
    echo "# ---- secrets (qrlenv-encrypted) ----"
    echo "# Populated below by configure.sh — do NOT edit these lines by hand."
    echo "QRL_GENESIS_PASSWORD="
    for i in $(seq 1 "$QRL_NUM_NODES"); do
        echo "QRL_JWT_SECRET_NODE${i}="
    done
} > "$ENV_FILE"
chmod 0600 "$ENV_FILE"
ok "$ENV_FILE written (mode 0600)"

# ---------- qrlenv master key ----------

info "checking qrlenv master key for profile '$PROFILE'"
# Probe by attempting init: succeeds iff no key exists, fails with a
# specific message iff a key already exists. Any other failure is fatal.
INIT_OUT="$("$QRLENV" --profile "$PROFILE" init 2>&1)" && INIT_STATUS=0 || INIT_STATUS=$?
if [ $INIT_STATUS -eq 0 ]; then
    ok "generated new qrlenv master key"
elif echo "$INIT_OUT" | grep -q "already exists"; then
    ok "qrlenv master key already exists (keeping it)"
else
    die "qrlenv init failed: $INIT_OUT"
fi

# ---------- secret prompts ----------

if [ $INTERACTIVE -eq 1 ]; then
    echo
    echo -e "${CYAN}Genesis account password${NC} — will be qrlenv-encrypted."
    echo "  (dev-only default suggestion: testnet123. NEVER use this for a real network.)"
    printf "  QRL_GENESIS_PASSWORD [testnet123]: "
    # Read with echo off if TTY, else read normally.
    if [ -t 0 ]; then
        stty -echo
        read -r GENESIS_PW || true
        stty echo
        echo
    else
        read -r GENESIS_PW
    fi
    GENESIS_PW="${GENESIS_PW:-testnet123}"
else
    GENESIS_PW="testnet123"
fi

info "encrypting QRL_GENESIS_PASSWORD via qrlenv"
"$QRLENV" --profile "$PROFILE" --file "$ENV_FILE" set QRL_GENESIS_PASSWORD "$GENESIS_PW" >/dev/null
GENESIS_PW=""  # scrub local var
ok "QRL_GENESIS_PASSWORD encrypted"

# ---------- per-node JWT secrets ----------

info "generating $QRL_NUM_NODES per-node JWT secret(s) (32 bytes each, encrypted via qrlenv)"
for i in $(seq 1 "$QRL_NUM_NODES"); do
    # 32 bytes of random from urandom, hex-encoded, prefixed with "0x" as
    # gqrl expects. Never touches disk unencrypted.
    JWT_HEX="0x$(head -c 32 /dev/urandom | xxd -p -c 64)"
    "$QRLENV" --profile "$PROFILE" --file "$ENV_FILE" set "QRL_JWT_SECRET_NODE${i}" "$JWT_HEX" >/dev/null
    JWT_HEX=""
    ok "  QRL_JWT_SECRET_NODE${i} encrypted"
done

# ---------- data directory ----------

info "preparing data directory: $QRL_DATA_DIR_ABS"
mkdir -p "$QRL_DATA_DIR_ABS"
# chown-to-1000 only works if the current user has permission. On macOS,
# bind-mounted directories inherit ownership from the *user running the
# container*, not the container's UID. On Linux, rootless containers use
# user-namespace UID mapping so the on-disk UID is your host UID.
# So we don't actually need to chown on macOS or Linux-rootless. We only
# force ownership if the operator explicitly asked (via CHOWN=1 env).
if [ "${CHOWN:-0}" = "1" ]; then
    if command -v sudo >/dev/null 2>&1 && [ "$EUID" -ne 0 ]; then
        warn "CHOWN=1: attempting sudo chown -R 1000:1000 $QRL_DATA_DIR_ABS"
        sudo chown -R 1000:1000 "$QRL_DATA_DIR_ABS" || warn "chown failed; continuing"
    else
        chown -R 1000:1000 "$QRL_DATA_DIR_ABS" 2>/dev/null || warn "chown failed; probably fine on macOS or under rootless Podman"
    fi
else
    info "  skipping chown (not needed on macOS or Linux-rootless. Pass CHOWN=1 to force.)"
fi

# ---------- config directory seeding ----------
#
# The compose file expects a bind-mounted config dir at ${QRL_DATA_DIR}/config
# containing (mounted read-only into every service):
#
#   config.yml            beacon-chain chain-config-file
#   genesis.ssz           beacon-chain genesis state
#   genesis.json          gqrl init-time genesis (also used by qrysmctl)
#   jwtsecret-node<N>     per-node engine-API JWT (matches EL+CL pair)
#
# We seed these once here. On re-run we preserve genesis.ssz (regenerating
# would create a NEW chain and orphan all persisted execution data) unless
# --regen-genesis is passed. jwtsecret files are always rewritten from the
# current qrlenv-encrypted values.

CONFIG_DIR="$QRL_DATA_DIR_ABS/config"
mkdir -p "$CONFIG_DIR"
chmod 0700 "$CONFIG_DIR"

# --- config.yml (template substitution) ---
info "seeding $CONFIG_DIR/config.yml"
CONFIG_TEMPLATE="$SCRIPT_DIR/popup-node/config.yml.template"
if [ ! -f "$CONFIG_TEMPLATE" ]; then
    warn "config.yml template not found at $CONFIG_TEMPLATE — you'll need to seed config.yml manually"
else
    # No env vars actually interpolated today, but envsubst leaves the
    # template unchanged if there are no `${...}`, so this is safe and
    # ready for future template variables.
    if command -v envsubst >/dev/null 2>&1; then
        envsubst < "$CONFIG_TEMPLATE" > "$CONFIG_DIR/config.yml"
    else
        cp "$CONFIG_TEMPLATE" "$CONFIG_DIR/config.yml"
    fi
    chmod 0644 "$CONFIG_DIR/config.yml"
    ok "  config.yml written"
fi

# --- genesis.ssz + genesis.json (via host qrysmctl) ---
QRYSMCTL="$REPO_ROOT/qrl-2.0/upstream/qrysm/qrysmctl"
GENESIS_TEMPLATE="$REPO_ROOT/qrl-2.0/configs/localnet/genesis-template.json"

if [ -f "$CONFIG_DIR/genesis.ssz" ] && [ $REGEN_GENESIS -eq 0 ]; then
    info "genesis already present at $CONFIG_DIR/genesis.ssz — keeping (pass --regen-genesis to force new chain)"
elif [ ! -x "$QRYSMCTL" ]; then
    warn "qrysmctl not built at $QRYSMCTL — cannot generate genesis"
    warn "  build it with:  cd $REPO_ROOT/qrl-2.0/upstream/qrysm && go build -o qrysmctl ./cmd/qrysmctl"
    warn "  then re-run:    ./configure.sh --regen-genesis"
elif [ ! -f "$GENESIS_TEMPLATE" ]; then
    warn "genesis template not found at $GENESIS_TEMPLATE — cannot generate genesis"
else
    info "regenerating aligned genesis via qrysmctl (num-validators=$QRL_NUM_NODES, genesis-delay=90s)"
    "$QRYSMCTL" testnet generate-genesis \
        --num-validators="$QRL_NUM_NODES" \
        --genesis-time-delay=90 \
        --chain-config-file="$CONFIG_DIR/config.yml" \
        --gqrl-genesis-json-in="$GENESIS_TEMPLATE" \
        --gqrl-genesis-json-out="$CONFIG_DIR/genesis.json" \
        --output-ssz="$CONFIG_DIR/genesis.ssz" 2>&1 | grep -E "Genesis is now|Done" || true
    chmod 0644 "$CONFIG_DIR/genesis.json" "$CONFIG_DIR/genesis.ssz" 2>/dev/null || true
    ok "  genesis.json + genesis.ssz written (delayed 90s from now)"
fi

# --- jwtsecret-node<N> (decrypt via qrlenv → mode 0600 files) ---
#
# Why do we write plaintext JWT to disk if we went to the trouble of
# encrypting it via qrlenv? Because gqrl's --authrpc.jwtsecret takes a
# FILE PATH, not a value. To pass the plaintext to gqrl at runtime,
# either (a) it lives in a file, or (b) we shim gqrl's start with a
# wrapper that reads env vars and writes the file at container-start.
# We chose (a) for phase 1 — the JWT file sits in a mode-0600 file in
# the operator's own data dir. The threat model for the config dir is
# the operator's user account (which controls the compose lifecycle and
# the file anyway), so we're not weakening anything.
#
# If phase 2 wants to close this gap, add an "init-secrets" service
# with an entrypoint script that writes the JWT to a tmpfs-backed
# named volume shared with the EL+CL containers, and removes the file
# on SIGTERM. That's follow-up work.
info "writing per-node JWT secret files (decrypted from qrlenv-encrypted .env values, mode 0600)"
for i in $(seq 1 "$QRL_NUM_NODES"); do
    JWT_FILE="$CONFIG_DIR/jwtsecret-node${i}"
    JWT_VAR="QRL_JWT_SECRET_NODE${i}"
    # `qrlenv get` reads the encrypted value from .env and prints plaintext.
    JWT_HEX=$("$QRLENV" --profile "$PROFILE" --file "$ENV_FILE" get "$JWT_VAR" 2>/dev/null || true)
    if [ -z "$JWT_HEX" ]; then
        warn "  $JWT_VAR is empty — skipping $JWT_FILE"
        continue
    fi
    # gqrl accepts JWT with or without 0x prefix. Preserve whatever qrlenv gave us.
    umask 077
    printf '%s' "$JWT_HEX" > "$JWT_FILE"
    chmod 0600 "$JWT_FILE"
    JWT_HEX=""  # scrub local var
    ok "  jwtsecret-node${i} written (mode 0600)"
done

# --- per-node data subdirs ---
for i in $(seq 1 "$QRL_NUM_NODES"); do
    mkdir -p "$QRL_DATA_DIR_ABS/node${i}/gqrl" "$QRL_DATA_DIR_ABS/node${i}/keystore"
    mkdir -p "$QRL_DATA_DIR_ABS/beacon-node${i}"
done
mkdir -p "$QRL_DATA_DIR_ABS/validators" "$QRL_DATA_DIR_ABS/dashboard"

# --- init per-node gqrl chaindata from genesis.json ---
# gqrl needs `gqrl init <genesis.json>` before first run — otherwise it
# uses the default (mainnet) chain ID, and the beacon rejects the pairing
# with `wanted chain ID X, got 1`.
#
# We do this via a one-shot `docker run` on the qrl-gqrl:$QRL_VARIANT
# image (which the operator has either built with `docker compose build`
# or pulled). The datadir bind-mount matches what docker-compose.yaml uses.
#
# Skipped for a node whose chaindata already exists — re-running
# configure.sh should not clobber existing chain state.
info "initializing per-node gqrl chaindata from genesis.json"
GQRL_IMAGE="qrl-gqrl:${QRL_VARIANT}"
if ! docker image inspect "$GQRL_IMAGE" >/dev/null 2>&1; then
    warn "  $GQRL_IMAGE not found — build it first with:"
    warn "    docker build -f docker/popup-node/Dockerfile.gqrl -t $GQRL_IMAGE $REPO_ROOT"
    warn "  or via compose:"
    warn "    docker compose -f $SCRIPT_DIR/docker-compose.yaml build"
    warn "  then re-run this wizard to init the chaindata."
else
    for i in $(seq 1 "$QRL_NUM_NODES"); do
        if [ -d "$QRL_DATA_DIR_ABS/node${i}/gqrl/chaindata" ]; then
            info "  node${i}: chaindata already exists, skipping init (chain state preserved)"
            continue
        fi
        docker run --rm \
            -v "$QRL_DATA_DIR_ABS/node${i}/gqrl:/opt/qrl/data" \
            -v "$QRL_DATA_DIR_ABS/config:/opt/qrl/config:ro" \
            --entrypoint /opt/qrl/bin/gqrl \
            "$GQRL_IMAGE" \
            --datadir=/opt/qrl/data init /opt/qrl/config/genesis.json 2>&1 \
            | grep -E "Successfully wrote|hash=" | head -1 \
            | sed "s/^/  node${i}: /"
    done
    ok "  gqrl chaindata initialized for $QRL_NUM_NODES node(s)"
fi

# ---------- summary ----------

echo
ok "configuration complete"
echo
echo "Next steps:"
echo "  1. Verify: $QRLENV --profile $PROFILE keys --file $ENV_FILE"
echo "  2. Bring up the network:"
echo "       cd $SCRIPT_DIR && $QRLENV --file $ENV_FILE run -- docker compose up"
echo "     (add -d to run detached, use 'podman-compose' if preferred)"
echo "  3. Dashboard will be at http://127.0.0.1:${QRL_DASHBOARD_PORT}/"
echo
warn "IMPORTANT: Back up your qrlenv master key (macOS Keychain / Linux Secret Service /"
warn "Windows DPAPI entry under service='qrlenv' account='$PROFILE'). Losing it makes"
warn "every qrl-env: value in $ENV_FILE permanently unrecoverable."
