package coordinator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/influxdata/influxdb"
	"github.com/influxdata/influxdb/models"
	"github.com/influxdata/influxdb/services/meta"
	"github.com/influxdata/influxdb/tsdb"
	"go.uber.org/zap"
)

// The keys for statistics generated by the "write" module.
const (
	statWriteReq           = "req"
	statPointWriteReq      = "pointReq"
	statPointWriteReqLocal = "pointReqLocal"
	statWriteOK            = "writeOk"
	statWriteDrop          = "writeDrop"
	statWriteTimeout       = "writeTimeout"
	statWriteErr           = "writeError"
	statSubWriteOK         = "subWriteOk"
	statSubWriteDrop       = "subWriteDrop"
)

var (
	// ErrTimeout is returned when a write times out.
	ErrTimeout = errors.New("timeout")

	// ErrPartialWrite is returned when a write partially succeeds but does
	// not meet the requested consistency level.
	ErrPartialWrite = errors.New("partial write")

	// ErrWriteFailed is returned when no writes succeeded.
	ErrWriteFailed = errors.New("write failed")
)

// PointsWriter handles writes across multiple local and remote data nodes.
type PointsWriter struct {
	mu           sync.RWMutex
	closing      chan struct{}
	WriteTimeout time.Duration
	Logger       *zap.Logger

	Node *influxdb.Node

	MetaClient interface {
		Database(name string) (di *meta.DatabaseInfo)
		RetentionPolicy(database, policy string) (*meta.RetentionPolicyInfo, error)
		CreateShardGroup(database, policy string, timestamp time.Time) (*meta.ShardGroupInfo, error)
	}

	TSDBStore interface {
		CreateShard(database, retentionPolicy string, shardID uint64, enabled bool) error
		WriteToShard(shardID uint64, points []models.Point) error
	}

	subPoints []chan<- *WritePointsRequest

	stats *WriteStatistics
}

// WritePointsRequest represents a request to write point data to the cluster.
type WritePointsRequest struct {
	Database        string
	RetentionPolicy string
	Points          []models.Point
}

// AddPoint adds a point to the WritePointRequest with field key 'value'
func (w *WritePointsRequest) AddPoint(name string, value interface{}, timestamp time.Time, tags map[string]string) {
	pt, err := models.NewPoint(
		name, models.NewTags(tags), map[string]interface{}{"value": value}, timestamp,
	)
	if err != nil {
		return
	}
	w.Points = append(w.Points, pt)
}

// NewPointsWriter returns a new instance of PointsWriter for a node.
func NewPointsWriter() *PointsWriter {
	return &PointsWriter{
		closing:      make(chan struct{}),
		WriteTimeout: DefaultWriteTimeout,
		Logger:       zap.NewNop(),
		stats:        &WriteStatistics{},
	}
}

// ShardMapping contains a mapping of shards to points.
type ShardMapping struct {
	n       int
	Points  map[uint64][]models.Point  // The points associated with a shard ID
	Shards  map[uint64]*meta.ShardInfo // The shards that have been mapped, keyed by shard ID
	Dropped []models.Point             // Points that were dropped
}

// NewShardMapping creates an empty ShardMapping.
func NewShardMapping(n int) *ShardMapping {
	return &ShardMapping{
		n:      n,
		Points: map[uint64][]models.Point{},
		Shards: map[uint64]*meta.ShardInfo{},
	}
}

// MapPoint adds the point to the ShardMapping, associated with the given shardInfo.
func (s *ShardMapping) MapPoint(shardInfo *meta.ShardInfo, p models.Point) {
	if cap(s.Points[shardInfo.ID]) < s.n {
		s.Points[shardInfo.ID] = make([]models.Point, 0, s.n)
	}
	s.Points[shardInfo.ID] = append(s.Points[shardInfo.ID], p)
	s.Shards[shardInfo.ID] = shardInfo
}

// Open opens the communication channel with the point writer.
func (w *PointsWriter) Open() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closing = make(chan struct{})
	return nil
}

// Close closes the communication channel with the point writer.
func (w *PointsWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closing != nil {
		close(w.closing)
	}
	if w.subPoints != nil {
		// 'nil' channels always block so this makes the
		// select statement in WritePoints hit its default case
		// dropping any in-flight writes.
		w.subPoints = nil
	}
	return nil
}

func (w *PointsWriter) AddWriteSubscriber(c chan<- *WritePointsRequest) {
	w.subPoints = append(w.subPoints, c)
}

// WithLogger sets the Logger on w.
func (w *PointsWriter) WithLogger(log *zap.Logger) {
	w.Logger = log.With(zap.String("service", "write"))
}

// WriteStatistics keeps statistics related to the PointsWriter.
type WriteStatistics struct {
	WriteReq           int64
	PointWriteReq      int64
	PointWriteReqLocal int64
	WriteOK            int64
	WriteDropped       int64
	WriteTimeout       int64
	WriteErr           int64
	SubWriteOK         int64
	SubWriteDrop       int64
}

// Statistics returns statistics for periodic monitoring.
func (w *PointsWriter) Statistics(tags map[string]string) []models.Statistic {
	return []models.Statistic{{
		Name: "write",
		Tags: tags,
		Values: map[string]interface{}{
			statWriteReq:           atomic.LoadInt64(&w.stats.WriteReq),
			statPointWriteReq:      atomic.LoadInt64(&w.stats.PointWriteReq),
			statPointWriteReqLocal: atomic.LoadInt64(&w.stats.PointWriteReqLocal),
			statWriteOK:            atomic.LoadInt64(&w.stats.WriteOK),
			statWriteDrop:          atomic.LoadInt64(&w.stats.WriteDropped),
			statWriteTimeout:       atomic.LoadInt64(&w.stats.WriteTimeout),
			statWriteErr:           atomic.LoadInt64(&w.stats.WriteErr),
			statSubWriteOK:         atomic.LoadInt64(&w.stats.SubWriteOK),
			statSubWriteDrop:       atomic.LoadInt64(&w.stats.SubWriteDrop),
		},
	}}
}

// MapShards maps the points contained in wp to a ShardMapping.  If a point
// maps to a shard group or shard that does not currently exist, it will be
// created before returning the mapping.
func (w *PointsWriter) MapShards(wp *WritePointsRequest) (*ShardMapping, error) {
	rp, err := w.MetaClient.RetentionPolicy(wp.Database, wp.RetentionPolicy)
	if err != nil {
		return nil, err
	} else if rp == nil {
		return nil, influxdb.ErrRetentionPolicyNotFound(wp.RetentionPolicy)
	}

	// Holds all the shard groups and shards that are required for writes.
	list := make(sgList, 0, 8)
	min := time.Unix(0, models.MinNanoTime)
	if rp.Duration > 0 {
		min = time.Now().Add(-rp.Duration)
	}

	for _, p := range wp.Points {
		// Either the point is outside the scope of the RP, or we already have
		// a suitable shard group for the point.
		if p.Time().Before(min) || list.Covers(p.Time()) {
			continue
		}

		// No shard groups overlap with the point's time, so we will create
		// a new shard group for this point.
		sg, err := w.MetaClient.CreateShardGroup(wp.Database, wp.RetentionPolicy, p.Time())
		if err != nil {
			return nil, err
		}

		if sg == nil {
			return nil, errors.New("nil shard group")
		}
		list = list.Append(*sg)
	}

	mapping := NewShardMapping(len(wp.Points))
	for _, p := range wp.Points {
		sg := list.ShardGroupAt(p.Time())
		if sg == nil {
			// We didn't create a shard group because the point was outside the
			// scope of the RP.
			mapping.Dropped = append(mapping.Dropped, p)
			atomic.AddInt64(&w.stats.WriteDropped, 1)
			continue
		}

		sh := sg.ShardFor(p)
		mapping.MapPoint(&sh, p)
	}
	return mapping, nil
}

// sgList is a wrapper around a meta.ShardGroupInfos where we can also check
// if a given time is covered by any of the shard groups in the list.
type sgList meta.ShardGroupInfos

func (l sgList) Covers(t time.Time) bool {
	if len(l) == 0 {
		return false
	}
	return l.ShardGroupAt(t) != nil
}

// ShardGroupAt attempts to find a shard group that could contain a point
// at the given time.
//
// Shard groups are sorted first according to end time, and then according
// to start time. Therefore, if there are multiple shard groups that match
// this point's time they will be preferred in this order:
//
//  - a shard group with the earliest end time;
//  - (assuming identical end times) the shard group with the earliest start time.
func (l sgList) ShardGroupAt(t time.Time) *meta.ShardGroupInfo {
	idx := sort.Search(len(l), func(i int) bool { return l[i].EndTime.After(t) })

	// We couldn't find a shard group the point falls into.
	if idx == len(l) || t.Before(l[idx].StartTime) {
		return nil
	}
	return &l[idx]
}

// Append appends a shard group to the list, and returns a sorted list.
func (l sgList) Append(sgi meta.ShardGroupInfo) sgList {
	next := append(l, sgi)
	sort.Sort(meta.ShardGroupInfos(next))
	return next
}

// WritePointsInto is a copy of WritePoints that uses a tsdb structure instead of
// a cluster structure for information. This is to avoid a circular dependency.
func (w *PointsWriter) WritePointsInto(p *IntoWriteRequest) error {
	return w.WritePointsPrivileged(p.Database, p.RetentionPolicy, models.ConsistencyLevelOne, p.Points)
}

// A wrapper for WritePointsWithContext()
func (w *PointsWriter) WritePoints(database, retentionPolicy string, consistencyLevel models.ConsistencyLevel, user meta.User, points []models.Point) error {
	return w.WritePointsWithContext(context.Background(), database, retentionPolicy, consistencyLevel, user, points)

}

type ContextKey int

const (
	StatPointsWritten = ContextKey(iota)
	StatValuesWritten
)

// WritePointsWithContext writes data to the underlying storage. consitencyLevel and user are only used for clustered scenarios.
//
func (w *PointsWriter) WritePointsWithContext(ctx context.Context, database, retentionPolicy string, consistencyLevel models.ConsistencyLevel, user meta.User, points []models.Point) error {
	return w.WritePointsPrivilegedWithContext(ctx, database, retentionPolicy, consistencyLevel, points)
}

func (w *PointsWriter) WritePointsPrivileged(database, retentionPolicy string, consistencyLevel models.ConsistencyLevel, points []models.Point) error {
	return w.WritePointsPrivilegedWithContext(context.Background(), database, retentionPolicy, consistencyLevel, points)
}

// WritePointsPrivilegedWithContext writes the data to the underlying storage,
// consitencyLevel is only used for clustered scenarios
//
// If a request for StatPointsWritten or StatValuesWritten of type ContextKey is
// sent via context values, this stores the total points and fields written in
// the memory pointed to by the associated wth the int64 pointers.
//
func (w *PointsWriter) WritePointsPrivilegedWithContext(ctx context.Context, database, retentionPolicy string, consistencyLevel models.ConsistencyLevel, points []models.Point) error {
	atomic.AddInt64(&w.stats.WriteReq, 1)
	atomic.AddInt64(&w.stats.PointWriteReq, int64(len(points)))

	if retentionPolicy == "" {
		db := w.MetaClient.Database(database)
		if db == nil {
			return influxdb.ErrDatabaseNotFound(database)
		}
		retentionPolicy = db.DefaultRetentionPolicy
	}

	shardMappings, err := w.MapShards(&WritePointsRequest{Database: database, RetentionPolicy: retentionPolicy, Points: points})
	if err != nil {
		return err
	}

	// Write each shard in it's own goroutine and return as soon as one fails.
	ch := make(chan error, len(shardMappings.Points))
	for shardID, points := range shardMappings.Points {
		go func(ctx context.Context, shard *meta.ShardInfo, database, retentionPolicy string, points []models.Point) {
			var numPoints, numValues int64
			ctx = context.WithValue(ctx, tsdb.StatPointsWritten, &numPoints)
			ctx = context.WithValue(ctx, tsdb.StatValuesWritten, &numValues)

			err := w.writeToShardWithContext(ctx, shard, database, retentionPolicy, points)
			if err == tsdb.ErrShardDeletion {
				err = tsdb.PartialWriteError{Reason: fmt.Sprintf("shard %d is pending deletion", shard.ID), Dropped: len(points)}
			}

			if v, ok := ctx.Value(StatPointsWritten).(*int64); ok {
				atomic.AddInt64(v, numPoints)
			}

			if v, ok := ctx.Value(StatValuesWritten).(*int64); ok {
				atomic.AddInt64(v, numValues)
			}

			ch <- err
		}(ctx, shardMappings.Shards[shardID], database, retentionPolicy, points)
	}

	// Send points to subscriptions if possible.
	var ok, dropped int64
	pts := &WritePointsRequest{Database: database, RetentionPolicy: retentionPolicy, Points: points}
	// We need to lock just in case the channel is about to be nil'ed
	w.mu.RLock()
	for _, ch := range w.subPoints {
		select {
		case ch <- pts:
			ok++
		default:
			dropped++
		}
	}
	w.mu.RUnlock()

	if ok > 0 {
		atomic.AddInt64(&w.stats.SubWriteOK, ok)
	}

	if dropped > 0 {
		atomic.AddInt64(&w.stats.SubWriteDrop, dropped)
	}

	if err == nil && len(shardMappings.Dropped) > 0 {
		err = tsdb.PartialWriteError{Reason: "points beyond retention policy", Dropped: len(shardMappings.Dropped)}

	}
	timeout := time.NewTimer(w.WriteTimeout)
	defer timeout.Stop()
	for range shardMappings.Points {
		select {
		case <-w.closing:
			return ErrWriteFailed
		case <-timeout.C:
			atomic.AddInt64(&w.stats.WriteTimeout, 1)
			// return timeout error to caller
			return ErrTimeout
		case err := <-ch:
			if err != nil {
				return err
			}
		}
	}
	return err
}

// writeToShards writes points to a shard.
func (w *PointsWriter) writeToShard(shard *meta.ShardInfo, database, retentionPolicy string, points []models.Point) error {
	return w.writeToShardWithContext(context.Background(), shard, database, retentionPolicy, points)
}

func (w *PointsWriter) writeToShardWithContext(ctx context.Context, shard *meta.ShardInfo, database, retentionPolicy string, points []models.Point) error {
	atomic.AddInt64(&w.stats.PointWriteReqLocal, int64(len(points)))

	// This is a small wrapper to make type-switching over w.TSDBStore a little
	// less verbose.
	writeToShard := func() error {
		type shardWriterWithContext interface {
			WriteToShardWithContext(context.Context, uint64, []models.Point) error
		}
		switch sw := w.TSDBStore.(type) {
		case shardWriterWithContext:
			if err := sw.WriteToShardWithContext(ctx, shard.ID, points); err != nil {
				return err
			}
		default:
			if err := w.TSDBStore.WriteToShard(shard.ID, points); err != nil {
				return err
			}
		}
		return nil
	}

	// Except tsdb.ErrShardNotFound no error can be handled here
	if err := writeToShard(); err == tsdb.ErrShardNotFound {
		// Shard doesn't exist -- lets create it and try again..

		// If we've written to shard that should exist on the current node, but the
		// store has not actually created this shard, tell it to create it and
		// retry the write
		if err = w.TSDBStore.CreateShard(database, retentionPolicy, shard.ID, true); err != nil {
			w.Logger.Info("Write failed", zap.Uint64("shard", shard.ID), zap.Error(err))
			atomic.AddInt64(&w.stats.WriteErr, 1)
			return err
		}

		// Now that we've created the shard, try to write to it again.
		if err := writeToShard(); err != nil {
			w.Logger.Info("Write failed", zap.Uint64("shard", shard.ID), zap.Error(err))
			atomic.AddInt64(&w.stats.WriteErr, 1)
			return err
		}
	} else {
		atomic.AddInt64(&w.stats.WriteErr, 1)
		return err
	}

	atomic.AddInt64(&w.stats.WriteOK, 1)
	return nil
}
