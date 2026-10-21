#!/bin/bash
#
# chain-snapshot.sh — named snapshot/restore for QRL 2.0 localnet chain state.
#
# Each snapshot is a full point-in-time capture of every stateful directory
# that makes the chain "the same chain":
#
#   node{1..4}/gqrl/          — execution layer chaindata + P2P nodekey
#   node{1..4}/keystore/      — funded genesis + dev accounts
#   beacon{,-node{2,3,4}}/    — beacon-chain DB, network keys, boot key
#   validators/               — validator DB + boot data
#   config/                   — genesis.json, genesis.ssz, config.yml, .env
#
# The .env is included because it pins the variant (standard vs rescue-edition);
# restoring a snapshot recovers the same variant that produced the state.
#
# Usage:
#   chain-snapshot.sh [--layout bare-metal|compose] save    <name> [--note "text"] [--force]
#   chain-snapshot.sh [--layout bare-metal|compose] restore <name> [--yes]
#   chain-snapshot.sh list
#   chain-snapshot.sh delete  <name> [--yes]
#   chain-snapshot.sh diff    <a> <b>
#   chain-snapshot.sh current
#
# Layout selection (in order of precedence):
#   1. --layout compose|bare-metal   flag
#   2. QRL_LAYOUT env var
#   3. Auto-detect: `compose` if nodes/docker/data/config/genesis.json
#      exists AND bare-metal genesis is absent; else `bare-metal`.
#
# Snapshot format is canonical regardless of source: node1's beacon
# always lives under `beacon/` in the snapshot dir. On restore, it's
# rewritten to `beacon/` (bare-metal) or `beacon-node1/` (compose) per
# target layout. Same snapshot restores cleanly onto either target.
#
# Safety:
#   - save refuses to overwrite an existing snapshot unless --force is passed.
#   - restore refuses to run unless the chain is stopped (no gqrl / beacon-chain
#     PIDs matching the localnet paths). Pass --yes to skip the confirmation.
#   - delete asks for confirmation unless --yes is passed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QRL_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"                   # qrl-2.0/
REPO_ROOT="$(cd "$QRL_ROOT/.." && pwd)"                    # nodes/

# ---------- layout: bare-metal vs docker-compose ----------
#
# Snapshots are canonical (bare-metal-shaped) on disk so a snapshot
# saved from a compose bring-up restores cleanly onto bare-metal (and
# vice versa). Only two things differ between layouts:
#
#   Bare-metal:   qrl-2.0/chains/localnet/{node{i}/gqrl, beacon,       beacon-node{2..4}, validators}
#                 qrl-2.0/configs/localnet/{config.yml, genesis.*, .env}
#   Compose:      nodes/docker/data/{node{i}/gqrl, beacon-node{1..4}, validators}
#                 nodes/docker/data/config/{config.yml, genesis.*}  +  nodes/docker/.env
#
# Node1's beacon is the interesting mismatch: `beacon` bare-metal,
# `beacon-node1` in compose. We normalize to `beacon` in the snapshot
# regardless of source, and denormalize on restore per target.
#
# Selection:
#   --layout bare-metal   (default) — traditional dirs
#   --layout compose      — nodes/docker/data
#   env  QRL_LAYOUT=compose|bare-metal
#
# Overriding manually:
#   QRL_CHAINS_DIR=/path  QRL_CONFIG_DIR=/path  QRL_ENV_FILE=/path
#   These take precedence over --layout.

detect_layout() {
    if [ -n "${QRL_LAYOUT:-}" ]; then
        echo "$QRL_LAYOUT"; return
    fi
    # Prefer compose if its data dir exists AND bare-metal is absent.
    # If both exist, default to bare-metal so pre-existing invocations
    # don't change semantics.
    if [ -d "$REPO_ROOT/docker/data/config" ] && [ -f "$REPO_ROOT/docker/data/config/genesis.json" ]; then
        if [ ! -f "$QRL_ROOT/configs/localnet/genesis.json" ]; then
            echo "compose"; return
        fi
    fi
    echo "bare-metal"
}

# Resolve LAYOUT from the current global (may have been set by top-level
# --layout flag parsing below) or fall back to auto-detect / QRL_LAYOUT.
resolve_layout_paths() {
    : "${LAYOUT:=$(detect_layout)}"
    case "$LAYOUT" in
        bare-metal)
            CHAINS_DIR="${QRL_CHAINS_DIR:-$QRL_ROOT/chains/localnet}"
            CONFIG_DIR="${QRL_CONFIG_DIR:-$QRL_ROOT/configs/localnet}"
            ENV_FILE="${QRL_ENV_FILE:-$CONFIG_DIR/.env}"
            NODE1_BEACON_DIR="beacon"
            ;;
        compose)
            CHAINS_DIR="${QRL_CHAINS_DIR:-$REPO_ROOT/docker/data}"
            CONFIG_DIR="${QRL_CONFIG_DIR:-$REPO_ROOT/docker/data/config}"
            ENV_FILE="${QRL_ENV_FILE:-$REPO_ROOT/docker/.env}"
            NODE1_BEACON_DIR="beacon-node1"
            ;;
        *)
            echo "unknown layout: $LAYOUT (use bare-metal or compose)" >&2
            exit 1
            ;;
    esac
    SNAP_ROOT="${QRL_SNAP_ROOT:-$CHAINS_DIR/.snapshots}"
}

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'

die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

usage() { sed -n '3,26p' "$0" | sed 's/^# \{0,1\}//'; exit 1; }

# ---------- helpers ----------

chain_is_running() {
    # Bare-metal: match any process running under this chains/localnet tree.
    # Compose: match any running qrl-popup-* container.
    # Returns 0 if something is alive, 1 if fully stopped.
    if [ "$LAYOUT" = "compose" ]; then
        if command -v docker >/dev/null 2>&1; then
            docker ps --filter label=com.docker.compose.project=qrl-popup --format '{{.Names}}' 2>/dev/null | grep -q '^qrl-' && return 0
        fi
        return 1
    fi
    if pgrep -f "gqrl.*$CHAINS_DIR" >/dev/null; then return 0; fi
    if pgrep -f "beacon-chain.*$CHAINS_DIR" >/dev/null; then return 0; fi
    if pgrep -f "validator.*$CHAINS_DIR" >/dev/null; then return 0; fi
    return 1
}

current_variant() {
    # Bare-metal .env sets GQRL_VARIANT / GZOND_VARIANT; compose .env
    # uses QRL_VARIANT. Prefer whichever exists in whichever file exists.
    if [ -f "$ENV_FILE" ]; then
        # shellcheck disable=SC1091
        (source "$ENV_FILE" 2>/dev/null; echo "${QRL_VARIANT:-${GQRL_VARIANT:-${GZOND_VARIANT:-standard}}}")
    else
        echo "standard"
    fi
}

# Best-effort: hit node1 for its current block height. Empty if unreachable.
# Guarded against pipefail — grep returning "no match" would abort the whole
# script under `set -euo pipefail` (from line 30). The `|| true` on each
# stage keeps the pipeline from failing when the chain isn't running.
current_block_hex() {
    local out
    out=$(/usr/bin/curl -s --max-time 2 -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"qrl_blockNumber","params":[],"id":1}' \
        http://127.0.0.1:9545 2>/dev/null || true)
    { echo "$out" | grep -oE '"result":"[^"]*"' || true; } \
        | { head -1 || true; } \
        | { sed 's/"result":"//;s/"//' || true; }
}

require_stopped() {
    if chain_is_running; then
        warn "Chain is running. Stop it first (kill gqrl / beacon-chain / validator processes)."
        exit 1
    fi
}

# ---------- commands ----------

cmd_save() {
    local name="${1:-}"; shift || true
    [ -z "$name" ] && die "usage: save <name> [--note ...] [--force]"
    local note="" force=0
    while [ $# -gt 0 ]; do
        case "$1" in
            --note) note="${2:-}"; shift 2 ;;
            --force) force=1; shift ;;
            *) die "unknown flag: $1" ;;
        esac
    done

    local dst="$SNAP_ROOT/$name"
    if [ -d "$dst" ]; then
        [ $force -eq 0 ] && die "snapshot '$name' already exists (use --force to overwrite)"
        warn "overwriting existing snapshot: $name"
        rm -rf "$dst"
    fi

    if chain_is_running; then
        warn "chain is running — snapshot may capture mid-write state"
        warn "recommend: stop chain first for a consistent snapshot"
    fi

    local variant block_hex
    variant="$(current_variant)"
    block_hex="$(current_block_hex)"

    info "saving snapshot: $name (variant=$variant, block=$block_hex)"
    mkdir -p "$dst"

    # config first — cheap, and if it fails we abort before slower copies
    mkdir -p "$dst/config"
    for f in genesis.json genesis.ssz config.yml; do
        [ -f "$CONFIG_DIR/$f" ] && cp -p "$CONFIG_DIR/$f" "$dst/config/"
    done
    # .env may live in $CONFIG_DIR (bare-metal) or elsewhere (compose puts
    # it in docker/.env, separate from the runtime config dir). Copy from
    # whichever ENV_FILE resolved to.
    if [ -f "$ENV_FILE" ]; then
        cp -p "$ENV_FILE" "$dst/config/.env"
    fi
    ok "config saved (genesis + variant .env)"

    # execution-layer chaindata + keystores per node
    for i in 1 2 3 4; do
        [ -d "$CHAINS_DIR/node$i/gqrl" ] || continue
        info "copying node$i EL chaindata..."
        mkdir -p "$dst/node$i"
        cp -R "$CHAINS_DIR/node$i/gqrl" "$dst/node$i/"
        [ -d "$CHAINS_DIR/node$i/keystore" ] && \
            cp -R "$CHAINS_DIR/node$i/keystore" "$dst/node$i/"
    done
    ok "EL chaindata saved for nodes 1-4"

    # Beacon (primary + followers). Normalize node1's beacon dir to
    # `beacon` in the snapshot regardless of source layout so restore
    # can retarget either layout.
    # Source pairs: (dir_in_source, dir_in_snapshot)
    local -a beacon_pairs
    beacon_pairs=(
        "$NODE1_BEACON_DIR:beacon"
        "beacon-node2:beacon-node2"
        "beacon-node3:beacon-node3"
        "beacon-node4:beacon-node4"
    )
    for pair in "${beacon_pairs[@]}"; do
        local src_name="${pair%:*}" dst_name="${pair#*:}"
        [ -d "$CHAINS_DIR/$src_name" ] || continue
        info "copying $src_name (→ snapshot as $dst_name)..."
        mkdir -p "$dst/$dst_name"
        for sub in beaconchaindata network-keys metaData boot.key tosaccepted; do
            [ -e "$CHAINS_DIR/$src_name/$sub" ] && cp -Rp "$CHAINS_DIR/$src_name/$sub" "$dst/$dst_name/" 2>/dev/null || true
        done
    done
    ok "beacon data saved (node1 normalized to 'beacon' in snapshot)"

    # validators
    if [ -d "$CHAINS_DIR/validators" ]; then
        info "copying validators..."
        mkdir -p "$dst/validators"
        for sub in validator.db tosaccepted; do
            [ -e "$CHAINS_DIR/validators/$sub" ] && cp -Rp "$CHAINS_DIR/validators/$sub" "$dst/validators/" 2>/dev/null || true
        done
        ok "validators saved"
    fi

    # keystores directory at top level (if used)
    if [ -d "$CHAINS_DIR/keystores" ]; then
        cp -Rp "$CHAINS_DIR/keystores" "$dst/"
        ok "top-level keystores saved"
    fi

    # manifest — everything a human or diff needs to reason about the snapshot
    local timestamp
    timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    local size_bytes
    size_bytes="$(du -sk "$dst" | awk '{print $1 * 1024}')"

    cat > "$dst/manifest.json" <<JSON
{
  "name": "$name",
  "variant": "$variant",
  "block_hex": "$block_hex",
  "created_utc": "$timestamp",
  "size_bytes": $size_bytes,
  "source_layout": "$LAYOUT",
  "note": $(printf '%s' "$note" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')
}
JSON

    ok "snapshot '$name' complete: $(du -sh "$dst" | awk '{print $1}')"
}

cmd_restore() {
    local name="${1:-}"; shift || true
    [ -z "$name" ] && die "usage: restore <name> [--yes]"
    local yes=0
    while [ $# -gt 0 ]; do
        case "$1" in
            --yes|-y) yes=1; shift ;;
            *) die "unknown flag: $1" ;;
        esac
    done

    local src="$SNAP_ROOT/$name"
    [ -d "$src" ] || die "snapshot '$name' not found"
    require_stopped

    local variant block
    variant=$(python3 -c "import json; print(json.load(open('$src/manifest.json'))['variant'])" 2>/dev/null || echo "?")
    block=$(python3 -c "import json; print(json.load(open('$src/manifest.json'))['block_hex'])" 2>/dev/null || echo "?")

    warn "about to restore snapshot '$name' (variant=$variant, block=$block)"
    warn "this WILL WIPE current chain state at $CHAINS_DIR"
    if [ $yes -eq 0 ]; then
        printf "type YES to proceed: "
        read -r reply
        [ "$reply" = "YES" ] || { info "aborted"; exit 0; }
    fi

    # Wipe every stateful directory in current localnet. Uses the
    # layout-aware NODE1_BEACON_DIR so we clean the right node1 beacon
    # regardless of target layout.
    for i in 1 2 3 4; do
        rm -rf "$CHAINS_DIR/node$i/gqrl"
    done
    for b in "$NODE1_BEACON_DIR" beacon-node2 beacon-node3 beacon-node4; do
        rm -rf "$CHAINS_DIR/$b/beaconchaindata" \
               "$CHAINS_DIR/$b/network-keys" \
               "$CHAINS_DIR/$b/metaData" \
               "$CHAINS_DIR/$b/boot.key"
    done
    rm -rf "$CHAINS_DIR/validators/validator.db" \
           "$CHAINS_DIR/validators/tosaccepted"
    ok "current state wiped"

    # Restore config. Handles both directions: bare-metal .env writes to
    # CONFIG_DIR/.env, compose .env writes to ENV_FILE (which lives
    # elsewhere).
    if [ -d "$src/config" ]; then
        mkdir -p "$CONFIG_DIR"
        for f in "$src/config"/*; do
            local base; base="$(basename "$f")"
            case "$base" in
                .env)
                    mkdir -p "$(dirname "$ENV_FILE")"
                    cp -p "$f" "$ENV_FILE"
                    ;;
                *)
                    cp -p "$f" "$CONFIG_DIR/$base"
                    ;;
            esac
        done
        ok "config restored (genesis + .env)"
    fi

    # Restore per-node EL
    for i in 1 2 3 4; do
        [ -d "$src/node$i" ] || continue
        mkdir -p "$CHAINS_DIR/node$i"
        [ -d "$src/node$i/gqrl" ] && cp -Rp "$src/node$i/gqrl" "$CHAINS_DIR/node$i/"
        [ -d "$src/node$i/keystore" ] && cp -Rp "$src/node$i/keystore" "$CHAINS_DIR/node$i/"
    done
    ok "EL chaindata restored"

    # Restore beacons. Snapshot's `beacon` (canonical node1) is
    # denormalized to the target layout's node1 dir name.
    # Pairs: (dir_in_snapshot, dir_in_target)
    local -a beacon_pairs
    beacon_pairs=(
        "beacon:$NODE1_BEACON_DIR"
        "beacon-node2:beacon-node2"
        "beacon-node3:beacon-node3"
        "beacon-node4:beacon-node4"
    )
    for pair in "${beacon_pairs[@]}"; do
        local src_name="${pair%:*}" dst_name="${pair#*:}"
        [ -d "$src/$src_name" ] || continue
        mkdir -p "$CHAINS_DIR/$dst_name"
        for sub in beaconchaindata network-keys metaData boot.key tosaccepted; do
            [ -e "$src/$src_name/$sub" ] && cp -Rp "$src/$src_name/$sub" "$CHAINS_DIR/$dst_name/" 2>/dev/null || true
        done
    done
    ok "beacon data restored (snapshot 'beacon' → target '$NODE1_BEACON_DIR')"

    # Validators
    if [ -d "$src/validators" ]; then
        mkdir -p "$CHAINS_DIR/validators"
        for sub in validator.db tosaccepted; do
            [ -e "$src/validators/$sub" ] && cp -Rp "$src/validators/$sub" "$CHAINS_DIR/validators/" 2>/dev/null || true
        done
    fi

    # Verify variant binary exists — warn if not.
    local target="$QRL_ROOT/upstream/go-qrl/build/bin/gqrl"
    if [ "$variant" = "rescue-edition" ]; then
        target="$QRL_ROOT/variants/rescue-edition/build/bin/gqrl"
    fi
    if [ ! -x "$target" ]; then
        warn "variant binary MISSING: $target"
        warn "  build with: cd $(dirname "$(dirname "$target")") && go build -buildvcs=false -o build/bin/gqrl ./cmd/gqrl"
        warn "  or (for rescue-edition): bash $QRL_ROOT/mods/rescue-prime/install.sh"
    else
        ok "variant binary present: $target"
    fi

    ok "restore complete — run start-nodes.sh + start-beacon.sh to bring the chain up"
}

cmd_list() {
    # --json emits a machine-readable array for programmatic consumers
    # (the dashboard UI, CI, etc.).
    local json=0
    if [ "${1:-}" = "--json" ]; then json=1; fi

    if [ ! -d "$SNAP_ROOT" ]; then
        [ $json -eq 1 ] && echo "[]" || echo "no snapshots"
        return 0
    fi

    if [ $json -eq 1 ]; then
        # Collect manifest paths + sizes, hand off to python for JSON assembly.
        python3 - "$SNAP_ROOT" <<'PY'
import json, os, sys, subprocess
root = sys.argv[1]
out = []
for name in sorted(os.listdir(root)):
    d = os.path.join(root, name)
    if not os.path.isdir(d):
        continue
    mf = os.path.join(d, "manifest.json")
    entry = {"name": name}
    if os.path.isfile(mf):
        try:
            with open(mf) as f:
                entry.update(json.load(f))
        except (OSError, json.JSONDecodeError):
            entry["error"] = "manifest read failed"
    # Size in bytes for the UI (formats itself); manifest may already carry it
    # but this is the ground truth.
    entry["size_bytes"] = int(subprocess.check_output(
        ["du", "-sk", d]).split()[0]) * 1024
    out.append(entry)
print(json.dumps(out))
PY
        return 0
    fi

    printf "%-20s %-16s %-10s %-20s %s\n" "NAME" "VARIANT" "BLOCK" "CREATED (UTC)" "SIZE"
    printf "%-20s %-16s %-10s %-20s %s\n" "----" "-------" "-----" "-------------" "----"
    for dir in "$SNAP_ROOT"/*/; do
        [ -d "$dir" ] || continue
        local name; name="$(basename "$dir")"
        local mf="$dir/manifest.json"
        local variant="?" block="?" created="?" size="?"
        if [ -f "$mf" ]; then
            variant=$(python3 -c "import json; print(json.load(open('$mf')).get('variant','?'))")
            block=$(python3 -c "import json; print(json.load(open('$mf')).get('block_hex','?'))")
            created=$(python3 -c "import json; print(json.load(open('$mf')).get('created_utc','?'))")
        fi
        size=$(du -sh "$dir" 2>/dev/null | awk '{print $1}')
        printf "%-20s %-16s %-10s %-20s %s\n" "$name" "$variant" "$block" "$created" "$size"
    done
}

cmd_delete() {
    local name="${1:-}"; shift || true
    [ -z "$name" ] && die "usage: delete <name> [--yes]"
    local yes=0
    while [ $# -gt 0 ]; do
        case "$1" in --yes|-y) yes=1; shift ;; *) die "unknown flag: $1" ;; esac
    done
    local dst="$SNAP_ROOT/$name"
    [ -d "$dst" ] || die "snapshot '$name' not found"
    if [ $yes -eq 0 ]; then
        printf "delete snapshot '$name' ($(du -sh "$dst" | awk '{print $1}'))? type YES: "
        read -r reply
        [ "$reply" = "YES" ] || { info "aborted"; exit 0; }
    fi
    rm -rf "$dst"
    ok "deleted: $name"
}

cmd_diff() {
    local a="${1:-}" b="${2:-}"
    [ -z "$a" ] || [ -z "$b" ] && die "usage: diff <a> <b>"
    local A="$SNAP_ROOT/$a" B="$SNAP_ROOT/$b"
    [ -d "$A" ] || die "no such snapshot: $a"
    [ -d "$B" ] || die "no such snapshot: $b"

    echo "=== $a vs $b ==="
    python3 - "$A/manifest.json" "$B/manifest.json" <<'PY'
import json, sys
a = json.load(open(sys.argv[1]))
b = json.load(open(sys.argv[2]))
keys = ["variant", "block_hex", "created_utc", "size_bytes", "note"]
for k in keys:
    va, vb = a.get(k), b.get(k)
    mark = "  " if va == vb else "!="
    print(f"{mark} {k:12s}  {va}  |  {vb}")
PY
    # Compare genesis hashes byte-for-byte
    if [ -f "$A/config/genesis.json" ] && [ -f "$B/config/genesis.json" ]; then
        local h1 h2
        h1=$(shasum -a 256 "$A/config/genesis.json" | awk '{print $1}')
        h2=$(shasum -a 256 "$B/config/genesis.json" | awk '{print $1}')
        if [ "$h1" = "$h2" ]; then
            echo "   genesis.json  IDENTICAL ($h1)"
        else
            echo "!= genesis.json  differ ($h1 vs $h2)"
        fi
    fi
}

cmd_current() {
    if chain_is_running; then
        echo "state:    running"
    else
        echo "state:    stopped"
    fi
    echo "layout:   $LAYOUT"
    echo "variant:  $(current_variant)"
    local blk; blk=$(current_block_hex)
    echo "block:    ${blk:-<unreachable>}"
    echo "chains:   $CHAINS_DIR"
    echo "config:   $CONFIG_DIR"
    echo "env:      $ENV_FILE"
    echo "snap dir: $SNAP_ROOT"
    if [ -d "$SNAP_ROOT" ]; then
        echo "saved:    $(find "$SNAP_ROOT" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ') snapshots"
    fi
}

# ---------- dispatcher ----------

# Strip --layout=... from anywhere in argv (before or after subcommand)
# so subcommand arg parsers don't have to know about it.
NEWARGS=()
while [ $# -gt 0 ]; do
    case "$1" in
        --layout) LAYOUT="${2:-}"; shift 2 ;;
        --layout=*) LAYOUT="${1#--layout=}"; shift ;;
        *) NEWARGS+=("$1"); shift ;;
    esac
done
set -- "${NEWARGS[@]}"

resolve_layout_paths

sub="${1:-}"; shift || true
case "$sub" in
    save)    cmd_save    "$@" ;;
    restore) cmd_restore "$@" ;;
    list)    cmd_list    "$@" ;;
    delete|rm) cmd_delete  "$@" ;;
    diff)    cmd_diff    "$@" ;;
    current) cmd_current "$@" ;;
    ""|-h|--help|help) usage ;;
    *) die "unknown subcommand: $sub (try --help)" ;;
esac
