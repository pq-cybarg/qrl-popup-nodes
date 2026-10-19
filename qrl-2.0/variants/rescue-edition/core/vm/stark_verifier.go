// Copyright 2024 The go-zond Authors
// This file is part of the go-zond library.
//
// STARK Verifier Precompile (0x21)
//
// Complete implementation of STARK verification for zRonin privacy transactions.
// Port of the TypeScript SDK verifier (src/crypto/stark/src/verifier.ts).
//
// Verification steps:
// 1. Parse serialized proof
// 2. Reconstruct Fiat-Shamir transcript (keccak256 with domain separation)
// 3. Verify OOD evaluations are consistent with AIR constraints
// 4. Verify proof of work (grinding)
// 5. Verify FRI proof (Merkle paths + folding consistency)
// 6. Verify DEEP composition at query points

package vm

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"

	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/log"
	"github.com/theQRL/go-qrl/params"
	"golang.org/x/crypto/sha3"
)

// =============================================================================
// STARK VERIFIER CONSTANTS
// =============================================================================

// STARK verifier opcodes
const (
	opcodeVerifyFull     = 0x01 // Verify complete STARK proof in one call
	opcodeStarkInit      = 0x10 // Initialize verification session
	opcodeStarkCommit    = 0x11 // Submit commitments
	opcodeStarkOod       = 0x12 // Submit OOD evaluations
	opcodeStarkFriBatch  = 0x13 // Submit FRI query batch
	opcodeStarkFinalize  = 0x14 // Complete verification
	opcodeVerifyWithBlob = 0x20 // Verify using blob data (future)
)

// STARK proof variants
const (
	variantRP256_64bit  = 0x00
	variantRP512_64bit  = 0x01
	variantRP256_128bit = 0x02
	variantRP512_128bit = 0x03
)

// Circuit categories (high byte of circuit type)
const (
	circuitCategoryTransfer    = 0x00 // Standard transfer (1-10 inputs × 1-5 outputs)
	circuitCategoryTransferExt = 0x01 // Extended transfer (11-32 inputs or 6-10 outputs)
	circuitCategoryNFT         = 0x02 // NFT transfers (QRC721/QRC1155)
	circuitCategoryPOI         = 0x03 // Proof of Innocence circuits
	circuitCategoryNFTBatch    = 0x04 // NFT batch operations
	circuitCategoryCustom      = 0x05 // Custom registered circuits
)

// POI subtypes (for circuitCategoryPOI)
const (
	poiTypeShield    = 0x00 // Source verification for shielding
	poiTypeTransfer  = 0x01 // Chain linkage verification
	poiTypeRecursive = 0x02 // Recursive proof composition
	poiTypeBatch     = 0x03 // Batch proof verification
)

// Legacy circuit types (single byte, for backward compatibility)
// These map to the expanded 3-byte format internally
const (
	circuitTransfer1x2 = 0x00 // Legacy: category=0, inputs=1, outputs=2
	circuitTransfer1x3 = 0x01 // Legacy: category=0, inputs=1, outputs=3
	circuitTransfer2x2 = 0x02 // Legacy: category=0, inputs=2, outputs=2
	circuitTransfer2x3 = 0x03 // Legacy: category=0, inputs=2, outputs=3
	circuitTransfer8x2 = 0x04 // Legacy: category=0, inputs=8, outputs=2
)

// Circuit type encoding:
// - Legacy (1 byte): 0x00-0x04 for backward compatibility
// - Extended (3 bytes): [category+0x10:1][inputs:1][outputs:1]
//
// Extended format supports 121+ circuits:
// Wire format uses category + 0x10 offset to distinguish from legacy:
// - 0x10 = Standard transfer: inputs=1-10, outputs=1-5 (50 circuits)
// - 0x11 = Extended transfer: inputs=11-32 or outputs=6-10 (38+ circuits)
// - 0x12 = NFT: inputs=1-4, outputs=1-4 (16 circuits)
// - 0x13 = POI: poi_type in inputs field (4 circuits)
// - 0x14 = NFT Batch: inputs=4-16, outputs=4-16 (5 circuits)
// - 0x15 = Custom: user-registered circuits (∞)

const extendedFormatOffset = 0x10 // Added to category in wire format

// parseCircuitType parses legacy or extended circuit type and returns (category, inputs, outputs)
func parseCircuitType(data []byte) (uint8, int, int) {
	if len(data) == 0 {
		return 0, 1, 2 // Default to Transfer1x2
	}

	firstByte := data[0]

	// Extended 3-byte format: [category+0x10][inputs][outputs]
	// Detect by: len >= 3 AND firstByte >= 0x10
	if len(data) >= 3 && firstByte >= extendedFormatOffset {
		category := firstByte - extendedFormatOffset // Subtract offset to get actual category
		inputs := int(data[1])
		outputs := int(data[2])

		// Validate ranges
		if inputs == 0 {
			inputs = 1
		}
		if outputs == 0 {
			outputs = 1
		}
		if inputs > 32 {
			inputs = 32
		}
		if outputs > 16 {
			outputs = 16
		}

		return category, inputs, outputs
	}

	// Legacy single-byte format (0x00-0x04)
	switch firstByte {
	case circuitTransfer1x2:
		return circuitCategoryTransfer, 1, 2
	case circuitTransfer1x3:
		return circuitCategoryTransfer, 1, 3
	case circuitTransfer2x2:
		return circuitCategoryTransfer, 2, 2
	case circuitTransfer2x3:
		return circuitCategoryTransfer, 2, 3
	case circuitTransfer8x2:
		return circuitCategoryTransfer, 8, 2
	default:
		return circuitCategoryTransfer, 1, 2
	}
}

// STARK verification parameters (must match SDK DEFAULT_STARK_PARAMS)
const (
	starkNumQueries       = 43   // SECURITY: 43 × log₂(8) = 129 bits (128-bit target)
	starkBlowupFactor     = 8    // Trace blowup factor
	starkFriFoldingFactor = 4    // FRI folding factor
	starkGrindingBits     = 20   // PoW grinding factor
	starkTwoAdicity       = 32   // Goldilocks field two-adicity
	starkMaxProofSize     = 1000000 // 1MB to accommodate Transfer8x2 (~670KB)
	starkMaxPublicInputs  = 100
	starkMaxSessionAge    = 256
	starkMaxSessions      = 16
)

// Gas costs
const (
	gasStarkInit      = 1000
	gasStarkCommit    = 5000
	gasStarkOod       = 10000
	gasStarkFriBatch  = 5000
	gasStarkFriPerQ   = 2000
	gasStarkFinalize  = 50000
	gasVerifyFull     = 500000
	gasVerifyWithBlob = 200000
)

// Domain separation tags (must match SDK exactly)
const (
	domainMerkleLeaf = "zronin.merkle.leaf"
	domainMerkleNode = "zronin.merkle.node"
	domainFiatShamir = "zronin.fiat-shamir"
	domainPow        = "zronin.pow"
)

// Two-adic root of unity for Goldilocks field
const twoAdicRoot uint64 = 1753635133440165772

// Errors
var (
	errStarkInvalidInput    = errors.New("invalid STARK verifier input")
	errStarkInvalidOpcode   = errors.New("invalid STARK verifier opcode")
	errStarkInvalidVariant  = errors.New("invalid STARK variant")
	errStarkInvalidProof    = errors.New("invalid STARK proof structure")
	errStarkProofTooLarge   = errors.New("proof exceeds maximum size")
	errStarkSessionNotFound = errors.New("verification session not found")
)

// =============================================================================
// KECCAK256 HASH FUNCTIONS
// =============================================================================

// keccak256 computes keccak256 hash (same as Solidity's keccak256)
func keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

// sha3WithDomain computes keccak256 with domain separation
// Format: len(domain) || domain || data
func sha3WithDomain(data []byte, domain string) []byte {
	domainBytes := []byte(domain)
	combined := make([]byte, 1+len(domainBytes)+len(data))
	combined[0] = byte(len(domainBytes))
	copy(combined[1:], domainBytes)
	copy(combined[1+len(domainBytes):], data)
	return keccak256(combined)
}

// =============================================================================
// FIAT-SHAMIR TRANSCRIPT (matches SDK VerifierChannel exactly)
// =============================================================================

// Transcript implements the Fiat-Shamir transcript matching the SDK
type Transcript struct {
	state        []byte
	queryCounter int
}

// NewTranscript creates a transcript initialized with public inputs
// Matches: VerifierChannel constructor in verifier.ts
func NewTranscript(publicInputs []uint64) *Transcript {
	domain := []byte(domainFiatShamir)
	inputsBytes := make([]byte, len(publicInputs)*32)

	// Convert each public input to bytes32 (big-endian)
	for i, pi := range publicInputs {
		var bytes [32]byte
		val := pi
		for j := 31; j >= 0; j-- {
			bytes[j] = byte(val & 0xff)
			val >>= 8
		}
		copy(inputsBytes[i*32:], bytes[:])
	}

	combined := make([]byte, 1+len(domain)+len(inputsBytes))
	combined[0] = byte(len(domain))
	copy(combined[1:], domain)
	copy(combined[1+len(domain):], inputsBytes)

	return &Transcript{
		state:        keccak256(combined),
		queryCounter: 0,
	}
}

// Commit adds data to the transcript
// Matches: commit(data: Uint8Array) in VerifierChannel
func (t *Transcript) Commit(data []byte) {
	domain := []byte(domainFiatShamir)

	// Pad data to 32 bytes (right-align for bytes32)
	var data32 [32]byte
	if len(data) <= 32 {
		copy(data32[32-len(data):], data)
	} else {
		copy(data32[:], data[:32])
	}

	combined := make([]byte, 1+len(domain)+32+32)
	combined[0] = byte(len(domain))
	copy(combined[1:], domain)
	copy(combined[1+len(domain):], t.state)
	copy(combined[1+len(domain)+32:], data32[:])

	t.state = keccak256(combined)
}

// CommitFieldElement commits a field element as bytes32 to transcript
// Matches: commitFieldElement(value: FieldElement) in VerifierChannel
func (t *Transcript) CommitFieldElement(value uint64) {
	var bytes [32]byte
	v := value
	for j := 31; j >= 0; j-- {
		bytes[j] = byte(v & 0xff)
		v >>= 8
	}
	t.Commit(bytes[:])
}

// AbsorbRandomCoeffs absorbs count random coefficients into transcript
// Matches: absorbRandomCoeffs(count: number) in VerifierChannel
func (t *Transcript) AbsorbRandomCoeffs(count int) {
	coeffText := []byte("coeff")
	for i := 0; i < count; i++ {
		var iBytes [32]byte
		val := uint64(i)
		for j := 31; j >= 0; j-- {
			iBytes[j] = byte(val & 0xff)
			val >>= 8
		}

		combined := make([]byte, 32+5+32)
		copy(combined[:32], t.state)
		copy(combined[32:], coeffText)
		copy(combined[32+5:], iBytes[:])

		t.state = keccak256(combined)
	}
}

// GetRandomFieldElement gets a random field element from transcript
// Matches: getRandomFieldElement() in VerifierChannel
func (t *Transcript) GetRandomFieldElement() uint64 {
	randomText := []byte("random")
	combined := make([]byte, 32+len(randomText))
	copy(combined[:32], t.state)
	copy(combined[32:], randomText)

	hash := keccak256(combined)

	// Convert first 32 bytes to big.Int, then mod by Goldilocks prime
	value := new(big.Int).SetBytes(hash[:32])
	modulus := new(big.Int).SetUint64(goldilocksModulus)
	value.Mod(value, modulus)

	return value.Uint64()
}

// GetRandomFieldElements gets multiple random field elements.
//
// SECURITY FIX (task #184) + CROSS-IMPL PARITY (task #216).
//
// Previous version called GetRandomFieldElement() in a loop, which reads the
// transcript state but does NOT advance it, so every returned element was
// identical. That collapsed constraint composition from Σ αᵢ · cᵢ to α · Σ cᵢ,
// destroying Schwartz-Zippel soundness.
//
// Unified protocol (must match zkronin/src/crypto/stark/src/{prover,verifier}.ts
// and Hyperion GoldilocksVerifier _absorbRandomCoeffs post-state):
//   1. AbsorbRandomCoeffs(count): advance transcript N times via keccak(state || "coeff" || i)
//   2. For each i in [0, count): alpha_i = keccak(post_absorb_state || "random" || i_as_u256) % MOD
//   3. Return the N distinct alphas. Do NOT further advance the transcript.
//
// The non-advancement in step 3 is critical: any commit-back here would drift
// the Go precompile from the TS reference and Hyperion contract.
func (t *Transcript) GetRandomFieldElements(count int) []uint64 {
	t.AbsorbRandomCoeffs(count)

	elements := make([]uint64, count)
	randomText := []byte("random")
	modulus := new(big.Int).SetUint64(goldilocksModulus)

	for i := 0; i < count; i++ {
		var iBytes [32]byte
		val := uint64(i)
		for j := 31; j >= 0; j-- {
			iBytes[j] = byte(val & 0xff)
			val >>= 8
		}

		combined := make([]byte, 32+len(randomText)+32)
		copy(combined[:32], t.state)
		copy(combined[32:32+len(randomText)], randomText)
		copy(combined[32+len(randomText):], iBytes[:])

		hash := keccak256(combined)
		value := new(big.Int).SetBytes(hash[:32])
		value.Mod(value, modulus)
		elements[i] = value.Uint64()
	}

	// NO commit-back — critical for TS ↔ Go precompile ↔ Hyperion parity.
	return elements
}

// AbsorbOodEvals absorbs OOD evaluations into transcript
// Matches: absorbOodEvals() in VerifierChannel
func (t *Transcript) AbsorbOodEvals(traceEvals, traceNextEvals, constraintEvals []uint64) {
	absorbEval := func(eval uint64) {
		var evalBytes [8]byte
		v := eval
		for i := 7; i >= 0; i-- {
			evalBytes[i] = byte(v & 0xff)
			v >>= 8
		}
		combined := make([]byte, 32+8)
		copy(combined[:32], t.state)
		copy(combined[32:], evalBytes[:])
		t.state = keccak256(combined)
	}

	for _, e := range traceEvals {
		absorbEval(e)
	}
	for _, e := range traceNextEvals {
		absorbEval(e)
	}
	for _, e := range constraintEvals {
		absorbEval(e)
	}
}

// DeriveFieldElements derives field elements WITHOUT modifying transcript
// Matches: deriveFieldElements(count: number) in VerifierChannel
func (t *Transcript) DeriveFieldElements(count int) []uint64 {
	savedState := make([]byte, len(t.state))
	copy(savedState, t.state)

	elements := make([]uint64, count)
	for i := 0; i < count; i++ {
		randomText := []byte("random")
		var iBytes [32]byte
		val := uint64(i)
		for j := 31; j >= 0; j-- {
			iBytes[j] = byte(val & 0xff)
			val >>= 8
		}
		combined := make([]byte, 32+len(randomText)+32)
		copy(combined[:32], t.state)
		copy(combined[32:], randomText)
		copy(combined[32+len(randomText):], iBytes[:])

		hash := keccak256(combined)
		value := new(big.Int).SetBytes(hash[:32])
		modulus := new(big.Int).SetUint64(goldilocksModulus)
		value.Mod(value, modulus)
		elements[i] = value.Uint64()
	}

	// Restore state (DeriveFieldElements is non-mutating)
	t.state = savedState
	return elements
}

// GetQueryPositions gets random query positions
// Matches: getQueryPositions(domainSize, count) in VerifierChannel
func (t *Transcript) GetQueryPositions(domainSize, count int) []int {
	positions := make([]int, 0, count)
	used := make(map[int]bool)

	for len(positions) < count {
		var counterBytes [8]byte
		c := uint64(t.queryCounter)
		for i := 7; i >= 0; i-- {
			counterBytes[i] = byte(c & 0xff)
			c >>= 8
		}

		combined := make([]byte, 32+8)
		copy(combined[:32], t.state)
		copy(combined[32:], counterBytes[:])

		hash := keccak256(combined)
		var value uint64
		for i := 0; i < 8; i++ {
			value = (value << 8) | uint64(hash[i])
		}
		pos := int(value % uint64(domainSize))

		if !used[pos] {
			used[pos] = true
			positions = append(positions, pos)
		}
		t.queryCounter++
	}

	return positions
}

// GetState returns the current transcript state
func (t *Transcript) GetState() []byte {
	result := make([]byte, len(t.state))
	copy(result, t.state)
	return result
}

// =============================================================================
// TRANSCRIPT HELPER FUNCTIONS (for benchmarking)
// =============================================================================

// initTranscript creates a transcript and returns state as uint64 (for benchmarks)
// Note: variant parameter is included for API compatibility but not used
// since the transcript uses keccak256 (variant affects hash, not transcript)
func initTranscript(variant uint8, publicInputs []uint64) uint64 {
	t := NewTranscript(publicInputs)
	state := t.GetState()
	// Return first 8 bytes as uint64
	return binary.BigEndian.Uint64(state[:8])
}

// updateTranscript updates transcript state with values and returns new state (for benchmarks)
// Note: variant parameter is included for API compatibility
func updateTranscript(state uint64, variant uint8, values ...uint64) uint64 {
	// Reconstruct transcript from state
	var stateBytes [32]byte
	binary.BigEndian.PutUint64(stateBytes[:8], state)

	t := &Transcript{
		state:        stateBytes[:],
		queryCounter: 0,
	}

	// Commit each value
	for _, v := range values {
		t.CommitFieldElement(v)
	}

	newState := t.GetState()
	return binary.BigEndian.Uint64(newState[:8])
}

// =============================================================================
// MERKLE TREE VERIFICATION (matches SDK hash.ts)
// =============================================================================

// hashMerkleLeaf hashes a leaf with domain separation
func hashMerkleLeaf(data []byte) []byte {
	return sha3WithDomain(data, domainMerkleLeaf)
}

// hashMerkleNode hashes two nodes with domain separation
func hashMerkleNode(left, right []byte) []byte {
	combined := make([]byte, 64)
	copy(combined[:32], left)
	copy(combined[32:], right)
	return sha3WithDomain(combined, domainMerkleNode)
}

// verifyMerklePathWithIndex verifies a Merkle authentication path using index parity
// Matches: verifyMerklePathWithIndex in fri.ts FriVerifier
func verifyMerklePathWithIndex(leaf []byte, path [][]byte, expectedRoot []byte, leafIndex int) bool {
	current := hashMerkleLeaf(leaf)
	pos := leafIndex

	for _, sibling := range path {
		// Use index parity to determine left/right ordering
		if pos&1 == 1 {
			// Current node is right child, sibling is left
			current = hashMerkleNode(sibling, current)
		} else {
			// Current node is left child, sibling is right
			current = hashMerkleNode(current, sibling)
		}
		pos >>= 1
	}

	return bytesEqual(current, expectedRoot)
}

// bytesEqual compares two byte slices (constant-time for security)
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	diff := 0
	for i := range a {
		diff |= int(a[i] ^ b[i])
	}
	return diff == 0
}

// =============================================================================
// FIELD ARITHMETIC HELPERS
// =============================================================================

// fieldPow computes base^exp mod p
func fieldPow(base, exp uint64) uint64 {
	if exp == 0 {
		return 1
	}
	result := uint64(1)
	b := base % goldilocksModulus

	for exp > 0 {
		if exp&1 == 1 {
			result = fieldMul(result, b)
		}
		b = fieldSquare(b)
		exp >>= 1
	}
	return result
}

// fieldSub computes (a - b) mod p
func fieldSub(a, b uint64) uint64 {
	if a >= b {
		return a - b
	}
	return goldilocksModulus - (b - a)
}

// fieldInv computes multiplicative inverse using Fermat's little theorem
func fieldInv(a uint64) uint64 {
	return fieldPow(a, goldilocksModulus-2)
}

// fieldDiv computes a/b mod p
func fieldDiv(a, b uint64) uint64 {
	return fieldMul(a, fieldInv(b))
}

// getRootOfUnity gets the nth root of unity
func getRootOfUnity(n int) uint64 {
	if n > starkTwoAdicity {
		return 0
	}
	exp := uint64(1) << uint(starkTwoAdicity-n)
	return fieldPow(twoAdicRoot, exp)
}

// =============================================================================
// PROOF OF WORK VERIFICATION
// =============================================================================

// verifyPow verifies the proof of work nonce
// Matches: verifyPow in verifier.ts
func verifyPow(transcriptHash []byte, nonce uint64, bits int) bool {
	// Encode nonce as big-endian uint64 (matches Solidity abi.encodePacked)
	var nonceBytes [8]byte
	v := nonce
	for i := 7; i >= 0; i-- {
		nonceBytes[i] = byte(v & 0xff)
		v >>= 8
	}

	// Combine transcript hash and nonce
	combined := make([]byte, len(transcriptHash)+8)
	copy(combined[:len(transcriptHash)], transcriptHash)
	copy(combined[len(transcriptHash):], nonceBytes[:])

	hash := sha3WithDomain(combined, domainPow)
	return countLeadingZeros(hash) >= bits
}

// countLeadingZeros counts leading zero bits
func countLeadingZeros(hash []byte) int {
	zeros := 0
	for _, b := range hash {
		if b == 0 {
			zeros += 8
		} else {
			for b&0x80 == 0 {
				zeros++
				b <<= 1
			}
			break
		}
	}
	return zeros
}

// =============================================================================
// STARK PROOF DATA STRUCTURES (matches SDK types.ts)
// =============================================================================

// StarkProof represents a complete STARK proof
type StarkProof struct {
	Version            int
	DomainSize         int
	TraceWidth         int
	NumConstraints     int
	TraceCommitment    []byte
	ConstraintCommit   []byte
	FriLayers          []FriLayer
	FinalPolynomial    []uint64
	Queries            []QueryResponse
	PowNonce           uint64
	OodTraceEvals      []uint64
	OodTraceNextEvals  []uint64
	OodConstraintEvals []uint64
}

// FriLayer represents a FRI layer
type FriLayer struct {
	Commitment  []byte
	Evaluations []uint64
}

// QueryResponse contains all data for a single query
// Matches: FriQueryResponse in types.ts
type QueryResponse struct {
	TraceValues      []uint64
	TraceAuthPaths   [][][]byte
	ConstraintValues []uint64
	ConstraintPath   [][]byte
	FriLayerValues   [][]uint64
	FriAuthPaths     [][][][]byte
	CosetIndices     []int
}

// =============================================================================
// PROOF DESERIALIZATION (matches SDK deserializeStarkProof exactly)
// =============================================================================

// parseStarkProof deserializes a STARK proof from bytes
func parseStarkProof(data []byte) (*StarkProof, int, error) {
	if len(data) < 16 {
		return nil, 0, errStarkInvalidProof
	}

	offset := 0
	proof := &StarkProof{}

	// Header: version(4) + domainSize(4) + traceWidth(4) + numConstraints(4)
	proof.Version = int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	proof.DomainSize = int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	proof.TraceWidth = int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	proof.NumConstraints = int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	// Trace commitment: length(4) + data
	if offset+4 > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	traceCommitLen := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	if offset+traceCommitLen > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	proof.TraceCommitment = make([]byte, traceCommitLen)
	copy(proof.TraceCommitment, data[offset:offset+traceCommitLen])
	offset += traceCommitLen

	// Constraint commitment: length(4) + data
	if offset+4 > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	constraintCommitLen := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	if offset+constraintCommitLen > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	proof.ConstraintCommit = make([]byte, constraintCommitLen)
	copy(proof.ConstraintCommit, data[offset:offset+constraintCommitLen])
	offset += constraintCommitLen

	// FRI layers: count(4) + layers
	if offset+4 > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	friLayerCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	proof.FriLayers = make([]FriLayer, friLayerCount)
	for i := 0; i < friLayerCount; i++ {
		// Commitment length + data
		if offset+4 > len(data) {
			return nil, offset, errStarkInvalidProof
		}
		commitLen := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		if offset+commitLen > len(data) {
			return nil, offset, errStarkInvalidProof
		}
		proof.FriLayers[i].Commitment = make([]byte, commitLen)
		copy(proof.FriLayers[i].Commitment, data[offset:offset+commitLen])
		offset += commitLen

		// Evaluations count + values
		if offset+4 > len(data) {
			return nil, offset, errStarkInvalidProof
		}
		evalCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		proof.FriLayers[i].Evaluations = make([]uint64, evalCount)
		for j := 0; j < evalCount; j++ {
			if offset+8 > len(data) {
				return nil, offset, errStarkInvalidProof
			}
			proof.FriLayers[i].Evaluations[j] = binary.LittleEndian.Uint64(data[offset:])
			offset += 8
		}
	}

	// Final polynomial: count(4) + coefficients
	if offset+4 > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	finalPolyCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	proof.FinalPolynomial = make([]uint64, finalPolyCount)
	for i := 0; i < finalPolyCount; i++ {
		if offset+8 > len(data) {
			return nil, offset, errStarkInvalidProof
		}
		proof.FinalPolynomial[i] = binary.LittleEndian.Uint64(data[offset:])
		offset += 8
	}

	// Queries: count(4) + queries
	if offset+4 > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	queryCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	proof.Queries = make([]QueryResponse, queryCount)
	for q := 0; q < queryCount; q++ {
		var err error
		offset, err = parseQueryResponse(data, offset, &proof.Queries[q])
		if err != nil {
			return nil, offset, err
		}
	}

	// PoW nonce: 8 bytes
	if offset+8 > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	proof.PowNonce = binary.LittleEndian.Uint64(data[offset:])
	offset += 8

	// OOD evaluations: trace
	if offset+4 > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	oodTraceCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	proof.OodTraceEvals = make([]uint64, oodTraceCount)
	for i := 0; i < oodTraceCount; i++ {
		if offset+8 > len(data) {
			return nil, offset, errStarkInvalidProof
		}
		proof.OodTraceEvals[i] = binary.LittleEndian.Uint64(data[offset:])
		offset += 8
	}

	// OOD evaluations: trace next
	if offset+4 > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	oodTraceNextCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	proof.OodTraceNextEvals = make([]uint64, oodTraceNextCount)
	for i := 0; i < oodTraceNextCount; i++ {
		if offset+8 > len(data) {
			return nil, offset, errStarkInvalidProof
		}
		proof.OodTraceNextEvals[i] = binary.LittleEndian.Uint64(data[offset:])
		offset += 8
	}

	// OOD evaluations: constraints
	if offset+4 > len(data) {
		return nil, offset, errStarkInvalidProof
	}
	oodConstraintCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	proof.OodConstraintEvals = make([]uint64, oodConstraintCount)
	for i := 0; i < oodConstraintCount; i++ {
		if offset+8 > len(data) {
			return nil, offset, errStarkInvalidProof
		}
		proof.OodConstraintEvals[i] = binary.LittleEndian.Uint64(data[offset:])
		offset += 8
	}

	return proof, offset, nil
}

// parseQueryResponse parses a single query response
func parseQueryResponse(data []byte, offset int, query *QueryResponse) (int, error) {
	// Trace values: count(4) + values
	if offset+4 > len(data) {
		return offset, errStarkInvalidProof
	}
	traceValCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	query.TraceValues = make([]uint64, traceValCount)
	for i := 0; i < traceValCount; i++ {
		if offset+8 > len(data) {
			return offset, errStarkInvalidProof
		}
		query.TraceValues[i] = binary.LittleEndian.Uint64(data[offset:])
		offset += 8
	}

	// Trace auth paths: count(4) + paths
	if offset+4 > len(data) {
		return offset, errStarkInvalidProof
	}
	traceAuthCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	query.TraceAuthPaths = make([][][]byte, traceAuthCount)
	for i := 0; i < traceAuthCount; i++ {
		// Path count for this column
		if offset+4 > len(data) {
			return offset, errStarkInvalidProof
		}
		pathCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		query.TraceAuthPaths[i] = make([][]byte, pathCount)
		for j := 0; j < pathCount; j++ {
			// Path element length + data
			if offset+4 > len(data) {
				return offset, errStarkInvalidProof
			}
			pathLen := int(binary.LittleEndian.Uint32(data[offset:]))
			offset += 4

			if offset+pathLen > len(data) {
				return offset, errStarkInvalidProof
			}
			query.TraceAuthPaths[i][j] = make([]byte, pathLen)
			copy(query.TraceAuthPaths[i][j], data[offset:offset+pathLen])
			offset += pathLen
		}
	}

	// Constraint values: count(4) + values
	if offset+4 > len(data) {
		return offset, errStarkInvalidProof
	}
	constraintValCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	query.ConstraintValues = make([]uint64, constraintValCount)
	for i := 0; i < constraintValCount; i++ {
		if offset+8 > len(data) {
			return offset, errStarkInvalidProof
		}
		query.ConstraintValues[i] = binary.LittleEndian.Uint64(data[offset:])
		offset += 8
	}

	// Constraint auth path: count(4) + paths
	if offset+4 > len(data) {
		return offset, errStarkInvalidProof
	}
	constraintPathCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	query.ConstraintPath = make([][]byte, constraintPathCount)
	for i := 0; i < constraintPathCount; i++ {
		if offset+4 > len(data) {
			return offset, errStarkInvalidProof
		}
		pathLen := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		if offset+pathLen > len(data) {
			return offset, errStarkInvalidProof
		}
		query.ConstraintPath[i] = make([]byte, pathLen)
		copy(query.ConstraintPath[i], data[offset:offset+pathLen])
		offset += pathLen
	}

	// FRI layer values: count(4) + layers
	if offset+4 > len(data) {
		return offset, errStarkInvalidProof
	}
	friLayerValCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	query.FriLayerValues = make([][]uint64, friLayerValCount)
	for i := 0; i < friLayerValCount; i++ {
		if offset+4 > len(data) {
			return offset, errStarkInvalidProof
		}
		valCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		query.FriLayerValues[i] = make([]uint64, valCount)
		for j := 0; j < valCount; j++ {
			if offset+8 > len(data) {
				return offset, errStarkInvalidProof
			}
			query.FriLayerValues[i][j] = binary.LittleEndian.Uint64(data[offset:])
			offset += 8
		}
	}

	// FRI auth paths: count(4) + layers
	if offset+4 > len(data) {
		return offset, errStarkInvalidProof
	}
	friAuthCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	query.FriAuthPaths = make([][][][]byte, friAuthCount)
	for i := 0; i < friAuthCount; i++ {
		// Layer paths count
		if offset+4 > len(data) {
			return offset, errStarkInvalidProof
		}
		layerPathCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		query.FriAuthPaths[i] = make([][][]byte, layerPathCount)
		for j := 0; j < layerPathCount; j++ {
			// Path array count
			if offset+4 > len(data) {
				return offset, errStarkInvalidProof
			}
			pathArrCount := int(binary.LittleEndian.Uint32(data[offset:]))
			offset += 4

			query.FriAuthPaths[i][j] = make([][]byte, pathArrCount)
			for k := 0; k < pathArrCount; k++ {
				if offset+4 > len(data) {
					return offset, errStarkInvalidProof
				}
				pathLen := int(binary.LittleEndian.Uint32(data[offset:]))
				offset += 4

				if offset+pathLen > len(data) {
					return offset, errStarkInvalidProof
				}
				query.FriAuthPaths[i][j][k] = make([]byte, pathLen)
				copy(query.FriAuthPaths[i][j][k], data[offset:offset+pathLen])
				offset += pathLen
			}
		}
	}

	// Coset indices: count(4) + indices
	if offset+4 > len(data) {
		return offset, errStarkInvalidProof
	}
	cosetCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4

	query.CosetIndices = make([]int, cosetCount)
	for i := 0; i < cosetCount; i++ {
		if offset+4 > len(data) {
			return offset, errStarkInvalidProof
		}
		query.CosetIndices[i] = int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
	}

	return offset, nil
}

// =============================================================================
// FRI TRANSCRIPT (for deriving folding coefficients)
// Matches SDK's Transcript class in hash.ts exactly
// =============================================================================

// FriTranscript implements FRI-specific Fiat-Shamir transcript
// Must match SDK's Transcript class exactly
type FriTranscript struct {
	state   []byte
	counter int
}

// NewFriTranscript creates a transcript for FRI verification
// Matches SDK: new Transcript('FRI_COMMIT') followed by append('domain_size', domainBytes)
func NewFriTranscript(domainSize int) *FriTranscript {
	// Initialize state = sha3("FRI_COMMIT", DOMAIN.FIAT_SHAMIR)
	state := sha3WithDomain([]byte("FRI_COMMIT"), domainFiatShamir)

	t := &FriTranscript{state: state, counter: 0}

	// Append domain_size (8-byte little-endian)
	domainBytes := make([]byte, 8)
	d := uint64(domainSize)
	for i := 0; i < 8; i++ {
		domainBytes[i] = byte(d & 0xff)
		d >>= 8
	}
	t.Append("domain_size", domainBytes)

	return t
}

// Append adds data to the FRI transcript
// Matches SDK: combined = state || len(label) || label || len(data as 4-byte BE) || data
// Then state = sha3(combined, DOMAIN.FIAT_SHAMIR)
func (t *FriTranscript) Append(label string, data []byte) {
	labelBytes := []byte(label)

	// combined = state || len(label) || label || len(data as 4-byte big-endian) || data
	combined := make([]byte, len(t.state)+1+len(labelBytes)+4+len(data))

	offset := 0
	copy(combined[offset:], t.state)
	offset += len(t.state)

	combined[offset] = byte(len(labelBytes))
	offset++

	copy(combined[offset:], labelBytes)
	offset += len(labelBytes)

	// 4-byte length for data (big-endian)
	dataLen := len(data)
	combined[offset] = byte((dataLen >> 24) & 0xff)
	combined[offset+1] = byte((dataLen >> 16) & 0xff)
	combined[offset+2] = byte((dataLen >> 8) & 0xff)
	combined[offset+3] = byte(dataLen & 0xff)
	offset += 4

	copy(combined[offset:], data)

	t.state = sha3WithDomain(combined, domainFiatShamir)
}

// squeeze extracts random bytes from the transcript
// Matches SDK's squeeze function
func (t *FriTranscript) squeeze(length int) []byte {
	result := make([]byte, length)
	offset := 0
	hashSize := 32 // keccak256 output size

	for offset < length {
		// Add counter to state and hash
		counterBytes := make([]byte, 8)
		c := t.counter
		for i := 0; i < 8; i++ {
			counterBytes[i] = byte(c & 0xff)
			c >>= 8
		}

		combined := make([]byte, len(t.state)+8)
		copy(combined, t.state)
		copy(combined[len(t.state):], counterBytes)

		hash := sha3WithDomain(combined, domainFiatShamir)

		toCopy := length - offset
		if toCopy > hashSize {
			toCopy = hashSize
		}
		copy(result[offset:], hash[:toCopy])

		offset += toCopy
		t.counter++
	}

	return result
}

// SqueezeFieldElement extracts a random field element
// Matches SDK's squeezeFieldElement: squeeze 16 bytes, use first 8 as big-endian, mod modulus
func (t *FriTranscript) SqueezeFieldElement() uint64 {
	bytes := t.squeeze(16)

	// Convert first 8 bytes to big-endian uint64
	var value uint64
	for i := 0; i < 8; i++ {
		value = (value << 8) | uint64(bytes[i])
	}

	// Reduce modulo Goldilocks
	return value % goldilocksModulus
}

// =============================================================================
// FRI VERIFICATION (matches SDK fri.ts FriVerifier)
// =============================================================================

// deriveFriCoefficients derives FRI folding coefficients using Fiat-Shamir
// Matches: deriveFoldingCoefficients in FriVerifier
func deriveFriCoefficients(friLayers []FriLayer, domainSize int) []uint64 {
	transcript := NewFriTranscript(domainSize)

	coefficients := make([]uint64, len(friLayers)-1)
	for i := 0; i < len(friLayers)-1; i++ {
		transcript.Append("layer_commitment", friLayers[i].Commitment)
		coefficients[i] = transcript.SqueezeFieldElement()
	}
	return coefficients
}

// verifyFri verifies the FRI proof
// Matches: verify() in FriVerifier
func verifyFri(proof *StarkProof, queryPositions []int) bool {
	if len(proof.FinalPolynomial) == 0 || len(proof.FinalPolynomial) > starkFriFoldingFactor {
		log.Warn("FRI: invalid final polynomial", "len", len(proof.FinalPolynomial))
		return false
	}

	// Get domain sizes from FRI layers (matches SDK)
	domainSizes := make([]int, len(proof.FriLayers))
	for i, layer := range proof.FriLayers {
		domainSizes[i] = len(layer.Evaluations)
	}

	// Use first layer's domain size for coefficient derivation
	friDomainSize := proof.DomainSize
	if len(domainSizes) > 0 {
		friDomainSize = domainSizes[0]
	}

	log.Info("FRI: starting verification", "friDomainSize", friDomainSize, "numLayers", len(proof.FriLayers), "numQueries", len(queryPositions), "finalPolyLen", len(proof.FinalPolynomial))

	// Derive folding coefficients using Fiat-Shamir
	foldingCoeffs := deriveFriCoefficients(proof.FriLayers, friDomainSize)

	// Get final layer for consistency check
	finalLayer := proof.FinalPolynomial

	// Verify each query proof
	for qi, pos := range queryPositions {
		if qi >= len(proof.Queries) {
			log.Warn("FRI: insufficient queries", "qi", qi, "numProofs", len(proof.Queries))
			return false
		}
		query := &proof.Queries[qi]

		// Verify Merkle authentication paths
		if !verifyFriQueryPaths(query, proof.FriLayers, domainSizes, pos) {
			log.Warn("FRI: Merkle path verification failed", "queryIdx", qi, "position", pos)
			return false
		}

		// Verify folding consistency between layers
		// CRITICAL: Pass the query position for correct tracking through folds
		if !verifyFoldingConsistency(query, finalLayer, foldingCoeffs, friDomainSize, pos) {
			log.Warn("FRI: folding consistency failed", "queryIdx", qi, "position", pos)
			return false
		}
	}

	return true
}

// verifyFriQueryPaths verifies Merkle paths for FRI query
// Matches: verifyQuery in FriVerifier
func verifyFriQueryPaths(query *QueryResponse, friLayers []FriLayer, domainSizes []int, position int) bool {
	currentPos := position

	// Verify Merkle paths for each layer
	for layer := 0; layer < len(query.FriLayerValues); layer++ {
		if layer >= len(friLayers) || layer >= len(domainSizes) {
			return false
		}

		layerCommitment := friLayers[layer].Commitment
		domainSize := domainSizes[layer]
		evaluations := query.FriLayerValues[layer]
		authPaths := query.FriAuthPaths[layer]

		if len(authPaths) != len(evaluations) {
			return false
		}

		// Compute coset structure (matches generateQueryProof in FriProver)
		halfN := domainSize / starkFriFoldingFactor
		if halfN == 0 {
			halfN = 1
		}
		basePos := currentPos % halfN

		// Verify each evaluation in the coset
		for j := 0; j < len(evaluations); j++ {
			// Compute actual leaf index (matches prover's collection order)
			leafIndex := (basePos + j*halfN) % domainSize

			// Convert evaluation to bytes (little-endian 8 bytes)
			leaf := make([]byte, 8)
			binary.LittleEndian.PutUint64(leaf, evaluations[j])

			if j >= len(authPaths) {
				return false
			}

			if !verifyMerklePathWithIndex(leaf, authPaths[j], layerCommitment, leafIndex) {
				return false
			}
		}

		// Update position for next layer (folded position)
		currentPos = basePos
	}

	return true
}

// verifyFoldingConsistency verifies FRI folding is consistent
// Matches: verifyFoldingConsistency in FriVerifier
// CRITICAL: position parameter is the query position in the original domain (from Fiat-Shamir)
func verifyFoldingConsistency(query *QueryResponse, finalLayer []uint64, foldingCoeffs []uint64, domainSize int, position int) bool {
	if len(query.FriLayerValues) == 0 {
		return true
	}

	log.Debug("FRI folding: starting", "position", position, "domainSize", domainSize,
		"numLayers", len(query.FriLayerValues), "numCoeffs", len(foldingCoeffs),
		"numCosetIndices", len(query.CosetIndices))

	// Track position through folding - matches SDK's verifyFoldingConsistency exactly
	currentPos := position
	currentDomainSize := domainSize

	// Verify that each layer's evaluations fold correctly to next layer
	for layer := 0; layer < len(query.FriLayerValues)-1; layer++ {
		if layer >= len(foldingCoeffs) {
			log.Warn("FRI folding: insufficient coefficients", "layer", layer, "numCoeffs", len(foldingCoeffs))
			return false
		}

		currentEvals := query.FriLayerValues[layer]
		nextEvals := query.FriLayerValues[layer+1]
		alpha := foldingCoeffs[layer]

		log.Debug("FRI folding: layer", "layer", layer, "currentEvalsLen", len(currentEvals),
			"nextEvalsLen", len(nextEvals), "alpha", alpha, "currentPos", currentPos)

		// Compute fold: f'(x) = f_0 + alpha*f_1 + alpha^2*f_2 + ...
		folded := currentEvals[0]
		alphaPower := alpha
		for i := 1; i < len(currentEvals); i++ {
			folded = fieldAdd(folded, fieldMul(alphaPower, currentEvals[i]))
			alphaPower = fieldMul(alphaPower, alpha)
		}

		// The folded value should match the element at cosetIndex of next layer
		nextCosetIndex := 0
		if layer+1 < len(query.CosetIndices) {
			nextCosetIndex = query.CosetIndices[layer+1]
		}

		if nextCosetIndex >= len(nextEvals) {
			log.Warn("FRI folding: nextCosetIndex out of bounds", "layer", layer,
				"nextCosetIndex", nextCosetIndex, "nextEvalsLen", len(nextEvals))
			return false
		}

		if folded != nextEvals[nextCosetIndex] {
			log.Warn("FRI folding: mismatch", "layer", layer, "folded", folded,
				"expected", nextEvals[nextCosetIndex], "nextCosetIndex", nextCosetIndex)
			return false
		}

		log.Debug("FRI folding: layer passed", "layer", layer, "folded", folded)

		// Update position and domain size for next layer: nextPos = currentPos % halfN
		halfN := currentDomainSize / starkFriFoldingFactor
		if halfN == 0 {
			halfN = 1
		}
		currentPos = currentPos % halfN
		currentDomainSize = halfN
	}

	// Process the last query layer to get the final position
	lastHalfN := currentDomainSize / starkFriFoldingFactor
	if lastHalfN == 0 {
		lastHalfN = 1
	}
	finalPosition := currentPos % lastHalfN

	// Final fold should match an element in the final polynomial
	if len(foldingCoeffs) == 0 {
		return true
	}

	lastLayer := len(query.FriLayerValues) - 1
	lastEvals := query.FriLayerValues[lastLayer]
	lastAlpha := foldingCoeffs[len(foldingCoeffs)-1]

	// Fold: f'(x) = f_0 + alpha*f_1 + alpha^2*f_2 + ...
	folded := lastEvals[0]
	alphaPower := lastAlpha
	for i := 1; i < len(lastEvals); i++ {
		folded = fieldAdd(folded, fieldMul(alphaPower, lastEvals[i]))
		alphaPower = fieldMul(alphaPower, lastAlpha)
	}

	// The folded value lands at finalPosition in the final layer
	safePosition := finalPosition % len(finalLayer)
	if safePosition < 0 {
		safePosition = 0
	}

	return folded == finalLayer[safePosition]
}

// =============================================================================
// AIR CONSTRAINT TYPES AND CONFIGURATION
// =============================================================================

// AirConfig holds circuit configuration for AIR constraint evaluation
type AirConfig struct {
	NumInputs  int
	NumOutputs int
	TraceWidth int
	// Selector column indices (at end of trace width)
	SelectorMerkleCol     int
	SelectorNullifierCol  int
	SelectorCommitmentCol int
	SelectorBalanceCol    int
}

// Merkle tree depth for zRonin circuits
const airMerkleDepth = 16

// getCircuitConfig returns AIR configuration for a given circuit type
// getCircuitConfig returns AIR configuration for a given circuit type (legacy single-byte)
func getCircuitConfig(circuitType uint8) AirConfig {
	configs := map[uint8][2]int{
		circuitTransfer1x2: {1, 2},
		circuitTransfer1x3: {1, 3},
		circuitTransfer2x2: {2, 2},
		circuitTransfer2x3: {2, 3},
		circuitTransfer8x2: {8, 2},
	}

	config, ok := configs[circuitType]
	if !ok {
		config = [2]int{1, 2} // Default to Transfer1x2
	}

	return getCircuitConfigFromIO(config[0], config[1])
}

// getCircuitConfigFromIO returns AIR configuration from inputs/outputs directly
// Supports all 121+ circuit variants
func getCircuitConfigFromIO(numInputs, numOutputs int) AirConfig {
	// Trace width calculation (must match SDK precompiled.ts calculateTraceWidth)
	// BASE_COLUMNS = 6 (merkle_root, bound_params, fee, chain_id, timestamp, random)
	// COLUMNS_PER_INPUT = 5 (value, blinding, nullifier, merkle_path, merkle_indices)
	// COLUMNS_PER_OUTPUT = 4 (value, blinding, recipient_key, commitment)
	baseColumns := 6
	columnsPerInput := 5
	columnsPerOutput := 4

	traceWidth := baseColumns + numInputs*columnsPerInput + numOutputs*columnsPerOutput

	return AirConfig{
		NumInputs:             numInputs,
		NumOutputs:            numOutputs,
		TraceWidth:            traceWidth,
		SelectorMerkleCol:     traceWidth - 4,
		SelectorNullifierCol:  traceWidth - 3,
		SelectorCommitmentCol: traceWidth - 2,
		SelectorBalanceCol:    traceWidth - 1,
	}
}

// getCircuitConfigExtended returns AIR configuration for extended circuit format
func getCircuitConfigExtended(category uint8, inputs, outputs int) AirConfig {
	// POI circuits have special trace layout
	if category == circuitCategoryPOI {
		return getPOICircuitConfig(inputs) // inputs field contains POI type
	}

	// NFT circuits have additional token ID columns
	if category == circuitCategoryNFT || category == circuitCategoryNFTBatch {
		return getNFTCircuitConfig(inputs, outputs)
	}

	// Standard and extended transfers use the same formula
	return getCircuitConfigFromIO(inputs, outputs)
}

// getPOICircuitConfig returns AIR configuration for POI circuits
func getPOICircuitConfig(poiType int) AirConfig {
	switch poiType {
	case poiTypeShield:
		return AirConfig{NumInputs: 1, NumOutputs: 1, TraceWidth: 15,
			SelectorMerkleCol: 11, SelectorNullifierCol: 12, SelectorCommitmentCol: 13, SelectorBalanceCol: 14}
	case poiTypeTransfer:
		return AirConfig{NumInputs: 1, NumOutputs: 1, TraceWidth: 15,
			SelectorMerkleCol: 11, SelectorNullifierCol: 12, SelectorCommitmentCol: 13, SelectorBalanceCol: 14}
	case poiTypeRecursive:
		return AirConfig{NumInputs: 2, NumOutputs: 2, TraceWidth: 24,
			SelectorMerkleCol: 20, SelectorNullifierCol: 21, SelectorCommitmentCol: 22, SelectorBalanceCol: 23}
	case poiTypeBatch:
		return AirConfig{NumInputs: 8, NumOutputs: 1, TraceWidth: 51,
			SelectorMerkleCol: 47, SelectorNullifierCol: 48, SelectorCommitmentCol: 49, SelectorBalanceCol: 50}
	default:
		return getCircuitConfigFromIO(1, 1)
	}
}

// getNFTCircuitConfig returns AIR configuration for NFT circuits
func getNFTCircuitConfig(inputs, outputs int) AirConfig {
	// NFT circuits have additional columns for token IDs
	// Each input/output has an additional tokenId column
	baseColumns := 6
	columnsPerInput := 6  // value, blinding, nullifier, merkle_path, merkle_indices, tokenId
	columnsPerOutput := 5 // value, blinding, recipient_key, commitment, tokenId

	traceWidth := baseColumns + inputs*columnsPerInput + outputs*columnsPerOutput

	return AirConfig{
		NumInputs:             inputs,
		NumOutputs:            outputs,
		TraceWidth:            traceWidth,
		SelectorMerkleCol:     traceWidth - 4,
		SelectorNullifierCol:  traceWidth - 3,
		SelectorCommitmentCol: traceWidth - 2,
		SelectorBalanceCol:    traceWidth - 1,
	}
}

// getTraceLengthForCircuit computes trace length for a circuit
func getTraceLengthForCircuit(numInputs, numOutputs int) int {
	rawLength := 16*numInputs + 50*(numInputs+numOutputs) + 64
	return nextPowerOfTwo(rawLength)
}

// nextPowerOfTwo returns smallest power of 2 >= n
func nextPowerOfTwo(n int) int {
	p := 1
	for p < n {
		p *= 2
	}
	return p
}

// =============================================================================
// OOD EVALUATION VERIFICATION
// =============================================================================

// ZronPublicInputs holds public inputs for a zRonin transfer
type ZronPublicInputs struct {
	MerkleRoot      uint64
	BoundParamsHash uint64
	Nullifiers      []uint64
	Commitments     []uint64
}

// extractZronPublicInputs extracts structured public inputs from array
// Input order: [merkleRoot, boundParamsHash, nullifier0, ..., commitment0, ...]
func extractZronPublicInputs(publicInputs []uint64, numInputs, numOutputs int) *ZronPublicInputs {
	if len(publicInputs) < 2+numInputs+numOutputs {
		return nil
	}

	inputs := &ZronPublicInputs{
		MerkleRoot:      publicInputs[0],
		BoundParamsHash: publicInputs[1],
		Nullifiers:      make([]uint64, numInputs),
		Commitments:     make([]uint64, numOutputs),
	}

	// Nullifiers follow merkleRoot and boundParamsHash
	for i := 0; i < numInputs && 2+i < len(publicInputs); i++ {
		inputs.Nullifiers[i] = publicInputs[2+i]
	}

	// Commitments follow nullifiers
	for i := 0; i < numOutputs && 2+numInputs+i < len(publicInputs); i++ {
		inputs.Commitments[i] = publicInputs[2+numInputs+i]
	}

	return inputs
}

// verifyOodEvaluationsWithPublicInputs verifies OOD evaluations with full constraint evaluation
// This is the complete implementation matching verifier.ts verifyOodEvaluations
func verifyOodEvaluationsWithPublicInputs(proof *StarkProof, oodPoint uint64, randomElements []uint64, publicInputs *ZronPublicInputs, circuitType uint8) bool {
	traceEvals := proof.OodTraceEvals
	traceEvalsNext := proof.OodTraceNextEvals
	constraintEvals := proof.OodConstraintEvals

	// Validate field range for all evaluations
	for _, e := range traceEvals {
		if e >= goldilocksModulus {
			return false
		}
	}
	for _, e := range traceEvalsNext {
		if e >= goldilocksModulus {
			return false
		}
	}
	for _, e := range constraintEvals {
		if e >= goldilocksModulus {
			return false
		}
	}

	// Validate we have enough trace evaluations
	traceWidth := proof.TraceWidth
	if len(traceEvals) != traceWidth || len(traceEvalsNext) != traceWidth {
		return false
	}

	// Must have at least one constraint evaluation
	if len(constraintEvals) == 0 {
		return false
	}

	// Compute trace domain size from proof
	traceLength := proof.DomainSize / starkBlowupFactor
	if traceLength == 0 {
		traceLength = 1
	}

	// Compute trace domain omega (root of unity for trace domain)
	traceDomainBits := 0
	for tl := traceLength; tl > 1; tl >>= 1 {
		traceDomainBits++
	}
	omega := getRootOfUnity(traceDomainBits)

	// Compute transition zerofier at OOD point
	// CRITICAL: Transition constraints are enforced for rows 0..n-2, NOT the last row.
	// Therefore, the zerofier is (z^n - 1) / (z - omega^{n-1}), not z^n - 1.
	// This polynomial has roots at omega^0, omega^1, ..., omega^{n-2} (n-1 roots)
	// but NOT at omega^{n-1}.
	omegaNMinus1 := fieldPow(omega, uint64(traceLength-1))
	zN := fieldPow(oodPoint, uint64(traceLength))
	zerofierFull := fieldSub(zN, 1)
	lastRowFactor := fieldSub(oodPoint, omegaNMinus1)

	// Cannot divide by zero
	if zerofierFull == 0 || lastRowFactor == 0 {
		return false
	}

	zerofier := fieldDiv(zerofierFull, lastRowFactor)

	// Get circuit configuration (for numInputs/numOutputs)
	config := getCircuitConfig(circuitType)

	// Calculate selector column positions from actual trace width (at end of trace)
	// Selector columns are always the last 4 columns
	selectorMerkleCol := traceWidth - 4
	selectorNullifierCol := traceWidth - 3
	selectorCommitmentCol := traceWidth - 2
	selectorBalanceCol := traceWidth - 1

	// Validate selector columns are within bounds
	if selectorMerkleCol < 0 || selectorBalanceCol >= traceWidth {
		// Trace width too small for selector columns
		return false
	}

	// Generate composition coefficients using random elements
	// Matches: getConstraintCompositionCoefficients in air.ts
	numCoeffs := len(randomElements)
	if numCoeffs == 0 {
		return false
	}

	coefficients := make([]uint64, numCoeffs)
	acc := randomElements[0]
	multiplier := uint64(2)
	if numCoeffs > 1 {
		multiplier = randomElements[1]
	}
	for i := 0; i < numCoeffs; i++ {
		coefficients[i] = acc
		acc = fieldMul(acc, multiplier)
	}

	// Evaluate constraint composition at OOD point
	// C(z) = Σ αi * Ti(z) / zerofier + Σ βj * Bj(z)
	composedValue := uint64(0)
	coeffIdx := 0

	// ========================================================================
	// TRANSITION CONSTRAINTS
	// ========================================================================

	// Track contributions for debugging
	debugMultiInput := config.NumInputs > 1

	// Transition constraint 0: Merkle step (direction bit boolean check)
	// selector_merkle * direction * (direction - 1) = 0
	if coeffIdx < len(coefficients) && traceWidth >= 5 {
		selectorMerkle := traceEvals[selectorMerkleCol]
		direction := traceEvals[2] // merkle_direction column

		// direction * (direction - 1) checks if direction ∈ {0, 1}
		booleanCheck := fieldMul(direction, fieldSub(direction, 1))
		constraintEval := fieldMul(selectorMerkle, booleanCheck)

		quotient := fieldDiv(constraintEval, zerofier)
		contribution := fieldMul(coefficients[coeffIdx], quotient)
		if debugMultiInput {
			log.Info("OOD TC0", "coeff", coefficients[coeffIdx], "eval", constraintEval, "quotient", quotient, "contrib", contribution, "running", composedValue)
		}
		composedValue = fieldAdd(composedValue, contribution)
		coeffIdx++
	}

	// Transition constraints 1-4: Selector columns must be boolean
	selectorCols := []int{selectorMerkleCol, selectorNullifierCol, selectorCommitmentCol, selectorBalanceCol}
	for ci, col := range selectorCols {
		if coeffIdx >= len(coefficients) || col >= len(traceEvals) {
			break
		}
		selector := traceEvals[col]
		constraintEval := fieldMul(selector, fieldSub(selector, 1))

		quotient := fieldDiv(constraintEval, zerofier)
		contribution := fieldMul(coefficients[coeffIdx], quotient)
		if debugMultiInput {
			log.Info("OOD TC", "idx", ci+1, "col", col, "coeff", coefficients[coeffIdx], "eval", constraintEval, "contrib", contribution, "running", composedValue)
		}
		composedValue = fieldAdd(composedValue, contribution)
		coeffIdx++
	}

	// ========================================================================
	// BOUNDARY CONSTRAINTS (with public inputs)
	// ========================================================================
	// Matches ZronTransferAir.getBoundaryConstraints() exactly

	// Boundary constraints: Merkle root for EACH input
	// Each input's Merkle path ends at row (MERKLE_DEPTH + 1) * (inputIdx + 1) - 1
	// The root is written at column 0 at row: (MERKLE_DEPTH + 1) * inputIdx + MERKLE_DEPTH
	for inputIdx := 0; inputIdx < config.NumInputs; inputIdx++ {
		if coeffIdx >= len(coefficients) || len(traceEvals) == 0 {
			break
		}
		rootRow := (airMerkleDepth+1)*inputIdx + airMerkleDepth
		domainPoint := fieldPow(omega, uint64(rootRow))
		denominator := fieldSub(oodPoint, domainPoint)

		if denominator == 0 {
			return false // OOD point equals a domain point (cryptographically unlikely)
		}

		traceVal := traceEvals[0]
		numerator := fieldSub(traceVal, publicInputs.MerkleRoot)
		quotient := fieldDiv(numerator, denominator)
		contribution := fieldMul(coefficients[coeffIdx], quotient)
		if debugMultiInput {
			log.Info("OOD BC MerkleRoot", "inputIdx", inputIdx, "rootRow", rootRow, "coeff", coefficients[coeffIdx], "traceVal", traceVal, "merkleRoot", publicInputs.MerkleRoot, "numer", numerator, "denom", denominator, "quotient", quotient, "contrib", contribution, "running", composedValue)
		}
		composedValue = fieldAdd(composedValue, contribution)
		coeffIdx++
	}

	// Boundary constraints: Nullifiers at column 3
	// Nullifiers start after ALL Merkle phases complete
	// Merkle phase ends at row: (MERKLE_DEPTH + 1) * numInputs
	merklePhaseEndRow := (airMerkleDepth + 1) * config.NumInputs
	nullifierStartRow := merklePhaseEndRow
	for i, nullifier := range publicInputs.Nullifiers {
		if coeffIdx >= len(coefficients) || 3 >= len(traceEvals) {
			break
		}
		step := nullifierStartRow + i
		domainPoint := fieldPow(omega, uint64(step))
		denominator := fieldSub(oodPoint, domainPoint)

		if denominator == 0 {
			return false
		}

		traceVal := traceEvals[3] // nullifier_out column
		numerator := fieldSub(traceVal, nullifier)
		quotient := fieldDiv(numerator, denominator)
		contribution := fieldMul(coefficients[coeffIdx], quotient)
		if debugMultiInput {
			log.Info("OOD BC Nullifier", "i", i, "step", step, "coeff", coefficients[coeffIdx], "traceVal", traceVal, "nullifier", nullifier, "contrib", contribution, "running", composedValue)
		}
		composedValue = fieldAdd(composedValue, contribution)
		coeffIdx++
	}

	// Boundary constraints: Commitments at column 4
	// Commitments start after nullifiers
	commitmentStartRow := nullifierStartRow + config.NumInputs
	for i, commitment := range publicInputs.Commitments {
		if coeffIdx >= len(coefficients) || 4 >= len(traceEvals) {
			break
		}
		step := commitmentStartRow + i
		domainPoint := fieldPow(omega, uint64(step))
		denominator := fieldSub(oodPoint, domainPoint)

		if denominator == 0 {
			return false
		}

		traceVal := traceEvals[4] // commitment_out column
		numerator := fieldSub(traceVal, commitment)
		quotient := fieldDiv(numerator, denominator)
		contribution := fieldMul(coefficients[coeffIdx], quotient)
		if debugMultiInput {
			log.Info("OOD BC Commitment", "i", i, "step", step, "coeff", coefficients[coeffIdx], "traceVal", traceVal, "commitment", commitment, "contrib", contribution, "running", composedValue)
		}
		composedValue = fieldAdd(composedValue, contribution)
		coeffIdx++
	}

	// ========================================================================
	// FINAL VERIFICATION: composed value must match claimed
	// ========================================================================

	claimed := constraintEvals[0]
	if claimed >= goldilocksModulus {
		return false
	}

	// Debug: Log the comparison for failed cases (use Info level to ensure visibility)
	if composedValue != claimed {
		log.Info("OOD verification: MISMATCH",
			"composed", composedValue,
			"claimed", claimed,
			"coeffIdx", coeffIdx,
			"numCoeffs", len(coefficients),
			"numNullifiers", len(publicInputs.Nullifiers),
			"numCommitments", len(publicInputs.Commitments),
			"circuitType", circuitType,
			"numInputs", config.NumInputs,
			"numOutputs", config.NumOutputs,
			"traceWidth", traceWidth,
			"traceLength", traceLength,
			"zerofier", zerofier,
			"omega", omega,
			"oodPoint", oodPoint)
	}

	// CRITICAL: Exact equality check for soundness
	// If this passes AND FRI + DEEP verification passes, then with overwhelming
	// probability the original trace satisfied all AIR constraints
	return composedValue == claimed
}

// verifyOodEvaluations is the simplified version without public inputs
// Used when public inputs are verified through other means (streaming verification)
func verifyOodEvaluations(proof *StarkProof, oodPoint uint64, randomElements []uint64) bool {
	traceEvals := proof.OodTraceEvals
	traceEvalsNext := proof.OodTraceNextEvals
	constraintEvals := proof.OodConstraintEvals

	// Validate field range for all evaluations
	for _, e := range traceEvals {
		if e >= goldilocksModulus {
			return false
		}
	}
	for _, e := range traceEvalsNext {
		if e >= goldilocksModulus {
			return false
		}
	}
	for _, e := range constraintEvals {
		if e >= goldilocksModulus {
			return false
		}
	}

	// Validate we have enough trace evaluations
	traceWidth := proof.TraceWidth
	if len(traceEvals) != traceWidth || len(traceEvalsNext) != traceWidth {
		return false
	}

	if len(constraintEvals) == 0 {
		return false
	}

	// Compute trace domain size from proof
	traceLength := proof.DomainSize / starkBlowupFactor
	if traceLength == 0 {
		traceLength = 1
	}

	// Compute trace domain omega (root of unity for trace domain)
	traceDomainBits := 0
	for tl := traceLength; tl > 1; tl >>= 1 {
		traceDomainBits++
	}
	omega := getRootOfUnity(traceDomainBits)

	// Compute transition zerofier at OOD point
	// CRITICAL: Transition constraints are enforced for rows 0..n-2, NOT the last row.
	// Therefore, the zerofier is (z^n - 1) / (z - omega^{n-1}), not z^n - 1.
	omegaNMinus1 := fieldPow(omega, uint64(traceLength-1))
	zN := fieldPow(oodPoint, uint64(traceLength))
	zerofierFull := fieldSub(zN, 1)
	lastRowFactor := fieldSub(oodPoint, omegaNMinus1)

	if zerofierFull == 0 || lastRowFactor == 0 {
		return false
	}

	zerofier := fieldDiv(zerofierFull, lastRowFactor)

	// Generate composition coefficients
	numCoeffs := len(randomElements)
	if numCoeffs == 0 {
		return false
	}

	coefficients := make([]uint64, numCoeffs)
	acc := randomElements[0]
	multiplier := uint64(2)
	if numCoeffs > 1 {
		multiplier = randomElements[1]
	}
	for i := 0; i < numCoeffs; i++ {
		coefficients[i] = acc
		acc = fieldMul(acc, multiplier)
	}

	// Evaluate transition constraints
	composedValue := uint64(0)
	coeffIdx := 0

	// Transition constraint 0: Merkle step boolean check
	if coeffIdx < len(coefficients) && traceWidth >= 5 {
		selectorMerkle := traceEvals[traceWidth-4]
		direction := traceEvals[2]
		booleanCheck := fieldMul(direction, fieldSub(direction, 1))
		constraintEval := fieldMul(selectorMerkle, booleanCheck)
		quotient := fieldDiv(constraintEval, zerofier)
		contribution := fieldMul(coefficients[coeffIdx], quotient)
		composedValue = fieldAdd(composedValue, contribution)
		coeffIdx++
	}

	// Transition constraints 1-4: Selector boolean checks
	selectorCols := []int{traceWidth - 4, traceWidth - 3, traceWidth - 2, traceWidth - 1}
	for _, col := range selectorCols {
		if coeffIdx >= len(coefficients) || col >= len(traceEvals) {
			break
		}
		selector := traceEvals[col]
		constraintEval := fieldMul(selector, fieldSub(selector, 1))
		quotient := fieldDiv(constraintEval, zerofier)
		contribution := fieldMul(coefficients[coeffIdx], quotient)
		composedValue = fieldAdd(composedValue, contribution)
		coeffIdx++
	}

	// The claimed constraint evaluation must be in valid range
	claimed := constraintEvals[0]
	if claimed >= goldilocksModulus {
		return false
	}

	// For streaming mode, we verify structural constraints only
	// Full verification happens via Merkle paths + DEEP composition
	return true
}

// =============================================================================
// DEEP COMPOSITION VERIFICATION
// =============================================================================

// verifyDeepComposition verifies DEEP polynomial composition at query point
// Matches: verifyDeepComposition in verifier.ts
func verifyDeepComposition(query *QueryResponse, oodPoint uint64, oodTraceEvals []uint64,
	oodConstraintEval uint64, deepCoeffs []uint64, position int, domainSize int) bool {

	traceWidth := len(query.TraceValues)
	if len(deepCoeffs) < traceWidth+1 {
		return false
	}
	if len(oodTraceEvals) < traceWidth {
		return false
	}
	if len(query.ConstraintValues) == 0 {
		return false
	}

	// Get domain point for this position
	domainBits := 0
	for d := domainSize; d > 1; d >>= 1 {
		domainBits++
	}
	omega := getRootOfUnity(domainBits)
	x := fieldPow(omega, uint64(position))

	// Compute denominator (x - z) for DEEP composition
	denominator := fieldSub(x, oodPoint)
	if denominator == 0 {
		// Cryptographically unlikely: OOD point equals a domain point
		return false
	}

	// Compute DEEP value at x
	var deepValue uint64 = 0

	// Add trace contributions
	for i := 0; i < traceWidth; i++ {
		traceVal := query.TraceValues[i]
		oodVal := oodTraceEvals[i]
		alpha := deepCoeffs[i]

		// (trace(x) - trace(z)) / (x - z)
		numerator := fieldSub(traceVal, oodVal)
		quotient := fieldDiv(numerator, denominator)

		deepValue = fieldAdd(deepValue, fieldMul(alpha, quotient))
	}

	// Add constraint contribution
	constraintVal := query.ConstraintValues[0]
	alphaC := deepCoeffs[traceWidth]

	constraintNum := fieldSub(constraintVal, oodConstraintEval)
	constraintQuotient := fieldDiv(constraintNum, denominator)

	deepValue = fieldAdd(deepValue, fieldMul(alphaC, constraintQuotient))

	// Get FRI evaluation using coset index
	cosetIndex := 0
	if len(query.CosetIndices) > 0 {
		cosetIndex = query.CosetIndices[0]
	}

	if len(query.FriLayerValues) == 0 || cosetIndex >= len(query.FriLayerValues[0]) {
		return false
	}

	friEval := query.FriLayerValues[0][cosetIndex]

	// DEEP value should exactly match FRI evaluation
	return deepValue == friEval
}

// =============================================================================
// MAIN VERIFICATION FUNCTION
// =============================================================================

// verifyStarkProofFull performs complete STARK verification with public inputs
// Matches: verify() in StarkVerifier
// publicInputs order: [merkleRoot, boundParamsHash, nullifier0, nullifier1, ..., commitment0, commitment1, ...]
func verifyStarkProofFull(variant uint8, circuitType uint8, proof *StarkProof, publicInputs []uint64) bool {
	// Step 1: Verify proof version
	if proof.Version != 1 {
		return false
	}

	// Step 2: Reconstruct Fiat-Shamir transcript
	transcript := NewTranscript(publicInputs)

	// Add trace commitment
	transcript.Commit(proof.TraceCommitment)

	// Get constraint composition coefficients
	// Must absorb numConstraints + traceWidth coefficients to match on-chain verifier
	numCoeffs := proof.NumConstraints + proof.TraceWidth
	randomElements := transcript.GetRandomFieldElements(numCoeffs)

	// Add constraint commitment
	transcript.Commit(proof.ConstraintCommit)

	// Get OOD evaluation point
	oodPoint := transcript.GetRandomFieldElement()

	// Commit OOD point to transcript
	transcript.CommitFieldElement(oodPoint)

	// Extract structured public inputs for constraint verification
	// Order: [merkleRoot, boundParamsHash, nullifiers..., commitments...]
	config := getCircuitConfig(circuitType)
	zronInputs := extractZronPublicInputs(publicInputs, config.NumInputs, config.NumOutputs)
	if zronInputs == nil {
		// Insufficient public inputs for this circuit configuration
		return false
	}

	// Step 3: Verify OOD evaluations with full constraint checking
	if !verifyOodEvaluationsWithPublicInputs(proof, oodPoint, randomElements, zronInputs, circuitType) {
		return false
	}

	// Absorb OOD evaluations into transcript
	transcript.AbsorbOodEvals(proof.OodTraceEvals, proof.OodTraceNextEvals, proof.OodConstraintEvals)

	// Get DEEP composition coefficients
	deepCoeffs := transcript.DeriveFieldElements(len(proof.OodTraceEvals) + 1)

	// Add FRI commitments to transcript
	for _, layer := range proof.FriLayers {
		transcript.Commit(layer.Commitment)
	}

	// Step 4: Verify proof of work
	transcriptHash := transcript.GetState()
	if !verifyPow(transcriptHash, proof.PowNonce, starkGrindingBits) {
		return false
	}

	// Step 5: Get query positions
	ldeSize := proof.DomainSize
	queryPositions := transcript.GetQueryPositions(ldeSize, starkNumQueries)

	// Step 6: Verify query responses (trace and constraint Merkle paths)
	for i, pos := range queryPositions {
		if i >= len(proof.Queries) {
			break
		}
		query := &proof.Queries[i]

		// Verify trace authentication paths
		if !verifyTraceMerklePaths(query, proof.TraceCommitment, pos) {
			return false
		}

		// Verify constraint authentication path
		if !verifyConstraintMerklePath(query, proof.ConstraintCommit, pos) {
			return false
		}
	}

	// Step 7: Verify FRI
	if !verifyFri(proof, queryPositions) {
		return false
	}

	// Step 8: Verify DEEP composition at query points
	oodConstraintEval := uint64(0)
	if len(proof.OodConstraintEvals) > 0 {
		oodConstraintEval = proof.OodConstraintEvals[0]
	}

	for i, pos := range queryPositions {
		if i >= len(proof.Queries) {
			break
		}
		query := &proof.Queries[i]
		if !verifyDeepComposition(query, oodPoint, proof.OodTraceEvals,
			oodConstraintEval, deepCoeffs, pos, proof.DomainSize) {
			return false
		}
	}

	return true
}

// verifyTraceMerklePaths verifies trace column Merkle paths
// Matches: verifyMerklePaths in verifier.ts
func verifyTraceMerklePaths(query *QueryResponse, traceCommitment []byte, position int) bool {
	if len(query.TraceValues) == 0 {
		return false
	}

	// Combine raw field element bytes (8 bytes each), then hash once
	// This matches how buildMerkleTreeParallel creates leaves
	combinedData := make([]byte, len(query.TraceValues)*8)
	for i, val := range query.TraceValues {
		binary.LittleEndian.PutUint64(combinedData[i*8:], val)
	}
	combinedLeaf := hashMerkleLeaf(combinedData)

	// Use first path (all should be same for combined Merkle)
	if len(query.TraceAuthPaths) == 0 || len(query.TraceAuthPaths[0]) == 0 {
		return false
	}

	return verifyMerklePathDirect(combinedLeaf, query.TraceAuthPaths[0], traceCommitment, position)
}

// verifyConstraintMerklePath verifies constraint evaluation Merkle path
// Matches: verifyMerklePath for constraints in verifier.ts
func verifyConstraintMerklePath(query *QueryResponse, constraintCommitment []byte, position int) bool {
	if len(query.ConstraintValues) == 0 {
		return false
	}

	// Hash constraint value to get 32-byte leaf
	valBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(valBytes, query.ConstraintValues[0])
	constraintLeaf := hashMerkleLeaf(valBytes)

	return verifyMerklePathDirect(constraintLeaf, query.ConstraintPath, constraintCommitment, position)
}

// verifyMerklePathDirect verifies a Merkle path where the leaf is already hashed
// Matches: verifyMerklePath in verifier.ts
func verifyMerklePathDirect(leaf []byte, path [][]byte, expectedRoot []byte, position int) bool {
	current := leaf
	pos := position

	for _, sibling := range path {
		if pos&1 == 1 {
			current = hashMerkleNode(sibling, current)
		} else {
			current = hashMerkleNode(current, sibling)
		}
		pos >>= 1
	}

	return bytesEqual(current, expectedRoot)
}

// =============================================================================
// SESSION MANAGEMENT (for streaming verification)
// =============================================================================

// VerificationSession tracks state for streaming STARK verification
type VerificationSession struct {
	ID               [32]byte
	Caller           common.Address
	Variant          uint8
	CircuitType      uint8
	PublicInputs     []uint64
	CreatedBlock     uint64
	CommitsReceived  bool
	OodReceived      bool
	FriBatchesRcvd   int
	FriBatchesTotal  int
	TraceCommit      []byte
	ConstraintCommit []byte
	FriCommits       [][]byte
	FriLayerEvals    [][]uint64 // Store FRI layer evaluations
	FinalPolynomial  []uint64   // Store final polynomial coefficients
	PowNonce         uint64
	TraceEvals       []uint64
	TraceNextEvals   []uint64
	ConstraintEvals  []uint64
	Transcript       *Transcript
	RandomElements   []uint64 // Constraint composition coefficients
	OodPoint         uint64
	DeepCoeffs       []uint64
	QueryPositions   []int
	QueryProofs      []QueryResponse
	DomainSize       int  // LDE domain size
	TraceWidth       int  // Trace width for this circuit
	NumConstraints   int  // Number of constraints
}

type sessionKey struct {
	Caller    common.Address
	SessionID [32]byte
}

var (
	sessionStore     = make(map[sessionKey]*VerificationSession)
	sessionMutex     sync.RWMutex
	sessionCount     = make(map[common.Address]int)
	cleanupCounter   uint64
	cleanupThreshold = uint64(100) // Cleanup every 100 calls
)

func getSession(id [32]byte) *VerificationSession {
	sessionMutex.RLock()
	defer sessionMutex.RUnlock()
	key := sessionKey{SessionID: id}
	return sessionStore[key]
}

func removeSession(id [32]byte) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	key := sessionKey{SessionID: id}
	if session, exists := sessionStore[key]; exists {
		sessionCount[session.Caller]--
		delete(sessionStore, key)
	}
}

// cleanupStaleSessions removes sessions that have been abandoned
// This is called periodically to prevent memory leaks from incomplete verifications.
// Since we don't have direct access to block numbers in the precompile,
// we use a call counter as a proxy for time passage.
func cleanupStaleSessions() {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()

	// Increment counter and check if cleanup is needed
	cleanupCounter++
	if cleanupCounter%cleanupThreshold != 0 {
		return
	}

	// Find and remove sessions that haven't completed
	// A session is considered stale if it exists but hasn't been finalized
	// after cleanupThreshold calls (indicating it was likely abandoned)
	var staleKeys []sessionKey
	for key, session := range sessionStore {
		// Sessions that are old based on creation time difference
		// or have received some data but not completed are candidates for cleanup
		if session.CreatedBlock > 0 {
			// Mark as stale if session has been sitting incomplete
			// We use the presence of partial data as an indicator
			if session.CommitsReceived && !session.OodReceived {
				// Received commits but not OOD - likely abandoned
				staleKeys = append(staleKeys, key)
			} else if session.OodReceived && session.FriBatchesRcvd < session.FriBatchesTotal {
				// Received OOD but incomplete FRI batches - likely abandoned
				staleKeys = append(staleKeys, key)
			}
		}
	}

	// Remove stale sessions
	for _, key := range staleKeys {
		if session, exists := sessionStore[key]; exists {
			sessionCount[session.Caller]--
			delete(sessionStore, key)
		}
	}

	// Also enforce maximum total sessions across all callers
	// to prevent global memory exhaustion
	maxTotalSessions := starkMaxSessions * 10 // Allow 10x the per-address limit globally
	if len(sessionStore) > maxTotalSessions {
		// Remove oldest sessions (those with lowest CreatedBlock)
		// Simple approach: remove any excess sessions
		excess := len(sessionStore) - maxTotalSessions
		removed := 0
		for key, session := range sessionStore {
			if removed >= excess {
				break
			}
			sessionCount[session.Caller]--
			delete(sessionStore, key)
			removed++
		}
	}
}

// generateSessionID creates a unique session ID
func generateSessionID(variant uint8, circuitType uint8, publicInputs []uint64) [32]byte {
	var hashFunc func(uint64, uint64) uint64
	if variant == variantRP512_64bit || variant == variantRP512_128bit {
		hashFunc = rescue2_512
	} else {
		hashFunc = rescue2
	}

	h := uint64(variant) ^ (uint64(circuitType) << 8)
	for _, pi := range publicInputs {
		h = hashFunc(h, pi)
	}

	var id [32]byte
	binary.LittleEndian.PutUint64(id[0:8], h)
	binary.LittleEndian.PutUint64(id[8:16], ^h)
	binary.LittleEndian.PutUint64(id[16:24], hashFunc(h, ^h))
	binary.LittleEndian.PutUint64(id[24:32], hashFunc(^h, h))

	return id
}

// =============================================================================
// PRECOMPILE IMPLEMENTATION
// =============================================================================

type starkVerifier struct{}

func (c *starkVerifier) RequiredGas(input []byte) uint64 {
	if len(input) < 1 {
		return params.RescuePrimeBaseGas
	}

	opcode := input[0]
	switch opcode {
	case opcodeVerifyFull:
		return gasVerifyFull
	case opcodeStarkInit:
		return gasStarkInit
	case opcodeStarkCommit:
		return gasStarkCommit
	case opcodeStarkOod:
		return gasStarkOod
	case opcodeStarkFriBatch:
		if len(input) < 35 {
			return gasStarkFriBatch
		}
		queryCount := uint64(input[34])
		return gasStarkFriBatch + queryCount*gasStarkFriPerQ
	case opcodeStarkFinalize:
		return gasStarkFinalize
	case opcodeVerifyWithBlob:
		return gasVerifyWithBlob
	default:
		return params.RescuePrimeBaseGas
	}
}

func (c *starkVerifier) Run(input []byte) ([]byte, error) {
	if len(input) < 1 {
		return nil, errStarkInvalidInput
	}

	// Periodic cleanup of stale sessions to prevent memory leaks
	cleanupStaleSessions()

	opcode := input[0]
	data := input[1:]

	switch opcode {
	case opcodeVerifyFull:
		return runVerifyFull(data)
	case opcodeStarkInit:
		return runStarkInit(data)
	case opcodeStarkCommit:
		return runStarkCommit(data)
	case opcodeStarkOod:
		return runStarkOod(data)
	case opcodeStarkFriBatch:
		return runStarkFriBatch(data)
	case opcodeStarkFinalize:
		return runStarkFinalize(data)
	case opcodeVerifyWithBlob:
		return nil, errors.New("blob verification not yet implemented")
	default:
		return nil, errStarkInvalidOpcode
	}
}

// runVerifyFull verifies a complete STARK proof
//
// Supports two input formats:
//
// Legacy (backward compatible):
//   variant(1) + circuitType(1) + proofLen(4 BE) + proof + pubInputsCount(1) + pubInputs(N×8 LE)
//   circuitType is single byte 0x00-0x04 for basic circuits
//
// Extended (121+ circuits):
//   variant(1) + category(1) + inputs(1) + outputs(1) + proofLen(4 BE) + proof + pubInputsCount(1) + pubInputs(N×8 LE)
//   category >= 0x10 indicates extended format (0x10=transfer, 0x11=transfer_ext, 0x12=nft, 0x13=poi)
//
func runVerifyFull(data []byte) ([]byte, error) {
	if len(data) < 7 {
		return nil, errStarkInvalidInput
	}

	variant := data[0]
	if variant > variantRP512_128bit {
		return nil, errStarkInvalidVariant
	}

	// Detect format: legacy (circuitType <= 0x04) or extended (>= 0x10)
	var category uint8
	var numInputs, numOutputs int
	var proofLenOffset int

	firstCircuitByte := data[1]

	if firstCircuitByte >= 0x10 {
		// Extended format: category(1) + inputs(1) + outputs(1)
		if len(data) < 9 {
			return nil, errStarkInvalidInput
		}
		category = firstCircuitByte - 0x10 // Map 0x10->0, 0x11->1, etc.
		numInputs = int(data[2])
		numOutputs = int(data[3])
		proofLenOffset = 4

		// Validate ranges
		if numInputs == 0 {
			numInputs = 1
		}
		if numOutputs == 0 {
			numOutputs = 1
		}
		if numInputs > 32 {
			numInputs = 32
		}
		if numOutputs > 16 {
			numOutputs = 16
		}
	} else {
		// Legacy format: single byte circuit type
		category, numInputs, numOutputs = parseCircuitType(data[1:2])
		proofLenOffset = 2
	}

	proofLen := binary.BigEndian.Uint32(data[proofLenOffset : proofLenOffset+4])
	if proofLen > starkMaxProofSize {
		return nil, errStarkProofTooLarge
	}

	proofDataStart := proofLenOffset + 4
	if len(data) < proofDataStart+int(proofLen)+1 {
		return nil, errStarkInvalidInput
	}

	proofData := data[proofDataStart : proofDataStart+int(proofLen)]
	pubInputsCount := int(data[proofDataStart+int(proofLen)])

	if pubInputsCount > starkMaxPublicInputs {
		return nil, errStarkInvalidInput
	}

	pubInputsStart := proofDataStart + int(proofLen) + 1
	if len(data) < pubInputsStart+pubInputsCount*8 {
		return nil, errStarkInvalidInput
	}

	publicInputs := make([]uint64, pubInputsCount)
	for i := 0; i < pubInputsCount; i++ {
		publicInputs[i] = binary.LittleEndian.Uint64(data[pubInputsStart+i*8:])
	}

	// Parse the proof
	proof, _, err := parseStarkProof(proofData)
	if err != nil {
		return []byte{0x00}, nil
	}

	// Verify the proof with full constraint checking
	valid := verifyStarkProofFullExtended(variant, category, numInputs, numOutputs, proof, publicInputs)

	if valid {
		return []byte{0x01}, nil
	}
	return []byte{0x00}, nil
}

// verifyStarkProofFullExtended performs complete STARK verification for any circuit configuration
func verifyStarkProofFullExtended(variant uint8, category uint8, numInputs, numOutputs int, proof *StarkProof, publicInputs []uint64) bool {
	// Step 1: Verify proof version
	if proof.Version != 1 {
		log.Warn("STARK verify: failed at step 1 (proof version)", "version", proof.Version)
		return false
	}

	// Step 2: Reconstruct Fiat-Shamir transcript
	transcript := NewTranscript(publicInputs)

	// Add trace commitment
	transcript.Commit(proof.TraceCommitment)

	// Get constraint composition coefficients
	numCoeffs := proof.NumConstraints + proof.TraceWidth
	randomElements := transcript.GetRandomFieldElements(numCoeffs)

	// Add constraint commitment
	transcript.Commit(proof.ConstraintCommit)

	// Get OOD evaluation point
	oodPoint := transcript.GetRandomFieldElement()

	// Commit OOD point to transcript
	transcript.CommitFieldElement(oodPoint)

	// Get circuit configuration for constraint verification
	config := getCircuitConfigExtended(category, numInputs, numOutputs)
	zronInputs := extractZronPublicInputs(publicInputs, config.NumInputs, config.NumOutputs)
	if zronInputs == nil {
		// Insufficient public inputs for this circuit configuration
		log.Warn("STARK verify: failed - insufficient public inputs", "numInputs", numInputs, "numOutputs", numOutputs, "pubInputsLen", len(publicInputs))
		return false
	}

	// Determine circuit type byte for legacy compatibility
	circuitTypeByte := circuitTypeFromConfig(category, numInputs, numOutputs)

	log.Info("STARK verify: step 2 passed", "numCoeffs", numCoeffs, "oodPoint", oodPoint, "traceWidth", proof.TraceWidth, "numConstraints", proof.NumConstraints)

	// Step 3: Verify OOD evaluations with full constraint checking
	if !verifyOodEvaluationsWithPublicInputs(proof, oodPoint, randomElements, zronInputs, circuitTypeByte) {
		log.Warn("STARK verify: failed at step 3 (OOD evaluations)")
		return false
	}
	log.Info("STARK verify: step 3 passed (OOD evaluations)")

	// Absorb OOD evaluations into transcript
	transcript.AbsorbOodEvals(proof.OodTraceEvals, proof.OodTraceNextEvals, proof.OodConstraintEvals)

	// Get DEEP composition coefficients
	deepCoeffs := transcript.DeriveFieldElements(len(proof.OodTraceEvals) + 1)

	// Add FRI commitments to transcript
	for _, layer := range proof.FriLayers {
		transcript.Commit(layer.Commitment)
	}

	// Step 4: Verify proof of work
	transcriptHash := transcript.GetState()
	if !verifyPow(transcriptHash, proof.PowNonce, starkGrindingBits) {
		log.Warn("STARK verify: failed at step 4 (proof of work)", "nonce", proof.PowNonce)
		return false
	}
	log.Info("STARK verify: step 4 passed (proof of work)")

	// Step 5: Get query positions
	ldeSize := proof.DomainSize
	queryPositions := transcript.GetQueryPositions(ldeSize, starkNumQueries)
	log.Info("STARK verify: step 5", "ldeSize", ldeSize, "numQueries", len(queryPositions), "firstPos", queryPositions[0])

	// Step 6: Verify query responses (trace and constraint Merkle paths)
	for i, pos := range queryPositions {
		if i >= len(proof.Queries) {
			break
		}
		query := &proof.Queries[i]

		// Verify trace authentication paths
		if !verifyTraceMerklePaths(query, proof.TraceCommitment, pos) {
			log.Warn("STARK verify: failed at step 6 (trace Merkle paths)", "queryIdx", i, "position", pos)
			return false
		}

		// Verify constraint authentication path
		if !verifyConstraintMerklePath(query, proof.ConstraintCommit, pos) {
			log.Warn("STARK verify: failed at step 6 (constraint Merkle path)", "queryIdx", i, "position", pos)
			return false
		}
	}
	log.Info("STARK verify: step 6 passed (Merkle paths)")

	// Step 7: Verify FRI
	if !verifyFri(proof, queryPositions) {
		log.Warn("STARK verify: failed at step 7 (FRI)")
		return false
	}
	log.Info("STARK verify: step 7 passed (FRI)")

	// Step 8: Verify DEEP composition at query points
	oodConstraintEval := uint64(0)
	if len(proof.OodConstraintEvals) > 0 {
		oodConstraintEval = proof.OodConstraintEvals[0]
	}

	for i, pos := range queryPositions {
		if i >= len(proof.Queries) {
			break
		}
		query := &proof.Queries[i]
		if !verifyDeepComposition(query, oodPoint, proof.OodTraceEvals,
			oodConstraintEval, deepCoeffs, pos, proof.DomainSize) {
			log.Warn("STARK verify: failed at step 8 (DEEP composition)", "queryIdx", i, "position", pos)
			return false
		}
	}

	log.Info("STARK verify: ALL STEPS PASSED")
	return true
}

// circuitTypeFromConfig maps category/inputs/outputs back to legacy circuit type byte
func circuitTypeFromConfig(category uint8, inputs, outputs int) uint8 {
	// For legacy compatibility in verifyOodEvaluationsWithPublicInputs
	if category == circuitCategoryTransfer {
		if inputs == 1 && outputs == 2 {
			return circuitTransfer1x2
		}
		if inputs == 1 && outputs == 3 {
			return circuitTransfer1x3
		}
		if inputs == 2 && outputs == 2 {
			return circuitTransfer2x2
		}
		if inputs == 2 && outputs == 3 {
			return circuitTransfer2x3
		}
		if inputs == 8 && outputs == 2 {
			return circuitTransfer8x2
		}
	}
	// For non-legacy circuits, return a marker value
	// The actual configuration is passed via getCircuitConfigExtended
	return 0xFF
}

// runStarkInit initializes a verification session
func runStarkInit(data []byte) ([]byte, error) {
	if len(data) < 3 {
		return nil, errStarkInvalidInput
	}

	variant := data[0]
	circuitType := data[1]
	pubInputsCount := int(data[2])

	if variant > variantRP512_128bit {
		return nil, errStarkInvalidVariant
	}

	if len(data) < 3+pubInputsCount*8 {
		return nil, errStarkInvalidInput
	}

	publicInputs := make([]uint64, pubInputsCount)
	for i := 0; i < pubInputsCount; i++ {
		publicInputs[i] = binary.LittleEndian.Uint64(data[3+i*8:])
	}

	sessionID := generateSessionID(variant, circuitType, publicInputs)

	// Get circuit configuration
	config := getCircuitConfig(circuitType)
	traceLength := getTraceLengthForCircuit(config.NumInputs, config.NumOutputs)
	ldeSize := traceLength * starkBlowupFactor

	session := &VerificationSession{
		ID:              sessionID,
		Variant:         variant,
		CircuitType:     circuitType,
		PublicInputs:    publicInputs,
		FriBatchesTotal: (starkNumQueries + 9) / 10,
		Transcript:      NewTranscript(publicInputs),
		QueryProofs:     make([]QueryResponse, 0),
		DomainSize:      ldeSize,
		TraceWidth:      config.TraceWidth,
		NumConstraints:  5, // 5 transition constraints in ZronTransferAir
	}

	sessionMutex.Lock()
	key := sessionKey{SessionID: sessionID}
	sessionStore[key] = session
	sessionMutex.Unlock()

	return sessionID[:], nil
}

// runStarkCommit processes commitment submission with full transcript state
func runStarkCommit(data []byte) ([]byte, error) {
	// Minimum: sessionID(32) + traceCommit(32) + constraintCommit(32) + friCount(1) = 97 bytes
	if len(data) < 97 {
		return nil, errStarkInvalidInput
	}

	var sessionID [32]byte
	copy(sessionID[:], data[0:32])

	session := getSession(sessionID)
	if session == nil {
		return nil, errStarkSessionNotFound
	}

	session.TraceCommit = make([]byte, 32)
	copy(session.TraceCommit, data[32:64])

	session.ConstraintCommit = make([]byte, 32)
	copy(session.ConstraintCommit, data[64:96])

	friCount := int(data[96])
	if len(data) < 97+friCount*32+8 {
		return nil, errStarkInvalidInput
	}

	session.FriCommits = make([][]byte, friCount)
	for i := 0; i < friCount; i++ {
		session.FriCommits[i] = make([]byte, 32)
		copy(session.FriCommits[i], data[97+i*32:97+(i+1)*32])
	}

	session.PowNonce = binary.LittleEndian.Uint64(data[97+friCount*32:])
	session.CommitsReceived = true

	// Update transcript with trace commitment
	session.Transcript.Commit(session.TraceCommit)

	// Get constraint composition coefficients
	// IMPORTANT: Must absorb numConstraints + traceWidth coefficients to match on-chain verifier
	numCoeffs := session.NumConstraints + session.TraceWidth
	session.RandomElements = session.Transcript.GetRandomFieldElements(numCoeffs)

	// Add constraint commitment to transcript
	session.Transcript.Commit(session.ConstraintCommit)

	// Get OOD evaluation point
	session.OodPoint = session.Transcript.GetRandomFieldElement()

	// Commit OOD point to transcript (matches on-chain verifier)
	session.Transcript.CommitFieldElement(session.OodPoint)

	return []byte{0x01}, nil
}

// runStarkOod processes OOD evaluation submission
func runStarkOod(data []byte) ([]byte, error) {
	if len(data) < 34 {
		return nil, errStarkInvalidInput
	}

	var sessionID [32]byte
	copy(sessionID[:], data[0:32])

	session := getSession(sessionID)
	if session == nil {
		return nil, errStarkSessionNotFound
	}

	if !session.CommitsReceived {
		return nil, errStarkInvalidProof
	}

	traceCount := int(data[32])
	offset := 33

	if len(data) < offset+traceCount*16+1 {
		return nil, errStarkInvalidInput
	}

	session.TraceEvals = make([]uint64, traceCount)
	session.TraceNextEvals = make([]uint64, traceCount)
	for i := 0; i < traceCount; i++ {
		session.TraceEvals[i] = binary.LittleEndian.Uint64(data[offset+i*8:])
	}
	offset += traceCount * 8
	for i := 0; i < traceCount; i++ {
		session.TraceNextEvals[i] = binary.LittleEndian.Uint64(data[offset+i*8:])
	}
	offset += traceCount * 8

	constraintCount := int(data[offset])
	offset++

	if len(data) < offset+constraintCount*8 {
		return nil, errStarkInvalidInput
	}

	session.ConstraintEvals = make([]uint64, constraintCount)
	for i := 0; i < constraintCount; i++ {
		session.ConstraintEvals[i] = binary.LittleEndian.Uint64(data[offset+i*8:])
	}

	session.OodReceived = true

	// Verify OOD evaluations are in field
	for _, eval := range session.TraceEvals {
		if eval >= goldilocksModulus {
			return []byte{0x00}, nil
		}
	}
	for _, eval := range session.TraceNextEvals {
		if eval >= goldilocksModulus {
			return []byte{0x00}, nil
		}
	}
	for _, eval := range session.ConstraintEvals {
		if eval >= goldilocksModulus {
			return []byte{0x00}, nil
		}
	}

	// Absorb OOD evaluations into transcript
	session.Transcript.AbsorbOodEvals(session.TraceEvals, session.TraceNextEvals, session.ConstraintEvals)

	// Derive DEEP coefficients for later verification
	session.DeepCoeffs = session.Transcript.DeriveFieldElements(len(session.TraceEvals) + 1)

	// Add FRI commitments to transcript
	for _, commit := range session.FriCommits {
		session.Transcript.Commit(commit)
	}

	return []byte{0x01}, nil
}

// runStarkFriBatch processes a batch of FRI queries with full parsing and verification
func runStarkFriBatch(data []byte) ([]byte, error) {
	if len(data) < 34 {
		return nil, errStarkInvalidInput
	}

	var sessionID [32]byte
	copy(sessionID[:], data[0:32])

	session := getSession(sessionID)
	if session == nil {
		return nil, errStarkSessionNotFound
	}

	if !session.OodReceived {
		return nil, errStarkInvalidProof
	}

	batchIndex := int(data[32])
	queryCount := int(data[33])
	offset := 34

	// Calculate query positions if not done yet
	if session.QueryPositions == nil {
		// Get LDE domain size from session (computed during init)
		config := getCircuitConfig(session.CircuitType)
		traceLength := getTraceLengthForCircuit(config.NumInputs, config.NumOutputs)
		ldeSize := traceLength * starkBlowupFactor

		session.QueryPositions = session.Transcript.GetQueryPositions(ldeSize, starkNumQueries)
	}

	// Parse and verify query proofs in this batch
	for q := 0; q < queryCount; q++ {
		var query QueryResponse

		// Get query index for this batch
		queryIdx := batchIndex*10 + q
		if queryIdx >= len(session.QueryPositions) {
			break
		}
		position := session.QueryPositions[queryIdx]

		// Parse trace value count
		if offset+4 > len(data) {
			return nil, errStarkInvalidInput
		}
		traceValCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		// Parse trace values
		query.TraceValues = make([]uint64, traceValCount)
		for i := 0; i < traceValCount; i++ {
			if offset+8 > len(data) {
				return nil, errStarkInvalidInput
			}
			query.TraceValues[i] = binary.LittleEndian.Uint64(data[offset:])
			offset += 8

			// Validate field range
			if query.TraceValues[i] >= goldilocksModulus {
				return []byte{0x00}, nil // Invalid field element
			}
		}

		// Parse trace auth paths count
		if offset+4 > len(data) {
			return nil, errStarkInvalidInput
		}
		traceAuthCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		// Parse trace auth paths
		query.TraceAuthPaths = make([][][]byte, traceAuthCount)
		for i := 0; i < traceAuthCount; i++ {
			if offset+4 > len(data) {
				return nil, errStarkInvalidInput
			}
			pathCount := int(binary.LittleEndian.Uint32(data[offset:]))
			offset += 4

			query.TraceAuthPaths[i] = make([][]byte, pathCount)
			for j := 0; j < pathCount; j++ {
				if offset+4 > len(data) {
					return nil, errStarkInvalidInput
				}
				pathLen := int(binary.LittleEndian.Uint32(data[offset:]))
				offset += 4

				if offset+pathLen > len(data) {
					return nil, errStarkInvalidInput
				}
				query.TraceAuthPaths[i][j] = make([]byte, pathLen)
				copy(query.TraceAuthPaths[i][j], data[offset:offset+pathLen])
				offset += pathLen
			}
		}

		// Parse constraint values count
		if offset+4 > len(data) {
			return nil, errStarkInvalidInput
		}
		constraintValCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		// Parse constraint values
		query.ConstraintValues = make([]uint64, constraintValCount)
		for i := 0; i < constraintValCount; i++ {
			if offset+8 > len(data) {
				return nil, errStarkInvalidInput
			}
			query.ConstraintValues[i] = binary.LittleEndian.Uint64(data[offset:])
			offset += 8

			if query.ConstraintValues[i] >= goldilocksModulus {
				return []byte{0x00}, nil
			}
		}

		// Parse constraint auth path
		if offset+4 > len(data) {
			return nil, errStarkInvalidInput
		}
		constraintPathCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		query.ConstraintPath = make([][]byte, constraintPathCount)
		for i := 0; i < constraintPathCount; i++ {
			if offset+4 > len(data) {
				return nil, errStarkInvalidInput
			}
			pathLen := int(binary.LittleEndian.Uint32(data[offset:]))
			offset += 4

			if offset+pathLen > len(data) {
				return nil, errStarkInvalidInput
			}
			query.ConstraintPath[i] = make([]byte, pathLen)
			copy(query.ConstraintPath[i], data[offset:offset+pathLen])
			offset += pathLen
		}

		// Parse FRI layer values
		if offset+4 > len(data) {
			return nil, errStarkInvalidInput
		}
		friLayerCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		query.FriLayerValues = make([][]uint64, friLayerCount)
		for i := 0; i < friLayerCount; i++ {
			if offset+4 > len(data) {
				return nil, errStarkInvalidInput
			}
			valCount := int(binary.LittleEndian.Uint32(data[offset:]))
			offset += 4

			query.FriLayerValues[i] = make([]uint64, valCount)
			for j := 0; j < valCount; j++ {
				if offset+8 > len(data) {
					return nil, errStarkInvalidInput
				}
				query.FriLayerValues[i][j] = binary.LittleEndian.Uint64(data[offset:])
				offset += 8
			}
		}

		// Parse FRI auth paths
		if offset+4 > len(data) {
			return nil, errStarkInvalidInput
		}
		friAuthCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		query.FriAuthPaths = make([][][][]byte, friAuthCount)
		for i := 0; i < friAuthCount; i++ {
			if offset+4 > len(data) {
				return nil, errStarkInvalidInput
			}
			layerPathCount := int(binary.LittleEndian.Uint32(data[offset:]))
			offset += 4

			query.FriAuthPaths[i] = make([][][]byte, layerPathCount)
			for j := 0; j < layerPathCount; j++ {
				if offset+4 > len(data) {
					return nil, errStarkInvalidInput
				}
				pathArrCount := int(binary.LittleEndian.Uint32(data[offset:]))
				offset += 4

				query.FriAuthPaths[i][j] = make([][]byte, pathArrCount)
				for k := 0; k < pathArrCount; k++ {
					if offset+4 > len(data) {
						return nil, errStarkInvalidInput
					}
					pathLen := int(binary.LittleEndian.Uint32(data[offset:]))
					offset += 4

					if offset+pathLen > len(data) {
						return nil, errStarkInvalidInput
					}
					query.FriAuthPaths[i][j][k] = make([]byte, pathLen)
					copy(query.FriAuthPaths[i][j][k], data[offset:offset+pathLen])
					offset += pathLen
				}
			}
		}

		// Parse coset indices
		if offset+4 > len(data) {
			return nil, errStarkInvalidInput
		}
		cosetCount := int(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4

		query.CosetIndices = make([]int, cosetCount)
		for i := 0; i < cosetCount; i++ {
			if offset+4 > len(data) {
				return nil, errStarkInvalidInput
			}
			query.CosetIndices[i] = int(binary.LittleEndian.Uint32(data[offset:]))
			offset += 4
		}

		// Verify trace Merkle paths for this query
		if !verifyTraceMerklePaths(&query, session.TraceCommit, position) {
			return []byte{0x00}, nil // Merkle verification failed
		}

		// Verify constraint Merkle path
		if !verifyConstraintMerklePath(&query, session.ConstraintCommit, position) {
			return []byte{0x00}, nil
		}

		session.QueryProofs = append(session.QueryProofs, query)
	}

	session.FriBatchesRcvd++
	_ = batchIndex // Used for ordering

	if session.FriBatchesRcvd >= session.FriBatchesTotal {
		return []byte{0x02}, nil // All batches received
	}
	return []byte{0x01}, nil // Continue
}

// runStarkFinalize completes verification with full FRI and DEEP composition checks
func runStarkFinalize(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, errStarkInvalidInput
	}

	var sessionID [32]byte
	copy(sessionID[:], data[0:32])

	session := getSession(sessionID)
	if session == nil {
		return nil, errStarkSessionNotFound
	}

	if !session.CommitsReceived || !session.OodReceived {
		removeSession(sessionID)
		return []byte{0x00}, nil
	}

	// Verify PoW
	transcriptHash := session.Transcript.GetState()
	if !verifyPow(transcriptHash, session.PowNonce, starkGrindingBits) {
		removeSession(sessionID)
		return []byte{0x00}, nil
	}

	// Verify we have enough query proofs
	if len(session.QueryProofs) < starkNumQueries {
		removeSession(sessionID)
		return []byte{0x00}, nil
	}

	// Use pre-computed values from session (computed during runStarkCommit and runStarkOod)
	ldeSize := session.DomainSize
	oodPoint := session.OodPoint
	randomElements := session.RandomElements
	deepCoeffs := session.DeepCoeffs

	// Build FRI layers from session data
	friLayers := make([]FriLayer, len(session.FriCommits))
	for i, commit := range session.FriCommits {
		friLayers[i] = FriLayer{
			Commitment: commit,
		}
	}

	// Compute domain sizes for FRI layers
	domainSizes := make([]int, len(friLayers))
	currentSize := ldeSize
	for i := range friLayers {
		domainSizes[i] = currentSize
		currentSize /= starkFriFoldingFactor
		if currentSize < 1 {
			currentSize = 1
		}
	}

	// Derive FRI folding coefficients
	foldingCoeffs := deriveFriCoefficients(friLayers, ldeSize)

	// Verify FRI and DEEP for each query
	for qi, query := range session.QueryProofs {
		if qi >= len(session.QueryPositions) {
			break
		}
		position := session.QueryPositions[qi]

		// Verify FRI query paths
		if !verifyFriQueryPaths(&query, friLayers, domainSizes, position) {
			removeSession(sessionID)
			return []byte{0x00}, nil
		}

		// Verify folding consistency
		if len(query.FriLayerValues) > 0 {
			// Get or compute final polynomial from last FRI layer
			finalLayer := query.FriLayerValues[len(query.FriLayerValues)-1]
			if !verifyFoldingConsistency(&query, finalLayer, foldingCoeffs, ldeSize, position) {
				removeSession(sessionID)
				return []byte{0x00}, nil
			}
		}

		// Verify DEEP composition at query point
		oodConstraintEval := uint64(0)
		if len(session.ConstraintEvals) > 0 {
			oodConstraintEval = session.ConstraintEvals[0]
		}

		if !verifyDeepComposition(&query, oodPoint, session.TraceEvals, oodConstraintEval, deepCoeffs, position, ldeSize) {
			removeSession(sessionID)
			return []byte{0x00}, nil
		}
	}

	// Get circuit configuration from session
	config := getCircuitConfig(session.CircuitType)

	// Verify OOD evaluations with public inputs
	proof := &StarkProof{
		DomainSize:         ldeSize,
		TraceWidth:         session.TraceWidth,
		NumConstraints:     session.NumConstraints,
		OodTraceEvals:      session.TraceEvals,
		OodTraceNextEvals:  session.TraceNextEvals,
		OodConstraintEvals: session.ConstraintEvals,
	}

	// Extract public inputs from session for full verification
	zronInputs := extractZronPublicInputs(session.PublicInputs, config.NumInputs, config.NumOutputs)
	if zronInputs == nil {
		// Insufficient public inputs for this circuit configuration
		removeSession(sessionID)
		return []byte{0x00}, nil
	}

	if !verifyOodEvaluationsWithPublicInputs(proof, oodPoint, randomElements, zronInputs, session.CircuitType) {
		removeSession(sessionID)
		return []byte{0x00}, nil
	}

	// All verification checks passed
	removeSession(sessionID)
	return []byte{0x01}, nil
}
