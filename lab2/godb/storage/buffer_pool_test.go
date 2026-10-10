package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mit.edu/dsg/godb/common"
)

func setupBufferPool(t *testing.T, numPages int) (*BufferPool, *StatsDBFileManager, string) {
	rootPath := t.TempDir()
	realSm := NewDiskStorageManager(rootPath)
	statsSm := &StatsDBFileManager{
		Inner: realSm,
		Files: xsync.NewMapOf[common.ObjectID, *StatsDBFile](),
	}

	bp := NewBufferPool(numPages, statsSm, NoopLogManager{})
	return bp, statsSm, rootPath
}

func createDummyFile(t *testing.T, bp *BufferPool, oid common.ObjectID, numPages int) {
	sm := bp.StorageManager()
	file, err := sm.GetDBFile(oid)
	require.NoError(t, err)

	_, err = file.AllocatePage(numPages)
	require.NoError(t, err)

	for i := 0; i < numPages; i++ {
		data := make([]byte, common.PageSize)
		copy(data, []byte(fmt.Sprintf("Page-%d", i)))
		err := file.WritePage(i, data)
		require.NoError(t, err)
	}

	// Zero counters for test start
	file.(*StatsDBFile).WriteCnt.Store(0)
	file.(*StatsDBFile).ReadCnt.Store(0)
}

// TestBufferPool_SimpleReadWrite verifies the basic functionality of the buffer pool.
// It checks that:
// 1. Pages are read from disk on first access.
// 2. Pages are cached and served from memory on subsequent accesses.
// 3. Dirty pages are written back to disk upon eviction.
// 4. Clean pages are not written back to disk.
func TestBufferPool_SimpleReadWrite(t *testing.T) {
	bp, statsSm, _ := setupBufferPool(t, 1)
	oid := common.ObjectID(1)

	createDummyFile(t, bp, oid, 2)
	stats, _ := statsSm.Files.Load(oid)

	pid0 := common.PageID{Oid: oid, PageNum: 0}
	f1, err := bp.GetPage(pid0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.ReadCnt.Load(), "First access should read from disk")
	assert.True(t, bytes.HasPrefix(f1.Bytes[:], []byte("Page-0")), "Access should read correct data")
	f2, err := bp.GetPage(pid0)
	require.NoError(t, err)
	assert.Equal(t, f1, f2, "Second access should return the same frame")
	assert.Equal(t, int64(1), stats.ReadCnt.Load(), "Second access should be cached")
	bp.UnpinPage(f1, false)
	bp.UnpinPage(f2, false)

	pid1 := common.PageID{Oid: oid, PageNum: 1}
	// Since buffer pool has capacity of 1, this should trigger an eviction
	f3, err := bp.GetPage(pid1)
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.ReadCnt.Load(), "Second access should read from disk")
	assert.Equal(t, f2, f3, "Frames should be reused")
	assert.Equal(t, int64(0), stats.WriteCnt.Load(), "undirtied page should not be written to disk")
	assert.True(t, bytes.HasPrefix(f3.Bytes[:], []byte("Page-1")), "Access should read correct data")

	dirtyData := []byte("DirtyData")
	copy(f3.Bytes[:], dirtyData)
	bp.UnpinPage(f3, true)
	// Now it should succeed
	f4, err := bp.GetPage(pid0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats.ReadCnt.Load(), "First access should read from disk")
	assert.Equal(t, int64(1), stats.WriteCnt.Load(), "dirty pages should be written to disk")
	assert.True(t, bytes.HasPrefix(f4.Bytes[:], []byte("Page-0")), "Access should read correct data")
	bp.UnpinPage(f4, false)

	f5, err := bp.GetPage(pid1)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(f5.Bytes[:], []byte("DirtyData")), "dirty page content is not currently flushed to disk")
}

// TestBufferPool_FlushAll tests the FlushAllPages method.
// It verifies that all dirty pages are written to disk when requested, regardless of whether
// they are currently pinned. This is important for recovery later in the semester
func TestBufferPool_FlushAll(t *testing.T) {
	bp, statsSm, rootPath := setupBufferPool(t, 5)
	oid := common.ObjectID(50)
	createDummyFile(t, bp, oid, 5)
	stats, _ := statsSm.Files.Load(oid)

	// Load and dirty 3 pages
	for i := 0; i < 3; i++ {
		pid := common.PageID{Oid: oid, PageNum: int32(i)}
		f, _ := bp.GetPage(pid)
		copy(f.Bytes[:], []byte(fmt.Sprintf("FlushTest-%d", i)))
		bp.UnpinPage(f, true)
	}

	f, err := bp.GetPage(common.PageID{Oid: oid, PageNum: 2})
	require.NoError(t, err)

	// Force flush everything, pins should not prevent flushing
	err = bp.FlushAllPages()
	require.NoError(t, err)

	// Verify disk
	filePath := filepath.Join(rootPath, fmt.Sprintf("dbo_%d.dat", oid))
	fileBytes, _ := os.ReadFile(filePath)
	assert.Equal(t, int64(3), stats.WriteCnt.Load(), "All dirty pages regardless of pin should be written to disk")

	for i := 0; i < 3; i++ {
		start := i * common.PageSize
		pageBytes := fileBytes[start : start+common.PageSize]
		expected := []byte(fmt.Sprintf("FlushTest-%d", i))
		assert.True(t, bytes.HasPrefix(pageBytes, expected), "Page %d not flushed", i)
	}

	bp.UnpinPage(f, false)
}

type SlowDBFile struct {
	DBFile
	Delay time.Duration
}

func (f *SlowDBFile) ReadPage(pageNum int, frame []byte) error {
	time.Sleep(f.Delay)
	return f.DBFile.ReadPage(pageNum, frame)
}

func (f *SlowDBFile) WritePage(pageNum int, frame []byte) error {
	time.Sleep(f.Delay)
	return f.DBFile.WritePage(pageNum, frame)
}

// TestBufferPool_IOConcurrency verifies that disk I/O does not block the entire Buffer Pool.
// It uses a mock file with an artificial delay to simulate slow I/O.
//
// Scenario:
// 1. Fill the pool with dirty pages.
// 2. 10 concurrent goroutines request 10 NEW pages, forcing evictions.
// 3. Each operation involves: Write (Flush) + Read (Fetch).
//
// Assertion:
// The total time taken should be significantly less than (10 * (write_delay + read_delay)),
// proving that the Buffer Pool releases the global lock (if any) during slow I/O operations.
func TestBufferPool_IOConcurrency(t *testing.T) {
	// Setup
	poolSize := 10
	numPages := 20
	bp, _, _ := setupBufferPool(t, poolSize)
	oid := common.ObjectID(888)
	createDummyFile(t, bp, oid, numPages)

	for i := 0; i < poolSize; i++ {
		pid := common.PageID{Oid: oid, PageNum: int32(i)}
		f, err := bp.GetPage(pid)
		require.NoError(t, err)
		f.Bytes[0] = 99 // Dirty the page
		bp.UnpinPage(f, true)
	}

	sm := bp.StorageManager().(*StatsDBFileManager)
	realFile, _ := sm.Inner.GetDBFile(oid)
	slowFile := &SlowDBFile{DBFile: realFile, Delay: 50 * time.Millisecond}

	statsFile := &StatsDBFile{DBFile: slowFile}
	sm.Files.Store(oid, statsFile)

	start := time.Now()
	var wg sync.WaitGroup

	// Launch 10 concurrent readers for DISJOINT NEW pages (10-19)
	// This forces 10 evictions of the dirty pages (0-9).
	for i := poolSize; i < numPages; i++ {
		wg.Add(1)
		go func(pg int) {
			defer wg.Done()
			pid := common.PageID{Oid: oid, PageNum: int32(pg)}
			f, err := bp.GetPage(pid)
			assert.NoError(t, err)
			bp.UnpinPage(f, false)
		}(i)
	}
	wg.Wait()
	duration := time.Since(start)

	// Verify that we actually performed I/O
	assert.Equal(t, int64(10), statsFile.ReadCnt.Load(), "Should have read 10 new pages")
	assert.Equal(t, int64(10), statsFile.WriteCnt.Load(), "Should have flushed 10 dirty pages")

	// Assert:
	// Sequential Cost: 10 * (50ms Write + 50ms Read) = 1000ms.
	// Parallel Cost: ~100ms (ideal) + overhead.
	// We set a threshold of 200ms to be safe but strict enough to catch sequential execution.
	assert.Less(t, duration, 200*time.Millisecond, "BufferPool appears to hold a global lock during Disk I/O (Flush+Fetch)")
}

// TestBufferPool_EvictionLiveness ensures that the eviction policy does not spend a long time findin a victim
// in a large pool when the pool is full of "hot" (referenced) pages.
//
// Scenario:
// 1. Fill the pool with pages and access them repeatedly to mark them as referenced.
// 2. Request one additional page to force eviction.
//
// Assertion:
// The request must succeed within a reasonable timeout, verifying that the policy eventually
// degrades the status of referenced pages (e.g., clears ref bits) to find a victim.
func TestBufferPool_EvictionLiveness(t *testing.T) {
	poolSize := 100000
	bp, _, _ := setupBufferPool(t, poolSize)
	oid := common.ObjectID(777)
	createDummyFile(t, bp, oid, poolSize+1)

	// Fill the pool and repeatedly access to ensure the pool thinks the pages are hot
	for i := 0; i < poolSize; i++ {
		pid := common.PageID{Oid: oid, PageNum: int32(i)}
		f, err := bp.GetPage(pid)
		require.NoError(t, err)
		bp.UnpinPage(f, false)
		f2, _ := bp.GetPage(pid)
		bp.UnpinPage(f2, false)
	}

	done := make(chan bool)
	go func() {
		pid := common.PageID{Oid: oid, PageNum: int32(poolSize)}
		f, err := bp.GetPage(pid)
		assert.NoError(t, err)
		if f != nil {
			bp.UnpinPage(f, false)
		}
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Millisecond):
		t.Fatal("GetPage timed out! Eviction policy likely spent too much time searching for victim.")
	}
}

// TestBufferPool_Concurrent_LostUpdate checks for "Lost Updates" and "Torn Reads".
//
// Scenario:
// 1. Writer: Latches (Write), Increments counter at 5 different offsets, Unlatches, Unpins(dirty=true).
// 2. Reader: Latches (Read), Checks that ALL 5 offsets have the SAME value, Unlatches, Unpins(dirty=false).
// 3. Flusher: Continuously flushes in background.
//
// Assertions:
// - Isolation: The Reader must never see mismatching values (torn write) while holding the RLock.
// - Persistence: After everything stops, the disk must reflect the final counter value (no lost dirty bits).
func TestBufferPool_Concurrent_LostUpdate(t *testing.T) {
	bp, statsSm, rootPath := setupBufferPool(t, 10)
	oid := common.ObjectID(200)
	createDummyFile(t, bp, oid, 1)

	stats, _ := statsSm.Files.Load(oid)
	stats.WriteCnt.Store(0)

	pid := common.PageID{Oid: oid, PageNum: 0}

	// Initialize page with 0 at all offsets
	f, err := bp.GetPage(pid)
	require.NoError(t, err)

	offsets := []int{8, 1000, 2000, 3000, 4088}

	f.PageLatch.Lock()
	for _, off := range offsets {
		binary.LittleEndian.PutUint64(f.Bytes[off:], 0)
	}
	f.PageLatch.Unlock()
	bp.UnpinPage(f, true)

	iterations := 100000
	var workerWg sync.WaitGroup
	var flusherWg sync.WaitGroup
	var stopFlusher atomic.Bool

	// Writer Thread
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		for i := 0; i < iterations; i++ {
			f, err := bp.GetPage(pid)
			assert.NoError(t, err, "Failed to get page for writing")
			f.PageLatch.Lock()
			val := binary.LittleEndian.Uint64(f.Bytes[offsets[0]:])
			newVal := val + 1

			for _, off := range offsets {
				binary.LittleEndian.PutUint64(f.Bytes[off:], newVal)
				runtime.Gosched()
			}

			f.PageLatch.Unlock()
			bp.UnpinPage(f, true)
			runtime.Gosched()
		}
	}()

	// Reader Thread
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		for i := 0; i < iterations; i++ {
			f, err := bp.GetPage(pid)
			assert.NoError(t, err, "Failed to get page for reading")
			f.PageLatch.RLock()
			// Integrity Check: All offsets must have the exact same value.
			// If not, it means we are seeing a "Torn Read".
			baseVal := binary.LittleEndian.Uint64(f.Bytes[offsets[0]:])

			for idx, off := range offsets {
				currVal := binary.LittleEndian.Uint64(f.Bytes[off:])
				assert.Equal(t, baseVal, currVal, "Torn Read detected at iter %d! Offset[%d]=%d", i, idx, currVal)
				runtime.Gosched()
			}
			f.PageLatch.RUnlock()

			bp.UnpinPage(f, false)
			runtime.Gosched()
		}
	}()

	// Background Flusher
	flusherWg.Add(1)
	go func() {
		defer flusherWg.Done()
		for !stopFlusher.Load() {
			_ = bp.FlushAllPages()
			time.Sleep(time.Millisecond)
		}
	}()

	// Wait for workers
	workerWg.Wait()

	// Stop Flusher
	stopFlusher.Store(true)
	flusherWg.Wait()

	// Force Final Flush
	err = bp.FlushAllPages()
	require.NoError(t, err)

	// Verify Disk Persistence (Lost Update Check)
	assert.Greater(t, stats.WriteCnt.Load(), int64(1), "Background flusher should have triggered writes")

	filePath := filepath.Join(rootPath, fmt.Sprintf("dbo_%d.dat", oid))
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	for idx, off := range offsets {
		val := binary.LittleEndian.Uint64(content[off:])
		assert.Equal(t, uint64(iterations), val, "Mismatch at offset idx %d. Expected %d, got %d. (Lost Update)", idx, iterations, val)
	}
}

// TestBufferPool_Concurrent_EvictionStorm stresses the Buffer Pool's eviction policy and locking.
//
// Scenario:
// 1. Pool smaller than working set.
// 2. Many Threads (10) randomly accessing a larger set of pages (10).
// 3. Each thread pins a page, writes a signature, sleeps (simulating work), and unpins.
//
// Assertions:
// - Deadlock Freedom: The system must not hang despite heavy contention on the eviction locks.
// - Exclusive Access: A frame must not be evicted while a thread has it pinned.
func TestBufferPool_Concurrent_EvictionStorm(t *testing.T) {
	numPages := 10
	poolSize := 8
	bp, _, _ := setupBufferPool(t, poolSize)
	oid := common.ObjectID(100)
	createDummyFile(t, bp, oid, numPages)
	var wg sync.WaitGroup
	numThreads := 2 * runtime.NumCPU()
	opsPerThread := 100000

	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		go func(tid int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(tid)))

			for j := 0; j < opsPerThread; j++ {
				pgNum := r.Intn(numPages)
				pid := common.PageID{Oid: oid, PageNum: int32(pgNum)}

				f, err := bp.GetPage(pid)
				assert.NoError(t, err, "Failed to get page for writing")

				// Write signature
				f.PageLatch.Lock()
				signature := fmt.Sprintf("T%d-%d", tid, j)
				signatureBytes := []byte(signature)
				copy(f.Bytes[:], signatureBytes)
				runtime.Gosched()
				assert.True(t, bytes.HasPrefix(f.Bytes[:], signatureBytes), "Signature mismatch")
				f.PageLatch.Unlock()
				bp.UnpinPage(f, true)
			}
		}(i)
	}

	wg.Wait()
}

func selectPages(r *rand.Rand, numPages int, oid common.ObjectID) (lower common.PageID, higher common.PageID) {
	// Pick two distinct accounts
	idx1 := r.Intn(numPages)
	idx2 := r.Intn(numPages)
	for idx1 == idx2 {
		idx2 = r.Intn(numPages)
	}

	// Enforce Lock Ordering (Min < Max) to prevent Application Deadlock
	lowIdx, highIdx := idx1, idx2
	if lowIdx > highIdx {
		lowIdx, highIdx = idx2, idx1
	}
	return common.PageID{Oid: oid, PageNum: int32(lowIdx)}, common.PageID{Oid: oid, PageNum: int32(highIdx)}
}

// TestBufferPool_Concurrent_Large tests a large scale concurrent workload to stress the buffer pool
//
// Scenario:
// 1. Storage: 100 Pages (Accounts), each initialized with balance 10. Total = 1000.
// 2. Memory: Buffer Pool numBits = 64. (Working Set > Memory -> Forces Eviction).
// 3. Concurrency: 2 * num_cores Threads.
// 4. Operation: Randomly transfer 1 unit between two distinct accounts while holding locks on both, only if balance >= 0
// 5. Reader: a reader interleaves by periodically scanning all pages to verify non-negative value
//
// Assertions:
// - No Deadlocks: System handles high contention and eviction pressure.
// - No Pinned Eviction: The eviction policy must not evict a page currently locked/pinned by a thread.
// - Invariant Preserved: Final sum of all 100 pages must be exactly 100,000.
func TestBufferPool_Concurrent_Large(t *testing.T) {
	numPages := 100
	poolSize := 64
	bp, _, rootPath := setupBufferPool(t, poolSize)
	oid := common.ObjectID(400)
	createDummyFile(t, bp, oid, numPages)

	initialBalance := int64(10)
	expectedTotal := initialBalance * int64(numPages)

	for i := 0; i < numPages; i++ {
		pid := common.PageID{Oid: oid, PageNum: int32(i)}
		f, err := bp.GetPage(pid)
		require.NoError(t, err)
		binary.LittleEndian.PutUint64(f.Bytes[:], uint64(initialBalance))
		bp.UnpinPage(f, true)
	}
	err := bp.FlushAllPages()
	require.NoError(t, err)

	var wg sync.WaitGroup
	var readerWg sync.WaitGroup

	stopCh := make(chan struct{})
	readerWg.Add(1)
	go func() {
		defer readerWg.Done()
		// Reader scans periodically until stopCh is closed
		for {
			select {
			case <-stopCh:
				return
			case <-time.After(time.Millisecond):
				// Scan all pages to ensure no negative balances exist
				for i := 0; i < numPages; i++ {
					pid := common.PageID{Oid: oid, PageNum: int32(i)}
					p, err := bp.GetPage(pid)
					assert.NoError(t, err, "Failed to get page for writing")
					p.PageLatch.RLock()
					val := int64(binary.LittleEndian.Uint64(p.Bytes[:]))
					assert.True(t, val >= 0, "Reader found negative balance on page %d: %d", i, val)
					p.PageLatch.RUnlock()
					bp.UnpinPage(p, false)
					runtime.Gosched()
				}
			}
		}
	}()

	numThreads := 2 * runtime.NumCPU()
	opsPerThread := 100000
	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		go func(tid int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(tid)))

			for j := 0; j < opsPerThread; j++ {
				pidLow, pidHigh := selectPages(r, numPages, oid)

				pLow, err := bp.GetPage(pidLow)
				assert.NoError(t, err, "Failed to get page for writing")
				pLow.PageLatch.Lock()
				balLow := int64(binary.LittleEndian.Uint64(pLow.Bytes[:]))
				if balLow <= 0 {
					pLow.PageLatch.Unlock()
					bp.UnpinPage(pLow, false)
					continue
				}
				runtime.Gosched()
				pHigh, err := bp.GetPage(pidHigh)
				assert.NoError(t, err, "Failed to get page for writing")
				pHigh.PageLatch.Lock()
				// Transfer
				balHigh := binary.LittleEndian.Uint64(pHigh.Bytes[:])
				binary.LittleEndian.PutUint64(pLow.Bytes[:], uint64(balLow-1))
				runtime.Gosched()
				binary.LittleEndian.PutUint64(pHigh.Bytes[:], balHigh+1)

				pHigh.PageLatch.Unlock()
				pLow.PageLatch.Unlock()
				bp.UnpinPage(pHigh, true)
				bp.UnpinPage(pLow, true)
			}
		}(i)
	}

	wg.Wait()
	close(stopCh)
	readerWg.Wait()

	err = bp.FlushAllPages()
	require.NoError(t, err)

	filePath := filepath.Join(rootPath, fmt.Sprintf("dbo_%d.dat", oid))
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)

	var totalSum uint64
	for i := 0; i < numPages; i++ {
		offset := i * common.PageSize
		pageVal := binary.LittleEndian.Uint64(content[offset:])
		totalSum += pageVal
	}

	assert.Equal(t, uint64(expectedTotal), totalSum, "Invariant broken! Money created or destroyed.")
}

// TestBufferPool_Concurrent_ScanResistance verifies that the Buffer Pool eviction policy is resistant
// to cache pollution from sequential scans.
//
// Scenario:
// 1. Storage: File numBits (320 pages) is 5x larger than Buffer Pool (64 pages).
// 2. Workload:
//   - Scanner: Continuously scans the entire file sequentially (Pollution Source).
//   - Workers: Randomly access pages with 80/20 skew (Hot Set = 32 pages, fits in pool).
//
// 3. Concurrency: Multiple workers and the scanner run in parallel to create contention.
//
// Assertions:
//   - Scan Resistance: The Hot Set (accessed by workers) must remain in memory despite the scan.
//   - Efficiency: Total disk reads should be close to (Scan Ops + Cold Misses), and far below
//     (Scan Ops + Total Worker Ops), verifying that hot pages are not evicted by the scanner.
func TestBufferPool_Concurrent_ScanResistance(t *testing.T) {
	poolSize := 64
	numPages := 320 // 5x pool numBits
	bp, statsSm, _ := setupBufferPool(t, poolSize)
	oid := common.ObjectID(999)
	createDummyFile(t, bp, oid, numPages)

	// Get the stats file wrapper to verify physical disk reads
	statsFile, ok := statsSm.Files.Load(oid)
	require.True(t, ok)

	var wg sync.WaitGroup

	numHot := 32
	hotProb := 0.80
	writeProb := 0.20
	numSkewedWorkers := 2 * runtime.NumCPU()
	opsPerWorker := 10000

	for i := 0; i < numSkewedWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(id) + 100))

			for k := 0; k < opsPerWorker; k++ {
				// Determine target page
				var pageNum int
				if r.Float64() < hotProb {
					// Hot Set access (0 to numHot-1)
					pageNum = r.Intn(numHot)
				} else {
					// Cold Set access (numHot to end)
					pageNum = numHot + r.Intn(numPages-numHot)
				}

				pid := common.PageID{Oid: oid, PageNum: int32(pageNum)}
				// Retry loop
				for {
					p, err := bp.GetPage(pid)
					if err == nil {
						if r.Float64() < writeProb {
							p.PageLatch.Lock()
							p.Bytes[0] = byte(id)
							p.PageLatch.Unlock()
							bp.UnpinPage(p, true)
						} else {
							bp.UnpinPage(p, false)
						}
						break
					}
					runtime.Gosched()
				}
				runtime.Gosched()
			}
		}(i)
	}

	numScan := atomic.Int64{}
	var scannerWg sync.WaitGroup
	numScan.Store(0)
	stopScan := make(chan struct{})
	scannerWg.Add(1)
	go func() {
		defer scannerWg.Done()
		for {
			select {
			case <-stopScan:
				return
			default:
				for i := 0; i < numPages; i++ {
					pid := common.PageID{Oid: oid, PageNum: int32(i)}
					for {
						p, err := bp.GetPage(pid)
						if err == nil {
							// Simulate tiny processing time
							time.Sleep(time.Millisecond)
							bp.UnpinPage(p, false)
							break
						}
						// Backoff slightly on full pool
						runtime.Gosched()
					}
				}
			}
		}
	}()

	wg.Wait()
	close(stopScan)
	scannerWg.Wait()

	actualReads := int(statsFile.ReadCnt.Load())
	totalScanOps := int(numScan.Load()) * numPages
	expectedColdMisses := int(float64(numSkewedWorkers*opsPerWorker) * (1.0 - hotProb))
	// We add a safety margin for concurrency races (e.g., a hot page momentarily evicted during a burst).
	margin := 100
	maxExpectedReads := totalScanOps + numHot + expectedColdMisses + margin

	fmt.Printf("Stats:\n")
	fmt.Printf("  Pool Size: %d, File Size: %d\n", poolSize, numPages)
	fmt.Printf("  Actual Disk Reads: %d\n", actualReads)
	fmt.Printf("  Max Expected Reads (Resistance Threshold): %d\n", maxExpectedReads)

	assert.LessOrEqual(t, actualReads, maxExpectedReads,
		"Cache Pollution Detected! The sequential scan likely evicted the hot working set repeatedly. "+
			"Actual Reads (%d) > Max Expected (%d).",
		actualReads, maxExpectedReads)
}
