// Package main is a generated GoMock package.
package main

import (
	reflect "reflect"

	autoscaling "github.com/aws/aws-sdk-go/service/autoscaling"
	gomock "github.com/golang/mock/gomock"
)

// MockAutoScalingAPI is a mock of AutoScalingAPI interface.
type MockAutoScalingAPI struct {
	ctrl     *gomock.Controller
	recorder *MockAutoScalingAPIMockRecorder
}

// MockAutoScalingAPIMockRecorder is the mock recorder for MockAutoScalingAPI.
type MockAutoScalingAPIMockRecorder struct {
	mock *MockAutoScalingAPI
}

// NewMockAutoScalingAPI creates a new mock instance.
func NewMockAutoScalingAPI(ctrl *gomock.Controller) *MockAutoScalingAPI {
	mock := &MockAutoScalingAPI{ctrl: ctrl}
	mock.recorder = &MockAutoScalingAPIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAutoScalingAPI) EXPECT() *MockAutoScalingAPIMockRecorder {
	return m.recorder
}

// DescribeAutoScalingGroups mocks base method.
func (m *MockAutoScalingAPI) DescribeAutoScalingGroups(arg0 *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DescribeAutoScalingGroups", arg0)
	ret0, _ := ret[0].(*autoscaling.DescribeAutoScalingGroupsOutput)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DescribeAutoScalingGroups indicates an expected call of DescribeAutoScalingGroups.
func (mr *MockAutoScalingAPIMockRecorder) DescribeAutoScalingGroups(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DescribeAutoScalingGroups", reflect.TypeOf((*MockAutoScalingAPI)(nil).DescribeAutoScalingGroups), arg0)
}

// UpdateAutoScalingGroup mocks base method.
func (m *MockAutoScalingAPI) UpdateAutoScalingGroup(arg0 *autoscaling.UpdateAutoScalingGroupInput) (*autoscaling.UpdateAutoScalingGroupOutput, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateAutoScalingGroup", arg0)
	ret0, _ := ret[0].(*autoscaling.UpdateAutoScalingGroupOutput)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// UpdateAutoScalingGroup indicates an expected call of UpdateAutoScalingGroup.
func (mr *MockAutoScalingAPIMockRecorder) UpdateAutoScalingGroup(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateAutoScalingGroup", reflect.TypeOf((*MockAutoScalingAPI)(nil).UpdateAutoScalingGroup), arg0)
}
