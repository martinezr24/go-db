package storage

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mit.edu/dsg/godb/common"
)

func TestDiskDBFile_Allocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_alloc.dat")
	f, err := os.Create(path)
	require.NoError(t, err)

	dbFile, err := NewDiskDBFile(f)
	require.NoError(t, err)
	defer dbFile.Close()

	// Initial state should be 0 pages
	pages, err := dbFile.NumPages()
	require.NoError(t, err)
	assert.Equal(t, 0, pages)

	// Allocate 5 pages
	startPage, err := dbFile.AllocatePage(5)
	require.NoError(t, err)
	assert.Equal(t, 0, startPage)

	// Verify Logical State
	pages, err = dbFile.NumPages()
	require.NoError(t, err)
	assert.Equal(t, 5, pages)

	// Verify Physical State (File numBits on disk)
	stat, err := f.Stat()
	require.NoError(t, err)
	expectedSize := int64(5 * common.PageSize)
	assert.Equal(t, expectedSize, stat.Size(), "Physical file numBits should match allocation")
}

func TestDiskDBFile_ReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_rw.dat")
	f, err := os.Create(path)
	require.NoError(t, err)

	dbFile, err := NewDiskDBFile(f)
	require.NoError(t, err)
	defer dbFile.Close()

	_, err = dbFile.AllocatePage(1)
	require.NoError(t, err)

	// Test Bounds Checking
	emptyBuf := make([]byte, common.PageSize)
	err = dbFile.ReadPage(1, emptyBuf)
	assert.Error(t, err, "Should fail to read page 1 (only 0 allocated)")
	err = dbFile.WritePage(1, emptyBuf)
	assert.Error(t, err, "Should fail to write page 1 (only 0 allocated)")

	// Test Write Persistence
	data := make([]byte, common.PageSize)
	copy(data, []byte("Hello Godb Storage Layer"))

	err = dbFile.WritePage(0, data)
	require.NoError(t, err)

	// Test Read verification
	readBuf := make([]byte, common.PageSize)
	err = dbFile.ReadPage(0, readBuf)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, readBuf))

	// Test Zero-Initialization of new pages
	_, err = dbFile.AllocatePage(1) // Allocates page 1
	require.NoError(t, err)

	err = dbFile.ReadPage(1, readBuf)
	require.NoError(t, err)
	// New page should be all zeros
	assert.True(t, bytes.Equal(make([]byte, common.PageSize), readBuf), "New page should be zero-filled")
}

func TestDiskDBFile_PersistenceReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_persist.dat")

	// Phase 1: Create and Write
	{
		f, err := os.Create(path)
		require.NoError(t, err)

		dbFile, err := NewDiskDBFile(f)
		require.NoError(t, err)

		_, err = dbFile.AllocatePage(1)
		require.NoError(t, err)

		data := make([]byte, common.PageSize)
		copy(data, []byte("Persistent Data"))
		err = dbFile.WritePage(0, data)
		require.NoError(t, err)

		dbFile.Close()
	}

	// Phase 2: Reopen and Verify
	{
		f, err := os.OpenFile(path, os.O_RDWR, 0666)
		require.NoError(t, err)

		dbFile, err := NewDiskDBFile(f)
		require.NoError(t, err)
		defer dbFile.Close()

		// Verify numBits restored
		pages, err := dbFile.NumPages()
		require.NoError(t, err)
		assert.Equal(t, 1, pages)

		// Verify data restored
		readBuf := make([]byte, common.PageSize)
		err = dbFile.ReadPage(0, readBuf)
		require.NoError(t, err)

		expected := make([]byte, common.PageSize)
		copy(expected, []byte("Persistent Data"))
		assert.True(t, bytes.Equal(expected, readBuf))
	}
}

func TestDiskDBFile_ConcurrentOperations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_concurrent.dat")
	f, err := os.Create(path)
	require.NoError(t, err)

	dbFile, err := NewDiskDBFile(f)
	require.NoError(t, err)
	defer dbFile.Close()

	numGoroutines := 20
	allocsPerRoutine := 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Stress Test: Multiple goroutines allocating and writing simultaneously.
	// This verifies:
	// 1. AllocatePage mutex is working (no race on file numBits or truncating).
	// 2. WritePage is thread-safe (pread/pwrite don't interfere).
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < allocsPerRoutine; j++ {
				// 1. Concurrent Allocation
				pgNo, err := dbFile.AllocatePage(1)
				assert.NoError(t, err)

				// 2. Concurrent Write to the page we just grabbed
				data := make([]byte, common.PageSize)
				// Create unique content signature: "RoutineID-Seq-PageNum"
				content := fmt.Sprintf("R%d-S%d-P%d", id, j, pgNo)
				copy(data, []byte(content))

				err = dbFile.WritePage(pgNo, data)
				assert.NoError(t, err)

				// 3. Concurrent Read back to verify
				readBuf := make([]byte, common.PageSize)
				err = dbFile.ReadPage(pgNo, readBuf)
				assert.NoError(t, err)

				// Using bytes.HasPrefix because the buffer is zero-padded to PageSize
				assert.True(t, bytes.HasPrefix(readBuf, []byte(content)), "Data corruption or mismatch on page %d", pgNo)
			}
		}(i)
	}

	wg.Wait()

	// Final Verification
	totalExpected := numGoroutines * allocsPerRoutine
	n, err := dbFile.NumPages()
	require.NoError(t, err)
	assert.Equal(t, totalExpected, n, "Total pages should match sum of all allocations")
}
