package influxdb

import (
	"context"
	"errors"

	"github.com/influxdata/flux"
	"github.com/influxdata/flux/execute"
	"github.com/influxdata/flux/memory"
	"github.com/influxdata/flux/plan"
	"github.com/influxdata/flux/semantic"
	"github.com/influxdata/influxdb/kit/tracing"
	"github.com/influxdata/influxdb/query"
	"github.com/influxdata/influxdb/tsdb/cursors"
)

func init() {
	execute.RegisterSource(ReadRangePhysKind, createReadFilterSource)
	execute.RegisterSource(ReadTagKeysPhysKind, createReadTagKeysSource)
}

type runner interface {
	run(ctx context.Context) error
}

type Source struct {
	id execute.DatasetID
	ts []execute.Transformation

	alloc *memory.Allocator
	stats cursors.CursorStats

	runner runner
}

func (s *Source) Run(ctx context.Context) {
	err := s.runner.run(ctx)
	for _, t := range s.ts {
		t.Finish(s.id, err)
	}
}

func (s *Source) AddTransformation(t execute.Transformation) {
	s.ts = append(s.ts, t)
}

func (s *Source) Metadata() flux.Metadata {
	return flux.Metadata{
		"influxdb/scanned-bytes":  []interface{}{s.stats.ScannedBytes},
		"influxdb/scanned-values": []interface{}{s.stats.ScannedValues},
	}
}

func (s *Source) processTables(ctx context.Context, tables TableIterator, watermark execute.Time) error {
	err := tables.Do(func(tbl flux.Table) error {
		for _, t := range s.ts {
			if err := t.Process(s.id, tbl); err != nil {
				return err
			}
			//TODO(nathanielc): Also add mechanism to send UpdateProcessingTime calls, when no data is arriving.
			// This is probably not needed for this source, but other sources should do so.
			if err := t.UpdateProcessingTime(s.id, execute.Now()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Track the number of bytes and values scanned.
	stats := tables.Statistics()
	s.stats.ScannedValues += stats.ScannedValues
	s.stats.ScannedBytes += stats.ScannedBytes

	for _, t := range s.ts {
		if err := t.UpdateWatermark(s.id, watermark); err != nil {
			return err
		}
	}
	return nil
}

type readFilterSource struct {
	Source
	reader   Reader
	readSpec ReadFilterSpec
}

func ReadFilterSource(id execute.DatasetID, r Reader, readSpec ReadFilterSpec, alloc *memory.Allocator) execute.Source {
	src := new(readFilterSource)

	src.id = id
	src.alloc = alloc

	src.reader = r
	src.readSpec = readSpec

	src.runner = src
	return src
}

func (s *readFilterSource) run(ctx context.Context) error {
	stop := s.readSpec.Bounds.Stop
	tables, err := s.reader.ReadFilter(
		ctx,
		s.readSpec,
		s.alloc,
	)
	if err != nil {
		return err
	}
	return s.processTables(ctx, tables, stop)
}

func createReadFilterSource(s plan.ProcedureSpec, id execute.DatasetID, a execute.Administration) (execute.Source, error) {
	span, ctx := tracing.StartSpanFromContext(context.TODO())
	defer span.Finish()

	spec := s.(*ReadRangePhysSpec)

	bounds := a.StreamContext().Bounds()
	if bounds == nil {
		return nil, errors.New("nil bounds passed to from")
	}

	deps := a.Dependencies()[FromKind].(Dependencies)

	req := query.RequestFromContext(a.Context())
	if req == nil {
		return nil, errors.New("missing request on context")
	}

	orgID := req.OrganizationID
	bucketID, err := spec.LookupBucketID(ctx, orgID, deps.BucketLookup)
	if err != nil {
		return nil, err
	}

	var filter *semantic.FunctionExpression
	if spec.FilterSet {
		filter = spec.Filter
	}
	return ReadFilterSource(
		id,
		deps.Reader,
		ReadFilterSpec{
			OrganizationID: orgID,
			BucketID:       bucketID,
			Bounds:         *bounds,
			Predicate:      filter,
		},
		a.Allocator(),
	), nil
}

func createReadTagKeysSource(prSpec plan.ProcedureSpec, dsid execute.DatasetID, a execute.Administration) (execute.Source, error) {
	span, ctx := tracing.StartSpanFromContext(context.TODO())
	defer span.Finish()

	spec := prSpec.(*ReadTagKeysPhysSpec)
	deps := a.Dependencies()[FromKind].(Dependencies)
	req := query.RequestFromContext(a.Context())
	if req == nil {
		return nil, errors.New("missing request on context")
	}
	orgID := req.OrganizationID

	bucketID, err := spec.LookupBucketID(ctx, orgID, deps.BucketLookup)
	if err != nil {
		return nil, err
	}

	var filter *semantic.FunctionExpression
	if spec.FilterSet {
		filter = spec.Filter
	}

	bounds := a.StreamContext().Bounds()
	return ReadTagKeysSource(
		dsid,
		deps.Reader,
		ReadTagKeysSpec{
			ReadFilterSpec: ReadFilterSpec{
				OrganizationID: orgID,
				BucketID:       bucketID,
				Bounds:         *bounds,
				Predicate:      filter,
			},
			ValueColumnName: spec.ValueColumnName,
		},
		a.Allocator(),
	), nil
}

type readTagKeysSource struct {
	Source

	reader   Reader
	readSpec ReadTagKeysSpec
}

func ReadTagKeysSource(id execute.DatasetID, r Reader, readSpec ReadTagKeysSpec, alloc *memory.Allocator) execute.Source {
	src := &readTagKeysSource{
		reader:   r,
		readSpec: readSpec,
	}
	src.id = id
	src.alloc = alloc
	src.runner = src
	return src
}

func (s *readTagKeysSource) run(ctx context.Context) error {
	ti, err := s.reader.ReadTagKeys(ctx, s.readSpec, s.alloc)
	if err != nil {
		return err
	}
	return s.processTables(ctx, ti, execute.Now())
}
