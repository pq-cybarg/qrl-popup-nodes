// ML-DSA-87 (FIPS 204) Signature Verification Precompile
//
// Address: 0x22 (34) — rescue-edition family
// Requested by: zRonin — smart contracts need to verify off-chain ML-DSA-87
// signatures AND bind them to QRL 2.0 addresses in one native call.
//
// Design conventions (matches rescue-edition family + QRL 2.0 word size):
//
//   - Fixed-width inputs, NO length prefixes.
//     rescue_prime.go takes packed uint64 elements; stark_verifier.go takes
//     packed structs with fixed offsets. This precompile takes a fixed
//     64-byte messageRepresentative + fixed-size pk/sig — same philosophy.
//
//   - Core payload layout is byte-compatible with moscow-chill's 0x06
//     precompile (msgRep(64) || sig(4627) || pk(2592)). A contract can build
//     one buffer and slice off the leading address byte to switch between
//     "verify + bind to address" (this precompile) and "verify only under
//     custom context" (moscow-chill 0x06). Cross-variant interop.
//
//   - Returns are QRL 2.0 native: 64-byte canonical bool for verify ops
//     (63 zeros || 0x01 = true, 64 zeros = false), 64-byte raw address for
//     the derive-address op. Same encoding moscow-chill's 0x06 uses; matches
//     one QRL 2.0 VM word.
//
//   - Signing context: caller-supplied FIPS 204 context (0..255 bytes),
//     appended to the fixed input. Forcing the verifier to pin ctx blocks
//     cross-protocol signature replay (a sig made under ctx="A" cannot
//     verify under ctx="B"). Empty ctx is permitted for interop with
//     off-chain tooling but contracts SHOULD reject that path in their
//     own logic. The descriptor used for ADDRESS derivation is separate
//     and always fixed — it defines what a QRL address IS, not a security
//     parameter.
//
// Sizes (from go-qrllib v0.8.0 crypto/ml_dsa_87/const.go):
//   Public key: CRYPTO_PUBLIC_KEY_BYTES = SEED_BYTES + K*POLY_T1_PACKED_BYTES
//                                       = 32 + 8*320 = 2592
//   Signature:  CRYPTO_BYTES = C_TILDE_BYTES + L*POLY_Z_PACKED_BYTES + POLY_VEC_H_PACKED_BYTES
//                            = 64 + 7*640 + (75+8) = 4627
//   Address:    AddressSize = 64
//   MsgRep:     64 bytes (matches moscow-chill 0x06 + QRL 2.0 VM word)
//   Descriptor: DescriptorSize = 3, ML-DSA-87 = {0x01, 0x00, 0x00}
//
// Address derivation (identical to go-qrllib wallet/common/address.go):
//   addr = SHAKE256(descriptor || pk).Read(64)

package vm

import (
	"errors"

	"github.com/theQRL/go-qrl/params"
	cryptomldsa87 "github.com/theQRL/go-qrllib/crypto/ml_dsa_87"
	qrllibcommon "github.com/theQRL/go-qrllib/wallet/common"
	"github.com/theQRL/go-qrllib/wallet/common/descriptor"
	"github.com/theQRL/go-qrllib/wallet/common/wallettype"
)

// Fixed-size constants matching go-qrllib ml_dsa_87 primitives.
const (
	mldsa87PKSize     = 2592
	mldsa87SigSize    = 4627
	mldsa87AddrSize   = 64 // qrllibcommon.AddressSize
	mldsa87MsgRepSize = 64 // fixed messageRepresentative width
	mldsa87DescSize   = 3  // descriptor.DescriptorSize
	mldsa87CtxMaxSize = 255 // FIPS 204 ctx bound

	// Wire layout: addr(64) || msgRep(64) || sig(4627) || pk(2592) || ctx(0..255)
	mldsa87FixedInputSize = mldsa87AddrSize + mldsa87MsgRepSize + mldsa87SigSize + mldsa87PKSize // 7347
	mldsa87MinInputSize   = mldsa87FixedInputSize                                                // ctx empty
	mldsa87MaxInputSize   = mldsa87FixedInputSize + mldsa87CtxMaxSize                            // 7602
)

var errMLDSA87InvalidInputLen = errors.New("mldsa87: input length outside 7347..7602")

// mldsa87Verify implements the PrecompiledContract interface at 0x22.
type mldsa87Verify struct{}

// RequiredGas is flat 203k. See commit message for the derivation and the
// cost comparison against composing moscow-chill 0x06 + 0x03 + memcmp.
func (c *mldsa87Verify) RequiredGas(input []byte) uint64 {
	return params.MLDSA87VerifyWithAddrGas
}

// Run parses the fixed-layout input and verifies the signature under the
// caller-supplied FIPS 204 context, then checks the address binding. Both
// must hold to return canonical-true.
//
// Rescue-edition family convention: length errors surface as Go errors
// (translated to a QRVM revert by the precompile call surface). Verify
// failures return canonical-false with no error so contracts can
// distinguish "you passed nonsense" (revert) from "the sig didn't
// verify" (false).
func (c *mldsa87Verify) Run(input []byte) ([]byte, error) {
	if len(input) < mldsa87MinInputSize || len(input) > mldsa87MaxInputSize {
		return nil, errMLDSA87InvalidInputLen
	}
	off := 0
	addr := input[off : off+mldsa87AddrSize]
	off += mldsa87AddrSize
	msgRep := input[off : off+mldsa87MsgRepSize]
	off += mldsa87MsgRepSize
	var sigArr [cryptomldsa87.CRYPTO_BYTES]uint8
	copy(sigArr[:], input[off:off+mldsa87SigSize])
	off += mldsa87SigSize
	var pkArr [cryptomldsa87.CRYPTO_PUBLIC_KEY_BYTES]uint8
	copy(pkArr[:], input[off:off+mldsa87PKSize])
	off += mldsa87PKSize
	ctx := input[off:] // 0..255 bytes, enforced by the length gate above

	// Address bind first — cheap SHAKE, fail-fast avoids the expensive verify
	// when the caller passed a mismatched addr.
	desc, _ := descriptor.FromBytes(mldsa87DescriptorBytes[:])
	derived := qrllibcommon.UnsafeGetAddress(pkArr[:], desc)
	if !constEq(derived[:], addr) {
		return mldsa87CanonicalFalse(), nil
	}
	if !cryptomldsa87.Verify(ctx, msgRep, sigArr, &pkArr) {
		return mldsa87CanonicalFalse(), nil
	}
	return mldsa87CanonicalTrue(), nil
}

// Canonical ML-DSA-87 descriptor bytes. Constant across all calls, precomputed.
var mldsa87DescriptorBytes = func() [mldsa87DescSize]byte {
	return descriptor.GetDescriptorBytes(wallettype.ML_DSA_87, [2]byte{0x00, 0x00})
}()

// mldsa87CanonicalTrue returns 63 zero bytes followed by 0x01 — the QRL 2.0
// canonical-true encoding shared with moscow-chill's 0x06 precompile.
// Fresh allocation per call, matching the draft QIP's mandate that concurrent
// execution cannot corrupt returned buffers.
func mldsa87CanonicalTrue() []byte {
	b := make([]byte, mldsa87AddrSize)
	b[mldsa87AddrSize-1] = 0x01
	return b
}

// mldsa87CanonicalFalse returns 64 zero bytes — the QRL 2.0 canonical-false
// encoding shared with moscow-chill's 0x06 precompile.
func mldsa87CanonicalFalse() []byte {
	return make([]byte, mldsa87AddrSize)
}

// constEq is a constant-time equality check for two byte slices of the same
// declared length. Length mismatch returns false without leaking timing info.
func constEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
