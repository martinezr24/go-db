# Lab 3 Write-up

## What I worked on

This lab was about adding transactions and concurrency control to GoDB. The main pieces I worked on were the lock manager, transaction context, transaction manager, and the hooks that connect transactions to the table heap, indexes, buffer pool, and write-ahead log.

The lock manager supports table-level and tuple-level locks. A transaction takes an intent lock on the table before locking individual tuples, which lets table scans and tuple operations coexist without creating a lock object for every row in the database. The manager checks lock compatibility, handles repeated requests and upgrades, queues blocked requests, and uses a wait-for graph to detect deadlocks.

Each transaction context keeps track of its locks and its own log records. The log-record buffer gives the transaction a compact history that can be walked backward during an abort. Contexts are also reset and reused so that locks, records, and deferred index actions from one transaction do not carry over to another.

I added transaction hooks to the table heap so reads and writes acquire the appropriate locks. Inserts, updates, and deletes append WAL records before changing a page and update the page's LSN. The buffer pool checks that a dirty page's log records have been flushed before writing that page to disk. The index lookup and scan executors also recheck the heap tuple after reading an index entry, since deferred index deletes can leave stale entries visible until commit.

The transaction manager handles the transaction lifecycle. `Begin` creates a context and writes a begin record. `Commit` waits for the commit record to be durable before applying deferred index deletions and releasing locks. `Abort` walks the transaction's changes in reverse order, writes a compensation log record before undoing each page change, and holds locks until the abort record is durable.

## A design choice: strict locking and WAL order

The important rule I followed was to keep a transaction's locks until its outcome is durable. Releasing locks as soon as the commit record is appended would allow another transaction to access the data before the commit was safely on disk. Similarly, an undo cannot just modify a page: a CLR has to be appended first, so recovery can tell that the undo happened if the page is written out before a crash.

This means the order of operations matters throughout the code: acquire locks before accessing tuples, append log records before changing pages, and wait for the relevant log records to flush before writing dirty pages or releasing transaction locks.

## What was challenging

The hardest part was coordinating several kinds of state at once: locks, page latches, buffer-pool pins, WAL records, and in-memory index changes. Each one protects something different, and getting the ordering wrong can lead to deadlocks, lost updates, or data that cannot be recovered correctly.

Abort was especially tricky because it has to undo updates in reverse order while still following WAL rules. I also had to account for the fact that indexes are in memory and handle deletes differently from table-page changes: inserts need to be visible to the transaction that made them, while deletes are delayed until commit.

## Overall reflection

This lab helped me understand that transactions are not just a start and end marker around a query. The database has to coordinate visibility, locking, logging, page writes, and rollback so that concurrent operations still behave correctly. The most useful lesson for me was how strongly correctness depends on ordering: the right operations in the wrong order can still make the database unsafe.
