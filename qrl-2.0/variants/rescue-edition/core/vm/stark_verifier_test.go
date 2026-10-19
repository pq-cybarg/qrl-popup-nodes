// Copyright 2024 The go-zond Authors
// This file is part of the go-zond library.
//
// STARK Verifier Precompile Tests

package vm

import (
	"encoding/binary"
	"testing"
)

func TestStarkVerifierGas(t *testing.T) {
	verifier := &starkVerifier{}

	tests := []struct {
		name        string
		opcode      byte
		expectedGas uint64
	}{
		{"verify_full", opcodeVerifyFull, gasVerifyFull},
		{"stark_init", opcodeStarkInit, gasStarkInit},
		{"stark_commit", opcodeStarkCommit, gasStarkCommit},
		{"stark_ood", opcodeStarkOod, gasStarkOod},
		{"stark_finalize", opcodeStarkFinalize, gasStarkFinalize},
		{"verify_blob", opcodeVerifyWithBlob, gasVerifyWithBlob},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte{tc.opcode}
			gas := verifier.RequiredGas(input)
			if gas != tc.expectedGas {
				t.Errorf("expected gas %d, got %d", tc.expectedGas, gas)
			}
		})
	}
}

func TestStarkVerifierInvalidOpcode(t *testing.T) {
	verifier := &starkVerifier{}
	input := []byte{0xFF}
	_, err := verifier.Run(input)
	if err != errStarkInvalidOpcode {
		t.Errorf("expected invalid opcode error, got %v", err)
	}
}

func TestStarkVerifierEmptyInput(t *testing.T) {
	verifier := &starkVerifier{}
	_, err := verifier.Run([]byte{})
	if err != errStarkInvalidInput {
		t.Errorf("expected invalid input error, got %v", err)
	}
}

func TestStarkInit(t *testing.T) {
	verifier := &starkVerifier{}

	// Create STARK_INIT input
	// Format: opcode(1) + variant(1) + circuit_type(1) + pub_inputs_count(1) + pub_inputs(N*8)
	publicInputs := []uint64{
		12345678901234567890, // merkle_root
		9876543210987654321,  // bound_params_hash
		1111111111111111111,  // nullifier_0
		2222222222222222222,  // commitment_0
		3333333333333333333,  // commitment_1
	}

	inputSize := 1 + 1 + 1 + 1 + len(publicInputs)*8
	input := make([]byte, inputSize)
	input[0] = opcodeStarkInit
	input[1] = variantRP256_64bit
	input[2] = circuitTransfer1x2
	input[3] = byte(len(publicInputs))

	for i, pi := range publicInputs {
		binary.LittleEndian.PutUint64(input[4+i*8:], pi)
	}

	// Run STARK_INIT
	output, err := verifier.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return 32-byte session ID
	if len(output) != 32 {
		t.Errorf("expected 32-byte session ID, got %d bytes", len(output))
	}

	t.Logf("Session ID: %x", output)
}

func TestStarkStreamingVerification(t *testing.T) {
	verifier := &starkVerifier{}

	// Step 1: Initialize session
	publicInputs := []uint64{123, 456, 789}

	initInput := make([]byte, 1+1+1+1+len(publicInputs)*8)
	initInput[0] = opcodeStarkInit
	initInput[1] = variantRP256_64bit
	initInput[2] = circuitTransfer1x2
	initInput[3] = byte(len(publicInputs))
	for i, pi := range publicInputs {
		binary.LittleEndian.PutUint64(initInput[4+i*8:], pi)
	}

	sessionID, err := verifier.Run(initInput)
	if err != nil {
		t.Fatalf("STARK_INIT failed: %v", err)
	}

	// Step 2: Submit commitments
	// Format: opcode(1) + sessionID(32) + traceCommit(32) + constraintCommit(32) + friCount(1) + friCommits(friCount*32) + powNonce(8)
	friCount := 4
	commitInput := make([]byte, 1+32+32+32+1+friCount*32+8)
	commitInput[0] = opcodeStarkCommit
	copy(commitInput[1:33], sessionID)
	// 32-byte trace commitment
	binary.LittleEndian.PutUint64(commitInput[33:41], 111111)
	// 32-byte constraint commitment
	binary.LittleEndian.PutUint64(commitInput[65:73], 222222)
	commitInput[97] = byte(friCount)
	for i := 0; i < friCount; i++ {
		binary.LittleEndian.PutUint64(commitInput[98+i*32:], uint64(i+333333))
	}
	binary.LittleEndian.PutUint64(commitInput[98+friCount*32:], 444444) // pow_nonce

	output, err := verifier.Run(commitInput)
	if err != nil {
		t.Fatalf("STARK_COMMIT failed: %v", err)
	}
	if output[0] != 0x01 {
		t.Errorf("STARK_COMMIT expected success (0x01), got 0x%02x", output[0])
	}

	// Step 3: Submit OOD evaluations
	traceCount := 4
	constraintCount := 2
	oodInput := make([]byte, 1+32+1+traceCount*16+1+constraintCount*8)
	oodInput[0] = opcodeStarkOod
	copy(oodInput[1:33], sessionID)
	oodInput[33] = byte(traceCount)
	offset := 34
	for i := 0; i < traceCount; i++ {
		binary.LittleEndian.PutUint64(oodInput[offset+i*8:], uint64(i+100000))
	}
	offset += traceCount * 8
	for i := 0; i < traceCount; i++ {
		binary.LittleEndian.PutUint64(oodInput[offset+i*8:], uint64(i+200000))
	}
	offset += traceCount * 8
	oodInput[offset] = byte(constraintCount)
	offset++
	for i := 0; i < constraintCount; i++ {
		binary.LittleEndian.PutUint64(oodInput[offset+i*8:], uint64(i+300000))
	}

	output, err = verifier.Run(oodInput)
	if err != nil {
		t.Fatalf("STARK_OOD failed: %v", err)
	}
	if output[0] != 0x01 {
		t.Errorf("STARK_OOD expected success (0x01), got 0x%02x", output[0])
	}

	// Step 4: Submit FRI batches (use fewer queries for simpler test)
	// FRI batch format per query:
	// traceValCount(4) + traceAuthCount(4) + constraintValCount(4) +
	// constraintPathCount(4) + friLayerCount(4) + friAuthCount(4) + cosetCount(4) = 28 bytes minimum per query
	numBatches := 5 // 43 queries / ~10 per batch = ~5 batches
	queriesPerBatch := 10

	for batchIdx := 0; batchIdx < numBatches; batchIdx++ {
		// Build properly formatted FRI batch with minimal (zero-count) query data
		// Header: opcode(1) + sessionID(32) + batchIndex(1) + queryCount(1) = 35 bytes
		// Per query: 7 × uint32(4) = 28 bytes (all counts = 0)
		headerSize := 1 + 32 + 1 + 1
		queryDataSize := queriesPerBatch * 28 // 7 zero-count uint32s per query
		friBatchInput := make([]byte, headerSize+queryDataSize)

		friBatchInput[0] = opcodeStarkFriBatch
		copy(friBatchInput[1:33], sessionID)
		friBatchInput[33] = byte(batchIdx)
		friBatchInput[34] = byte(queriesPerBatch)

		// Query data is already zeroed (all counts = 0), which is valid format
		// This will fail Merkle verification but pass parsing

		output, err = verifier.Run(friBatchInput)
		if err != nil {
			t.Fatalf("STARK_FRI_BATCH %d failed to parse: %v", batchIdx, err)
		}

		// With zero-count data, Merkle verification will fail (0x00) or succeed parsing
		// Accept 0x00 (verification failed), 0x01 (continue), or 0x02 (complete)
		if output[0] != 0x00 && output[0] != 0x01 && output[0] != 0x02 {
			t.Errorf("STARK_FRI_BATCH unexpected result: 0x%02x", output[0])
		}
		t.Logf("STARK_FRI_BATCH %d result: 0x%02x", batchIdx, output[0])

		// If verification failed, we can still continue to test the flow
		if output[0] == 0x00 {
			t.Logf("Batch %d: Merkle verification failed (expected with zero-count test data)", batchIdx)
		}
	}

	// Step 5: Finalize
	finalizeInput := make([]byte, 1+32)
	finalizeInput[0] = opcodeStarkFinalize
	copy(finalizeInput[1:33], sessionID)

	output, err = verifier.Run(finalizeInput)
	if err != nil {
		t.Fatalf("STARK_FINALIZE failed: %v", err)
	}

	t.Logf("Verification result: 0x%02x (1=valid, 0=invalid)", output[0])
}

func TestVerifyFull(t *testing.T) {
	verifier := &starkVerifier{}

	// Create a minimal proof structure
	// This is a simplified test - real proofs would be much larger
	// Public inputs for Transfer1x2: [merkleRoot, boundParamsHash, nullifier0, commitment0, commitment1]
	publicInputs := []uint64{123, 456, 789, 1011, 1213}

	// Minimal proof: trace_commit(8) + constraint_commit(8) + fri_commits(32) + queries_placeholder(64) + pow_nonce(8)
	proofSize := 8 + 8 + 32 + 64 + 8
	proof := make([]byte, proofSize)

	// Fill with dummy data
	binary.LittleEndian.PutUint64(proof[0:8], 111111)         // trace_commit
	binary.LittleEndian.PutUint64(proof[8:16], 222222)        // constraint_commit
	binary.LittleEndian.PutUint64(proof[proofSize-8:], 12345) // pow_nonce

	// Build VERIFY_FULL input
	// Format: opcode(1) + variant(1) + circuitType(1) + proofLen(4) + proof + pubInputsCount(1) + pubInputs
	inputSize := 1 + 1 + 1 + 4 + proofSize + 1 + len(publicInputs)*8
	input := make([]byte, inputSize)
	input[0] = opcodeVerifyFull
	input[1] = variantRP256_64bit
	input[2] = circuitTransfer1x2
	binary.BigEndian.PutUint32(input[3:7], uint32(proofSize))
	copy(input[7:7+proofSize], proof)
	input[7+proofSize] = byte(len(publicInputs))
	for i, pi := range publicInputs {
		binary.LittleEndian.PutUint64(input[8+proofSize+i*8:], pi)
	}

	output, err := verifier.Run(input)
	if err != nil {
		t.Fatalf("VERIFY_FULL failed: %v", err)
	}

	t.Logf("VERIFY_FULL result: 0x%02x", output[0])
}

func TestVariants(t *testing.T) {
	verifier := &starkVerifier{}

	variants := []struct {
		name    string
		variant uint8
	}{
		{"RP256_64bit", variantRP256_64bit},
		{"RP512_64bit", variantRP512_64bit},
		{"RP256_128bit", variantRP256_128bit},
		{"RP512_128bit", variantRP512_128bit},
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			// Test session creation with each variant
			publicInputs := []uint64{123, 456}
			input := make([]byte, 1+1+1+1+len(publicInputs)*8)
			input[0] = opcodeStarkInit
			input[1] = v.variant
			input[2] = circuitTransfer1x2
			input[3] = byte(len(publicInputs))
			for i, pi := range publicInputs {
				binary.LittleEndian.PutUint64(input[4+i*8:], pi)
			}

			sessionID, err := verifier.Run(input)
			if err != nil {
				t.Fatalf("STARK_INIT failed for %s: %v", v.name, err)
			}

			if len(sessionID) != 32 {
				t.Errorf("expected 32-byte session ID, got %d bytes", len(sessionID))
			}

			t.Logf("Session ID for %s: %x", v.name, sessionID[:8])
		})
	}
}

func TestInvalidVariant(t *testing.T) {
	verifier := &starkVerifier{}

	input := make([]byte, 10)
	input[0] = opcodeStarkInit
	input[1] = 0xFF // Invalid variant
	input[2] = circuitTransfer1x2
	input[3] = 0

	_, err := verifier.Run(input)
	if err != errStarkInvalidVariant {
		t.Errorf("expected invalid variant error, got %v", err)
	}
}

func TestCircuitTypes(t *testing.T) {
	circuits := []struct {
		name       string
		circuitID  uint8
		inputs     int
		outputs    int
	}{
		{"Transfer1x2", circuitTransfer1x2, 1, 2},
		{"Transfer1x3", circuitTransfer1x3, 1, 3},
		{"Transfer2x2", circuitTransfer2x2, 2, 2},
		{"Transfer2x3", circuitTransfer2x3, 2, 3},
		{"Transfer8x2", circuitTransfer8x2, 8, 2},
	}

	verifier := &starkVerifier{}

	for _, c := range circuits {
		t.Run(c.name, func(t *testing.T) {
			// Public inputs: merkle_root + bound_params + nullifiers + commitments
			numPublicInputs := 2 + c.inputs + c.outputs
			publicInputs := make([]uint64, numPublicInputs)
			for i := range publicInputs {
				publicInputs[i] = uint64(i + 1)
			}

			input := make([]byte, 1+1+1+1+len(publicInputs)*8)
			input[0] = opcodeStarkInit
			input[1] = variantRP256_64bit
			input[2] = c.circuitID
			input[3] = byte(len(publicInputs))
			for i, pi := range publicInputs {
				binary.LittleEndian.PutUint64(input[4+i*8:], pi)
			}

			sessionID, err := verifier.Run(input)
			if err != nil {
				t.Fatalf("failed to create session for %s: %v", c.name, err)
			}

			t.Logf("Created session for %s (inputs=%d, outputs=%d): %x...",
				c.name, c.inputs, c.outputs, sessionID[:8])
		})
	}
}

func TestFriBatchGasScaling(t *testing.T) {
	verifier := &starkVerifier{}

	queryTests := []struct {
		queryCount  int
		expectedGas uint64
	}{
		{1, gasStarkFriBatch + 1*gasStarkFriPerQ},
		{5, gasStarkFriBatch + 5*gasStarkFriPerQ},
		{10, gasStarkFriBatch + 10*gasStarkFriPerQ},
		{30, gasStarkFriBatch + 30*gasStarkFriPerQ},
	}

	for _, tc := range queryTests {
		t.Run("queries_"+string(rune(tc.queryCount)), func(t *testing.T) {
			// Build FRI batch input with specified query count
			input := make([]byte, 35)
			input[0] = opcodeStarkFriBatch
			// session_id is bytes 1-32
			input[33] = 0                      // batch_index
			input[34] = byte(tc.queryCount)    // query_count

			gas := verifier.RequiredGas(input)
			if gas != tc.expectedGas {
				t.Errorf("expected gas %d for %d queries, got %d", tc.expectedGas, tc.queryCount, gas)
			}
		})
	}
}

func BenchmarkVerifyFull(b *testing.B) {
	verifier := &starkVerifier{}

	// Create test input
	// Public inputs for Transfer1x2: [merkleRoot, boundParamsHash, nullifier0, commitment0, commitment1]
	publicInputs := []uint64{123, 456, 789, 1011, 1213}
	proofSize := 8 + 8 + 32 + 64 + 8
	proof := make([]byte, proofSize)
	binary.LittleEndian.PutUint64(proof[0:8], 111111)
	binary.LittleEndian.PutUint64(proof[8:16], 222222)
	binary.LittleEndian.PutUint64(proof[proofSize-8:], 12345)

	// Format: opcode(1) + variant(1) + circuitType(1) + proofLen(4) + proof + pubInputsCount(1) + pubInputs
	inputSize := 1 + 1 + 1 + 4 + proofSize + 1 + len(publicInputs)*8
	input := make([]byte, inputSize)
	input[0] = opcodeVerifyFull
	input[1] = variantRP256_64bit
	input[2] = circuitTransfer1x2
	binary.BigEndian.PutUint32(input[3:7], uint32(proofSize))
	copy(input[7:7+proofSize], proof)
	input[7+proofSize] = byte(len(publicInputs))
	for i, pi := range publicInputs {
		binary.LittleEndian.PutUint64(input[8+proofSize+i*8:], pi)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		verifier.Run(input)
	}
}

func BenchmarkTranscriptInit(b *testing.B) {
	publicInputs := []uint64{123, 456, 789, 101112, 131415}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		initTranscript(variantRP256_64bit, publicInputs)
	}
}

func BenchmarkTranscriptUpdate(b *testing.B) {
	state := uint64(12345678901234567890)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updateTranscript(state, variantRP256_64bit, uint64(i), uint64(i+1))
	}
}

// =============================================================================
// EXTENDED CIRCUIT FORMAT TESTS (121+ circuits)
// =============================================================================

func TestParseCircuitType(t *testing.T) {
	tests := []struct {
		name           string
		input          []byte
		wantCategory   uint8
		wantInputs     int
		wantOutputs    int
	}{
		// Legacy single-byte formats
		{"Legacy Transfer1x2", []byte{0x00}, circuitCategoryTransfer, 1, 2},
		{"Legacy Transfer1x3", []byte{0x01}, circuitCategoryTransfer, 1, 3},
		{"Legacy Transfer2x2", []byte{0x02}, circuitCategoryTransfer, 2, 2},
		{"Legacy Transfer2x3", []byte{0x03}, circuitCategoryTransfer, 2, 3},
		{"Legacy Transfer8x2", []byte{0x04}, circuitCategoryTransfer, 8, 2},

		// Extended 3-byte formats: [category+0x10][inputs][outputs]
		{"Extended Transfer10x5", []byte{0x10, 10, 5}, circuitCategoryTransfer, 10, 5},
		{"Extended Transfer16x2", []byte{0x11, 16, 2}, circuitCategoryTransferExt, 16, 2},
		{"Extended NFT4x4", []byte{0x12, 4, 4}, circuitCategoryNFT, 4, 4},
		{"Extended POI_shield", []byte{0x13, 0, 0}, circuitCategoryPOI, 1, 1}, // 0 gets clamped to 1
		{"Extended NFT_batch_8x8", []byte{0x14, 8, 8}, circuitCategoryNFTBatch, 8, 8},

		// Edge cases
		{"Extended max inputs", []byte{0x11, 32, 2}, circuitCategoryTransferExt, 32, 2},
		{"Extended max outputs", []byte{0x11, 2, 16}, circuitCategoryTransferExt, 2, 16},
		{"Extended overflow inputs", []byte{0x11, 100, 2}, circuitCategoryTransferExt, 32, 2}, // Clamped
		{"Extended overflow outputs", []byte{0x11, 2, 50}, circuitCategoryTransferExt, 2, 16}, // Clamped

		// Empty/invalid input
		{"Empty input", []byte{}, circuitCategoryTransfer, 1, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			category, inputs, outputs := parseCircuitType(tc.input)
			if category != tc.wantCategory {
				t.Errorf("category: got %d, want %d", category, tc.wantCategory)
			}
			if inputs != tc.wantInputs {
				t.Errorf("inputs: got %d, want %d", inputs, tc.wantInputs)
			}
			if outputs != tc.wantOutputs {
				t.Errorf("outputs: got %d, want %d", outputs, tc.wantOutputs)
			}
		})
	}
}

func TestExtendedCircuitTypes(t *testing.T) {
	// Test all 121+ circuit configurations via extended format
	circuits := []struct {
		name     string
		category uint8
		inputs   int
		outputs  int
	}{
		// Standard transfers (50 circuits: 1-10 inputs × 1-5 outputs)
		{"Transfer1x1", circuitCategoryTransfer, 1, 1},
		{"Transfer5x5", circuitCategoryTransfer, 5, 5},
		{"Transfer10x5", circuitCategoryTransfer, 10, 5},

		// Extended transfers (38 circuits)
		{"Transfer11x2", circuitCategoryTransferExt, 11, 2},
		{"Transfer16x4", circuitCategoryTransferExt, 16, 4},
		{"Transfer20x2", circuitCategoryTransferExt, 20, 2},
		{"Transfer32x2", circuitCategoryTransferExt, 32, 2},
		{"Transfer2x8", circuitCategoryTransferExt, 2, 8},
		{"Transfer4x10", circuitCategoryTransferExt, 4, 10},

		// NFT circuits (16 circuits: 1-4 × 1-4)
		{"NFT1x1", circuitCategoryNFT, 1, 1},
		{"NFT2x2", circuitCategoryNFT, 2, 2},
		{"NFT4x4", circuitCategoryNFT, 4, 4},

		// NFT batch (5 circuits)
		{"NFT_batch_4x4", circuitCategoryNFTBatch, 4, 4},
		{"NFT_batch_8x8", circuitCategoryNFTBatch, 8, 8},
		{"NFT_batch_16x16", circuitCategoryNFTBatch, 16, 16},

		// POI circuits (4 circuits)
		{"POI_shield", circuitCategoryPOI, 0, 0},    // poi_type in inputs
		{"POI_transfer", circuitCategoryPOI, 1, 0},  // poi_type in inputs
		{"POI_recursive", circuitCategoryPOI, 2, 0}, // poi_type in inputs
		{"POI_batch", circuitCategoryPOI, 3, 0},     // poi_type in inputs
	}

	for _, c := range circuits {
		t.Run(c.name, func(t *testing.T) {
			// Test getCircuitConfigExtended
			config := getCircuitConfigExtended(c.category, c.inputs, c.outputs)

			// Verify config is valid
			if config.NumInputs <= 0 && c.category != circuitCategoryPOI {
				t.Errorf("invalid NumInputs: %d", config.NumInputs)
			}
			if config.NumOutputs <= 0 && c.category != circuitCategoryPOI {
				t.Errorf("invalid NumOutputs: %d", config.NumOutputs)
			}
			if config.TraceWidth <= 0 {
				t.Errorf("invalid TraceWidth: %d", config.TraceWidth)
			}
			if config.SelectorMerkleCol < 0 {
				t.Errorf("invalid SelectorMerkleCol: %d", config.SelectorMerkleCol)
			}

			t.Logf("%s: inputs=%d, outputs=%d, traceWidth=%d",
				c.name, config.NumInputs, config.NumOutputs, config.TraceWidth)
		})
	}
}

func TestVerifyFullExtendedFormat(t *testing.T) {
	verifier := &starkVerifier{}

	// Test extended format with Transfer10x5
	inputs := 10
	outputs := 5

	// Public inputs: merkle_root + bound_params + nullifiers + commitments
	numPublicInputs := 2 + inputs + outputs
	publicInputs := make([]uint64, numPublicInputs)
	for i := range publicInputs {
		publicInputs[i] = uint64(i + 1)
	}

	// Minimal proof
	proofSize := 8 + 8 + 32 + 64 + 8
	proof := make([]byte, proofSize)
	binary.LittleEndian.PutUint64(proof[0:8], 111111)
	binary.LittleEndian.PutUint64(proof[8:16], 222222)
	binary.LittleEndian.PutUint64(proof[proofSize-8:], 12345)

	// Build VERIFY_FULL input with extended format
	// Format: opcode(1) + variant(1) + category+0x10(1) + inputs(1) + outputs(1) + proofLen(4) + proof + pubInputsCount(1) + pubInputs
	inputSize := 1 + 1 + 1 + 1 + 1 + 4 + proofSize + 1 + len(publicInputs)*8
	input := make([]byte, inputSize)
	input[0] = opcodeVerifyFull
	input[1] = variantRP256_64bit
	input[2] = 0x10 // category TRANSFER + 0x10
	input[3] = byte(inputs)
	input[4] = byte(outputs)
	binary.BigEndian.PutUint32(input[5:9], uint32(proofSize))
	copy(input[9:9+proofSize], proof)
	input[9+proofSize] = byte(len(publicInputs))
	for i, pi := range publicInputs {
		binary.LittleEndian.PutUint64(input[10+proofSize+i*8:], pi)
	}

	output, err := verifier.Run(input)
	if err != nil {
		t.Fatalf("VERIFY_FULL extended format failed: %v", err)
	}

	t.Logf("VERIFY_FULL extended format (Transfer%dx%d) result: 0x%02x", inputs, outputs, output[0])
}

func TestVerifyFullExtendedNFT(t *testing.T) {
	verifier := &starkVerifier{}

	// Test extended format with NFT4x4
	inputs := 4
	outputs := 4

	// Public inputs for NFT: merkle_root + bound_params + nullifiers + commitments
	numPublicInputs := 2 + inputs + outputs
	publicInputs := make([]uint64, numPublicInputs)
	for i := range publicInputs {
		publicInputs[i] = uint64(i + 1000)
	}

	// Minimal proof
	proofSize := 8 + 8 + 32 + 64 + 8
	proof := make([]byte, proofSize)
	binary.LittleEndian.PutUint64(proof[0:8], 333333)
	binary.LittleEndian.PutUint64(proof[8:16], 444444)
	binary.LittleEndian.PutUint64(proof[proofSize-8:], 54321)

	// Build VERIFY_FULL input with NFT extended format
	inputSize := 1 + 1 + 1 + 1 + 1 + 4 + proofSize + 1 + len(publicInputs)*8
	input := make([]byte, inputSize)
	input[0] = opcodeVerifyFull
	input[1] = variantRP512_64bit // Use 512-bit variant for NFT
	input[2] = 0x12               // category NFT + 0x10
	input[3] = byte(inputs)
	input[4] = byte(outputs)
	binary.BigEndian.PutUint32(input[5:9], uint32(proofSize))
	copy(input[9:9+proofSize], proof)
	input[9+proofSize] = byte(len(publicInputs))
	for i, pi := range publicInputs {
		binary.LittleEndian.PutUint64(input[10+proofSize+i*8:], pi)
	}

	output, err := verifier.Run(input)
	if err != nil {
		t.Fatalf("VERIFY_FULL NFT extended format failed: %v", err)
	}

	t.Logf("VERIFY_FULL NFT extended format (NFT%dx%d) result: 0x%02x", inputs, outputs, output[0])
}

func TestCircuitConfigFromIO(t *testing.T) {
	// Test trace width calculation matches SDK formula
	tests := []struct {
		inputs        int
		outputs       int
		expectedWidth int
	}{
		// BASE_COLUMNS=6, COLUMNS_PER_INPUT=5, COLUMNS_PER_OUTPUT=4
		// width = 6 + inputs*5 + outputs*4
		{1, 2, 6 + 1*5 + 2*4},  // 19
		{1, 3, 6 + 1*5 + 3*4},  // 23
		{2, 2, 6 + 2*5 + 2*4},  // 24
		{8, 2, 6 + 8*5 + 2*4},  // 54
		{10, 5, 6 + 10*5 + 5*4}, // 76
		{16, 2, 6 + 16*5 + 2*4}, // 94
		{32, 2, 6 + 32*5 + 2*4}, // 174
	}

	for _, tc := range tests {
		config := getCircuitConfigFromIO(tc.inputs, tc.outputs)
		if config.TraceWidth != tc.expectedWidth {
			t.Errorf("TraceWidth for %dx%d: got %d, want %d",
				tc.inputs, tc.outputs, config.TraceWidth, tc.expectedWidth)
		}
		if config.NumInputs != tc.inputs {
			t.Errorf("NumInputs for %dx%d: got %d, want %d",
				tc.inputs, tc.outputs, config.NumInputs, tc.inputs)
		}
		if config.NumOutputs != tc.outputs {
			t.Errorf("NumOutputs for %dx%d: got %d, want %d",
				tc.inputs, tc.outputs, config.NumOutputs, tc.outputs)
		}
	}
}
