#!/bin/bash
# Generate a self-signed ECDSA-P256 TLS certificate for the popup-node
# dashboard's PQ TLS listener (see status-server.js).
#
# The MLKEM key exchange is post-quantum. The CERT signature is ECDSA-P256
# by default (X.509 tooling in Go / browsers / CAs doesn't fully support
# ML-DSA-87 cert signatures yet — see docs/ml-dsa-87-ml-kem-migration.md
# for the upstream patch pathway).
#
# With `--mldsa`, this script also:
#   - generates an ML-DSA-87 keypair via qrl-2.0/tools/mldsa87keygen
#   - signs the DER-encoded cert with the ML-DSA-87 private key
#   - stores the detached signature at cert.pem.mldsa.sig
#   - stores the ML-DSA-87 public key at mldsa.pub (hex)
#
# Operators who pin out-of-band can then verify BOTH:
#   - ECDSA signature via the standard TLS handshake (what the client stack does)
#   - ML-DSA-87 signature via mldsa87keygen -verify (PQ guarantee)
#
# This is strictly stronger than ECDSA-only: an adversary who gains a
# quantum capability + the ECDSA cert still cannot forge an ML-DSA-87
# signature on a new TBS.
#
# Usage:
#   ./docker/gen-tls-cert.sh                              # ECDSA-only
#   ./docker/gen-tls-cert.sh --mldsa                      # ECDSA + ML-DSA-87 hybrid
#   OUT_DIR=/etc/qrl/tls ./docker/gen-tls-cert.sh --mldsa
#   CN=mynode.example.com DAYS=365 ./docker/gen-tls-cert.sh
#
# Env:
#   OUT_DIR    where to write cert.pem + key.pem (default: docker/tls/)
#   CN         Common Name in the cert (default: localhost)
#   DAYS       cert validity days (default: 365)
#   SAN        Subject Alt Names, comma-separated (default: DNS:localhost,IP:127.0.0.1)
#   MLDSA_BIN  path to mldsa87keygen (default: qrl-2.0/tools/mldsa87keygen/mldsa87keygen)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/tls}"
CN="${CN:-localhost}"
DAYS="${DAYS:-365}"
SAN="${SAN:-DNS:localhost,IP:127.0.0.1}"
MLDSA_BIN="${MLDSA_BIN:-${REPO_ROOT}/qrl-2.0/tools/mldsa87keygen/mldsa87keygen}"

WITH_MLDSA=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --mldsa)   WITH_MLDSA=1; shift ;;
    --hybrid-ecdsa-mldsa) WITH_MLDSA=1; shift ;;  # alias
    -h|--help) sed -n '2,36p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

command -v openssl >/dev/null 2>&1 || die "openssl not found — install with 'brew install openssl' (macOS) or 'apt install openssl' (Debian)"

info "generating self-signed ECDSA-P256 cert (for PQ MLKEM key exchange)"
info "  CN:        $CN"
info "  SAN:       $SAN"
info "  validity:  ${DAYS} days"
info "  output:    $OUT_DIR/{cert,key}.pem"

mkdir -p "$OUT_DIR"
chmod 700 "$OUT_DIR"

# The X.509v3 config: put the SAN in place, single-purpose serverAuth EKU.
CONF=$(mktemp)
trap 'rm -f "$CONF"' EXIT
cat > "$CONF" <<EOF
[req]
distinguished_name = dn
prompt = no
x509_extensions = v3_ext
[dn]
CN = $CN
[v3_ext]
basicConstraints = critical, CA:FALSE
keyUsage = critical, digitalSignature, keyAgreement
extendedKeyUsage = serverAuth
subjectAltName = $SAN
EOF

# ECDSA-P256 key + self-signed cert in one shot.
openssl req -x509 \
    -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
    -nodes \
    -keyout "$OUT_DIR/key.pem" \
    -out "$OUT_DIR/cert.pem" \
    -days "$DAYS" \
    -config "$CONF" 2>&1 | grep -vE "^-{4,}|^\+" | tail -3 || true

chmod 600 "$OUT_DIR/key.pem"
chmod 644 "$OUT_DIR/cert.pem"

ok "cert written: $OUT_DIR/cert.pem"
ok "key written:  $OUT_DIR/key.pem"

# Print fingerprint so operator can pin it out-of-band.
FP=$(openssl x509 -in "$OUT_DIR/cert.pem" -noout -fingerprint -sha256 | sed 's/.*=//')
info "SHA-256 fingerprint: $FP"

# ---- optional ML-DSA-87 hybrid signature ----
if [[ "${WITH_MLDSA}" -eq 1 ]]; then
    echo
    info "generating ML-DSA-87 hybrid signature over the cert DER"
    if [[ ! -x "$MLDSA_BIN" ]]; then
        die "mldsa87keygen not found at $MLDSA_BIN
  build it with:
    cd qrl-2.0/tools/mldsa87keygen && go build -o mldsa87keygen ."
    fi

    # Generate ML-DSA-87 keypair
    kg_out=$("$MLDSA_BIN")
    mldsa_seed=$(python3 -c "import json,sys; print(json.loads(sys.stdin.read())['seed'])" <<< "$kg_out")
    mldsa_pub=$(python3 -c "import json,sys; print(json.loads(sys.stdin.read())['publicKey'])" <<< "$kg_out")

    # Store seed. Create the file with 0600 BEFORE writing so no
    # window exists where umask-default perms expose it to another
    # process scanning OUT_DIR. (Reviewer flag: TOCTOU window
    # between printf and chmod 600 — closed by install first.)
    install -m 0600 /dev/null "$OUT_DIR/mldsa.seed"
    printf '%s\n' "$mldsa_seed" > "$OUT_DIR/mldsa.seed"
    printf '%s\n' "$mldsa_pub" > "$OUT_DIR/mldsa.pub"
    chmod 644 "$OUT_DIR/mldsa.pub"

    # Compute cert DER hex (openssl -outform der | hex)
    cert_der_hex=$(openssl x509 -in "$OUT_DIR/cert.pem" -outform DER | od -An -tx1 | tr -d ' \n')

    # Sign the DER with ML-DSA-87
    sign_out=$("$MLDSA_BIN" -sign -seed "$mldsa_seed" -message "$cert_der_hex")
    mldsa_sig=$(python3 -c "import json,sys; print(json.loads(sys.stdin.read())['signature'])" <<< "$sign_out")

    # Store detached signature + a sidecar manifest
    printf '%s\n' "$mldsa_sig" > "$OUT_DIR/cert.pem.mldsa.sig"
    chmod 644 "$OUT_DIR/cert.pem.mldsa.sig"
    {
        echo "# ML-DSA-87 hybrid signature manifest"
        echo "# Pin cert_sha256 + mldsa_pubkey_sha256 out-of-band."
        echo "cert_file=cert.pem"
        echo "cert_sha256=$(openssl dgst -sha256 "$OUT_DIR/cert.pem" | awk '{print $NF}')"
        echo "mldsa_pub=mldsa.pub"
        echo "mldsa_pub_sha256=$(printf '%s' "$mldsa_pub" | shasum -a 256 | awk '{print $1}')"
        echo "mldsa_sig=cert.pem.mldsa.sig"
        echo "generated_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    } > "$OUT_DIR/hybrid.manifest"
    chmod 644 "$OUT_DIR/hybrid.manifest"

    # Immediately wipe the seed from this process's memory space
    unset mldsa_seed kg_out sign_out

    ok "ML-DSA-87 pubkey:  $OUT_DIR/mldsa.pub   (sha256 $(cat "$OUT_DIR/mldsa.pub" | shasum -a 256 | awk '{print $1}' | head -c 16)…)"
    ok "ML-DSA-87 seed:    $OUT_DIR/mldsa.seed  (chmod 600 — rotate on compromise)"
    ok "detached signat.:  $OUT_DIR/cert.pem.mldsa.sig"
    ok "hybrid manifest:   $OUT_DIR/hybrid.manifest"
    echo
    info "verify both legs:"
    echo "  # ECDSA (TLS stack does this on handshake):"
    echo "  openssl verify -CAfile $OUT_DIR/cert.pem $OUT_DIR/cert.pem"
    echo "  # ML-DSA-87 (out-of-band PQ guarantee):"
    echo "  CERT_HEX=\$(openssl x509 -in $OUT_DIR/cert.pem -outform DER | od -An -tx1 | tr -d ' \\n')"
    echo "  $MLDSA_BIN -verify \\"
    echo "      -pubkey \"\$(cat $OUT_DIR/mldsa.pub)\" \\"
    echo "      -message \"\$CERT_HEX\" \\"
    echo "      -signature \"\$(cat $OUT_DIR/cert.pem.mldsa.sig)\""
fi

echo
info "enable PQ TLS on the dashboard:"
echo "    QRL_TLS_CERT=$OUT_DIR/cert.pem \\"
echo "    QRL_TLS_KEY=$OUT_DIR/key.pem \\"
echo "    node status-server.js"
echo "  or via compose:"
echo "    docker compose \\"
echo "        -f docker-compose.yaml \\"
echo "        -f docker-compose.tls.yaml \\"
echo "        up"
echo "  then test:"
echo "    curl -sk --tls-max 1.3 https://127.0.0.1:8443/api/status"
echo "    openssl s_client -connect 127.0.0.1:8443 -groups X25519MLKEM768 -tls1_3"
