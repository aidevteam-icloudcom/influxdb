// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/influxdata/influxdb/v2/replications (interfaces: ReplicationValidator)

// Package mock is a generated GoMock package.
package mock

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	"github.com/influxdata/influxdb/v2"
)

// MockReplicationValidator is a mock of ReplicationValidator interface.
type MockReplicationValidator struct {
	ctrl     *gomock.Controller
	recorder *MockReplicationValidatorMockRecorder
}

// MockReplicationValidatorMockRecorder is the mock recorder for MockReplicationValidator.
type MockReplicationValidatorMockRecorder struct {
	mock *MockReplicationValidator
}

// NewMockReplicationValidator creates a new mock instance.
func NewMockReplicationValidator(ctrl *gomock.Controller) *MockReplicationValidator {
	mock := &MockReplicationValidator{ctrl: ctrl}
	mock.recorder = &MockReplicationValidatorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockReplicationValidator) EXPECT() *MockReplicationValidatorMockRecorder {
	return m.recorder
}

// ValidateReplication mocks base method.
func (m *MockReplicationValidator) ValidateReplication(arg0 context.Context, arg1 *influxdb.ReplicationHTTPConfig) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ValidateReplication", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// ValidateReplication indicates an expected call of ValidateReplication.
func (mr *MockReplicationValidatorMockRecorder) ValidateReplication(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ValidateReplication", reflect.TypeOf((*MockReplicationValidator)(nil).ValidateReplication), arg0, arg1)
}
