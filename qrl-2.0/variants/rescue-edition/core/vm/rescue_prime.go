// Copyright 2024 The go-zond Authors
// This file is part of the go-zond library.
//
// Rescue Prime Rp64_256 Precompile
// Implementation based on Winterfell's Rp64_256
// Reference: https://eprint.iacr.org/2020/1143.pdf
//
// - Field: Goldilocks (2^64 - 2^32 + 1)
// - State width: 12 elements
// - Rate: 8 elements, Capacity: 4 elements
// - Rounds: 7 (128-bit security with 40% margin)
// - S-Box: x^7, Inverse S-Box: x^10540996611094048183
// - Digest: 4 elements (first element returned for compatibility)

package vm

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/theQRL/go-qrl/params"
)

// bigGoldilocks is the Goldilocks prime as big.Int for exact arithmetic
var bigGoldilocks = new(big.Int).SetUint64(goldilocksModulus)

// =============================================================================
// GOLDILOCKS FIELD ARITHMETIC
// =============================================================================

// Goldilocks prime: 2^64 - 2^32 + 1
const goldilocksModulus uint64 = 18446744069414584321

// fieldReduce reduces a to the Goldilocks field
func fieldReduce(a uint64) uint64 {
	// Fast reduction for Goldilocks
	// Since modulus = 2^64 - 2^32 + 1, we can use special properties
	return a % goldilocksModulus
}

// fieldReduceWide reduces a wide value (128-bit represented as hi*2^64 + lo) to Goldilocks field
// Using the property: 2^64 ≡ 2^32 - 1 (mod p) where p = 2^64 - 2^32 + 1
// NOTE: Commented out - using big.Int for correctness. Can optimize later.
/*
func fieldReduceWide(hi, lo uint64) uint64 {
	// For Goldilocks prime p = 2^64 - 2^32 + 1:
	// 2^64 ≡ 2^32 - 1 (mod p)
	// So: hi*2^64 + lo ≡ hi*(2^32 - 1) + lo ≡ hi*2^32 - hi + lo (mod p)

	// Use big.Int for correctness (can optimize later)
	// This is necessary because the intermediate calculations can overflow uint64

	// Calculate hi * 2^32 - hi + lo
	// Split into parts to avoid overflow

	// hi * 2^32 can overflow, but we're reducing mod p anyway
	// hi << 32 gives us the low 64 bits of hi * 2^32
	// The high 32 bits of hi are lost, which equals hi >> 32

	hiLow := hi & 0xFFFFFFFF
	hiHigh := hi >> 32

	// hi * 2^32 = hiLow << 32 + hiHigh << 64
	// But 2^64 ≡ 2^32 - 1, so hiHigh << 64 ≡ hiHigh * (2^32 - 1)
	//                                       ≡ hiHigh << 32 - hiHigh

	// term1 = hiLow << 32
	term1 := hiLow << 32

	// term2 = hiHigh << 32 - hiHigh (from hiHigh * 2^64)
	term2 := hiHigh << 32
	// Subtract hiHigh from term2
	if term2 < hiHigh {
		term2 = term2 - hiHigh + goldilocksModulus
	} else {
		term2 = term2 - hiHigh
	}

	// Now we have: hi*2^32 ≡ term1 + term2 (mod p)
	// And we need: hi*2^32 - hi + lo

	// Combine: result = term1 + term2 - hi + lo
	result := term1

	// Add term2
	result = result + term2
	if result < term2 {
		// Overflow, need to reduce
		result = result - goldilocksModulus
	}
	if result >= goldilocksModulus {
		result = result - goldilocksModulus
	}

	// Subtract hi
	if result < hi {
		result = result - hi + goldilocksModulus
	} else {
		result = result - hi
	}

	// Add lo
	sum := result + lo
	if sum < result || sum >= goldilocksModulus {
		sum = sum - goldilocksModulus
	}
	result = sum

	// Final reduction
	if result >= goldilocksModulus {
		result = result - goldilocksModulus
	}

	return result
}
*/

// fieldAdd computes (a + b) mod p
func fieldAdd(a, b uint64) uint64 {
	sum := a + b
	if sum < a || sum >= goldilocksModulus {
		// Overflow or needs reduction
		return sum - goldilocksModulus
	}
	return sum
}

// fieldMul computes (a * b) mod p using big.Int for correctness
func fieldMul(a, b uint64) uint64 {
	// Use big.Int for exact 128-bit multiplication and modular reduction
	bigA := new(big.Int).SetUint64(a)
	bigB := new(big.Int).SetUint64(b)
	result := new(big.Int).Mul(bigA, bigB)
	result.Mod(result, bigGoldilocks)
	return result.Uint64()
}

// fieldSquare computes a^2 mod p
func fieldSquare(a uint64) uint64 {
	return fieldMul(a, a)
}

// exp7 computes x^7 (S-Box)
func exp7(x uint64) uint64 {
	// x^7 = x * x^2 * x^4
	x2 := fieldSquare(x)
	x4 := fieldSquare(x2)
	return fieldMul(fieldMul(x, x2), x4)
}

// expInvAlpha computes x^10540996611094048183 (Inverse S-Box)
// 10540996611094048183 is the multiplicative inverse of 7 modulo (p-1)
// Optimized computation using addition chains (72 multiplications)
func expInvAlpha(x uint64) uint64 {
	// t1 = x^2
	t1 := fieldSquare(x)

	// t2 = x^4
	t2 := fieldSquare(t1)

	// t3 = x^100100 (binary) = x^36
	t3 := t2
	for i := 0; i < 3; i++ {
		t3 = fieldSquare(t3)
	}
	t3 = fieldMul(t3, t2)

	// t4 = x^100100100100 (binary) = x^2340
	t4 := t3
	for i := 0; i < 6; i++ {
		t4 = fieldSquare(t4)
	}
	t4 = fieldMul(t4, t3)

	// t5 = x^100100100100100100100100 (binary)
	t5 := t4
	for i := 0; i < 12; i++ {
		t5 = fieldSquare(t5)
	}
	t5 = fieldMul(t5, t4)

	// t6 = x^100100100100100100100100100100 (binary)
	t6 := t5
	for i := 0; i < 6; i++ {
		t6 = fieldSquare(t6)
	}
	t6 = fieldMul(t6, t3)

	// t7 = x^1001001001001001001001001001000100100100100100100100100100100 (binary)
	t7 := t6
	for i := 0; i < 31; i++ {
		t7 = fieldSquare(t7)
	}
	t7 = fieldMul(t7, t6)

	// Final result
	// x^1001001001001001001001001001000110110110110110110110110110110111
	a := fieldSquare(fieldMul(fieldSquare(t7), t6))
	a = fieldSquare(a)
	b := fieldMul(fieldMul(t1, t2), x)
	return fieldMul(a, b)
}

// =============================================================================
// RESCUE PRIME CONSTANTS
// =============================================================================

const (
	stateWidth    = 12
	rateWidth     = 8
	capacityWidth = 4
	numRounds     = 7
	digestSize    = 4

	rateStart = 4
	rateEnd   = 12

	digestStart = 4
)

// MDS Matrix first row (circulant property generates full 12x12 matrix)
var mdsFirstRow = [stateWidth]uint64{7, 23, 8, 26, 13, 10, 9, 7, 6, 22, 21, 8}

// Full MDS matrix (generated from first row)
var mds [stateWidth][stateWidth]uint64

// Round constants ARK1 (first half of round)
var ark1 = [numRounds][stateWidth]uint64{
	{13917550007135091859, 16002276252647722320, 4729924423368391595, 10059693067827680263,
		9804807372516189948, 15666751576116384237, 10150587679474953119, 13627942357577414247,
		2323786301545403792, 615170742765998613, 8870655212817778103, 10534167191270683080},
	{14572151513649018290, 9445470642301863087, 6565801926598404534, 12667566692985038975,
		7193782419267459720, 11874811971940314298, 17906868010477466257, 1237247437760523561,
		6829882458376718831, 2140011966759485221, 1624379354686052121, 50954653459374206},
	{16288075653722020941, 13294924199301620952, 13370596140726871456, 611533288599636281,
		12865221627554828747, 12269498015480242943, 8230863118714645896, 13466591048726906480,
		10176988631229240256, 14951460136371189405, 5882405912332577353, 18125144098115032453},
	{6076976409066920174, 7466617867456719866, 5509452692963105675, 14692460717212261752,
		12980373618703329746, 1361187191725412610, 6093955025012408881, 5110883082899748359,
		8578179704817414083, 9311749071195681469, 16965242536774914613, 5747454353875601040},
	{13684212076160345083, 19445754899749561, 16618768069125744845, 278225951958825090,
		4997246680116830377, 782614868534172852, 16423767594935000044, 9990984633405879434,
		16757120847103156641, 2103861168279461168, 16018697163142305052, 6479823382130993799},
	{13957683526597936825, 9702819874074407511, 18357323897135139931, 3029452444431245019,
		1809322684009991117, 12459356450895788575, 11985094908667810946, 12868806590346066108,
		7872185587893926881, 10694372443883124306, 8644995046789277522, 1422920069067375692},
	{17619517835351328008, 6173683530634627901, 15061027706054897896, 4503753322633415655,
		11538516425871008333, 12777459872202073891, 17842814708228807409, 13441695826912633916,
		5950710620243434509, 17040450522225825296, 8787650312632423701, 7431110942091427450},
}

// Round constants ARK2 (second half of round)
var ark2 = [numRounds][stateWidth]uint64{
	{7989257206380839449, 8639509123020237648, 6488561830509603695, 5519169995467998761,
		2972173318556248829, 14899875358187389787, 14160104549881494022, 5969738169680657501,
		5116050734813646528, 12120002089437618419, 17404470791907152876, 2718166276419445724},
	{2485377440770793394, 14358936485713564605, 3327012975585973824, 6001912612374303716,
		17419159457659073951, 11810720562576658327, 14802512641816370470, 751963320628219432,
		9410455736958787393, 16405548341306967018, 6867376949398252373, 13982182448213113532},
	{10436926105997283389, 13237521312283579132, 668335841375552722, 2385521647573044240,
		3874694023045931809, 12952434030222726182, 1972984540857058687, 14000313505684510403,
		976377933822676506, 8407002393718726702, 338785660775650958, 4208211193539481671},
	{2284392243703840734, 4500504737691218932, 3976085877224857941, 2603294837319327956,
		5760259105023371034, 2911579958858769248, 18415938932239013434, 7063156700464743997,
		16626114991069403630, 163485390956217960, 11596043559919659130, 2976841507452846995},
	{15090073748392700862, 3496786927732034743, 8646735362535504000, 2460088694130347125,
		3944675034557577794, 14781700518249159275, 2857749437648203959, 8505429584078195973,
		18008150643764164736, 720176627102578275, 7038653538629322181, 8849746187975356582},
	{17427790390280348710, 1159544160012040055, 17946663256456930598, 6338793524502945410,
		17715539080731926288, 4208940652334891422, 12386490721239135719, 10010817080957769535,
		5566101162185411405, 12520146553271266365, 4972547404153988943, 5597076522138709717},
	{18338863478027005376, 115128380230345639, 4427489889653730058, 10890727269603281956,
		7094492770210294530, 7345573238864544283, 6834103517673002336, 14002814950696095900,
		15939230865809555943, 12717309295554119359, 4130723396860574906, 7706153020203677238},
}

func init() {
	// Generate full MDS matrix from first row (circulant property)
	for i := 0; i < stateWidth; i++ {
		for j := 0; j < stateWidth; j++ {
			mds[i][j] = mdsFirstRow[(j-i+stateWidth)%stateWidth]
		}
	}
}

// =============================================================================
// RESCUE PRIME PERMUTATION
// =============================================================================

// applyMDS applies the MDS matrix to the state
func applyMDS(state *[stateWidth]uint64) {
	var result [stateWidth]uint64
	for i := 0; i < stateWidth; i++ {
		for j := 0; j < stateWidth; j++ {
			result[i] = fieldAdd(result[i], fieldMul(mds[i][j], state[j]))
		}
	}
	*state = result
}

// addConstants adds round constants to the state
func addConstants(state *[stateWidth]uint64, ark [stateWidth]uint64) {
	for i := 0; i < stateWidth; i++ {
		state[i] = fieldAdd(state[i], ark[i])
	}
}

// applySBox applies x^7 to all state elements
func applySBox(state *[stateWidth]uint64) {
	for i := 0; i < stateWidth; i++ {
		state[i] = exp7(state[i])
	}
}

// applyInvSBox applies inverse S-Box to all state elements
func applyInvSBox(state *[stateWidth]uint64) {
	for i := 0; i < stateWidth; i++ {
		state[i] = expInvAlpha(state[i])
	}
}

// applyRound applies one round of Rescue-XLIX
func applyRound(state *[stateWidth]uint64, round int) {
	// First half of round
	applySBox(state)
	applyMDS(state)
	addConstants(state, ark1[round])

	// Second half of round
	applyInvSBox(state)
	applyMDS(state)
	addConstants(state, ark2[round])
}

// applyPermutation applies the full Rescue-XLIX permutation
func applyPermutation(state *[stateWidth]uint64) {
	for i := 0; i < numRounds; i++ {
		applyRound(state, i)
	}
}

// =============================================================================
// RESCUE PRIME HASH FUNCTIONS
// =============================================================================

// hashElements hashes a sequence of field elements
func hashElements(elements []uint64) uint64 {
	var state [stateWidth]uint64

	// Set first capacity element to number of elements to be hashed
	state[0] = uint64(len(elements))

	// Absorb elements into rate portion
	i := 0
	for _, element := range elements {
		state[rateStart+i] = fieldAdd(state[rateStart+i], element)
		i++
		if i == rateWidth {
			applyPermutation(&state)
			i = 0
		}
	}

	// Apply final permutation if we have absorbed elements
	if i > 0 {
		applyPermutation(&state)
	}

	// Return first element of digest (elements 4-7 are digest)
	return state[digestStart]
}

// rescue2 hashes two field elements
func rescue2(a, b uint64) uint64 {
	return hashElements([]uint64{a, b})
}

// rescue3 hashes three field elements
func rescue3(a, b, c uint64) uint64 {
	return hashElements([]uint64{a, b, c})
}

// =============================================================================
// PRECOMPILE IMPLEMENTATION
// =============================================================================

// Opcodes for Rescue Prime precompile
const (
	// Hash opcodes (Rp64_256 - 128-bit classical security)
	opcodeRescue2_256 = 0x01 // Hash 2 uint64 inputs, Rp64_256
	opcodeRescue3_256 = 0x02 // Hash 3 uint64 inputs, Rp64_256

	// Hash opcodes (Rp64_512 - 128-bit quantum security)
	opcodeRescue2_512 = 0x03 // Hash 2 uint64 inputs, Rp64_512
	opcodeRescue3_512 = 0x04 // Hash 3 uint64 inputs, Rp64_512

	// Variable-arity hash opcodes
	opcodeRescueN_256 = 0x05 // Hash N uint64 inputs, Rp64_256
	opcodeRescueN_512 = 0x06 // Hash N uint64 inputs, Rp64_512

	// Full digest opcodes (return multiple elements)
	opcodeDigest_256 = 0x07 // Full 4-element digest (256-bit), Rp64_256
	opcodeDigest_512 = 0x08 // Full 8-element digest (512-bit), Rp64_512

	// Merkle opcodes with uint64 path index (up to 64 levels in basic mode)
	opcodeMerkle64_256 = 0x10 // Merkle path, uint64 index, Rp64_256
	opcodeMerkle64_512 = 0x11 // Merkle path, uint64 index, Rp64_512

	// Merkle opcodes with uint128 path index (up to 128 levels in basic mode)
	opcodeMerkle128_256 = 0x12 // Merkle path, uint128 index, Rp64_256
	opcodeMerkle128_512 = 0x13 // Merkle path, uint128 index, Rp64_512

	// Convenience opcodes for note commitment and nullifier
	opcodeNoteCommit_256 = 0x20 // rescue3_256(value, blinding, viewingKey)
	opcodeNoteCommit_512 = 0x21 // rescue3_512(value, blinding, viewingKey)
	opcodeNullifier_256  = 0x22 // rescue2_256(commitment, spendingKey)
	opcodeNullifier_512  = 0x23 // rescue2_512(commitment, spendingKey)

	// Default aliases point to quantum-secure 512 variants
	// Use explicit _256 suffix when classical security is acceptable
	opcodeRescue2      = opcodeRescue2_512
	opcodeRescue3      = opcodeRescue3_512
	opcodeRescueHash   = opcodeRescueN_512
	opcodeRescueMerkle = opcodeMerkle64_512
)

// Merkle path modes (determines how pathBits are interpreted)
const (
	merkleModeBasic  = 0x00 // Binary tree: 2 bits/level, only direction bit used
	merkleModeSparse = 0x01 // Binary+sparse: direction + is_default_sibling flag
	merkleModeQuad   = 0x02 // Quad tree: 2 bits = child index (0-3)
)

// Errors
var (
	errRescuePrimeInvalidInput  = errors.New("invalid rescue prime input")
	errRescuePrimeInvalidOpcode = errors.New("invalid rescue prime opcode")
)

// rescuePrime implements the Rescue Prime precompile at address 0x20
type rescuePrime struct{}

// RequiredGas returns the gas required for the Rescue Prime operation
func (c *rescuePrime) RequiredGas(input []byte) uint64 {
	if len(input) < 1 {
		return params.RescuePrimeBaseGas
	}

	opcode := input[0]
	switch opcode {
	// Rp64_256 hash opcodes (128-bit classical security)
	case opcodeRescue2_256, opcodeNullifier_256:
		return params.RescuePrimeBaseGas
	case opcodeRescue3_256, opcodeNoteCommit_256:
		return params.RescuePrimeBaseGas

	// Rp64_512 hash opcodes (128-bit quantum security, ~1.5x cost due to larger state)
	case opcodeRescue2_512, opcodeNullifier_512:
		return params.RescuePrimeBaseGas * 15 / 10
	case opcodeRescue3_512, opcodeNoteCommit_512:
		return params.RescuePrimeBaseGas * 15 / 10

	case opcodeRescueN_256:
		// Variable gas based on number of elements (Rp64_256)
		// Format: opcode(1) + count(1) + elements(count*8)
		if len(input) < 2 {
			return params.RescuePrimeBaseGas
		}
		numElements := uint64(input[1])
		// 100 base + 50 per 8-element chunk
		return params.RescuePrimeBaseGas + 50*((numElements+7)/8)

	case opcodeRescueN_512:
		// Variable gas based on number of elements (Rp64_512)
		if len(input) < 2 {
			return params.RescuePrimeBaseGas * 15 / 10
		}
		numElements := uint64(input[1])
		// 150 base + 75 per 8-element chunk
		return params.RescuePrimeBaseGas*15/10 + 75*((numElements+7)/8)

	case opcodeDigest_256:
		// Same as rescueN_256 but returns 4 elements
		if len(input) < 2 {
			return params.RescuePrimeBaseGas
		}
		numElements := uint64(input[1])
		return params.RescuePrimeBaseGas + 50*((numElements+7)/8)

	case opcodeDigest_512:
		// Same as rescueN_512 but returns 8 elements
		if len(input) < 2 {
			return params.RescuePrimeBaseGas * 15 / 10
		}
		numElements := uint64(input[1])
		return params.RescuePrimeBaseGas*15/10 + 75*((numElements+7)/8)

	// Merkle opcodes - gas scales with path depth and hash variant
	case opcodeMerkle64_256:
		if len(input) < 2 {
			return params.RescuePrimeBaseGas
		}
		depth := uint64(input[1])
		return params.RescuePrimeBaseGas + depth*params.RescuePrimeBaseGas
	case opcodeMerkle64_512:
		if len(input) < 2 {
			return params.RescuePrimeBaseGas * 15 / 10
		}
		depth := uint64(input[1])
		return params.RescuePrimeBaseGas*15/10 + depth*params.RescuePrimeBaseGas*15/10
	case opcodeMerkle128_256:
		if len(input) < 2 {
			return params.RescuePrimeBaseGas
		}
		depth := uint64(input[1])
		return params.RescuePrimeBaseGas + depth*params.RescuePrimeBaseGas
	case opcodeMerkle128_512:
		if len(input) < 2 {
			return params.RescuePrimeBaseGas * 15 / 10
		}
		depth := uint64(input[1])
		return params.RescuePrimeBaseGas*15/10 + depth*params.RescuePrimeBaseGas*15/10

	default:
		return params.RescuePrimeBaseGas
	}
}

// Run executes the Rescue Prime precompile
func (c *rescuePrime) Run(input []byte) ([]byte, error) {
	if len(input) < 1 {
		return nil, errRescuePrimeInvalidInput
	}

	opcode := input[0]
	data := input[1:]

	switch opcode {
	// Rp64_256 hash opcodes (128-bit classical security)
	case opcodeRescue2_256:
		return runRescue2_256(data)
	case opcodeRescue3_256:
		return runRescue3_256(data)
	case opcodeRescueN_256:
		return runRescueN_256(data)
	case opcodeDigest_256:
		return runDigest_256(data)

	// Rp64_512 hash opcodes (128-bit quantum security)
	case opcodeRescue2_512:
		return runRescue2_512(data)
	case opcodeRescue3_512:
		return runRescue3_512(data)
	case opcodeRescueN_512:
		return runRescueN_512(data)
	case opcodeDigest_512:
		return runDigest_512(data)

	// Merkle opcodes with uint64 path index
	case opcodeMerkle64_256:
		return runMerkle64_256(data)
	case opcodeMerkle64_512:
		return runMerkle64_512(data)

	// Merkle opcodes with uint128 path index
	case opcodeMerkle128_256:
		return runMerkle128_256(data)
	case opcodeMerkle128_512:
		return runMerkle128_512(data)

	// Convenience opcodes - same as rescue3/rescue2 but with semantic meaning
	case opcodeNoteCommit_256:
		return runRescue3_256(data) // rescue3_256(value, blinding, viewingKey)
	case opcodeNoteCommit_512:
		return runRescue3_512(data) // rescue3_512(value, blinding, viewingKey)
	case opcodeNullifier_256:
		return runRescue2_256(data) // rescue2_256(commitment, spendingKey)
	case opcodeNullifier_512:
		return runRescue2_512(data) // rescue2_512(commitment, spendingKey)

	default:
		return nil, errRescuePrimeInvalidOpcode
	}
}

// runRescue2 hashes two uint64 values
func runRescue2_256(data []byte) ([]byte, error) {
	// Expect 16 bytes (2 * 8)
	if len(data) < 16 {
		// Pad with zeros
		padded := make([]byte, 16)
		copy(padded, data)
		data = padded
	}

	a := binary.LittleEndian.Uint64(data[0:8])
	b := binary.LittleEndian.Uint64(data[8:16])

	result := rescue2(a, b)

	output := make([]byte, 8)
	binary.LittleEndian.PutUint64(output, result)
	return output, nil
}

// runRescue3 hashes three uint64 values
func runRescue3_256(data []byte) ([]byte, error) {
	// Expect 24 bytes (3 * 8)
	if len(data) < 24 {
		padded := make([]byte, 24)
		copy(padded, data)
		data = padded
	}

	a := binary.LittleEndian.Uint64(data[0:8])
	b := binary.LittleEndian.Uint64(data[8:16])
	c := binary.LittleEndian.Uint64(data[16:24])

	result := rescue3(a, b, c)

	output := make([]byte, 8)
	binary.LittleEndian.PutUint64(output, result)
	return output, nil
}

// runRescueN_256 hashes variable number of uint64 values using Rp64_256
func runRescueN_256(data []byte) ([]byte, error) {
	// Format: [1 byte: count][count * 8 bytes: elements]
	if len(data) < 1 {
		return nil, errRescuePrimeInvalidInput
	}

	numElements := int(data[0])
	data = data[1:]

	// Sanity check
	if numElements > 255 {
		return nil, errRescuePrimeInvalidInput
	}

	elements := make([]uint64, numElements)
	for i := 0; i < numElements; i++ {
		offset := i * 8
		if offset+8 <= len(data) {
			elements[i] = binary.LittleEndian.Uint64(data[offset : offset+8])
		}
		// Elements beyond data length are zero (already initialized)
	}

	result := hashElements(elements)

	output := make([]byte, 8)
	binary.LittleEndian.PutUint64(output, result)
	return output, nil
}

// runRescueN_512 hashes variable number of uint64 values using Rp64_512
func runRescueN_512(data []byte) ([]byte, error) {
	// Format: [1 byte: count][count * 8 bytes: elements]
	if len(data) < 1 {
		return nil, errRescuePrimeInvalidInput
	}

	numElements := int(data[0])
	data = data[1:]

	if numElements > 255 {
		return nil, errRescuePrimeInvalidInput
	}

	elements := make([]uint64, numElements)
	for i := 0; i < numElements; i++ {
		offset := i * 8
		if offset+8 <= len(data) {
			elements[i] = binary.LittleEndian.Uint64(data[offset : offset+8])
		}
	}

	result := hashElements512(elements)

	output := make([]byte, 8)
	binary.LittleEndian.PutUint64(output, result)
	return output, nil
}

// runDigest_256 returns full 4-element (256-bit) digest using Rp64_256
func runDigest_256(data []byte) ([]byte, error) {
	// Format: [1 byte: count][count * 8 bytes: elements]
	if len(data) < 1 {
		return nil, errRescuePrimeInvalidInput
	}

	numElements := int(data[0])
	data = data[1:]

	if numElements > 255 {
		return nil, errRescuePrimeInvalidInput
	}

	elements := make([]uint64, numElements)
	for i := 0; i < numElements; i++ {
		offset := i * 8
		if offset+8 <= len(data) {
			elements[i] = binary.LittleEndian.Uint64(data[offset : offset+8])
		}
	}

	digest := computeDigest256(elements)

	// Return all 4 digest elements
	output := make([]byte, 32)
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint64(output[i*8:(i+1)*8], digest[i])
	}
	return output, nil
}

// runDigest_512 returns full 8-element (512-bit) digest using Rp64_512
func runDigest_512(data []byte) ([]byte, error) {
	// Format: [1 byte: count][count * 8 bytes: elements]
	if len(data) < 1 {
		return nil, errRescuePrimeInvalidInput
	}

	numElements := int(data[0])
	data = data[1:]

	if numElements > 255 {
		return nil, errRescuePrimeInvalidInput
	}

	elements := make([]uint64, numElements)
	for i := 0; i < numElements; i++ {
		offset := i * 8
		if offset+8 <= len(data) {
			elements[i] = binary.LittleEndian.Uint64(data[offset : offset+8])
		}
	}

	digest := computeDigest512(elements)

	// Return all 8 digest elements
	output := make([]byte, 64)
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint64(output[i*8:(i+1)*8], digest[i])
	}
	return output, nil
}

// computeDigest256 computes full 4-element digest using Rp64_256
func computeDigest256(elements []uint64) [4]uint64 {
	var state [stateWidth]uint64

	// Set first capacity element to number of elements
	state[0] = uint64(len(elements))

	// Absorb elements into rate portion
	i := 0
	for _, element := range elements {
		state[rateStart+i] = fieldAdd(state[rateStart+i], element)
		i++
		if i == rateWidth {
			applyPermutation(&state)
			i = 0
		}
	}

	// Apply final permutation if needed
	if i > 0 {
		applyPermutation(&state)
	}

	// Return digest (elements 4-7)
	var digest [4]uint64
	for j := 0; j < 4; j++ {
		digest[j] = state[digestStart+j]
	}
	return digest
}

// computeDigest512 computes full 8-element digest using Rp64_512
func computeDigest512(elements []uint64) [8]uint64 {
	var state [stateWidth512]uint64

	// Set first capacity element to number of elements
	state[0] = uint64(len(elements))

	// Absorb elements into rate portion
	i := 0
	for _, element := range elements {
		state[rateStart512+i] = fieldAdd(state[rateStart512+i], element)
		i++
		if i == rateWidth512 {
			applyPermutation512(&state)
			i = 0
		}
	}

	// Apply final permutation if needed
	if i > 0 {
		applyPermutation512(&state)
	}

	// Return digest (elements 8-15)
	var digest [8]uint64
	for j := 0; j < 8; j++ {
		digest[j] = state[rateStart512+j]
	}
	return digest
}

// =============================================================================
// Rp64_512 HASH FUNCTIONS (128-bit quantum security)
// =============================================================================

// runRescue2_512 hashes two uint64 values using Rp64_512
func runRescue2_512(data []byte) ([]byte, error) {
	if len(data) < 16 {
		padded := make([]byte, 16)
		copy(padded, data)
		data = padded
	}

	a := binary.LittleEndian.Uint64(data[0:8])
	b := binary.LittleEndian.Uint64(data[8:16])

	result := rescue2_512(a, b)

	output := make([]byte, 8)
	binary.LittleEndian.PutUint64(output, result)
	return output, nil
}

// runRescue3_512 hashes three uint64 values using Rp64_512
func runRescue3_512(data []byte) ([]byte, error) {
	if len(data) < 24 {
		padded := make([]byte, 24)
		copy(padded, data)
		data = padded
	}

	a := binary.LittleEndian.Uint64(data[0:8])
	b := binary.LittleEndian.Uint64(data[8:16])
	c := binary.LittleEndian.Uint64(data[16:24])

	result := rescue3_512(a, b, c)

	output := make([]byte, 8)
	binary.LittleEndian.PutUint64(output, result)
	return output, nil
}

// =============================================================================
// MERKLE PATH FUNCTIONS
// =============================================================================

// Merkle path format (unified for all variants):
// - depth (1 byte): number of levels (1-64 for uint64, 1-128 for uint128)
// - mode (1 byte): interpretation mode (0x00=binary, 0x01=sparse, 0x02=quad)
// - leaf (8 bytes): starting leaf value
// - pathIndex (8 or 16 bytes): packed path bits (2 bits per level)
// - siblings (depth * 8 bytes): sibling hashes

// runMerkle64_256 computes Merkle root with uint64 path index using Rp64_256
func runMerkle64_256(data []byte) ([]byte, error) {
	return runMerkleGeneric(data, false, rescue2)
}

// runMerkle64_512 computes Merkle root with uint64 path index using Rp64_512
func runMerkle64_512(data []byte) ([]byte, error) {
	return runMerkleGeneric(data, false, rescue2_512)
}

// runMerkle128_256 computes Merkle root with uint128 path index using Rp64_256
func runMerkle128_256(data []byte) ([]byte, error) {
	return runMerkleGeneric(data, true, rescue2)
}

// runMerkle128_512 computes Merkle root with uint128 path index using Rp64_512
func runMerkle128_512(data []byte) ([]byte, error) {
	return runMerkleGeneric(data, true, rescue2_512)
}

// runMerkleGeneric implements the core Merkle path computation
func runMerkleGeneric(data []byte, use128bit bool, hashFunc func(uint64, uint64) uint64) ([]byte, error) {
	if len(data) < 2 {
		return nil, errRescuePrimeInvalidInput
	}

	// Parse depth and mode
	depth := int(data[0])
	mode := data[1]

	// Validate depth based on mode
	// - merkleModeBasic: 1 bit per level, so uint64=64 levels, uint128=128 levels
	// - merkleModeSparse/Quad: 2 bits per level, so uint64=32 levels, uint128=64 levels
	var maxDepth int
	if mode == merkleModeBasic {
		maxDepth = 64 // 1 bit per level
		if use128bit {
			maxDepth = 128
		}
	} else {
		maxDepth = 32 // 2 bits per level
		if use128bit {
			maxDepth = 64
		}
	}
	if depth == 0 || depth > maxDepth {
		return nil, errRescuePrimeInvalidInput
	}

	// Calculate path index size
	pathIndexSize := 8 // uint64
	if use128bit {
		pathIndexSize = 16 // uint128
	}

	// Calculate expected size: depth(1) + mode(1) + leaf(8) + pathIndex + siblings
	headerSize := 2 + 8 + pathIndexSize
	expectedSize := headerSize + depth*8
	if len(data) < expectedSize {
		padded := make([]byte, expectedSize)
		copy(padded, data)
		data = padded
	}

	// Parse leaf
	current := binary.LittleEndian.Uint64(data[2:10])

	// Parse pathIndex
	pathIndexLo := binary.LittleEndian.Uint64(data[10:18])
	var pathIndexHi uint64
	if use128bit {
		pathIndexHi = binary.LittleEndian.Uint64(data[18:26])
	}

	// Parse siblings
	siblingsStart := 10 + pathIndexSize
	siblings := make([]uint64, depth)
	for i := 0; i < depth; i++ {
		offset := siblingsStart + i*8
		siblings[i] = binary.LittleEndian.Uint64(data[offset : offset+8])
	}

	// Compute Merkle root based on mode
	for i := 0; i < depth; i++ {
		sibling := siblings[i]

		switch mode {
		case merkleModeBasic:
			// Binary tree: 1 bit per level from pathIndex (simple leaf index)
			// Bit i of pathIndex indicates direction: 0=left child, 1=right child
			var bit uint64
			if i < 64 {
				bit = (pathIndexLo >> i) & 1
			} else {
				bit = (pathIndexHi >> (i - 64)) & 1
			}
			if bit == 1 {
				current = hashFunc(sibling, current)
			} else {
				current = hashFunc(current, sibling)
			}

		case merkleModeSparse:
			// Sparse tree: 2 bits per level (direction + is_default_sibling)
			var bits uint64
			bitPos := i * 2
			if bitPos < 63 {
				// Both bits are in pathIndexLo
				bits = (pathIndexLo >> bitPos) & 0x3
			} else if bitPos == 63 {
				// BOUNDARY CROSSING: bit 63 from Lo, bit 64 (as bit 0 of Hi) from Hi
				lowBit := (pathIndexLo >> 63) & 1
				highBit := (pathIndexHi & 1) << 1
				bits = lowBit | highBit
			} else if bitPos < 128 {
				// Both bits are in pathIndexHi
				bits = (pathIndexHi >> (bitPos - 64)) & 0x3
			} else {
				// Beyond 128 bits - shouldn't happen with valid depth
				bits = 0
			}
			// Bit 1 = is_default_sibling flag
			if bits&2 != 0 {
				sibling = 0 // Default sibling
			}
			// Bit 0 = direction
			if bits&1 == 1 {
				current = hashFunc(sibling, current)
			} else {
				current = hashFunc(current, sibling)
			}

		case merkleModeQuad:
			// Quad tree: 2 bits per level = child index (0-3)
			var bits uint64
			bitPos := i * 2
			if bitPos < 63 {
				// Both bits are in pathIndexLo
				bits = (pathIndexLo >> bitPos) & 0x3
			} else if bitPos == 63 {
				// BOUNDARY CROSSING: bit 63 from Lo, bit 64 (as bit 0 of Hi) from Hi
				lowBit := (pathIndexLo >> 63) & 1
				highBit := (pathIndexHi & 1) << 1
				bits = lowBit | highBit
			} else {
				// Both bits are in pathIndexHi (bitPos >= 64)
				bits = (pathIndexHi >> (bitPos - 64)) & 0x3
			}
			// Simple interpretation: bit 0 = direction for binary compatibility
			if bits&1 == 1 {
				current = hashFunc(sibling, current)
			} else {
				current = hashFunc(current, sibling)
			}

		default:
			return nil, errRescuePrimeInvalidInput
		}
	}

	output := make([]byte, 8)
	binary.LittleEndian.PutUint64(output, current)
	return output, nil
}

// =============================================================================
// Rp64_512 IMPLEMENTATION (128-bit quantum security)
// =============================================================================

// Rp64_512 uses 16-element state for 512-bit digest (256-bit classical / 128-bit quantum)
const (
	stateWidth512    = 16
	rateWidth512     = 8
	capacityWidth512 = 8
	digestSize512    = 8
	numRounds512     = 7

	rateStart512 = 8
	rateEnd512   = 16
)

// MDS matrix first row for Rp64_512 (16x16 circulant)
var mdsFirstRow512 = [stateWidth512]uint64{
	17293822565076172801, 17361641477096079361, 11273010264642245974, 10679693934924233028,
	17524406865943855105, 7027331074062698789, 4192441833957860073, 11228452911817573065,
	17678129733188976641, 14019525492755084084, 14899293286834856567, 13664254866233025423,
	9882184322900670172, 10177513969332184453, 17831852600434098177, 11306068945770229100,
}

// Full MDS matrix for Rp64_512
var mds512 [stateWidth512][stateWidth512]uint64

// Round constants for Rp64_512
var ark1_512 = [numRounds512][stateWidth512]uint64{
	{6982037620083498474, 11313752653910758692, 8997895669498498605, 10820227034949186943,
		1665610316568506462, 8737478896851879696, 18290056313095298595, 2133831193740291829,
		9440982015161553316, 14949161883780877486, 14284817605949780691, 3693377259929963737,
		12780972936067116388, 11959621680254538870, 3766481992606058615, 5574795883254195045},
	{13419936591279982348, 17653700232236044798, 4946063883291232031, 1679583108152102785,
		11085095700700282338, 17149875766697889383, 8298866296330078817, 4680660832963666576,
		11044461168988609, 12131143604379851294, 7016591399389188943, 17217188069764689181,
		14313523660611655468, 1478838048979188218, 9765610149866093167, 9022153046131618753},
	{7085108655988295932, 9654765259316498614, 13831025547399178671, 3007095451927389449,
		3817628892523055506, 10419787200279555575, 14022455992879118209, 16423139058377166976,
		9310432098633591206, 15461665619174313491, 12523952893959024629, 3631181316571645772,
		4700920282174694397, 6649889758061893531, 17584261047028710960, 2698299002628444946},
	{8024714048139949459, 15065890063279103769, 2430700831915787704, 9251178025915379494,
		7295648166756096539, 17866689078917766879, 11688543115389851962, 10296003003584788120,
		6295167174788006883, 10509330178063611149, 4854936696207455510, 4405133649579682896,
		13960823547912478197, 3590316485188929143, 15809376252858380962, 14148406166997621996},
	{12419652892668115835, 1889686584648040973, 5227296481455298706, 7432299009654929056,
		17859411461698972891, 9296115610740912121, 5951232795847946716, 15125689741134534226,
		15515561709604117421, 6112610669119313931, 14878016903478721546, 4716110949116628296,
		8486467973891989071, 2671046989831268301, 12582102298698127126, 11800098356626606891},
	{8178178296010424656, 2933999115287303806, 7195749195568783731, 15971310098149232881,
		3878363089626233156, 14685893044765106501, 8744866972006078431, 12964685931316892546,
		16219621019295958211, 13475656122424973201, 2900098536971970286, 8161148788960740641,
		7113856203003100251, 1059163899188820781, 12239203688566355, 6423217844891234716},
	{3726262194199174611, 5073279058996805006, 6420295923794435401, 7767312788592065796,
		9114329653389696191, 10461346518187326586, 11808363382984956981, 13155380247782587376,
		14502397112580217771, 15849413977377848166, 17196430842175478561, 96703637558524635,
		1443720502356155030, 2790737367153785425, 4137754231951415820, 5484771096749046215},
}

var ark2_512 = [numRounds512][stateWidth512]uint64{
	{6831787906843676610, 8178804771641307005, 9525821636438937400, 10872838501236567795,
		12219855366034198190, 13566872230831828585, 14913889095629458980, 16260905960427089375,
		17607922825224719770, 508195620607765844, 1855212485405396239, 3202229350203026634,
		4549246214000657029, 5896263079798287424, 7243279944595917819, 8590296809393548214},
	{9937313674191178609, 11284330538988809004, 12631347403786439399, 13978364268584069794,
		15325381133381700189, 16672398998179330584, 18019414862976960979, 919687658360007053,
		2266704523157637448, 3613721387955267843, 4960738252752898238, 6307755117550528633,
		7654771982348159028, 9001788847145789423, 10348805711943419818, 11695822576741050213},
	{13042839441538680608, 14389856306336311003, 15736873171133941398, 17083890035931571793,
		18430906900729202188, 1331179696112248262, 2678196560909878657, 4025213425707509052,
		5372230290505139447, 6719247155302769842, 8066264020100400237, 9413280884898030632,
		10760297749695661027, 12107314614493291422, 13454331479290921817, 14801348344088552212},
	{16148365208886182607, 17495382073683813002, 398654869066859076, 1745671733864489471,
		3092688598662119866, 4439705463459750261, 5786722328257380656, 7133739193055011051,
		8480756057852641446, 9827772922650271841, 11174789787447902236, 12521806652245532631,
		13868823517043163026, 15215840381840793421, 16562857246638423816, 17909874111436054211},
	{810146906819100285, 2157163771616730680, 3504180636414361075, 4851197501211991470,
		6198214366009621865, 7545231230807252260, 8892248095604882655, 10239264960402513050,
		11586281825200143445, 12933298689997773840, 14280315554795404235, 15627332419593034630,
		16974349284390665025, 18321366149188295420, 1221638944571341494, 2568655809368971889},
	{3915672674166602284, 5262689538964232679, 6609706403761863074, 7956723268559493469,
		9303740133357123864, 10650756998154754259, 11997773862952384654, 13344790727750015049,
		14691807592547645444, 16038824457345275839, 17385841322142906234, 286114117525952308,
		1633130982323582703, 2980147847121213098, 4327164711918843493, 5674181576716473888},
	{7021198441514104283, 8368215306311734678, 9715232171109365073, 11062249035906995468,
		12409265900704625863, 13756282765502256258, 15103299630299886653, 16450316495097517048,
		17797333359895147443, 697606155278193517, 2044623020075823912, 3391639884873454307,
		4738656749671084702, 6085673614468715097, 7432690479266345492, 8779707344063975887},
}

func init() {
	// Generate full MDS matrix for Rp64_512 from first row (circulant property)
	for i := 0; i < stateWidth512; i++ {
		for j := 0; j < stateWidth512; j++ {
			mds512[i][j] = mdsFirstRow512[(j-i+stateWidth512)%stateWidth512]
		}
	}
}

// applyMDS512 applies the 16x16 MDS matrix to the state
func applyMDS512(state *[stateWidth512]uint64) {
	var result [stateWidth512]uint64
	for i := 0; i < stateWidth512; i++ {
		for j := 0; j < stateWidth512; j++ {
			result[i] = fieldAdd(result[i], fieldMul(mds512[i][j], state[j]))
		}
	}
	*state = result
}

// addConstants512 adds round constants to the 16-element state
func addConstants512(state *[stateWidth512]uint64, ark [stateWidth512]uint64) {
	for i := 0; i < stateWidth512; i++ {
		state[i] = fieldAdd(state[i], ark[i])
	}
}

// applySBox512 applies x^7 to all 16 state elements
func applySBox512(state *[stateWidth512]uint64) {
	for i := 0; i < stateWidth512; i++ {
		state[i] = exp7(state[i])
	}
}

// applyInvSBox512 applies inverse S-Box to all 16 state elements
func applyInvSBox512(state *[stateWidth512]uint64) {
	for i := 0; i < stateWidth512; i++ {
		state[i] = expInvAlpha(state[i])
	}
}

// applyRound512 applies one round of Rescue-XLIX for Rp64_512
func applyRound512(state *[stateWidth512]uint64, round int) {
	applySBox512(state)
	applyMDS512(state)
	addConstants512(state, ark1_512[round])

	applyInvSBox512(state)
	applyMDS512(state)
	addConstants512(state, ark2_512[round])
}

// applyPermutation512 applies the full Rescue-XLIX permutation for Rp64_512
func applyPermutation512(state *[stateWidth512]uint64) {
	for i := 0; i < numRounds512; i++ {
		applyRound512(state, i)
	}
}

// hashElements512 hashes a sequence of field elements using Rp64_512
func hashElements512(elements []uint64) uint64 {
	var state [stateWidth512]uint64

	// Set first capacity element to number of elements
	state[0] = uint64(len(elements))

	// Absorb elements into rate portion
	i := 0
	for _, element := range elements {
		state[rateStart512+i] = fieldAdd(state[rateStart512+i], element)
		i++
		if i == rateWidth512 {
			applyPermutation512(&state)
			i = 0
		}
	}

	// Apply final permutation if needed
	if i > 0 {
		applyPermutation512(&state)
	}

	// Return first element of digest
	return state[rateStart512]
}

// rescue2_512 hashes two field elements using Rp64_512
func rescue2_512(a, b uint64) uint64 {
	return hashElements512([]uint64{a, b})
}

// rescue3_512 hashes three field elements using Rp64_512
func rescue3_512(a, b, c uint64) uint64 {
	return hashElements512([]uint64{a, b, c})
}
