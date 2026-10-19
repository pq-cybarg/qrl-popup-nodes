// SHAKE256 Precompile at 0x03 — DigitalGuards / Moscow Chill Draft QIP
//
// Draft QIP: docs/draft-qrl2-pq-precompiles.md (DigitalGuards/myqrlwallet-qns)
// Layer: core/security, status: draft/incomplete, created 2026-08-22
//
// Spec:
//   Input:  arbitrary byte string (the full call input)
//   Output: exactly 64 bytes equal to SHAKE256(input, 64)
//   Gas:    240 + 48 * ceil(input_length / 64)
//
// Rationale for slot 0x03 (per draft): the QRL execution registry uses slots
// 0x01, 0x02, 0x04, 0x05 today. 0x03 is unused (Ethereum's ripemd160 slot,
// which QRL removed). Reserving it for SHAKE256 aligns the QRL 2.0 64-byte
// word size with a native, fixed-width post-quantum hash primitive.

package vm

import (
	"github.com/theQRL/go-qrl/params"
	"golang.org/x/crypto/sha3"
)

// shake256Precompile implements PrecompiledContract at 0x03.
type shake256Precompile struct{}

// RequiredGas: 240 + 48 * ceil(inputLen / 64).
// The draft calls out that implementations MUST NOT wrap around and must
// saturate to max-uint64 if the multiplication overflows. In practice a call
// data length of ~2^58 bytes would be required to overflow uint64 given the
// 48x multiplier — well beyond any executable block — but the check is cheap.
func (c *shake256Precompile) RequiredGas(input []byte) uint64 {
	words := uint64((len(input) + 63) / 64)
	// Saturating multiply guard: if 48*words would overflow, cap at max-uint64.
	if words != 0 && 48 > (^uint64(0))/words {
		return ^uint64(0)
	}
	return params.Shake256BaseGas + params.Shake256PerWordGas*words
}

// Run returns SHAKE256(input, 64). The draft mandates the returned buffer be
// newly owned or immutable so concurrent execution can't corrupt results —
// this call allocates a fresh 64-byte slice per invocation.
func (c *shake256Precompile) Run(input []byte) ([]byte, error) {
	h := sha3.NewShake256()
	// Write always returns len(input), nil for shake — no error path.
	_, _ = h.Write(input)
	out := make([]byte, 64)
	_, _ = h.Read(out)
	return out, nil
}
