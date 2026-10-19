// ML-DSA-87 verify precompile tests (moscow-chill / DigitalGuards proposal).
//
// Uses real ML-DSA-87 keypairs and signatures produced by the same crypto
// primitive the chain runs at consensus time. Exercises the wire layout,
// context propagation, boundary lengths, and the exact 64-byte return
// encoding defined by the draft QIP.

package vm

import (
	"testing"

	cryptomldsa87 "github.com/theQRL/go-qrllib/crypto/ml_dsa_87"
)

// mchillGen returns a fresh (pk, sig) for a given messageRep + context. The
// signature is produced with the SAME context that will be passed to the
// precompile, satisfying the FIPS 204 requirement that ctx match on sign and
// verify.
func mchillGen(t *testing.T, messageRep, ctx []byte) (pk []byte, sig []byte) {
	t.Helper()

	d, err := cryptomldsa87.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sigArr, err := d.Sign(ctx, messageRep)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pkArr := d.GetPK()
	pk = pkArr[:]
	sig = sigArr[:]
	return
}

// packInput builds the exact wire layout the precompile expects:
// messageRep(64) || signature(4627) || publicKey(2592) || context(<=255)
func packInput(messageRep, sig, pk, ctx []byte) []byte {
	out := make([]byte, len(messageRep)+len(sig)+len(pk)+len(ctx))
	off := 0
	copy(out[off:], messageRep)
	off += len(messageRep)
	copy(out[off:], sig)
	off += len(sig)
	copy(out[off:], pk)
	off += len(pk)
	copy(out[off:], ctx)
	return out
}

func TestMChillMLDSA87_ValidReturnsCanonicalTrue(t *testing.T) {
	messageRep := make([]byte, 64)
	copy(messageRep, []byte("moscow-chill-mldsa87-precompile-happy-path-vector-01"))
	ctx := []byte("QNS-SIGN-v1")

	pk, sig := mchillGen(t, messageRep, ctx)
	input := packInput(messageRep, sig, pk, ctx)

	p := &mchillMLDSA87Verify{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out) != 64 {
		t.Fatalf("output len = %d, want 64", len(out))
	}
	// Canonical true: 63 zeros then 0x01.
	for i := 0; i < 63; i++ {
		if out[i] != 0 {
			t.Fatalf("out[%d] = %02x, want 00", i, out[i])
		}
	}
	if out[63] != 0x01 {
		t.Fatalf("out[63] = %02x, want 01", out[63])
	}
}

func TestMChillMLDSA87_EmptyContextValid(t *testing.T) {
	messageRep := make([]byte, 64)
	copy(messageRep, []byte("moscow-chill-mldsa87-empty-ctx-must-verify"))
	ctx := []byte{} // zero-length allowed per spec (0..255)

	pk, sig := mchillGen(t, messageRep, ctx)
	input := packInput(messageRep, sig, pk, ctx)

	if len(input) != mchillFixedSize {
		t.Fatalf("empty-ctx input len = %d, want %d", len(input), mchillFixedSize)
	}

	p := &mchillMLDSA87Verify{}
	out, _ := p.Run(input)
	if out[63] != 0x01 {
		t.Fatalf("empty-ctx verify failed (out[63] = %02x)", out[63])
	}
}

func TestMChillMLDSA87_MaxContextValid(t *testing.T) {
	messageRep := make([]byte, 64)
	copy(messageRep, []byte("moscow-chill-mldsa87-max-ctx-255-bytes"))
	ctx := make([]byte, 255) // exactly the maximum
	for i := range ctx {
		ctx[i] = byte(i)
	}

	pk, sig := mchillGen(t, messageRep, ctx)
	input := packInput(messageRep, sig, pk, ctx)

	if len(input) != mchillMaxValidInput {
		t.Fatalf("max-ctx input len = %d, want %d", len(input), mchillMaxValidInput)
	}

	p := &mchillMLDSA87Verify{}
	out, _ := p.Run(input)
	if out[63] != 0x01 {
		t.Fatal("max-ctx verify failed")
	}
}

func TestMChillMLDSA87_ContextMismatchReturnsFalse(t *testing.T) {
	messageRep := make([]byte, 64)
	copy(messageRep, []byte("moscow-chill-mldsa87-ctx-mismatch-rejected"))
	signCtx := []byte("ctx-A")
	verifyCtx := []byte("ctx-B") // different context = must fail verify

	pk, sig := mchillGen(t, messageRep, signCtx)
	input := packInput(messageRep, sig, pk, verifyCtx)

	p := &mchillMLDSA87Verify{}
	out, _ := p.Run(input)
	if out[63] != 0 {
		t.Fatal("ctx mismatch should return false but got true")
	}
	// Must be all zeros.
	for i, b := range out {
		if b != 0 {
			t.Fatalf("false must be all-zero, out[%d]=%02x", i, b)
		}
	}
}

func TestMChillMLDSA87_TamperedMessageRepresentativeRejected(t *testing.T) {
	messageRep := make([]byte, 64)
	copy(messageRep, []byte("moscow-chill-mldsa87-tampered-msg-rep-vector"))
	ctx := []byte("QNS-SIGN-v1")

	pk, sig := mchillGen(t, messageRep, ctx)
	messageRep[0] ^= 0xFF // flip after signing
	input := packInput(messageRep, sig, pk, ctx)

	p := &mchillMLDSA87Verify{}
	out, _ := p.Run(input)
	if out[63] != 0 {
		t.Fatal("tampered msg-rep must return false")
	}
}

func TestMChillMLDSA87_TamperedSignatureRejected(t *testing.T) {
	messageRep := make([]byte, 64)
	copy(messageRep, []byte("moscow-chill-mldsa87-tampered-sig-vector"))
	ctx := []byte("QNS-SIGN-v1")

	pk, sig := mchillGen(t, messageRep, ctx)
	sig[0] ^= 0xFF
	input := packInput(messageRep, sig, pk, ctx)

	p := &mchillMLDSA87Verify{}
	out, _ := p.Run(input)
	if out[63] != 0 {
		t.Fatal("tampered sig must return false")
	}
}

func TestMChillMLDSA87_MalformedLengthReturnsFalseNotError(t *testing.T) {
	p := &mchillMLDSA87Verify{}
	for _, size := range []int{0, 100, mchillFixedSize - 1, mchillMaxValidInput + 1, mchillMaxValidInput + 100} {
		out, err := p.Run(make([]byte, size))
		if err != nil {
			t.Fatalf("Run(%d bytes) returned err %v — draft says malformed → false, not error", size, err)
		}
		if len(out) != 64 {
			t.Fatalf("Run(%d bytes) returned %d-byte out — must be 64", size, len(out))
		}
		for i, b := range out {
			if b != 0 {
				t.Fatalf("Run(%d bytes) out[%d]=%02x — must be all zero", size, i, b)
			}
		}
	}
}

func TestMChillMLDSA87_GasIsFlat(t *testing.T) {
	p := &mchillMLDSA87Verify{}
	for _, size := range []int{0, mchillFixedSize, mchillMaxValidInput, 999999} {
		g := p.RequiredGas(make([]byte, size))
		if g != 250000 {
			t.Errorf("RequiredGas(%d bytes) = %d, want 250000 (flat per spec)", size, g)
		}
	}
}
