package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
)

// AutoscalingAPI defines the interface for AWS Auto Scaling API operations.
type AutoscalingAPI interface {
	DescribeAutoScalingGroups(context.Context, *autoscaling.DescribeAutoScalingGroupsInput, ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
	UpdateAutoScalingGroup(context.Context, *autoscaling.UpdateAutoScalingGroupInput, ...func(*autoscaling.Options)) (*autoscaling.UpdateAutoScalingGroupOutput, error)
}
