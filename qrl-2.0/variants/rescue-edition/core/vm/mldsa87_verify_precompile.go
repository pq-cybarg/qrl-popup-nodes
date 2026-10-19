// ML-DSA-87 Verify Precompile at 0x06 — DigitalGuards / Moscow Chill Draft QIP
//
// Draft QIP: docs/draft-qrl2-pq-precompiles.md (DigitalGuards/myqrlwallet-qns)
//
// Spec:
//   Input layout (raw packed, fixed positions, NO length prefixes):
//     messageRepresentative  64 bytes
//     signature              4627 bytes
//     publicKey              2592 bytes
//     context                0 to 255 bytes
//   Fixed portion = 7283 bytes. Total valid = 7283..7538 bytes inclusive.
//
//   Output: always exactly 64 bytes
//     valid signature   → 63 zero bytes || 0x01
//     invalid / malformed input → 64 zero bytes
//
//   Gas: 250000 flat (provisional per draft — subject to benchmark review)
//
// Verification calls raw FIPS 204 ML-DSA-87 Verify with `context` as the FIPS
// 204 context string, `messageRepresentative` as the message, `signature` as
// the detached signature, and `publicKey` as the public key.
//
// The precompile MUST NOT derive or return an account address — identity
// binding is an application concern (contrast with 0x22 in rescue-edition,
// which does derive the address).

package vm

import (
	"github.com/theQRL/go-qrl/params"
	cryptomldsa87 "github.com/theQRL/go-qrllib/crypto/ml_dsa_87"
)

const (
	mchillMsgRepSize    = 64
	mchillSigSize       = cryptomldsa87.CRYPTO_BYTES            // 4627
	mchillPKSize        = cryptomldsa87.CRYPTO_PUBLIC_KEY_BYTES // 2592
	mchillFixedSize     = mchillMsgRepSize + mchillSigSize + mchillPKSize
	mchillCtxMaxSize    = 255
	mchillMinValidInput = mchillFixedSize
	mchillMaxValidInput = mchillFixedSize + mchillCtxMaxSize
)

// mchillMLDSA87Verify implements PrecompiledContract at 0x06.
type mchillMLDSA87Verify struct{}

// RequiredGas is a flat 250000 for every call, matching the draft QIP. The
// flat charge is deliberate: it prevents malformed short inputs from receiving
// a discount and pushes the cost model toward the worst-case verify path.
func (c *mchillMLDSA87Verify) RequiredGas(input []byte) uint64 {
	return params.MoscowChillMLDSA87VerifyGas
}

// Run performs the verification. The 64-byte return convention is:
//   valid   → 63 zeros || 0x01
//   invalid → 64 zeros
// Any parse failure (bad length, etc.) returns 64 zeros with NO error, so a
// contract can distinguish "verify said no" from QRVM-level exceptions.
func (c *mchillMLDSA87Verify) Run(input []byte) ([]byte, error) {
	// Length validation — inclusive bounds per spec.
	if len(input) < mchillMinValidInput || len(input) > mchillMaxValidInput {
		return mchillFalse(), nil
	}

	messageRep := input[:mchillMsgRepSize]
	off := mchillMsgRepSize

	// Signature must be exactly 4627 bytes; convert to the fixed-size array
	// the crypto layer expects.
	var sigArr [cryptomldsa87.CRYPTO_BYTES]uint8
	copy(sigArr[:], input[off:off+mchillSigSize])
	off += mchillSigSize

	// Public key must be exactly 2592 bytes; convert to the fixed-size array
	// the crypto layer expects.
	var pkArr [cryptomldsa87.CRYPTO_PUBLIC_KEY_BYTES]uint8
	copy(pkArr[:], input[off:off+mchillPKSize])
	off += mchillPKSize

	// Context is 0..255 bytes — whatever remains after the fixed portion.
	ctx := input[off:]

	// Call the raw FIPS 204 verify path directly. cryptomldsa87.Verify already
	// no-ops safely on a nil pk (TOB-QRLLIB-11) but we always pass a valid
	// pointer here because pkArr is stack-allocated.
	ok := cryptomldsa87.Verify(ctx, messageRep, sigArr, &pkArr)
	if ok {
		return mchillTrue(), nil
	}
	return mchillFalse(), nil
}

// mchillTrue returns 63 zero bytes followed by 0x01 as a freshly-allocated
// 64-byte slice.
func mchillTrue() []byte {
	b := make([]byte, 64)
	b[63] = 0x01
	return b
}

// mchillFalse returns 64 zero bytes as a freshly-allocated slice.
func mchillFalse() []byte {
	return make([]byte, 64)
}
