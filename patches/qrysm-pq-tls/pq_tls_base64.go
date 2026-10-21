// Package gateway — isolated base64 import so the pq_tls.go core
// stays import-minimal and auditable.
package gateway

import "encoding/base64"

func b64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
