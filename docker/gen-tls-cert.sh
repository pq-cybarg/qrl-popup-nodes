#!/bin/bash
# Generate a self-signed ECDSA-P256 TLS certificate for the popup-node
# dashboard's PQ TLS listener (see status-server.js).
#
# The MLKEM key exchange is post-quantum; the CERT signature is still
# classical (ECDSA-P256). This is deliberate — X.509 tooling (browsers,
# CAs) hasn't caught up to ML-DSA-87 certs yet. The forward-secrecy
# path (session encryption key derived from MLKEM) is PQ-safe even if
# the cert signature isn't.
#
# For a production release the cert should come from a proper CA (or
# ACME/Let's Encrypt) — self-signed only for localhost dev. Once ACME
# supports ML-DSA-87 (post 2026, per NIST-ACME liaison), swap the cert
# algorithm to MLDSA87 here.
#
# Usage:
#   ./docker/gen-tls-cert.sh                              # writes ./tls/{cert,key}.pem
#   OUT_DIR=/etc/qrl/tls ./docker/gen-tls-cert.sh
#   CN=mynode.example.com DAYS=365 ./docker/gen-tls-cert.sh
#
# Env:
#   OUT_DIR    where to write cert.pem + key.pem (default: docker/tls/)
#   CN         Common Name in the cert (default: localhost)
#   DAYS       cert validity days (default: 365)
#   SAN        Subject Alt Names, comma-separated (default: DNS:localhost,IP:127.0.0.1)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/tls}"
CN="${CN:-localhost}"
DAYS="${DAYS:-365}"
SAN="${SAN:-DNS:localhost,IP:127.0.0.1}"

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
