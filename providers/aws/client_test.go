package aws

import (
	"context"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

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
		mock.Anything, // withRegion(...) functional option
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
		svc:            mockSvc,
		fallbackRegion: "us-east-1",
	}

	allocated, desired, err := client.GetCurrentCapacity("test-asg", "eu-west-1")

	assert.NoError(t, err)
	assert.Equal(t, int64(2), allocated)
	assert.Equal(t, int64(3), desired)

	mockSvc.AssertExpectations(t)
}

// TestGetCurrentCapacity_UsesExplicitRegionOverClientDefault verifies that the explicit region argument
// overrides both AWS_REGION env var and the client's fallbackRegion.
func TestGetCurrentCapacity_UsesExplicitRegionOverClientDefault(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-2")

	mockSvc := &mocks.MockAutoscalingAPI{}

	// Capture the functional option to verify withRegion was called with eu-west-1 (explicit), not eu-west-2 (env).
	var capturedOption func(*autoscaling.Options)
	mockSvc.On("DescribeAutoScalingGroups",
		context.TODO(),
		&autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []string{"test-asg"},
		},
		mock.Anything,
	).Return(&autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []types.AutoScalingGroup{
			{
				AutoScalingGroupName: aws.String("test-asg"),
				Instances:            []types.Instance{},
				DesiredCapacity:      aws.Int32(1),
			},
		},
	}, nil).Run(func(args mock.Arguments) {
		// Extract the functional option (3rd argument, variadic)
		if len(args) >= 3 {
			for _, opt := range args[2:3] {
				capturedOption = opt.(func(*autoscaling.Options))
			}
		}
	})

	client := &AWSClient{
		svc:            mockSvc,
		fallbackRegion: "us-east-1",
	}

	allocated, desired, err := client.GetCurrentCapacity("test-asg", "eu-west-1")

	assert.NoError(t, err)
	assert.Equal(t, int64(0), allocated)
	assert.Equal(t, int64(1), desired)

	// Verify the functional option applied eu-west-1 (explicit region), not eu-west-2 (env).
	assert.NotNil(t, capturedOption)
	o := &autoscaling.Options{}
	capturedOption(o)
	assert.Equal(t, "eu-west-1", o.Region)

	mockSvc.AssertExpectations(t)
}

// TestUpdateASGCapacity_FailsFastWithoutRegion verifies that when region is not resolved
// (explicit empty, env vars unset, fallbackRegion empty), the method returns an error
// before making any AWS API call.
func TestUpdateASGCapacity_FailsFastWithoutRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	mockSvc := &mocks.MockAutoscalingAPI{}

	client := &AWSClient{
		svc:            mockSvc,
		fallbackRegion: "", // intentionally empty — no region anywhere
	}

	err := client.UpdateASGCapacity("test-asg", 5, "")
	assert.Error(t, err)
	mockSvc.AssertNotCalled(t, "UpdateAutoScalingGroup")
}

// TestResolveRegion_Priority tests the full resolveRegion priority chain.
func TestResolveRegion_Priority(t *testing.T) {
	cases := []struct {
		name        string
		region      string // explicit argument to resolveRegion
		awsRegion   string // AWS_REGION env var
		aawsDefault string // AWS_DEFAULT_REGION env var
		fallback    string // client.fallbackRegion
		want        string
		wantErr     bool
	}{
		{
			name:        "explicit region wins",
			region:      "eu-west-1",
			awsRegion:   "eu-west-2",
			aawsDefault: "ap-southeast-1",
			fallback:    "us-east-1",
			want:        "eu-west-1",
			wantErr:     false,
		},
		{
			name:        "AWS_REGION fallback when explicit is empty",
			region:      "",
			awsRegion:   "eu-west-2",
			aawsDefault: "ap-southeast-1",
			fallback:    "us-east-1",
			want:        "eu-west-2",
			wantErr:     false,
		},
		{
			name:        "AWS_DEFAULT_REGION fallback when AWS_REGION is empty",
			region:      "",
			awsRegion:   "",
			aawsDefault: "ap-southeast-1",
			fallback:    "us-east-1",
			want:        "ap-southeast-1",
			wantErr:     false,
		},
		{
			name:        "client fallbackRegion when all else empty",
			region:      "",
			awsRegion:   "",
			aawsDefault: "",
			fallback:    "us-east-1",
			want:        "us-east-1",
			wantErr:     false,
		},
		{
			name:        "error when nothing is set",
			region:      "",
			awsRegion:   "",
			aawsDefault: "",
			fallback:    "",
			want:        "",
			wantErr:     true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Set env vars for this sub-test.
			if c.awsRegion != "" {
				os.Setenv("AWS_REGION", c.awsRegion)
			} else {
				t.Setenv("AWS_REGION", "")
			}
			if c.aawsDefault != "" {
				os.Setenv("AWS_DEFAULT_REGION", c.aawsDefault)
			} else {
				t.Setenv("AWS_DEFAULT_REGION", "")
			}

			client := &AWSClient{
				fallbackRegion: c.fallback,
			}

			got, err := client.resolveRegion(c.region)

			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, c.want, got)
			}
		})
	}
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
		mock.Anything, // withRegion(...) functional option
	).Return(&autoscaling.UpdateAutoScalingGroupOutput{}, nil)

	client := &AWSClient{
		svc:            mockSvc,
		fallbackRegion: "us-east-1",
	}

	err := client.UpdateASGCapacity("test-asg", 5, "eu-west-1")
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

	err := client.UpdateASGCapacity("test-asg", -1, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot set capacity below 0")

	mockSvc.AssertExpectations(t)
}
