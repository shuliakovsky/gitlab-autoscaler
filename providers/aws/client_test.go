package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/stretchr/testify/assert"

	mocks "github.com/shuliakovsky/gitlab-autoscaler/mocks/github.com/shuliakovsky/gitlab-autoscaler/providers/aws"
)

// TestGetCurrentCapacity verifies the GetCurrentCapacity method correctly calculates active instances and desired capacity from AWS response
// Expected behavior:
//   - Returns allocatedCount = 2 (InService + Pending states)
//   - Returns desiredCapacity = 3
//   - No error returned for valid ASG configuration
func TestGetCurrentCapacity(t *testing.T) {
	mockSvc := &mocks.MockAutoscalingAPI{}

	mockSvc.On("DescribeAutoScalingGroups",
		context.TODO(),
		&autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []string{"test-asg"},
		},
	).Return(&autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []types.AutoScalingGroup{
			{
				AutoScalingGroupName: aws.String("test-asg"),
				Instances: []types.Instance{
					{LifecycleState: "InService"},
					{LifecycleState: "Pending"},
					{LifecycleState: ""},
				},
				DesiredCapacity: aws.Int32(3),
			},
		},
	}, nil)

	client := &AWSClient{
		svc: mockSvc,
	}

	allocated, desired, err := client.GetCurrentCapacity("test-asg")

	assert.NoError(t, err)
	assert.Equal(t, int64(2), allocated)
	assert.Equal(t, int64(3), desired)

	mockSvc.AssertExpectations(t)
}

// TestUpdateASGCapacity_Success verifies the UpdateASGCapacity method successfully scales ASG to a valid capacity
// Expected behavior:
//   - No error returned when updating to valid capacity (5)
//   - AWS SDK's UpdateAutoScalingGroup is called with correct parameters:
//   - MinSize=5, MaxSize=5, DesiredCapacity=5
//   - AutoScalingGroupName="test-asg"
func TestUpdateASGCapacity_Success(t *testing.T) {
	mockSvc := &mocks.MockAutoscalingAPI{}

	mockSvc.On("UpdateAutoScalingGroup",
		context.TODO(),
		&autoscaling.UpdateAutoScalingGroupInput{
			AutoScalingGroupName: aws.String("test-asg"),
			MinSize:              aws.Int32(5),
			MaxSize:              aws.Int32(5),
			DesiredCapacity:      aws.Int32(5),
		},
	).Return(&autoscaling.UpdateAutoScalingGroupOutput{}, nil)

	client := &AWSClient{
		svc: mockSvc,
	}

	err := client.UpdateASGCapacity("test-asg", 5)
	assert.NoError(t, err)

	mockSvc.AssertExpectations(t)
}

// TestUpdateASGCapacity_InvalidCapacity verifies error handling when attempting invalid capacity (negative value)
// Expected behavior:
//   - Returns an error with message containing "cannot set capacity below 0"
//   - No AWS API call is made for invalid capacity
func TestUpdateASGCapacity_InvalidCapacity(t *testing.T) {
	mockSvc := &mocks.MockAutoscalingAPI{}

	client := &AWSClient{
		svc: mockSvc,
	}

	err := client.UpdateASGCapacity("test-asg", -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot set capacity below 0")

	mockSvc.AssertExpectations(t)
}
