package storage

import (
	"sync"

	"mit.edu/dsg/godb/common"
)

// BufferPool manages the reading and writing of database pages between the DiskFileManager and memory.
// It acts as a central cache to keep "hot" pages in memory with fixed capacity and selectively evicts
// pages to disk when the pool becomes full. Users will need to coordinate concurrent access to pages
// using page-level latches and metadata (which you should define in page.go). All methods
// must be thread-safe, as multiple threads will request the same or different pages concurrently.
// To get full credit, you likely need to do better than coarse-grained latching (i.e., a global latch for the entire
// BufferPool instance).
type BufferPool struct {
	// add more fields here...
	frames         []*PageFrame
	freeFrames     []int
	numPages       int
	storageManager DBFileManager
	logManager     LogManager
	pageTable      map[common.PageID]*PageFrame
	mutex          sync.Mutex
	loading        map[common.PageID]chan struct{}
	available      *sync.Cond
	recentlyUsed   map[*PageFrame]bool
	framePos       int
}

// need to store page frames, which page is in each frame, whether its pinned, and whether its dirty

// NewBufferPool creates a new BufferPool with a fixed capacity defined by numPages. It requires a
// storageManager to handle the underlying disk I/O operations.
//
// Hint: You will need to worry about logManager until Lab 3
func NewBufferPool(numPages int, storageManager DBFileManager, logManager LogManager) *BufferPool {

	freeFrames := make([]int, numPages)
	for i := range freeFrames {
		freeFrames[i] = i
	}

	bp := &BufferPool{
		frames:         make([]*PageFrame, numPages),
		freeFrames:     freeFrames,
		numPages:       numPages,
		storageManager: storageManager,
		logManager:     logManager,
		pageTable:      make(map[common.PageID]*PageFrame),
		loading:        make(map[common.PageID]chan struct{}),
		recentlyUsed:   make(map[*PageFrame]bool),
	}
	bp.available = sync.NewCond(&bp.mutex)
	return bp
}

// StorageManager returns the underlying disk manager.
func (bp *BufferPool) StorageManager() DBFileManager {
	return bp.storageManager
}

// GetPage retrieves a page from the buffer pool, ensuring it is pinned (i.e. prevented from eviction until
// unpinned) and ready for use. If the page is already in the pool, the cached bytes are returned. If the page is not
// present, the method must first make space by selecting a victim frame to evict
// (potentially writing it to disk if dirty), and then read the requested page from disk into that frame.
func (bp *BufferPool) GetPage(pageID common.PageID) (*PageFrame, error) {
	for {
		bp.mutex.Lock()
		frame, found := bp.pageTable[pageID]
		if found {
			frame.pinCount++
			bp.recentlyUsed[frame] = true
			bp.mutex.Unlock()
			return frame, nil
		}

		if done, isLoading := bp.loading[pageID]; isLoading {
			bp.mutex.Unlock()
			<-done
			continue
		}

		if len(bp.freeFrames) > 0 {
			lastFree := len(bp.freeFrames) - 1
			freeFrameIndex := bp.freeFrames[lastFree]
			bp.freeFrames = bp.freeFrames[:lastFree]
			freeFrame := bp.frames[freeFrameIndex]
			if freeFrame == nil {
				freeFrame = &PageFrame{}
				bp.frames[freeFrameIndex] = freeFrame
			}
			freeFrame.pinCount = 1
			freeFrame.pageID = pageID

			done := make(chan struct{})
			bp.loading[pageID] = done
			bp.mutex.Unlock()

			file, err := bp.storageManager.GetDBFile(pageID.Oid)
			if err == nil {
				err = file.ReadPage(int(pageID.PageNum), freeFrame.Bytes[:])
			}
			if err != nil {
				bp.mutex.Lock()
				freeFrame.pageID = common.PageID{}
				freeFrame.pinCount = 0
				bp.freeFrames = append(bp.freeFrames, freeFrameIndex)
				delete(bp.loading, pageID)
				close(done)
				bp.available.Signal()
				bp.mutex.Unlock()
				return nil, err
			}

			bp.mutex.Lock()
			bp.recentlyUsed[freeFrame] = false
			bp.pageTable[pageID] = freeFrame
			delete(bp.loading, pageID)
			close(done)
			bp.mutex.Unlock()

			return freeFrame, nil
		}

		var evictFrame *PageFrame
		var fallback *PageFrame
		var evictFrameIndex int
		var fallbackIndex int

		searchLimit := bp.numPages
		if searchLimit > 64 {
			searchLimit = 64
		}

		for checked := 0; checked < searchLimit; checked++ {
			ind := bp.framePos
			bp.framePos = (ind + 1) % bp.numPages

			cur := bp.frames[ind]
			if cur.pinCount > 0 {
				continue
			}

			if fallback == nil {
				fallback = cur
				fallbackIndex = ind
			}

			if !bp.recentlyUsed[cur] {
				evictFrame = cur
				evictFrameIndex = ind
				break
			}

			bp.recentlyUsed[cur] = false
		}
		if evictFrame == nil {
			evictFrame = fallback
			evictFrameIndex = fallbackIndex
		}
		if evictFrame == nil {
			bp.available.Wait()
			bp.mutex.Unlock()
			continue
		}
		oldPageID := evictFrame.pageID
		wasDirty := evictFrame.dirty
		evictFrame.pinCount = 1
		delete(bp.pageTable, oldPageID)

		oldDone := make(chan struct{})
		bp.loading[oldPageID] = oldDone
		done := make(chan struct{})
		bp.loading[pageID] = done

		bp.mutex.Unlock()

		var err error
		if wasDirty {
			oldFile, fileErr := bp.storageManager.GetDBFile(oldPageID.Oid)
			if fileErr != nil {
				err = fileErr
			} else {
				err = oldFile.WritePage(int(oldPageID.PageNum), evictFrame.Bytes[:])
			}
		}
		if err != nil {
			bp.mutex.Lock()
			evictFrame.pinCount = 0
			bp.pageTable[oldPageID] = evictFrame
			delete(bp.loading, oldPageID)
			close(oldDone)
			delete(bp.loading, pageID)
			close(done)
			bp.mutex.Unlock()
			return nil, err
		}

		newFile, err := bp.storageManager.GetDBFile(pageID.Oid)
		if err == nil {
			err = newFile.ReadPage(int(pageID.PageNum), evictFrame.Bytes[:])
		}
		if err != nil {
			bp.mutex.Lock()
			evictFrame.pageID = common.PageID{}
			evictFrame.pinCount = 0
			evictFrame.dirty = false
			bp.freeFrames = append(bp.freeFrames, evictFrameIndex)
			delete(bp.loading, oldPageID)
			close(oldDone)
			delete(bp.loading, pageID)
			close(done)
			bp.available.Signal()
			bp.mutex.Unlock()
			return nil, err
		}

		bp.mutex.Lock()
		evictFrame.pageID = pageID
		evictFrame.dirty = false
		bp.recentlyUsed[evictFrame] = false
		bp.pageTable[pageID] = evictFrame
		delete(bp.loading, oldPageID)
		close(oldDone)
		delete(bp.loading, pageID)
		close(done)
		bp.mutex.Unlock()
		return evictFrame, nil
	}
}

// UnpinPage indicates that the caller is done using a page. It unpins the page, making the page potentially evictable
// if no other thread is accessing it. If the setDirty flag is true, the page is marked as modified, ensuring
// it will be written back to disk before eviction.
func (bp *BufferPool) UnpinPage(frame *PageFrame, setDirty bool) {
	if frame == nil {
		return
	}

	bp.mutex.Lock()
	if frame.pinCount > 0 {
		frame.pinCount--
	}
	if setDirty {
		frame.dirty = true
	}
	if frame.pinCount == 0 {
		bp.available.Signal()
	}
	bp.mutex.Unlock()
}

// FlushAllPages flushes all dirty pages to disk that have an LSN less than `flushedUntil`, regardless of pins.
// This is typically called during a checkpoint or Shutdown to ensure durability, but also useful for tests
func (bp *BufferPool) FlushAllPages() error {
	for i := range bp.frames {
		bp.mutex.Lock()
		frame := bp.frames[i]
		if frame == nil {
			bp.mutex.Unlock()
			continue
		}

		if !frame.dirty || frame.pageID.IsNil() {
			bp.mutex.Unlock()
			continue
		}

		pageID := frame.pageID
		frame.pinCount++
		bp.mutex.Unlock()

		frame.PageLatch.RLock()
		file, err := bp.storageManager.GetDBFile(pageID.Oid)
		if err == nil {
			err = file.WritePage(int(pageID.PageNum), frame.Bytes[:])
		}

		if err == nil {
			bp.mutex.Lock()
			frame.dirty = false
			frame.pinCount--
			bp.mutex.Unlock()
			frame.PageLatch.RUnlock()
			continue
		}

		frame.PageLatch.RUnlock()
		bp.mutex.Lock()
		frame.pinCount--
		bp.mutex.Unlock()
		return err
	}

	return nil
}

// GetDirtyPageTableSnapshot returns a map of all currently dirty pages and their RecoveryLSN.
// This is called during checkpoint to snapshot the current DPT into the log.
//
// Hint: You do not need to worry about this function until lab 4
func (bp *BufferPool) GetDirtyPageTableSnapshot() map[common.PageID]LSN {
	// You will not need to implement this until lab4
	panic("unimplemented")
}
