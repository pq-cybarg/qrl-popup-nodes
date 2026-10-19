// SHAKE256 precompile tests — includes the two normative test vectors from
// the DigitalGuards draft QIP (Test vectors section).

package vm

import (
	"encoding/hex"
	"testing"
)

func TestMChillShake256_EmptyInput_MatchesDraftVector(t *testing.T) {
	// Normative vector from draft-qrl2-pq-precompiles.md:
	//   SHAKE256("", 64) =
	//   46b9dd2b0ba88d13233b3feb743eeb243fcd52ea62b81b82b50c27646ed5762f
	//   d75dc4ddd8c0f200cb05019d67b592f6fc821c49479ab48640292eacb3b7c4be
	const want = "46b9dd2b0ba88d13233b3feb743eeb243fcd52ea62b81b82b50c27646ed5762f" +
		"d75dc4ddd8c0f200cb05019d67b592f6fc821c49479ab48640292eacb3b7c4be"

	p := &shake256Precompile{}
	out, err := p.Run(nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := hex.EncodeToString(out); got != want {
		t.Fatalf("SHAKE256(empty, 64) mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestMChillShake256_ABC_MatchesDraftVector(t *testing.T) {
	// Normative vector:
	//   SHAKE256("abc", 64) =
	//   483366601360a8771c6863080cc4114d8db44530f8f1e1ee4f94ea37e78b5739
	//   d5a15bef186a5386c75744c0527e1faa9f8726e462a12a4feb06bd8801e751e4
	const want = "483366601360a8771c6863080cc4114d8db44530f8f1e1ee4f94ea37e78b5739" +
		"d5a15bef186a5386c75744c0527e1faa9f8726e462a12a4feb06bd8801e751e4"

	p := &shake256Precompile{}
	out, err := p.Run([]byte("abc"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := hex.EncodeToString(out); got != want {
		t.Fatalf("SHAKE256(\"abc\", 64) mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestMChillShake256_OutputAlwaysLen64(t *testing.T) {
	p := &shake256Precompile{}
	for _, size := range []int{0, 1, 63, 64, 65, 512, 4096} {
		in := make([]byte, size)
		for i := range in {
			in[i] = byte(i % 251)
		}
		out, err := p.Run(in)
		if err != nil {
			t.Fatalf("Run(%d bytes): %v", size, err)
		}
		if len(out) != 64 {
			t.Fatalf("output len = %d, want 64 (input %d bytes)", len(out), size)
		}
	}
}

func TestMChillShake256_GasSchedule(t *testing.T) {
	// Formula: 240 + 48 * ceil(input_length / 64)
	//   empty: 240 + 48*0 = 240
	//   1 byte: 240 + 48*1 = 288
	//   64 bytes: 240 + 48*1 = 288
	//   65 bytes: 240 + 48*2 = 336
	//   128 bytes: 240 + 48*2 = 336
	p := &shake256Precompile{}
	cases := []struct {
		size int
		want uint64
	}{
		{0, 240},
		{1, 288},
		{64, 288},
		{65, 336},
		{128, 336},
	}
	for _, tc := range cases {
		got := p.RequiredGas(make([]byte, tc.size))
		if got != tc.want {
			t.Errorf("RequiredGas(%d bytes) = %d, want %d", tc.size, got, tc.want)
		}
	}
}

func TestMChillShake256_ReturnedBufferIsFresh(t *testing.T) {
	// Draft mandates: "Both precompiles must return newly owned or immutable
	// output buffers so concurrent execution cannot corrupt results."
	// Two successive calls must return distinct backing arrays.
	p := &shake256Precompile{}
	a, _ := p.Run([]byte("test"))
	b, _ := p.Run([]byte("test"))
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("empty return")
	}
	// Mutating a must not affect b.
	a[0] ^= 0xFF
	if a[0] == b[0] {
		t.Fatal("second call's buffer aliases the first — not fresh")
	}
}
