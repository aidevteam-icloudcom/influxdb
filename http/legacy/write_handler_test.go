package legacy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/influxdata/influxdb/v2"
	"github.com/influxdata/influxdb/v2/authorizer"
	pcontext "github.com/influxdata/influxdb/v2/context"
	"github.com/influxdata/influxdb/v2/dbrp"
	"github.com/influxdata/influxdb/v2/http/mocks"
	kithttp "github.com/influxdata/influxdb/v2/kit/transport/http"
	"github.com/influxdata/influxdb/v2/models"
	"github.com/influxdata/influxdb/v2/snowflake"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

var generator = snowflake.NewDefaultIDGenerator()

func TestWriteHandler_BucketAndMappingExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		// Mocked Services
		eventRecorder  = mocks.NewMockEventRecorder(ctrl)
		dbrpMappingSvc = mocks.NewMockDBRPMappingServiceV2(ctrl)
		bucketService  = mocks.NewMockBucketService(ctrl)
		pointsWriter   = mocks.NewMockPointsWriter(ctrl)

		// Found Resources
		orgID  = generator.ID()
		bucket = &influxdb.Bucket{
			ID:                  generator.ID(),
			OrgID:               orgID,
			Name:                "mydb/autogen",
			RetentionPolicyName: "autogen",
			RetentionPeriod:     72 * time.Hour,
		}
		mapping = &influxdb.DBRPMappingV2{
			OrganizationID:  orgID,
			BucketID:        bucket.ID,
			Database:        "mydb",
			RetentionPolicy: "autogen",
		}

		lineProtocolBody = "m,t1=v1 f1=2 100"
	)

	findAutogenMapping := dbrpMappingSvc.
		EXPECT().
		FindMany(gomock.Any(), influxdb.DBRPMappingFilterV2{
			OrgID:    &mapping.OrganizationID,
			Database: &mapping.Database,
		}).Return([]*influxdb.DBRPMappingV2{mapping}, 1, nil)

	findBucketByID := bucketService.
		EXPECT().
		FindBucketByID(gomock.Any(), bucket.ID).Return(bucket, nil)

	points := parseLineProtocol(t, lineProtocolBody)
	writePoints := pointsWriter.
		EXPECT().
		WritePoints(gomock.Any(), orgID, bucket.ID, pointsMatcher{points}).Return(nil)

	recordWriteEvent := eventRecorder.EXPECT().
		Record(gomock.Any(), gomock.Any())

	gomock.InOrder(
		findAutogenMapping,
		findBucketByID,
		writePoints,
		recordWriteEvent,
	)

	perms := newPermissions(influxdb.BucketsResourceType, &orgID, nil)
	auth := newAuthorization(orgID, perms...)
	ctx := pcontext.SetAuthorizer(context.Background(), auth)
	r := newWriteRequest(ctx, lineProtocolBody)
	params := r.URL.Query()
	params.Set("db", "mydb")
	params.Set("rp", "")
	r.URL.RawQuery = params.Encode()

	handler := NewWriterHandler(&PointsWriterBackend{
		HTTPErrorHandler:   DefaultErrorHandler,
		Logger:             zaptest.NewLogger(t),
		BucketService:      authorizer.NewBucketService(bucketService),
		DBRPMappingService: dbrp.NewAuthorizedService(dbrpMappingSvc),
		PointsWriter:       pointsWriter,
		EventRecorder:      eventRecorder,
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "", w.Body.String())
}

func TestWriteHandler_MappingNotExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		// Mocked Services
		eventRecorder  = mocks.NewMockEventRecorder(ctrl)
		dbrpMappingSvc = mocks.NewMockDBRPMappingServiceV2(ctrl)
		bucketService  = mocks.NewMockBucketService(ctrl)
		pointsWriter   = mocks.NewMockPointsWriter(ctrl)

		// Found Resources
		orgID  = generator.ID()
		bucket = &influxdb.Bucket{
			ID:                  generator.ID(),
			OrgID:               orgID,
			Name:                "mydb/autogen",
			RetentionPolicyName: "autogen",
			RetentionPeriod:     72 * time.Hour,
		}
		mapping = &influxdb.DBRPMappingV2{
			OrganizationID:  orgID,
			BucketID:        bucket.ID,
			Database:        "mydb",
			RetentionPolicy: "autogen",
		}

		lineProtocolBody = "m,t1=v1 f1=2 100"
	)

	findAutogenMapping := dbrpMappingSvc.
		EXPECT().
		FindMany(gomock.Any(), influxdb.DBRPMappingFilterV2{
			OrgID:    &mapping.OrganizationID,
			Database: &mapping.Database,
		}).Return(nil, 0, dbrp.ErrDBRPNotFound)

	recordWriteEvent := eventRecorder.EXPECT().
		Record(gomock.Any(), gomock.Any())

	gomock.InOrder(
		findAutogenMapping,
		recordWriteEvent,
	)

	perms := newPermissions(influxdb.BucketsResourceType, &orgID, nil)
	auth := newAuthorization(orgID, perms...)
	ctx := pcontext.SetAuthorizer(context.Background(), auth)
	r := newWriteRequest(ctx, lineProtocolBody)
	params := r.URL.Query()
	params.Set("db", "mydb")
	params.Set("rp", "")
	r.URL.RawQuery = params.Encode()

	handler := NewWriterHandler(&PointsWriterBackend{
		HTTPErrorHandler:   DefaultErrorHandler,
		Logger:             zaptest.NewLogger(t),
		BucketService:      authorizer.NewBucketService(bucketService),
		DBRPMappingService: dbrp.NewAuthorizedService(dbrpMappingSvc),
		PointsWriter:       pointsWriter,
		EventRecorder:      eventRecorder,
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, `{"code":"not found","message":"unable to find DBRP"}`, w.Body.String())
}

var DefaultErrorHandler = kithttp.ErrorHandler(0)

func parseLineProtocol(t *testing.T, line string) []models.Point {
	t.Helper()
	points, err := models.ParsePoints([]byte(line))
	if err != nil {
		t.Error(err)
	}
	return points
}

type pointsMatcher struct {
	points []models.Point
}

func (m pointsMatcher) Matches(x interface{}) bool {
	other, ok := x.([]models.Point)
	if !ok {
		return false
	}

	if len(m.points) != len(other) {
		return false
	}

	for i := 0; i < len(m.points)-1; i++ {
		p := m.points[i]
		op := other[i]

		if !reflect.DeepEqual(p.Name(), op.Name()) {
			return false
		}

		if !reflect.DeepEqual(p.Tags(), op.Tags()) {
			return false
		}

		fields, err := p.Fields()
		if err != nil {
			return false
		}
		ofields, err := op.Fields()
		if err != nil {
			return false
		}
		if !reflect.DeepEqual(fields, ofields) {
			return false
		}
	}

	return true
}

func (m pointsMatcher) String() string {
	return fmt.Sprintf("%#v", m.points)
}

type bucketMatcher struct {
	*influxdb.Bucket
}

func (m bucketMatcher) Matches(x interface{}) bool {
	other, ok := x.(*influxdb.Bucket)
	if !ok {
		return false
	}
	return cmp.Equal(m.Bucket, other, cmpopts.IgnoreFields(influxdb.Bucket{}, "CRUDLog"))
}

func (m bucketMatcher) String() string {
	return fmt.Sprintf("%#v", m.Bucket)
}

func newPermissions(resourceType influxdb.ResourceType, orgID, id *influxdb.ID) []influxdb.Permission {
	return []influxdb.Permission{
		{
			Action: influxdb.WriteAction,
			Resource: influxdb.Resource{
				Type:  resourceType,
				OrgID: orgID,
				ID:    id,
			},
		},
		{
			Action: influxdb.ReadAction,
			Resource: influxdb.Resource{
				Type:  resourceType,
				OrgID: orgID,
				ID:    id,
			},
		},
	}
}

func newAuthorization(orgID influxdb.ID, permissions ...influxdb.Permission) *influxdb.Authorization {
	return &influxdb.Authorization{
		ID:          generator.ID(),
		Status:      influxdb.Active,
		OrgID:       orgID,
		Permissions: permissions,
	}
}

func newWriteRequest(ctx context.Context, body string) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	return httptest.NewRequest(http.MethodPost, "http://localhost:9999/write", r).WithContext(ctx)
}
