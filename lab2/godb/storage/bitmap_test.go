package storage

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func verifyBitmap(t *testing.T, bm Bitmap, shadow []bool) {
	for i := 0; i < len(shadow); i++ {
		actual := bm.LoadBit(i)
		expected := shadow[i]
		assert.Equal(t, expected, actual, "Mismatch at bit %d", i)
	}
}

func checkFindFirstZero(t *testing.T, bm Bitmap, startIndex int, expected int) {
	actual := bm.FindFirstZero(startIndex)
	assert.Equal(t, expected, actual, "FindFirstZero mismatch starting at index %d", startIndex)
	if expected != -1 {
		assert.False(t, bm.LoadBit(actual), "FindFirstZero returned a set bit")
	}
}

func verifyFindFirstZero(t *testing.T, bm Bitmap, shadow []bool, startIndex int) int {
	expected := -1
	for i := startIndex; i < len(shadow); i++ {
		if !shadow[i] {
			expected = i
			break
		}
	}
	if expected == -1 {
		for i := 0; i < startIndex; i++ {
			if !shadow[i] {
				expected = i
				break
			}
		}
	}
	actual := bm.FindFirstZero(startIndex)
	assert.Equal(t, expected, actual, "FindFirstZero mismatch starting at index %d", startIndex)
	return actual
}

// Helper to verify memory guard pages (canaries)
func checkCanaries(t *testing.T, rawMemory []byte, canarySize int, canaryPattern byte) {
	for i := 0; i < canarySize; i++ {
		assert.Equal(t, canaryPattern, rawMemory[i], "Memory corruption in PRE-canary at byte %d", i)
	}
	endStart := len(rawMemory) - canarySize
	for i := endStart; i < len(rawMemory); i++ {
		assert.Equal(t, canaryPattern, rawMemory[i], "Memory corruption in POST-canary at byte %d", i)
	}
}

// runRandomizedTest executes a randomized test on the Bitmap implementation against a simple reference of []bool.
//
// Scenario:
// 1. Memory Layout: Allocates a byte slice with "Canary" patterns at the start and end to detect out-of-bounds writes.
// 3. Operations: Performs 100,000 random operations including:
//   - SetBit: Toggles random bits.
//   - LoadBit: Verifies bit values against shadow.
//   - FindFirstZero: Searches for free slots and fills them (simulating allocation).
//   - Range Set: Toggles a contiguous range of bits (stress test).
//
// Assertions:
// - Correctness: Every LoadBit must match the Shadow slice.
// - Search: FindFirstZero must return the correct first index as determined by the Shadow slice.
// - Memory Safety: The Canary bytes at the boundaries must remain untouched (no buffer overflows).
func runRandomizedTest(t *testing.T, numBits int, seed int64) {
	r := rand.New(rand.NewSource(seed))

	// Layout: [Canary] [Bitmap Data] [Canary]
	canarySize := 8
	canaryPattern := byte(0xAA)
	payloadSize := (numBits + 63) / 64 * 8
	totalSize := payloadSize + (2 * canarySize)

	rawMemory := make([]byte, totalSize)

	for i := 0; i < canarySize; i++ {
		rawMemory[i] = canaryPattern
	}
	for i := totalSize - canarySize; i < totalSize; i++ {
		rawMemory[i] = canaryPattern
	}

	// Initialize buffer with random data for the payload section only
	bitmapData := rawMemory[canarySize : canarySize+payloadSize]
	r.Read(bitmapData)

	bm := AsBitmap(bitmapData, numBits)

	shadow := make([]bool, numBits)
	for i := 0; i < len(shadow); i++ {
		shadow[i] = bm.LoadBit(i)
	}

	iterations := 100000

	for i := 0; i < iterations; i++ {
		op := r.Intn(5)

		switch op {
		case 0: // Set random bit
			idx := r.Intn(numBits)
			on := r.Intn(2) == 0
			prev := bm.SetBit(idx, on)
			assert.Equal(t, shadow[idx], prev, "SetBit return value mismatch at iter %d", i)
			shadow[idx] = on

		case 1: // Standard: Check LoadBit
			idx := r.Intn(numBits)
			assert.Equal(t, shadow[idx], bm.LoadBit(idx), "LoadBit mismatch at iter %d", i)

		case 2: // FindFirstZero and immediately fill it (Simulation of allocation)
			startHint := r.Intn(numBits)
			idx := verifyFindFirstZero(t, bm, shadow, startHint)
			if idx != -1 {
				// If we found a zero, fill it and update shadow
				bm.SetBit(idx, true)
				shadow[idx] = true
			}
		case 3: // Mass toggle a range (Stress test)
			start := r.Intn(numBits)
			length := r.Intn(20) + 1 // Toggle up to 20 bits
			for j := 0; j < length; j++ {
				target := start + j
				if target >= numBits {
					break
				}
				val := r.Intn(2) == 0
				bm.SetBit(target, val)
				shadow[target] = val
			}

		case 4: // Verify consistency
			verifyBitmap(t, bm, shadow)
			checkCanaries(t, rawMemory, canarySize, canaryPattern)
		}
	}

	// Final verification
	verifyBitmap(t, bm, shadow)
	checkCanaries(t, rawMemory, canarySize, canaryPattern)
}

func TestBitmapSimpleSetLoad(t *testing.T) {
	numBits := 100
	buf := make([]byte, 16)
	bm := AsBitmap(buf, numBits)
	shadow := make([]bool, numBits)

	verifyBitmap(t, bm, shadow)
	// Set bits crossing word boundaries
	indicesToSet := []int{0, 1, 63, 64, 99}
	for _, idx := range indicesToSet {
		prev := bm.SetBit(idx, true)
		assert.Equal(t, shadow[idx], prev, "Unexpected previous value at %d", idx)
		shadow[idx] = true
	}
	verifyBitmap(t, bm, shadow)

	indicesToUnset := []int{0, 2, 63, 60, 98}
	for _, idx := range indicesToUnset {
		prev := bm.SetBit(idx, false)
		assert.Equal(t, shadow[idx], prev, "Unexpected previous value at %d", idx)
		shadow[idx] = false
	}
	verifyBitmap(t, bm, shadow)

	indicesToSet = []int{1, 2, 65, 31}
	for _, idx := range indicesToSet {
		prev := bm.SetBit(idx, true)
		assert.Equal(t, shadow[idx], prev, "Unexpected previous value at %d", idx)
		shadow[idx] = true
	}
	verifyBitmap(t, bm, shadow)
}

func TestBitmapSimpleFindFirstZero(t *testing.T) {
	numBits := 100
	buf := make([]byte, 16)
	bm := AsBitmap(buf, numBits)

	// Initially all zero
	checkFindFirstZero(t, bm, 0, 0)
	checkFindFirstZero(t, bm, 42, 42)

	for i := 0; i < 10; i++ {
		bm.SetBit(i, true)
	}
	checkFindFirstZero(t, bm, 0, 10)
	checkFindFirstZero(t, bm, 5, 10)
	checkFindFirstZero(t, bm, 10, 10)
	checkFindFirstZero(t, bm, 64, 64)

	for i := 10; i < 64; i++ {
		bm.SetBit(i, true)
	}
	checkFindFirstZero(t, bm, 31, 64)
	checkFindFirstZero(t, bm, 65, 65)

	for i := 64; i < numBits; i++ {
		bm.SetBit(i, true)
	}
	checkFindFirstZero(t, bm, 64, -1)
	checkFindFirstZero(t, bm, 99, -1)
	checkFindFirstZero(t, bm, 4, -1)

	bm.SetBit(50, false)

	checkFindFirstZero(t, bm, 0, 50)
	checkFindFirstZero(t, bm, 99, 50)
	checkFindFirstZero(t, bm, 63, 50)

	// 6. Create a hole at the very end
	lastIdx := 98
	bm.SetBit(lastIdx, false)
	checkFindFirstZero(t, bm, 16, 50)
	checkFindFirstZero(t, bm, 63, 98)
	checkFindFirstZero(t, bm, 99, 50)
}

func TestBitmapRandomizedSmall(t *testing.T) {
	runRandomizedTest(t, 43, 65830)
}

func TestBitmapRandomizedLarge(t *testing.T) {
	runRandomizedTest(t, 500, 65831)
}
