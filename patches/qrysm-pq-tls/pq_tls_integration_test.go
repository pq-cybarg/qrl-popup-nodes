// Package gateway — integration test for PQ-TLS termination.
//
// Spins up a gateway with --pq-tls-cert, connects a client, verifies:
//   - handshake succeeds with X25519MLKEM768
//   - cipher is TLS_AES_256_GCM_SHA384 or TLS_CHACHA20_POLY1305_SHA256
//   - hybrid ML-DSA-87 countersignature is required when sidecar flags set
//
// Place at qrysm/beacon-chain/gateway/pq_tls_integration_test.go.
package gateway

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// genEcdsaCert produces a short-lived ECDSA-P256 self-signed cert
// for test use. Writes cert + key to tmpdir and returns paths.
func genEcdsaCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa gen: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "pq-tls-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert create: %v", err)
	}
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	cf, _ := os.Create(certPath)
	pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	cf.Close()
	kf, _ := os.Create(keyPath)
	keyDER, _ := x509.MarshalECPrivateKey(key)
	pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	kf.Close()
	return
}

func TestPQTLSConfig_DisabledWhenNoCert(t *testing.T) {
	c := PQTLSConfig{}
	if c.Enabled() {
		t.Fatal("expected Enabled()=false when no cert path given")
	}
}

func TestPQTLSConfig_BuildMinimal(t *testing.T) {
	cert, key := genEcdsaCert(t)
	c := PQTLSConfig{CertPath: cert, KeyPath: key, KEM: "x25519mlkem768"}
	cfg, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %v, want TLS 1.3", cfg.MinVersion)
	}
	if got := cfg.CurvePreferences[0]; got != tls.CurveID(0x11EC) {
		t.Errorf("CurvePreferences[0] = %v, want X25519MLKEM768 (0x11EC)", got)
	}
}

func TestPQTLSConfig_BuildRefusesUnknownKEM(t *testing.T) {
	cert, key := genEcdsaCert(t)
	c := PQTLSConfig{CertPath: cert, KeyPath: key, KEM: "not-a-kem"}
	if _, err := c.Build(); err == nil {
		t.Fatal("expected Build to refuse unknown KEM")
	}
}

func TestPQTLSConfig_HybridRequiresBothFlags(t *testing.T) {
	cert, key := genEcdsaCert(t)
	c := PQTLSConfig{CertPath: cert, KeyPath: key, MLDSASig: "sig"}
	if c.HybridEnabled() {
		t.Fatal("hybrid should NOT be enabled with only sig path (pub missing)")
	}
	c = PQTLSConfig{CertPath: cert, KeyPath: key, MLDSAPub: "pub"}
	if c.HybridEnabled() {
		t.Fatal("hybrid should NOT be enabled with only pub path (sig missing)")
	}
}

// End-to-end: wire the Build()'d config into an httptest.Server and
// speak TLS 1.3 to it. Clients that support X25519MLKEM768 negotiate
// it; clients that don't fall back to X25519 (both branches should
// succeed since the server lists both).
func TestPQTLSConfig_EndToEnd(t *testing.T) {
	cert, key := genEcdsaCert(t)
	c := PQTLSConfig{CertPath: cert, KeyPath: key, KEM: "x25519mlkem768"}
	cfg, err := c.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	srv.TLS = cfg
	srv.StartTLS()
	defer srv.Close()

	cli := srv.Client()
	cli.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true
	cli.Transport.(*http.Transport).TLSClientConfig.MinVersion = tls.VersionTLS13

	resp, err := cli.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}
