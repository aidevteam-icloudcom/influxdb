package tsm1

import (
	"expvar"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/influxdata/influxdb"
)

var ErrCacheMemoryExceeded = fmt.Errorf("cache maximum memory size exceeded")
var ErrCacheInvalidCheckpoint = fmt.Errorf("invalid checkpoint")

// entry is a set of values and some metadata.
type entry struct {
	values   Values // All stored values.
	needSort bool   // true if the values are out of order and require deduping.
}

// newEntry returns a new instance of entry.
func newEntry() *entry {
	return &entry{}
}

// add adds the given values to the entry.
func (e *entry) add(values []Value) {
	// See if the new values are sorted or contain duplicate timestamps
	var prevTime int64
	for _, v := range values {
		if v.UnixNano() <= prevTime {
			e.needSort = true
			break
		}
		prevTime = v.UnixNano()
	}

	// if there are existing values make sure they're all less than the first of
	// the new values being added
	if len(e.values) == 0 {
		e.values = values
	} else {
		l := len(e.values)
		lastValTime := e.values[l-1].UnixNano()
		if lastValTime >= values[0].UnixNano() {
			e.needSort = true
		}
		e.values = append(e.values, values...)
	}
}

// deduplicate sorts and orders the entry's values. If values are already deduped and
// and sorted, the function does no work and simply returns.
func (e *entry) deduplicate() {
	if !e.needSort || len(e.values) <= 1 {
		return
	}
	e.values = e.values.Deduplicate()
	e.needSort = false
}

// Statistics gathered by the Cache.
const (
	// levels - point in time measures

	statCacheMemoryBytes = "memBytes"      // level: Size of in-memory cache in bytes
	statCacheDiskBytes   = "diskBytes"     // level: Size of on-disk snapshots in bytes
	statSnapshots        = "snapshotCount" // level: Number of active snapshots.
	statCacheAgeMs       = "cacheAgeMs"    // level: Number of milliseconds since cache was last snapshoted at sample time

	// counters - accumulative measures

	statCachedBytes         = "cachedBytes"         // counter: Total number of bytes written into snapshots.
	statWALCompactionTimeMs = "WALCompactionTimeMs" // counter: Total number of milliseconds spent compacting snapshots
)

// Cache maintains an in-memory store of Values for a set of keys.
type Cache struct {
	commit  sync.Mutex
	mu      sync.RWMutex
	store   map[string]*entry
	dirty   map[string]*entry
	size    uint64
	maxSize uint64

	// snapshots are the cache objects that are currently being written to tsm files
	// they're kept in memory while flushing so they can be queried along with the cache.
	// they are read only and should never be modified
	snapshots     []*Cache
	snapshotsSize uint64
	files         []string

	statMap      *expvar.Map // nil for snapshots.
	lastSnapshot time.Time
}

// NewCache returns an instance of a cache which will use a maximum of maxSize bytes of memory.
// Only used for engine caches, never for snapshots
func NewCache(maxSize uint64, path string) *Cache {
	c := &Cache{
		maxSize:      maxSize,
		store:        make(map[string]*entry),
		statMap:      influxdb.NewStatistics("tsm1_cache:"+path, "tsm1_cache", map[string]string{"path": path}),
		lastSnapshot: time.Now(),
	}
	c.UpdateAge()
	c.UpdateCompactTime(0)
	c.updateCachedBytes(0)
	c.updateMemSize(0)
	c.updateSnapshots()
	return c
}

// Write writes the set of values for the key to the cache. This function is goroutine-safe.
// It returns an error if the cache has exceeded its max size.
func (c *Cache) Write(key string, values []Value) error {
	c.mu.Lock()

	// Enough room in the cache?
	addedSize := Values(values).Size()
	newSize := c.size + uint64(addedSize)
	if c.maxSize > 0 && newSize+c.snapshotsSize > c.maxSize {
		c.mu.Unlock()
		return ErrCacheMemoryExceeded
	}

	c.write(key, values)
	c.size = newSize
	c.mu.Unlock()

	// Update the memory size stat
	c.updateMemSize(int64(addedSize))

	return nil
}

// WriteMulti writes the map of keys and associated values to the cache. This function is goroutine-safe.
// It returns an error if the cache has exceeded its max size.
func (c *Cache) WriteMulti(values map[string][]Value) error {
	totalSz := 0
	for _, v := range values {
		totalSz += Values(v).Size()
	}

	// Enough room in the cache?
	c.mu.RLock()
	newSize := c.size + uint64(totalSz)
	if c.maxSize > 0 && newSize+c.snapshotsSize > c.maxSize {
		c.mu.RUnlock()
		return ErrCacheMemoryExceeded
	}
	c.mu.RUnlock()

	c.mu.Lock()
	for k, v := range values {
		c.write(k, v)
	}
	c.size = newSize
	c.mu.Unlock()

	// Update the memory size stat
	c.updateMemSize(int64(totalSz))

	return nil
}

// Answers the names WAL segment files which are captured by the snapshot. The contents
// of the specified files and the receiving snapshot should be identical.
func (c *Cache) Files() []string {
	return c.files
}

// Filter the specified list of files to exclude any file already referenced
// by an existing snapshot
func (c *Cache) newFiles(files []string) []string {
	filtered := []string{}
	existing := map[string]bool{}
	for _, s := range c.snapshots {
		for _, f := range s.files {
			existing[f] = true
		}
	}
	for _, f := range files {
		if !existing[f] {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// PrepareSnapshots accepts a list of the closed files and prepares a new snapshot corresponding
// to the changes in newly closed files that were not captured by previous snapshots. It returns a slice
// containing references to every snapshot that has not yet been successfully committed.
//
// Every call to this method must be matched with exactly one corresponding call to either
// CommitSnapshots() or RollbackSnapshots().
func (c *Cache) PrepareSnapshots(files []string) []*Cache {

	c.commit.Lock() // released by RollbackSnapshot() or CommitSnapshot()

	c.mu.Lock()
	defer c.mu.Unlock()

	snapshot := &Cache{
		store: c.store,
		size:  c.size,
		dirty: make(map[string]*entry),
		files: c.newFiles(files),
	}

	for k, e := range c.store {
		if e.needSort {
			snapshot.dirty[k] = &entry{needSort: true, values: e.values}
		} else {
			snapshot.dirty[k] = e
		}
	}

	c.store = make(map[string]*entry)
	c.size = 0
	c.lastSnapshot = time.Now()

	c.snapshots = append(c.snapshots, snapshot)
	c.snapshotsSize += snapshot.size

	c.updateMemSize(-int64(snapshot.size))
	c.updateCachedBytes(snapshot.size)
	c.updateSnapshots()

	clone := make([]*Cache, len(c.snapshots))
	copy(clone, c.snapshots)

	return clone
}

// Deduplicate sorts the snapshot before returning it. The compactor and any queries
// coming in while it writes will need the values sorted
func (c *Cache) Deduplicate() {
	for _, e := range c.dirty {
		e.deduplicate()
	}
}

// This method must be called while holding the write lock of the cache that
// create this snapshot.
func (c *Cache) UpdateStore() {
	c.store, c.dirty = c.dirty, nil
}

// RollbackSnapshot rolls back a previously prepared snapshot by releasing the commit lock.
//
// We leave the snapshots slice untouched because we need to use it to resolve
// queries that hit the WAL segments.
// RollbackSnapshot rolls back a previously prepared snapshot by resetting
// the

func (c *Cache) RollbackSnapshots(incomplete []*Cache) {
	defer c.commit.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.snapshots = make([]*Cache, 0, len(incomplete)) // not strictly necessary since we expect incomplete[i] != nil for at least one i.
	c.snapshotsSize = 0

	// remove any snapshots that have been nil'd
	for _, s := range incomplete {
		if s != nil {
			c.snapshots = append(c.snapshots, s)
			c.snapshotsSize += s.Size()
		}
	}
}

// CommitSnapshot commits a previously prepared snapshot by reset the snapshots array
// and releasing the commit lock.
func (c *Cache) CommitSnapshots() {
	defer c.commit.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.snapshots = nil
	c.snapshotsSize = 0

	c.updateSnapshots()
}

// Size returns the number of point-calcuated bytes the cache currently uses.
func (c *Cache) Size() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.size
}

// MaxSize returns the maximum number of bytes the cache may consume.
func (c *Cache) MaxSize() uint64 {
	return c.maxSize
}

// Keys returns a sorted slice of all keys under management by the cache.
func (c *Cache) Keys() []string {
	var a []string
	for k, _ := range c.store {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}

// Values returns a copy of all values, deduped and sorted, for the given key.
func (c *Cache) Values(key string) Values {
	c.mu.RLock()
	e := c.store[key]
	if e != nil && e.needSort {
		// Sorting is needed, so unlock and run the merge operation with
		// a write-lock. It is actually possible that the data will be
		// sorted by the time the merge runs, which would mean very occasionally
		// a write-lock will be held when only a read-lock is required.
		c.mu.RUnlock()
		return func() Values {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.merged(key)
		}()
	}

	// No sorting required for key, so just merge while continuing to hold read-lock.
	return func() Values {
		defer c.mu.RUnlock()
		return c.merged(key)
	}()
}

// Delete will remove the keys from the cache
func (c *Cache) Delete(keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, k := range keys {
		delete(c.store, k)
	}
}

// merged returns a copy of hot and snapshot values. The copy will be merged, deduped, and
// sorted. It assumes all necessary locks have been taken. If the caller knows that the
// the hot source data for the key will not be changed, it is safe to call this function
// with a read-lock taken. Otherwise it must be called with a write-lock taken.
func (c *Cache) merged(key string) Values {
	e := c.store[key]
	if e == nil {
		if len(c.snapshots) == 0 {
			// No values in hot cache or snapshots.
			return nil
		}
	} else {
		e.deduplicate()
	}

	// Build the sequence of entries that will be returned, in the correct order.
	// Calculate the required size of the destination buffer.
	var entries []*entry
	sz := 0
	for _, s := range c.snapshots {
		e := s.store[key]
		if e != nil {
			entries = append(entries, e)
			sz += len(e.values)
		}
	}
	if e != nil {
		entries = append(entries, e)
		sz += len(e.values)
	}

	// Any entries? If not, return.
	if sz == 0 {
		return nil
	}

	// Create the buffer, and copy all hot values and snapshots. Individual
	// entries are sorted at this point, so now the code has to check if the
	// resultant buffer will be sorted from start to finish.
	var needSort bool
	values := make(Values, sz)
	n := 0
	for _, e := range entries {
		if !needSort && n > 0 {
			needSort = values[n-1].UnixNano() >= e.values[0].UnixNano()
		}
		n += copy(values[n:], e.values)
	}

	if needSort {
		values = values.Deduplicate()
	}

	return values
}

// Store returns the underlying cache store. This is not goroutine safe!
// Protect access by using the Lock and Unlock functions on Cache.
func (c *Cache) Store() map[string]*entry {
	return c.store
}

func (c *Cache) Lock() {
	c.mu.Lock()
}

func (c *Cache) Unlock() {
	c.mu.Unlock()
}

// values returns the values for the key. It doesn't lock and assumes the data is
// already sorted. Should only be used in compact.go in the CacheKeyIterator
func (c *Cache) values(key string) Values {
	e := c.store[key]
	if e == nil {
		return nil
	}
	return e.values
}

// write writes the set of values for the key to the cache. This function assumes
// the lock has been taken and does not enforce the cache size limits.
func (c *Cache) write(key string, values []Value) {
	e, ok := c.store[key]
	if !ok {
		e = newEntry()
		c.store[key] = e
	}
	e.add(values)
}

// CacheLoader processes a set of WAL segment files, and loads a cache with the data
// contained within those files.  Processing of the supplied files take place in the
// order they exist in the files slice.
type CacheLoader struct {
	files []string

	Logger *log.Logger
}

// NewCacheLoader returns a new instance of a CacheLoader.
func NewCacheLoader(files []string) *CacheLoader {
	return &CacheLoader{
		files:  files,
		Logger: log.New(os.Stderr, "[cacheloader] ", log.LstdFlags),
	}
}

// Load returns a cache loaded with the data contained within the segment files.
// If, during reading of a segment file, corruption is encountered, that segment
// file is truncated up to and including the last valid byte, and processing
// continues with the next segment file.
func (cl *CacheLoader) Load(cache *Cache) error {
	for _, fn := range cl.files {
		if err := func() error {
			f, err := os.OpenFile(fn, os.O_CREATE|os.O_RDWR, 0666)
			if err != nil {
				return err
			}

			// Log some information about the segments.
			stat, err := os.Stat(f.Name())
			if err != nil {
				return err
			}
			cl.Logger.Printf("reading file %s, size %d", f.Name(), stat.Size())

			r := NewWALSegmentReader(f)
			defer r.Close()

			for r.Next() {
				entry, err := r.Read()
				if err != nil {
					n := r.Count()
					cl.Logger.Printf("file %s corrupt at position %d, truncating", f.Name(), n)
					if err := f.Truncate(n); err != nil {
						return err
					}
					break
				}

				switch t := entry.(type) {
				case *WriteWALEntry:
					if err := cache.WriteMulti(t.Values); err != nil {
						return err
					}
				case *DeleteWALEntry:
					cache.Delete(t.Keys)
				}
			}

			return nil
		}(); err != nil {
			return err
		}
	}
	return nil
}

// Updates the age statistic
func (c *Cache) UpdateAge() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ageStat := new(expvar.Int)
	ageStat.Set(int64(time.Now().Sub(c.lastSnapshot) / time.Millisecond))
	c.statMap.Set(statCacheAgeMs, ageStat)
}

// Updates WAL compaction time statistic
func (c *Cache) UpdateCompactTime(d time.Duration) {
	c.statMap.Add(statWALCompactionTimeMs, int64(d/time.Millisecond))
}

// Update the cachedBytes counter
func (c *Cache) updateCachedBytes(b uint64) {
	c.statMap.Add(statCachedBytes, int64(b))
}

// Update the memSize level
func (c *Cache) updateMemSize(b int64) {
	c.statMap.Add(statCacheMemoryBytes, b)
}

// Update the snapshotsCount and the diskSize levels
func (c *Cache) updateSnapshots() {
	// Update disk stats
	diskSizeStat := new(expvar.Int)
	diskSizeStat.Set(int64(c.snapshotsSize))
	c.statMap.Set(statCacheDiskBytes, diskSizeStat)

	snapshotsStat := new(expvar.Int)
	snapshotsStat.Set(int64(len(c.snapshots)))
	c.statMap.Set(statSnapshots, snapshotsStat)
}
