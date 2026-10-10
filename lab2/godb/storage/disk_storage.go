package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/puzpuzpuz/xsync/v3"
	"mit.edu/dsg/godb/common"
)

// DiskDBFile implements the DBFile interface using a standard OS file.
type DiskDBFile struct {
	file *os.File
	// numPages is a cached value of the file numBits (in pages) to avoid stat() syscalls on every read.
	// It is updated atomically after physical allocation.
	numPages atomic.Int32
	// allocMu serializes file expansion operations (Truncate) to ensure thread safety
	// during allocation.
	allocMu sync.Mutex
}

// NewDiskDBFile creates a new DiskDBFile wrapper around an already open OS file.
// It initializes the page count based on the current file numBits.
func NewDiskDBFile(file *os.File) (*DiskDBFile, error) {
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	// Init logical count based on physical numBits
	// Note: We assume the file numBits is always a multiple of PageSize.
	numPages := int32(stat.Size() / int64(common.PageSize))

	dbFile := &DiskDBFile{
		file: file,
	}
	dbFile.numPages.Store(numPages)
	return dbFile, nil
}

// AllocatePage grows the underlying file by `numPages` pages.
func (f *DiskDBFile) AllocatePage(numPages int) (int, error) {
	common.Assert(numPages > 0, "cannot allocate negative number of pages")
	f.allocMu.Lock()
	defer f.allocMu.Unlock()

	currentPages := f.numPages.Load()
	newTotalPages := currentPages + int32(numPages)
	newSizeBytes := int64(newTotalPages) * int64(common.PageSize)

	// Physically extend the file. This ensures the OS changes the file numBits immediately, although it may not be
	// backed by physical pages yet. Reads from the new area will return zeros. In a real production system, we would
	// likely want to physically allocate the pages here.
	if err := f.file.Truncate(newSizeBytes); err != nil {
		return 0, fmt.Errorf("failed to allocate pages: %w", err)
	}
	f.numPages.Store(newTotalPages)
	return int(currentPages), nil
}

// ReadPage reads the content of the page identified by `pageNum` into `frame`. Returns error if the page does not exist.
func (f *DiskDBFile) ReadPage(pageNum int, frame []byte) error {
	common.Assert(len(frame) == common.PageSize, "buffer numBits must match PageSize")
	if int32(pageNum) >= f.numPages.Load() {
		return fmt.Errorf("read out of bounds: page %d does not exist (file has %d pages)", pageNum, f.numPages.Load())
	}

	offset := int64(pageNum) * int64(common.PageSize)
	_, err := f.file.ReadAt(frame, offset)

	if err != nil {
		return err
	}

	return nil
}

// WritePage writes the content of `frame` to the page identified by `pageNum`. Returns error if the page does not exist
func (f *DiskDBFile) WritePage(pageNum int, frame []byte) error {
	common.Assert(len(frame) == common.PageSize, "buffer numBits must match PageSize")

	if int32(pageNum) >= f.numPages.Load() {
		return fmt.Errorf("write out of bounds: page %d does not exist", pageNum)
	}

	offset := int64(pageNum) * int64(common.PageSize)
	_, err := f.file.WriteAt(frame, offset)
	if err != nil {
		return err
	}
	return nil
}

// Sync flushes writes to stable storage.
func (f *DiskDBFile) Sync() error {
	return f.file.Sync()
}

// Close closes the underlying OS file.
func (f *DiskDBFile) Close() error {
	return f.file.Close()
}

// NumPages returns the number of pages currently in the file.
func (f *DiskDBFile) NumPages() (int, error) {
	return int(f.numPages.Load()), nil
}

// DiskDBFileManager manages a collection of DiskDBFiles rooted at a specific directory.
type DiskDBFileManager struct {
	rootPath  string
	fileCache *xsync.MapOf[common.ObjectID, DBFile]
}

// NewDiskStorageManager initializes a manager rooted at `rootPath`.
func NewDiskStorageManager(rootPath string) *DiskDBFileManager {
	return &DiskDBFileManager{
		rootPath:  rootPath,
		fileCache: xsync.NewMapOf[common.ObjectID, DBFile](),
	}
}

// GetDBFile retrieves or creates a DBFile for the given ObjectID.
//
// It maintains a cache of open files to ensure only one instance of DiskDBFile
// exists per physical file.
func (dsm *DiskDBFileManager) GetDBFile(oid common.ObjectID) (DBFile, error) {
	if file, ok := dsm.fileCache.Load(oid); ok {
		return file, nil
	}

	path := filepath.Join(dsm.rootPath, fmt.Sprintf("dbo_%d.dat", oid))
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}
	newDBFile, err := NewDiskDBFile(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	actualFile, loaded := dsm.fileCache.LoadOrStore(oid, newDBFile)
	if loaded {
		// We lost the race. Another thread opened the file and inserted it first.
		// Close our unnecessary file handle and use theirs.
		_ = newDBFile.Close()
		return actualFile, nil
	}

	return newDBFile, nil
}

// DeleteDBFile permanently deletes the file backing the given ObjectID.
//
// Warning: The caller must ensure that no other threads are currently using/getting the file.
func (dsm *DiskDBFileManager) DeleteDBFile(oid common.ObjectID) error {
	file, loaded := dsm.fileCache.LoadAndDelete(oid)
	if loaded {
		if err := file.Close(); err != nil {
			// We continue even if close fails, to ensure physical deletion
			fmt.Printf("Failed to close DB file %d when deleting: %v, proceeding with deletion\n", oid, err)
		}
	}

	path := filepath.Join(dsm.rootPath, fmt.Sprintf("dbo_%d.dat", oid))
	return os.Remove(path)
}

// Wrappers around normal DBFile for testing purposes
type StatsDBFile struct {
	DBFile
	ReadCnt, WriteCnt atomic.Int64
}

func (f *StatsDBFile) ReadPage(pageNum int, frame []byte) error {
	f.ReadCnt.Add(1)
	return f.DBFile.ReadPage(pageNum, frame)
}

func (f *StatsDBFile) WritePage(pageNum int, frame []byte) error {
	f.WriteCnt.Add(1)
	return f.DBFile.WritePage(pageNum, frame)
}

type StatsDBFileManager struct {
	Inner DBFileManager
	Files *xsync.MapOf[common.ObjectID, *StatsDBFile]
}

func (m *StatsDBFileManager) GetDBFile(oid common.ObjectID) (DBFile, error) {
	if f, ok := m.Files.Load(oid); ok {
		return f, nil
	}
	realFile, err := m.Inner.GetDBFile(oid)
	if err != nil {
		return nil, err
	}
	statsFile := &StatsDBFile{DBFile: realFile}
	actual, _ := m.Files.LoadOrStore(oid, statsFile)
	return actual, nil
}

func (m *StatsDBFileManager) DeleteDBFile(oid common.ObjectID) error {
	m.Files.Delete(oid)
	return m.Inner.DeleteDBFile(oid)
}
