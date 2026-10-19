// ML-DSA-87 verify-with-address precompile tests (0x22, rescue-edition).
//
// Wire format: addr(64) || msgRep(64) || sig(4627) || pk(2592) || ctx(0..255).
// Return: 64-byte QRL 2.0 canonical bool (63 zeros || 0x01 / 64 zeros).
//
// Tests for previously-existing sub-ops moved to moscow-chill:
//   - "verify sig only" tests: see mods/moscow-chill/mldsa87_verify_precompile_test.go
//     TestMChillMLDSA87_ValidReturnsCanonicalTrue et al. (0x06 covers this case)
//   - "addr from pk" test: composable in-contract as SHAKE256(descriptor || pk)
//     via moscow-chill's 0x03; no dedicated precompile in this redesign.

package vm

import (
	"testing"

	cryptomldsa87 "github.com/theQRL/go-qrllib/crypto/ml_dsa_87"
	qrllibcommon "github.com/theQRL/go-qrllib/wallet/common"
	"github.com/theQRL/go-qrllib/wallet/common/descriptor"
)

// newMLDSA87SigForTest generates a real ML-DSA-87 keypair via the raw crypto
// primitive and signs a fixed 64-byte messageRepresentative under the given
// FIPS 204 context. Real bytes on the wire — no baked vectors.
func newMLDSA87SigForTest(t *testing.T, msgRep [mldsa87MsgRepSize]byte, ctx []byte) (pk, sig []byte) {
	t.Helper()
	d, err := cryptomldsa87.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sigArr, err := d.Sign(ctx, msgRep[:])
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pkArr := d.GetPK()
	return pkArr[:], sigArr[:]
}

// addrFromPK derives the canonical QRL address (SHAKE256(descriptor || pk)[64])
// the same way the precompile does, for setting up test inputs.
func addrFromPK(pk []byte) []byte {
	desc, _ := descriptor.FromBytes(mldsa87DescriptorBytes[:])
	derived := qrllibcommon.UnsafeGetAddress(pk, desc)
	return derived[:]
}

// buildInput packs the precompile's wire format.
func buildInput(addr []byte, msgRep [mldsa87MsgRepSize]byte, sig, pk, ctx []byte) []byte {
	out := make([]byte, mldsa87FixedInputSize+len(ctx))
	off := 0
	copy(out[off:], addr)
	off += mldsa87AddrSize
	copy(out[off:], msgRep[:])
	off += mldsa87MsgRepSize
	copy(out[off:], sig)
	off += mldsa87SigSize
	copy(out[off:], pk)
	off += mldsa87PKSize
	copy(out[off:], ctx)
	return out
}

// assertCanonicalTrue verifies the return matches QRL 2.0 canonical-true.
func assertCanonicalTrue(t *testing.T, out []byte) {
	t.Helper()
	if len(out) != mldsa87AddrSize {
		t.Fatalf("output length = %d, want %d", len(out), mldsa87AddrSize)
	}
	for i := 0; i < mldsa87AddrSize-1; i++ {
		if out[i] != 0 {
			t.Fatalf("out[%d] = %02x, want 00 (canonical-true prefix must be zeros)", i, out[i])
		}
	}
	if out[mldsa87AddrSize-1] != 0x01 {
		t.Fatalf("out[%d] = %02x, want 01 (canonical-true final byte)", mldsa87AddrSize-1, out[mldsa87AddrSize-1])
	}
}

// assertCanonicalFalse verifies the return matches QRL 2.0 canonical-false.
func assertCanonicalFalse(t *testing.T, out []byte) {
	t.Helper()
	if len(out) != mldsa87AddrSize {
		t.Fatalf("output length = %d, want %d", len(out), mldsa87AddrSize)
	}
	for i, b := range out {
		if b != 0 {
			t.Fatalf("out[%d] = %02x, want 00 (canonical-false is all zeros)", i, b)
		}
	}
}

var defaultCtx = []byte("zRonin-test-ctx-v1")

func TestMLDSA87VerifyWithAddr_HappyPath(t *testing.T) {
	var msgRep [mldsa87MsgRepSize]byte
	copy(msgRep[:], []byte("mldsa87-precompile-verify-with-addr-happy-path-vector-2026-01-a"))

	pk, sig := newMLDSA87SigForTest(t, msgRep, defaultCtx)
	addr := addrFromPK(pk)

	p := &mldsa87Verify{}
	out, err := p.Run(buildInput(addr, msgRep, sig, pk, defaultCtx))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertCanonicalTrue(t, out)
}

func TestMLDSA87VerifyWithAddr_EmptyCtxValid(t *testing.T) {
	// Empty ctx is permitted (FIPS 204 default) even though contracts should
	// reject that path in their own logic to prevent cross-protocol replay.
	var msgRep [mldsa87MsgRepSize]byte
	copy(msgRep[:], []byte("mldsa87-precompile-verify-empty-ctx-vector-2026-empty-ctx-happy"))

	pk, sig := newMLDSA87SigForTest(t, msgRep, nil)
	addr := addrFromPK(pk)

	p := &mldsa87Verify{}
	out, err := p.Run(buildInput(addr, msgRep, sig, pk, nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertCanonicalTrue(t, out)
}

func TestMLDSA87VerifyWithAddr_MaxCtxValid(t *testing.T) {
	var msgRep [mldsa87MsgRepSize]byte
	copy(msgRep[:], []byte("mldsa87-precompile-verify-max-ctx-vector-2026-max-ctx-255-b-len"))
	ctx := make([]byte, 255)
	for i := range ctx {
		ctx[i] = byte(i)
	}
	pk, sig := newMLDSA87SigForTest(t, msgRep, ctx)
	addr := addrFromPK(pk)

	p := &mldsa87Verify{}
	out, err := p.Run(buildInput(addr, msgRep, sig, pk, ctx))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertCanonicalTrue(t, out)
}

// The security-critical test: a signature made under one context CANNOT be
// verified under a different context. This is what the ctx field exists for.
func TestMLDSA87VerifyWithAddr_CtxMismatchRejected(t *testing.T) {
	var msgRep [mldsa87MsgRepSize]byte
	copy(msgRep[:], []byte("mldsa87-precompile-verify-ctx-mismatch-vector-2026-ctx-replay-b"))

	signCtx := []byte("protocol-A-auth-v1")
	verifyCtx := []byte("protocol-B-vote-v2") // different — must fail

	pk, sig := newMLDSA87SigForTest(t, msgRep, signCtx)
	addr := addrFromPK(pk)

	p := &mldsa87Verify{}
	out, err := p.Run(buildInput(addr, msgRep, sig, pk, verifyCtx))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertCanonicalFalse(t, out)
}

func TestMLDSA87VerifyWithAddr_WrongAddress(t *testing.T) {
	var msgRep [mldsa87MsgRepSize]byte
	copy(msgRep[:], []byte("mldsa87-precompile-verify-with-addr-wrong-address-vector-2026-b"))

	pk, sig := newMLDSA87SigForTest(t, msgRep, defaultCtx)
	// Different keypair yields a different address — signature is still valid
	// under pk but the claimed address doesn't hash to pk.
	wrongPK, _ := newMLDSA87SigForTest(t, msgRep, defaultCtx)
	wrongAddr := addrFromPK(wrongPK)

	p := &mldsa87Verify{}
	out, err := p.Run(buildInput(wrongAddr, msgRep, sig, pk, defaultCtx))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertCanonicalFalse(t, out)
}

func TestMLDSA87VerifyWithAddr_TamperedSig(t *testing.T) {
	var msgRep [mldsa87MsgRepSize]byte
	copy(msgRep[:], []byte("mldsa87-precompile-verify-with-addr-tampered-sig-vector-2026-cc"))

	pk, sig := newMLDSA87SigForTest(t, msgRep, defaultCtx)
	addr := addrFromPK(pk)
	tampered := make([]byte, len(sig))
	copy(tampered, sig)
	tampered[0] ^= 0xFF

	p := &mldsa87Verify{}
	out, err := p.Run(buildInput(addr, msgRep, tampered, pk, defaultCtx))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertCanonicalFalse(t, out)
}

func TestMLDSA87VerifyWithAddr_TamperedMsg(t *testing.T) {
	var msgRep [mldsa87MsgRepSize]byte
	copy(msgRep[:], []byte("mldsa87-precompile-verify-with-addr-tampered-msg-vector-2026-dd"))

	pk, sig := newMLDSA87SigForTest(t, msgRep, defaultCtx)
	addr := addrFromPK(pk)
	tamperedMsg := msgRep
	tamperedMsg[0] ^= 0x01

	p := &mldsa87Verify{}
	out, err := p.Run(buildInput(addr, tamperedMsg, sig, pk, defaultCtx))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertCanonicalFalse(t, out)
}

func TestMLDSA87_InvalidInputLengthRejected(t *testing.T) {
	p := &mldsa87Verify{}
	for _, size := range []int{0, 100, mldsa87MinInputSize - 1, mldsa87MaxInputSize + 1} {
		if _, err := p.Run(make([]byte, size)); err == nil {
			t.Errorf("expected error for input length %d, got nil", size)
		}
	}
}

func TestMLDSA87_CanonicalTrueFreshBuffer(t *testing.T) {
	// Same rule the draft QIP mandates for moscow-chill: returned buffers must
	// be freshly owned so concurrent execution cannot corrupt them. Mutating
	// one return must not affect a subsequent return.
	a := mldsa87CanonicalTrue()
	b := mldsa87CanonicalTrue()
	a[63] = 0xFF
	if b[63] != 0x01 {
		t.Fatal("canonical-true buffers alias each other — not fresh")
	}
	a2 := mldsa87CanonicalFalse()
	b2 := mldsa87CanonicalFalse()
	a2[0] = 0xFF
	if b2[0] != 0x00 {
		t.Fatal("canonical-false buffers alias each other — not fresh")
	}
}

func TestMLDSA87VerifyRequiredGas(t *testing.T) {
	// Gas is flat — same value for any valid input size (7347..7602).
	p := &mldsa87Verify{}
	minGas := p.RequiredGas(make([]byte, mldsa87MinInputSize))
	maxGas := p.RequiredGas(make([]byte, mldsa87MaxInputSize))
	if minGas != maxGas {
		t.Fatalf("gas must be flat, got %d vs %d", minGas, maxGas)
	}
}

// Documents that the core payload after the leading addr is byte-identical
// to moscow-chill 0x06's input, so a contract can pack one buffer and route
// it to either precompile depending on the desired context binding.
func TestMLDSA87VerifyWithAddr_CorePayloadInteropWithMoscowChill(t *testing.T) {
	// After the leading addr(64), the layout is:
	//   msgRep(64) || sig(4627) || pk(2592) || ctx(0..255)
	// This is identical to moscow-chill 0x06's input.
	if mldsa87MsgRepSize != 64 || mldsa87SigSize != 4627 || mldsa87PKSize != 2592 {
		t.Fatal("field sizes drifted — cross-variant interop would break")
	}
	if mldsa87CtxMaxSize != 255 {
		t.Fatal("ctx bound drifted from moscow-chill 0x06")
	}
}
