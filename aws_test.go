package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestNewAWSClient(t *testing.T) {
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String("us-west-2"),
	}))
	client := NewAWSClient(sess)
	assert.NotNil(t, client)
}

func TestDescribeAutoScalingGroups(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockAutoScalingAPI(ctrl)

	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{"test-asg-1", "test-asg-2"}),
	}
	output := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			{
				AutoScalingGroupName: aws.String("test-asg-1"),
				DesiredCapacity:      aws.Int64(3),
				MinSize:              aws.Int64(1),
				MaxSize:              aws.Int64(5),
				Instances:            []*autoscaling.Instance{},
				Tags:                 []*autoscaling.TagDescription{},
			},
			{
				AutoScalingGroupName: aws.String("test-asg-2"),
				DesiredCapacity:      aws.Int64(0),
				MinSize:              aws.Int64(0),
				MaxSize:              aws.Int64(6),
				Instances:            []*autoscaling.Instance{},
				Tags:                 []*autoscaling.TagDescription{},
			},
		},
	}

	mockSvc.EXPECT().DescribeAutoScalingGroups(input).Return(output, nil)

	awsClient := &AWSClient{svc: mockSvc}
	result, err := awsClient.DescribeAutoScalingGroups(input)

	assert.NoError(t, err)
	assert.Equal(t, output, result)
}

func TestGetCurrentCapacity(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockAutoScalingAPI(ctrl)

	asgName := "test-asg-1"
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{asgName}),
	}
	output := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			{
				AutoScalingGroupName: aws.String(asgName),
				DesiredCapacity:      aws.Int64(3),
				Instances:            []*autoscaling.Instance{}, // no instances => allocatedCount == 0
			},
		},
	}

	mockSvc.EXPECT().DescribeAutoScalingGroups(input).Return(output, nil)

	awsClient := &AWSClient{svc: mockSvc}
	allocated, desired, err := GetCurrentCapacity(awsClient, asgName)

	assert.NoError(t, err)
	assert.Equal(t, int64(0), allocated) // no instances in output
	assert.Equal(t, int64(3), desired)   // desired from ASG
}

func TestUpdateASGCapacity(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockAutoScalingAPI(ctrl)

	asg := AutoScalingGroup{Name: "test-asg-2"}
	capacity := int64(6)
	maxCapacity := int64(6)
	input := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(asg.Name),
		MinSize:              aws.Int64(capacity),
		MaxSize:              aws.Int64(capacity),
		DesiredCapacity:      aws.Int64(capacity),
	}

	mockSvc.EXPECT().UpdateAutoScalingGroup(input).Return(&autoscaling.UpdateAutoScalingGroupOutput{}, nil)

	awsClient := &AWSClient{svc: mockSvc}
	err := UpdateASGCapacity(awsClient, asg, capacity, maxCapacity)

	assert.NoError(t, err)
}
func TestUpdateASGCapacity_MaxCapacityReached(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockAutoScalingAPI(ctrl)

	asg := AutoScalingGroup{Name: "test-asg-2"}
	capacity := int64(7) // Exceeds max capacity
	maxCapacity := int64(6)

	awsClient := &AWSClient{svc: mockSvc}
	err := UpdateASGCapacity(awsClient, asg, capacity, maxCapacity)

	expectedErrMsg := fmt.Sprintf("cannot update ASG %s%s%s: desired capacity  %s%d%s exceeds max capacity  %s%d%s",
		LightGray, "test-asg-2", Reset, Green, 7, Reset, Green, 6, Reset)

	assert.Error(t, err)
	assert.Equal(t, expectedErrMsg, err.Error())
}
func TestHandleASG_DoesNotScaleDownWhenJobsWithTagExist(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockAutoScalingAPI(ctrl)

	// ASG name and describe input
	asgName := "test-asg-keep"
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{asgName}),
	}
	// ASG has DesiredCapacity=1 and one instance in InService (allocatedCount == 1)
	output := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			{
				AutoScalingGroupName: aws.String(asgName),
				DesiredCapacity:      aws.Int64(1),
				Instances: []*autoscaling.Instance{
					{
						InstanceId:     aws.String("i-012345"),
						LifecycleState: aws.String("InService"),
					},
				},
				Tags: []*autoscaling.TagDescription{}, // not used for test
			},
		},
	}

	// DescribeAutoScalingGroups will be called and should return our ASG snapshot
	mockSvc.EXPECT().DescribeAutoScalingGroups(input).Return(output, nil)

	// We expect that UpdateAutoScalingGroup is NOT called (scale-down must be blocked)
	mockSvc.EXPECT().UpdateAutoScalingGroup(gomock.Any()).Times(0)

	awsClient := &AWSClient{svc: mockSvc}

	// prepare ASG config: name with tag "keep-tag"
	asgCfg := Asg{
		Name:           asgName,
		Tags:           []string{"keep-tag"},
		MaxAsgCapacity: 5,
		ScaleToZero:    true,
		Region:         "us-west-2",
	}

	// Simulate there is one pending job with tag "keep-tag"
	pendingJobsWithTags := map[string]int{"keep-tag": 1}
	runningJobsWithTags := map[string]int{}

	// totals: one pending job overall
	var totalPendingJobs int64 = 1
	var totalRunningJobs int64 = 0
	var totalPendingWithoutTags int64 = 0
	var totalRunningWithoutTags int64 = 0

	var totalCapacity int64 = 0
	mu := &sync.Mutex{}

	// Call HandleASG; because pendingJobMatchingTags should be true, scale-down path must not execute.
	HandleASG(awsClient, asgCfg, totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags,
		bool(asgCfg.ScaleToZero), int64(asgCfg.MaxAsgCapacity), pendingJobsWithTags, runningJobsWithTags, &totalCapacity, mu)

	// If UpdateAutoScalingGroup was called unexpectedly, gomock will fail the test automatically
}
func TestScaleUp_MultipleASGs_MultipleProjects(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockAutoScalingAPI(ctrl)

	// ASG A (amd64): current desired=1, allocated=1 (one InService instance)
	asgA := "asg-amd64"
	inputA := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{asgA}),
	}
	outputA := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			{
				AutoScalingGroupName: aws.String(asgA),
				DesiredCapacity:      aws.Int64(1),
				Instances: []*autoscaling.Instance{
					{InstanceId: aws.String("i-a1"), LifecycleState: aws.String("InService")},
				},
			},
		},
	}

	// ASG B (arm64): current desired=0, allocated=0
	asgB := "asg-arm64"
	inputB := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{asgB}),
	}
	outputB := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			{
				AutoScalingGroupName: aws.String(asgB),
				DesiredCapacity:      aws.Int64(0),
				Instances:            []*autoscaling.Instance{},
			},
		},
	}

	// Expect Describe called for each ASG (order not important)
	mockSvc.EXPECT().DescribeAutoScalingGroups(inputA).Return(outputA, nil)
	mockSvc.EXPECT().DescribeAutoScalingGroups(inputB).Return(outputB, nil)

	// We expect UpdateAutoScalingGroup for each ASG with correct DesiredCapacity:
	// Scenario:
	// - Several projects produce pending jobs:
	//   * projects produce 4 amd64 pending jobs total
	//   * projects produce 2 arm64 pending jobs total
	// For ASG A (amd64): allocated=1, pendingForASG=4 -> additionalNeeded=3 -> proposed = desired(1)+3 = 4
	// For ASG B (arm64): allocated=0, pendingForASG=2 -> additionalNeeded=2 -> proposed = desired(0)+2 = 2

	// Expect Update for ASG A with DesiredCapacity == 4
	mockSvc.EXPECT().
		UpdateAutoScalingGroup(gomock.Any()).
		Do(func(in *autoscaling.UpdateAutoScalingGroupInput) {
			// check ASG name and DesiredCapacity
			if aws.StringValue(in.AutoScalingGroupName) != asgA {
				t.Fatalf("unexpected ASG in first update: %s", aws.StringValue(in.AutoScalingGroupName))
			}
			if aws.Int64Value(in.DesiredCapacity) != int64(4) {
				t.Fatalf("asg %s: desired expected %d got %d", asgA, 4, aws.Int64Value(in.DesiredCapacity))
			}
		}).Return(&autoscaling.UpdateAutoScalingGroupOutput{}, nil)

	// Expect Update for ASG B with DesiredCapacity == 2
	mockSvc.EXPECT().
		UpdateAutoScalingGroup(gomock.Any()).
		Do(func(in *autoscaling.UpdateAutoScalingGroupInput) {
			if aws.StringValue(in.AutoScalingGroupName) != asgB {
				t.Fatalf("unexpected ASG in second update: %s", aws.StringValue(in.AutoScalingGroupName))
			}
			if aws.Int64Value(in.DesiredCapacity) != int64(2) {
				t.Fatalf("asg %s: desired expected %d got %d", asgB, 2, aws.Int64Value(in.DesiredCapacity))
			}
		}).Return(&autoscaling.UpdateAutoScalingGroupOutput{}, nil)

	awsClient := &AWSClient{svc: mockSvc}

	// Prepare ASG configs
	asgCfgA := Asg{Name: asgA, Tags: []string{"amd64"}, MaxAsgCapacity: 10, ScaleToZero: true, Region: "us-west-2"}
	asgCfgB := Asg{Name: asgB, Tags: []string{"arm64"}, MaxAsgCapacity: 10, ScaleToZero: true, Region: "us-west-2"}

	// Simulate aggregated pending jobs coming from multiple projects:
	// e.g., project1 has 2 amd64, project2 has 2 amd64, project3 has 2 arm64
	pendingJobsWithTags := map[string]int{
		"amd64": 4,
		"arm64": 2,
	}
	runningJobsWithTags := map[string]int{}

	// Totals (used for global no-tag shortages) — we set to the sum of pending across projects
	var totalPendingJobs int64 = 6
	var totalRunningJobs int64 = 0
	var totalPendingWithoutTags int64 = 0
	var totalRunningWithoutTags int64 = 0

	var totalCapacity int64 = 0
	mu := &sync.Mutex{}

	// Run scale for both ASG (call HandleASG for each ASG as ScaleAutoScalingGroups would)
	HandleASG(awsClient, asgCfgA, totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags,
		bool(asgCfgA.ScaleToZero), int64(asgCfgA.MaxAsgCapacity), pendingJobsWithTags, runningJobsWithTags, &totalCapacity, mu)

	HandleASG(awsClient, asgCfgB, totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags,
		bool(asgCfgB.ScaleToZero), int64(asgCfgB.MaxAsgCapacity), pendingJobsWithTags, runningJobsWithTags, &totalCapacity, mu)

	// gomock will fail the test if UpdateAutoScalingGroup expectations are not met
}
func TestScaleUp_OverlappingTags_DistributedPerASG(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockAutoScalingAPI(ctrl)

	// ASG1: tags [common, a]; desired=1, allocated=1
	asg1 := "asg-1"
	in1 := &autoscaling.DescribeAutoScalingGroupsInput{AutoScalingGroupNames: aws.StringSlice([]string{asg1})}
	out1 := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{{AutoScalingGroupName: aws.String(asg1), DesiredCapacity: aws.Int64(1),
			Instances: []*autoscaling.Instance{{InstanceId: aws.String("i-1"), LifecycleState: aws.String("InService")}}}},
	}
	// ASG2: tags [common, b]; desired=0, allocated=0
	asg2 := "asg-2"
	in2 := &autoscaling.DescribeAutoScalingGroupsInput{AutoScalingGroupNames: aws.StringSlice([]string{asg2})}
	out2 := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{{AutoScalingGroupName: aws.String(asg2), DesiredCapacity: aws.Int64(0), Instances: []*autoscaling.Instance{}}}}
	// Expectations for describes
	mockSvc.EXPECT().DescribeAutoScalingGroups(in1).Return(out1, nil)
	mockSvc.EXPECT().DescribeAutoScalingGroups(in2).Return(out2, nil)

	// Simulate aggregated pending jobs from multiple projects:
	// total pending with tag "common" = 3
	// total pending with tag "a" = 1
	// total pending with tag "b" = 2
	pendingJobsWithTags := map[string]int{"common": 3, "a": 1, "b": 2}
	runningJobsWithTags := map[string]int{}

	// Expectations for Updates:
	// For ASG1 (tags [common,a]): allocated=1, pendingForASG = common(3)+a(1)=4 -> additionalNeeded = 4-1 = 3
	// proposed desired = desired(1) + 3 = 4
	mockSvc.EXPECT().
		UpdateAutoScalingGroup(gomock.Any()).
		Do(func(in *autoscaling.UpdateAutoScalingGroupInput) {
			if aws.StringValue(in.AutoScalingGroupName) != asg1 {
				t.Fatalf("expected update for %s, got %s", asg1, aws.StringValue(in.AutoScalingGroupName))
			}
			if aws.Int64Value(in.DesiredCapacity) != int64(4) {
				t.Fatalf("asg1 desired want 4 got %d", aws.Int64Value(in.DesiredCapacity))
			}
		}).Return(&autoscaling.UpdateAutoScalingGroupOutput{}, nil)

	// For ASG2 (tags [common,b]): allocated=0, pendingForASG = common(3)+b(2)=5 -> additionalNeeded = 5-0 =5
	// proposed desired = desired(0) +5 =5
	mockSvc.EXPECT().
		UpdateAutoScalingGroup(gomock.Any()).
		Do(func(in *autoscaling.UpdateAutoScalingGroupInput) {
			if aws.StringValue(in.AutoScalingGroupName) != asg2 {
				t.Fatalf("expected update for %s, got %s", asg2, aws.StringValue(in.AutoScalingGroupName))
			}
			if aws.Int64Value(in.DesiredCapacity) != int64(5) {
				t.Fatalf("asg2 desired want 5 got %d", aws.Int64Value(in.DesiredCapacity))
			}
		}).Return(&autoscaling.UpdateAutoScalingGroupOutput{}, nil)

	awsClient := &AWSClient{svc: mockSvc}

	asgCfg1 := Asg{Name: asg1, Tags: []string{"common", "a"}, MaxAsgCapacity: 100, ScaleToZero: true, Region: "us-west-2"}
	asgCfg2 := Asg{Name: asg2, Tags: []string{"common", "b"}, MaxAsgCapacity: 100, ScaleToZero: true, Region: "us-west-2"}

	var totalPendingJobs int64 = 6 // sum of pending
	var totalRunningJobs int64 = 0
	var totalPendingWithoutTags int64 = 0
	var totalRunningWithoutTags int64 = 0

	var totalCapacity int64 = 0
	mu := &sync.Mutex{}

	HandleASG(awsClient, asgCfg1, totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags,
		bool(asgCfg1.ScaleToZero), int64(asgCfg1.MaxAsgCapacity), pendingJobsWithTags, runningJobsWithTags, &totalCapacity, mu)

	HandleASG(awsClient, asgCfg2, totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags,
		bool(asgCfg2.ScaleToZero), int64(asgCfg2.MaxAsgCapacity), pendingJobsWithTags, runningJobsWithTags, &totalCapacity, mu)
}
func TestHandleASG_NoScaleUpWhenAllocatedSufficient_ForArm64(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockAutoScalingAPI(ctrl)

	asgName := "asg-arm64-keep"
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{asgName}),
	}
	// ASG: desired=7, 7 instances InService => allocatedCount == 7
	output := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			{
				AutoScalingGroupName: aws.String(asgName),
				DesiredCapacity:      aws.Int64(7),
				Instances: []*autoscaling.Instance{
					{InstanceId: aws.String("i-1"), LifecycleState: aws.String("InService")},
					{InstanceId: aws.String("i-2"), LifecycleState: aws.String("InService")},
					{InstanceId: aws.String("i-3"), LifecycleState: aws.String("InService")},
					{InstanceId: aws.String("i-4"), LifecycleState: aws.String("InService")},
					{InstanceId: aws.String("i-5"), LifecycleState: aws.String("InService")},
					{InstanceId: aws.String("i-6"), LifecycleState: aws.String("InService")},
					{InstanceId: aws.String("i-7"), LifecycleState: aws.String("InService")},
				},
			},
		},
	}

	// Describe called and returns snapshot with 7 allocated
	mockSvc.EXPECT().DescribeAutoScalingGroups(input).Return(output, nil)

	// UpdateAutoScalingGroup must NOT be called (no scale-up, no scale-down)
	mockSvc.EXPECT().UpdateAutoScalingGroup(gomock.Any()).Times(0)

	awsClient := &AWSClient{svc: mockSvc}

	// ASG configuration serving tag "arm64"
	asgCfg := Asg{
		Name:           asgName,
		Tags:           []string{"arm64"},
		MaxAsgCapacity: 10,
		ScaleToZero:    true,
		Region:         "us-west-2",
	}

	// Simulate jobs across projects:
	// - runningJobsWithTags: 3 running jobs for arm64 (they occupy runners)
	// - pendingJobsWithTags: 1 new pending job for arm64 (just arrived)
	runningJobsWithTags := map[string]int{"arm64": 3}
	pendingJobsWithTags := map[string]int{"arm64": 1}

	// Totals used by HandleASG (sum of all projects)
	var totalPendingJobs int64 = 1
	var totalRunningJobs int64 = 3
	var totalPendingWithoutTags int64 = 0
	var totalRunningWithoutTags int64 = 0

	var totalCapacity int64 = 0
	mu := &sync.Mutex{}

	// Call HandleASG: because allocatedCount (7) >= pendingForASG+runningForASG (4),
	// there should be no UpdateAutoScalingGroup call.
	HandleASG(awsClient, asgCfg,
		totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags,
		bool(asgCfg.ScaleToZero), int64(asgCfg.MaxAsgCapacity),
		pendingJobsWithTags, runningJobsWithTags, &totalCapacity, mu)
}
func TestHandleASG_ScaleUpWhenAllAllocatedBusyAndNewPendingAppears(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockAutoScalingAPI(ctrl)

	asgName := "asg-amd64"
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{asgName}),
	}
	output := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			{
				AutoScalingGroupName: aws.String(asgName),
				DesiredCapacity:      aws.Int64(4),
				Instances: []*autoscaling.Instance{
					{InstanceId: aws.String("i-1"), LifecycleState: aws.String("InService")},
					{InstanceId: aws.String("i-2"), LifecycleState: aws.String("InService")},
					{InstanceId: aws.String("i-3"), LifecycleState: aws.String("InService")},
					{InstanceId: aws.String("i-4"), LifecycleState: aws.String("InService")},
				},
			},
		},
	}

	// Describe возвращает 4 инстанса
	mockSvc.EXPECT().DescribeAutoScalingGroups(input).Return(output, nil)

	// Ожидаем апдейт до 5
	mockSvc.EXPECT().
		UpdateAutoScalingGroup(gomock.Any()).
		Do(func(in *autoscaling.UpdateAutoScalingGroupInput) {
			if aws.StringValue(in.AutoScalingGroupName) != asgName {
				t.Fatalf("unexpected ASG: %s", aws.StringValue(in.AutoScalingGroupName))
			}
			if aws.Int64Value(in.DesiredCapacity) != int64(5) {
				t.Fatalf("expected desired=5, got %d", aws.Int64Value(in.DesiredCapacity))
			}
		}).
		Return(&autoscaling.UpdateAutoScalingGroupOutput{}, nil)

	awsClient := &AWSClient{svc: mockSvc}

	asgCfg := Asg{
		Name:           asgName,
		Tags:           []string{"amd64"},
		MaxAsgCapacity: 10,
		ScaleToZero:    true,
		Region:         "us-west-2",
	}

	// 4 running + 1 pending
	runningJobsWithTags := map[string]int{"amd64": 4}
	pendingJobsWithTags := map[string]int{"amd64": 1}

	var totalPendingJobs int64 = 1
	var totalRunningJobs int64 = 4
	var totalPendingWithoutTags int64 = 0
	var totalRunningWithoutTags int64 = 0

	var totalCapacity int64 = 0
	mu := &sync.Mutex{}

	HandleASG(awsClient, asgCfg,
		totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags,
		bool(asgCfg.ScaleToZero), int64(asgCfg.MaxAsgCapacity),
		pendingJobsWithTags, runningJobsWithTags, &totalCapacity, mu)
}
