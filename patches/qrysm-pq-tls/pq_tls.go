// Package gateway — pq_tls.go
//
// Native PQ-TLS termination for the qrysm beacon-chain gRPC gateway.
// Builds a *tls.Config that offers an ML-KEM-wrapped KEM group
// (X25519MLKEM768 by default) and loads an ECDSA cert whose DER is
// also countersigned out-of-band with ML-DSA-87 (hybrid mode).
//
// Design:
//   - KEM: Go 1.23+ supports X25519MLKEM768 natively via tls.CurveID
//     0x11EC (per draft-kwiatkowski-tls-ecdhe-mlkem). No cgo needed.
//   - Cert signature: ECDSA-P256 (standard x509), with a parallel
//     ML-DSA-87 detached signature verified at startup by this code.
//     On signature mismatch the beacon REFUSES TO START.
//   - Fallback: when the flags are unset, this returns (nil, nil) and
//     the caller must fall back to classical TLS or plain TCP.
//
// Place at qrysm/beacon-chain/gateway/pq_tls.go. Called from
// gateway.New() when the beacon-chain CLI passes --pq-tls-cert.
package gateway

import (
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PQTLSConfig describes the flags passed by the beacon-chain CLI.
type PQTLSConfig struct {
	CertPath     string // --pq-tls-cert
	KeyPath      string // --pq-tls-key
	KEM          string // --pq-tls-kem — "x25519mlkem768" or "mlkem768"
	MLDSASig     string // --pq-tls-mldsa-sig
	MLDSAPub     string // --pq-tls-mldsa-pub
	MLDSAKeygen  string // path to mldsa87keygen binary (optional override)
}

// Enabled returns true iff the operator asked for PQ-TLS.
func (c PQTLSConfig) Enabled() bool { return c.CertPath != "" && c.KeyPath != "" }

// HybridEnabled returns true iff the operator asked for the ML-DSA-87
// hybrid-signature validation.
func (c PQTLSConfig) HybridEnabled() bool { return c.MLDSASig != "" && c.MLDSAPub != "" }

// Build returns a *tls.Config ready to be assigned to
// http.Server.TLSConfig for the gRPC-gateway's listener.
//
// Caller should guard with PQTLSConfig.Enabled() first.
func (c PQTLSConfig) Build() (*tls.Config, error) {
	if !c.Enabled() {
		return nil, errors.New("PQ-TLS not enabled — call .Enabled() first")
	}

	cert, err := tls.LoadX509KeyPair(c.CertPath, c.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("load cert/key: %w", err)
	}

	// If hybrid mode, verify the ML-DSA-87 countersignature over the
	// cert DER BEFORE serving. A mismatch means the cert was swapped
	// (classical TLS validators wouldn't catch this).
	if c.HybridEnabled() {
		if err := c.verifyHybridSignature(); err != nil {
			return nil, fmt.Errorf("ML-DSA-87 hybrid signature invalid: %w", err)
		}
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}

	// KEM selection — tls.CurveID 0x11EC is X25519MLKEM768 (Go 1.23+).
	// Fall back to pure X25519 if the Go runtime doesn't support it.
	switch strings.ToLower(c.KEM) {
	case "x25519mlkem768", "":
		cfg.CurvePreferences = []tls.CurveID{
			tls.CurveID(0x11EC), // X25519MLKEM768
			tls.X25519,          // fallback for pre-1.23 clients
		}
	case "mlkem768":
		cfg.CurvePreferences = []tls.CurveID{
			tls.CurveID(0x0768), // ML-KEM-768 (not finalized in Go stdlib)
		}
	default:
		return nil, fmt.Errorf("unknown --pq-tls-kem value %q (want x25519mlkem768 or mlkem768)", c.KEM)
	}

	return cfg, nil
}

// verifyHybridSignature invokes mldsa87keygen -verify against the
// cert DER + the pinned pubkey + the detached signature. Returns nil
// on valid; error on any mismatch or tool failure.
func (c PQTLSConfig) verifyHybridSignature() error {
	certDER, err := certDERFromPEM(c.CertPath)
	if err != nil {
		return err
	}
	pubHex, err := readTrimmed(c.MLDSAPub)
	if err != nil {
		return err
	}
	sigHex, err := readTrimmed(c.MLDSASig)
	if err != nil {
		return err
	}
	bin := c.MLDSAKeygen
	if bin == "" {
		bin = "/opt/qrl/bin/mldsa87keygen"
	}
	cmd := exec.Command(bin,
		"-verify",
		"-pubkey", pubHex,
		"-message", hex.EncodeToString(certDER),
		"-signature", sigHex,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mldsa87keygen -verify: %w — output: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// certDERFromPEM reads a cert PEM and returns its DER bytes.
func certDERFromPEM(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// crude PEM parser — avoid pulling encoding/pem to keep imports small
	begin := strings.Index(string(raw), "-----BEGIN CERTIFICATE-----")
	end := strings.Index(string(raw), "-----END CERTIFICATE-----")
	if begin < 0 || end < 0 {
		return nil, fmt.Errorf("no cert block in %s", path)
	}
	b64 := strings.ReplaceAll(string(raw[begin+len("-----BEGIN CERTIFICATE-----"):end]), "\n", "")
	b64 = strings.ReplaceAll(b64, "\r", "")
	b64 = strings.TrimSpace(b64)
	der, err := base64StdDecode(b64)
	if err != nil {
		return nil, fmt.Errorf("decode cert PEM: %w", err)
	}
	return der, nil
}

func readTrimmed(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// base64StdDecode wraps encoding/base64.StdEncoding.DecodeString —
// isolated so the import is explicit and auditable.
func base64StdDecode(s string) ([]byte, error) {
	// Standard base64 of the PEM body.
	return b64Decode(s)
}
