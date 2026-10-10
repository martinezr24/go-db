package common

import "fmt"

// Align8 rounds the given integer up to the nearest multiple of 8.
// This is typically used to ensure proper memory alignment for 64-bit
// structures or to pad allocations.
func Align8(n int) int {
	return (n + 7) &^ 7
}

// AlignedTo8 returns true if the integer is a multiple of 8.
// This is useful for asserting that a pointer or offset is valid for
// 64-bit reads/writes without padding.
func AlignedTo8(n int) bool {
	return n%8 == 0
}

// Assert checks a condition and panics if it is false.
//
// WHY USE THIS INSTEAD OF RETURNING ERROR?
// In idiomatic Go, you are encouraged to return error values for conditions that might reasonably happen
// (e.g., "file not found" or "network timeout"). However, complex system engineering often relies on invariants:
//
//	truths about the system state that must always be valid. Assertions are useful for the following cases:
//	1. Fail Fast: In a database, if internal logic is broken (e.g., a lock count is negative),
//	   continuing execution is dangerous. It is better to crash and restart than to persist corrupted data.
//	2. Documentation: An Assert tells other developers: "I guarantee this condition is true here."
//	3. Debugging: The panic provides a stack trace immediately pointing to the logic error.
//
// WHEN TO USE:
// - Checking for "impossible" conditions (e.g., switch default cases that shouldn't be reached).
// - Verifying internal data structure integrity (e.g., head.prev should be nil).
//
// WHEN NOT TO USE:
// - Validating user input (return an error instead).
// - Handling I/O failures like "disk full" (return an error instead).
func Assert(cond bool, format string, args ...any) {
	if !cond {
		panic(fmt.Sprintf(format, args...))
	}
}

const (
	offset64 = 14695981039346656037
	prime64  = 1099511628211
)

// Hash computes the FNV-1a 64-bit hash of the provided byte slice without allocation. This is preferrable to the
// standard go library version that will force allocation. It is a non-cryptographic hash function optimized for speed
// and distribution, suitable for use in Hash Maps or Bloom Filters.
func Hash(data []byte) uint64 {
	// Inline FNV-1a implementation
	var h uint64 = offset64
	for _, b := range data {
		h ^= uint64(b)
		h *= prime64
	}
	return h
}
