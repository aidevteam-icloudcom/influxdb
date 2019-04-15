package query

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/influxdata/flux"
	"github.com/influxdata/flux/iocounter"
	"github.com/influxdata/influxdb/kit/check"
	"github.com/influxdata/influxdb/kit/tracing"
)

// LoggingProxyQueryService wraps a ProxyQueryService and logs the queries.
type LoggingProxyQueryService struct {
	ProxyQueryService ProxyQueryService
	QueryLogger       Logger
	NowFunction       func() time.Time
}

// Query executes and logs the query.
func (s *LoggingProxyQueryService) Query(ctx context.Context, w io.Writer, req *ProxyRequest) (stats flux.Statistics, err error) {
	span, ctx := tracing.StartSpanFromContext(ctx)
	defer span.Finish()

	var n int64
	defer func() {
		r := recover()
		if r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
		var now time.Time
		if s.NowFunction != nil {
			now = s.NowFunction()
		} else {
			now = time.Now()
		}
		log := Log{
			OrganizationID: req.Request.OrganizationID,
			ProxyRequest:   req,
			ResponseSize:   n,
			Time:           now,
			Statistics:     stats,
			Error:          err,
		}
		s.QueryLogger.Log(log)
	}()

	wc := &iocounter.Writer{Writer: w}
	stats, err = s.ProxyQueryService.Query(ctx, wc, req)
	if err != nil {
		return stats, tracing.LogError(span, err)
	}
	n = wc.Count()
	return stats, nil
}

func (s *LoggingProxyQueryService) Check(ctx context.Context) check.Response {
	return s.ProxyQueryService.Check(ctx)
}
