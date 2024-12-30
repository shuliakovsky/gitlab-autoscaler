package main

import (
	"fmt"
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
			},
		},
	}

	mockSvc.EXPECT().DescribeAutoScalingGroups(input).Return(output, nil)

	awsClient := &AWSClient{svc: mockSvc}
	capacity, err := GetCurrentCapacity(awsClient, asgName)

	assert.NoError(t, err)
	assert.Equal(t, int64(3), capacity)
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
