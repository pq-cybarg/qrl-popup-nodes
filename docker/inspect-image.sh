#!/bin/bash
# Inspect a popup-node OCI image tarball produced by build-images.sh.
# Prints image manifest, extracts + summarizes the SPDX SBOM, and
# extracts the SLSA provenance predicate.
#
# Usage:
#   ./docker/inspect-image.sh docker/out/qrl-dashboard-latest.oci.tar
#   ./docker/inspect-image.sh docker/out/qrl-dashboard-latest.oci.tar --sbom  # dump full SBOM to stdout
#   ./docker/inspect-image.sh docker/out/qrl-dashboard-latest.oci.tar --provenance
#
# Requires: tar, python3 (for JSON parsing). No cosign / no oras dependency.

set -euo pipefail

TAR="${1:-}"
MODE="${2:-summary}"

[ -n "$TAR" ] && [ -f "$TAR" ] || {
    echo "usage: $0 <oci-tarball> [--sbom | --provenance | --config]" >&2
    exit 1
}

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; NC='\033[0m'
info() { echo -e "${CYAN}==>${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

extract_blob() {
    # $1 = digest with "sha256:" prefix — strip prefix, read from blobs/sha256/
    local digest="$1"
    local hex="${digest#sha256:}"
    tar -xOf "$TAR" "blobs/sha256/$hex"
}

info "reading index.json from $TAR"
INDEX=$(tar -xOf "$TAR" index.json)
INNER_DIGEST=$(printf '%s' "$INDEX" | python3 -c '
import json,sys
d=json.load(sys.stdin)
m=d["manifests"][0]
print(m["digest"])
')

# The inner index has (image-manifest, attestation-manifest) per platform.
INNER=$(extract_blob "$INNER_DIGEST")

IMAGE_DIGEST=$(printf '%s' "$INNER" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for m in d.get("manifests",[]):
    ann = m.get("annotations") or {}
    if ann.get("vnd.docker.reference.type") != "attestation-manifest":
        print(m["digest"]); break
')
ATTEST_DIGEST=$(printf '%s' "$INNER" | python3 -c '
import json,sys
d=json.load(sys.stdin)
for m in d.get("manifests",[]):
    ann = m.get("annotations") or {}
    if ann.get("vnd.docker.reference.type") == "attestation-manifest":
        print(m["digest"]); break
')

[ -n "$IMAGE_DIGEST" ]  || { echo "no image manifest found in $TAR" >&2; exit 1; }
[ -n "$ATTEST_DIGEST" ] || { echo "no attestation manifest found — was --sbom/--provenance passed to buildx?" >&2; exit 1; }

info "image manifest: $IMAGE_DIGEST"
info "attestation manifest: $ATTEST_DIGEST"

ATTEST=$(extract_blob "$ATTEST_DIGEST")

case "$MODE" in
    --config)
        CFG_DIGEST=$(printf '%s' "$(extract_blob "$IMAGE_DIGEST")" | python3 -c 'import json,sys; print(json.load(sys.stdin)["config"]["digest"])')
        extract_blob "$CFG_DIGEST" | python3 -m json.tool
        ;;
    --sbom)
        SBOM_DIGEST=$(printf '%s' "$ATTEST" | python3 -c '
import json,sys
for l in json.load(sys.stdin).get("layers",[]):
    if (l.get("annotations") or {}).get("in-toto.io/predicate-type") == "https://spdx.dev/Document":
        print(l["digest"]); break
')
        [ -n "$SBOM_DIGEST" ] || { echo "no SBOM in attestation manifest" >&2; exit 1; }
        extract_blob "$SBOM_DIGEST"
        ;;
    --provenance)
        PROV_DIGEST=$(printf '%s' "$ATTEST" | python3 -c '
import json,sys
for l in json.load(sys.stdin).get("layers",[]):
    if (l.get("annotations") or {}).get("in-toto.io/predicate-type","").startswith("https://slsa.dev/"):
        print(l["digest"]); break
')
        [ -n "$PROV_DIGEST" ] || { echo "no SLSA provenance in attestation manifest" >&2; exit 1; }
        extract_blob "$PROV_DIGEST"
        ;;
    summary|*)
        info "SBOM summary"
        SBOM_DIGEST=$(printf '%s' "$ATTEST" | python3 -c '
import json,sys
for l in json.load(sys.stdin).get("layers",[]):
    if (l.get("annotations") or {}).get("in-toto.io/predicate-type") == "https://spdx.dev/Document":
        print(l["digest"]); break
')
        if [ -n "$SBOM_DIGEST" ]; then
            extract_blob "$SBOM_DIGEST" | python3 -c '
import json,sys
d = json.load(sys.stdin)
p = d.get("predicate") or d  # accept both in-toto envelope and bare predicate
print(f"  SPDX version:   {p.get("spdxVersion")}")
print(f"  created:        {p.get("creationInfo",{}).get("created")}")
pkgs = p.get("packages", [])
print(f"  packages:       {len(pkgs)}")
for pk in pkgs[:5]:
    print(f"    - {pk["name"]} @ {pk.get("versionInfo","?")}")
if len(pkgs) > 5:
    print(f"    ...and {len(pkgs)-5} more (run with --sbom for full)")
'
            ok "SBOM extracted"
        else
            echo "  no SBOM attested to this image" >&2
        fi

        info "SLSA provenance summary"
        PROV_DIGEST=$(printf '%s' "$ATTEST" | python3 -c '
import json,sys
for l in json.load(sys.stdin).get("layers",[]):
    if (l.get("annotations") or {}).get("in-toto.io/predicate-type","").startswith("https://slsa.dev/"):
        print(l["digest"]); break
')
        if [ -n "$PROV_DIGEST" ]; then
            extract_blob "$PROV_DIGEST" | python3 -c '
import json,sys
d = json.load(sys.stdin)
p = d.get("predicate") or d
b = p.get("builder", {})
inv = p.get("invocation", {})
print(f"  builder.id:        {b.get("id","?")}")
print(f"  builder.version:   {b.get("version","?")}")
print(f"  invocation.parameters.frontend: {inv.get("parameters",{}).get("frontend","?")}")
mats = p.get("materials", [])
print(f"  materials:         {len(mats)} (source inputs pinned by digest)")
for m in mats[:3]:
    d = m.get("digest",{})
    hh = next(iter(d.values()),"?")[:24]
    print(f"    - {m.get("uri","?")}  {hh}...")
'
            ok "provenance extracted"
        else
            echo "  no SLSA provenance attested to this image" >&2
        fi
        ;;
esac
