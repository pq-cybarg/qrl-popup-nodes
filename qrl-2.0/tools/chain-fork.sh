#!/bin/bash
#
# chain-fork.sh — clone an existing snapshot into a new snapshot that carries
# the same chain history but is pinned to a different variant.
#
# Why this exists: the user has an existing live chain (say "standard" variant,
# 1000 blocks in) and wants a test copy that runs under a modded variant
# (rescue-edition) starting from that same state. This tool creates a new
# named snapshot they can restore into localnet whenever they want to poke at
# the modded version without disturbing the original chain state.
#
# The forked snapshot inherits:
#   - all node{1..4}/gqrl/ EL chaindata      (same blocks, same accounts)
#   - all beacon{,-node*}/  beacon state     (same slots, same finalization)
#   - all validators/ data                    (same keys)
#   - the same genesis.json / genesis.ssz    (must match for state continuity)
#
# The forked snapshot changes:
#   - configs/localnet/.env pinned to the new variant
#   - manifest fields (name, note, variant marker)
#
# Compatibility (fork direction matters!):
#   standard → rescue-edition   ALWAYS SAFE. rescue-edition is a strict
#                               superset — adds precompiles at addresses no
#                               historical block could have called.
#
#   rescue-edition → standard   SAFE ONLY if no historical block invoked a
#                               rescue-edition-only precompile (0x20-0x22).
#                               If any did, the standard chain will get an
#                               empty return where the rescue chain got real
#                               output, forking state divergence. Tool warns
#                               but does not block — dev choice.
#
# Usage:
#   chain-fork.sh <src-snapshot> <new-name> --to-variant <target> [--note "..."] [--force]
#
# Then to actually test the fork:
#   chain-snapshot.sh restore <new-name>
#   ./configs/localnet/start-nodes.sh   # will use target variant

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QRL_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"                   # qrl-2.0/
CHAINS_DIR="$QRL_ROOT/chains/localnet"
SNAP_ROOT="$CHAINS_DIR/.snapshots"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

usage() { sed -n '3,44p' "$0" | sed 's/^# \{0,1\}//'; exit 1; }

# ---------- arg parsing ----------

[ $# -lt 4 ] && usage

SRC="$1"; NEW="$2"; shift 2
TARGET_VARIANT=""; NOTE=""; FORCE=0
while [ $# -gt 0 ]; do
    case "$1" in
        --to-variant) TARGET_VARIANT="${2:-}"; shift 2 ;;
        --note)       NOTE="${2:-}"; shift 2 ;;
        --force)      FORCE=1; shift ;;
        *) die "unknown flag: $1" ;;
    esac
done

[ -z "$TARGET_VARIANT" ] && die "--to-variant <standard|rescue-edition> is required"

# ---------- validate ----------

SRC_DIR="$SNAP_ROOT/$SRC"
NEW_DIR="$SNAP_ROOT/$NEW"
[ -d "$SRC_DIR" ] || die "source snapshot '$SRC' not found"
[ -f "$SRC_DIR/manifest.json" ] || die "source snapshot has no manifest — corrupt?"

if [ -d "$NEW_DIR" ]; then
    [ $FORCE -eq 0 ] && die "new snapshot '$NEW' already exists (use --force to overwrite)"
    warn "overwriting existing snapshot: $NEW"
    rm -rf "$NEW_DIR"
fi

SRC_VARIANT=$(python3 -c "import json; print(json.load(open('$SRC_DIR/manifest.json'))['variant'])")
SRC_BLOCK=$(python3 -c "import json; print(json.load(open('$SRC_DIR/manifest.json'))['block_hex'])")

info "forking: $SRC ($SRC_VARIANT @ $SRC_BLOCK) → $NEW ($TARGET_VARIANT)"

# ---------- compat warnings ----------

#
# Variant containment (as of 2026-08-25):
#   standard        ⊂ moscow-chill ⊂ rescue-edition
#   standard        : base precompiles only
#   moscow-chill    : + 0x03 SHAKE256, + 0x06 ML-DSA-87 detached verify
#   rescue-edition  : + moscow-chill's 0x03/0x06
#                     + 0x20 Rescue Prime, + 0x21 STARK, + 0x22 verify-with-addr
#
# Superset transitions (SRC ⊂ TARGET): always safe — TARGET has strictly more
# precompiles at previously-empty slots, so historical blocks (which couldn't
# have called the new slots) replay identically.
#
# Subset transitions (SRC ⊃ TARGET): safe ONLY if no historical block called
# the disappearing slots. Historical calls that hit a removed precompile now
# see empty code (returns nil), which diverges state.
#
case "$SRC_VARIANT:$TARGET_VARIANT" in
    "standard:moscow-chill"|"standard:rescue-edition"|"moscow-chill:rescue-edition")
        ok "fork direction is a superset — safe"
        ;;
    "rescue-edition:moscow-chill")
        warn "fork direction DROPS zRonin features — precompiles at 0x20/0x21/0x22 disappear"
        warn "if any historical block called them, state will diverge on restart"
        warn "proceed only if you know the source chain never touched those addresses"
        ;;
    "rescue-edition:standard")
        warn "fork direction DROPS moscow-chill + zRonin features"
        warn "  moscow-chill lost: 0x03 SHAKE256, 0x06 ML-DSA-87 verify"
        warn "  zRonin lost:       0x20 Rescue Prime, 0x21 STARK, 0x22 verify-with-addr"
        warn "if any historical block called any of those, state will diverge"
        ;;
    "moscow-chill:standard")
        warn "fork direction DROPS moscow-chill features — precompiles at 0x03/0x06 disappear"
        warn "if any historical block called them, state will diverge on restart"
        warn "proceed only if you know the source chain never touched those addresses"
        ;;
    "$SRC_VARIANT:$SRC_VARIANT")
        info "same variant on both sides — this is a plain clone"
        ;;
    *)
        warn "unknown variant transition — proceed at your own risk"
        ;;
esac

# ---------- verify target variant binary exists ----------

TARGET_BIN="$QRL_ROOT/upstream/go-qrl/build/bin/gqrl"
if [ "$TARGET_VARIANT" = "rescue-edition" ]; then
    TARGET_BIN="$QRL_ROOT/variants/rescue-edition/build/bin/gqrl"
fi
if [ ! -x "$TARGET_BIN" ]; then
    warn "target variant binary MISSING at $TARGET_BIN"
    warn "the fork snapshot will still be created, but restore will fail until you build it:"
    if [ "$TARGET_VARIANT" = "rescue-edition" ]; then
        warn "  bash $QRL_ROOT/mods/rescue-prime/install.sh"
    else
        warn "  cd $QRL_ROOT/upstream/go-qrl && go build -buildvcs=false -o build/bin/gqrl ./cmd/gqrl"
    fi
fi

# ---------- clone ----------

info "cloning snapshot files..."
cp -R "$SRC_DIR" "$NEW_DIR"
ok "cloned: $(du -sh "$NEW_DIR" | awk '{print $1}')"

# Switch the .env in the cloned config to the target variant. The rest of the
# state (chaindata, beacon state, keystores) is byte-identical to the source.
if [ -f "$NEW_DIR/config/.env" ]; then
    if grep -q "^GQRL_VARIANT=" "$NEW_DIR/config/.env"; then
        # macOS sed needs '' after -i; portable via a temp file
        tmpf=$(mktemp)
        sed "s|^GQRL_VARIANT=.*|GQRL_VARIANT=\"$TARGET_VARIANT\"|" "$NEW_DIR/config/.env" > "$tmpf"
        mv "$tmpf" "$NEW_DIR/config/.env"
        ok ".env variant pinned to '$TARGET_VARIANT'"
    else
        echo "GQRL_VARIANT=\"$TARGET_VARIANT\"" >> "$NEW_DIR/config/.env"
        ok "GQRL_VARIANT added to .env: '$TARGET_VARIANT'"
    fi
else
    warn "no .env in source config — creating one for target variant"
    mkdir -p "$NEW_DIR/config"
    cat > "$NEW_DIR/config/.env" <<EOF
GQRL_VARIANT="$TARGET_VARIANT"
NETWORK_ID=32333
CHAIN_ID=32333
EOF
fi

# Rebuild manifest — same block height (state is identical) but new variant + name.
TIMESTAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
SIZE_BYTES=$(du -sk "$NEW_DIR" | awk '{print $1 * 1024}')
FORK_NOTE=$(printf 'forked from %s (%s @ %s). %s' "$SRC" "$SRC_VARIANT" "$SRC_BLOCK" "$NOTE")

python3 - "$NEW_DIR/manifest.json" "$NEW" "$TARGET_VARIANT" "$SRC_BLOCK" "$TIMESTAMP" "$SIZE_BYTES" "$FORK_NOTE" <<'PY'
import json, sys
mf, name, variant, block, ts, size, note = sys.argv[1:]
data = {
    "name": name,
    "variant": variant,
    "block_hex": block,
    "created_utc": ts,
    "size_bytes": int(size),
    "note": note,
    "forked_from": {"snapshot": sys.argv[7].split(' (')[0].replace('forked from ', '') if 'forked from ' in sys.argv[7] else None},
}
with open(mf, "w") as f:
    json.dump(data, f, indent=2)
PY

ok "fork snapshot '$NEW' ready"
echo ""
echo "To activate it in localnet:"
echo "  $SCRIPT_DIR/chain-snapshot.sh restore $NEW"
echo "  $QRL_ROOT/configs/localnet/start-nodes.sh"
echo "  $QRL_ROOT/configs/localnet/start-beacon.sh"
