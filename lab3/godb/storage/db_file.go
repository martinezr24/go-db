package storage

import (
	"mit.edu/dsg/godb/common"
)

// DBFile abstracts the physical file on storage that stores a table.
// It handles page-level reads and writes, as well as space allocation.
//
// Implementation should be safe for concurrent use. Specifically, multiple threads
// should be able to ReadPage and WritePage to different pages simultaneously.
// AllocatePage should be thread-safe and atomic with respect to other allocations.
type DBFile interface {
	// AllocatePage reserves a sequential block of `numPages` pages in the file.
	// It returns the page number of the first page in the allocated block. The new pages
	// are filled with zeros.
	AllocatePage(numPages int) (int, error)
	// ReadPage reads the contents of the page identified by `pageNum` into the
	// provided byte slice. The slice `frame` must be exactly godb.PageSize bytes.
	ReadPage(pageNum int, frame []byte) error
	// WritePage writes the content of `frame` to the page identified by `pageNum`.
	// The slice `frame` must be exactly godb.PageSize bytes, and `pageNum` must be strictly less than the
	//  current NumPages(). This method cannot be used to extend the file; use AllocatePage instead.
	WritePage(pageNum int, frame []byte) error
	// Sync forces any buffered writes to stable storage, ensuring durability.
	Sync() error
	// Close closes the underlying file handle and releases resources.
	Close() error
	// NumPages returns the number of pages allocated in the file.
	NumPages() (int, error)
}

// DBFileManager manages the lifecycle and caching of DBFile instances.
// It acts as the registry for all open files in the system.
type DBFileManager interface {
	// GetDBFile retrieves the DBFile handle for the given table ObjectID. If the file is already open, the
	// existing handle is returned. If the file does not exist on disk, it is created.
	GetDBFile(oid common.ObjectID) (DBFile, error)
	// DeleteDBFile permanently removes the physical file associated with the ObjectID.
	// The caller is responsible for ensuring that no other threads are currently accessing or modifying this
	//  DBFile (e.g., via the BufferPool, or calling GetDBFile).
	DeleteDBFile(oid common.ObjectID) error
}
