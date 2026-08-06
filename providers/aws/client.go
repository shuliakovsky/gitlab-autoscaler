package aws

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"

	"github.com/shuliakovsky/gitlab-autoscaler/core"
)

const minCapacity = 0

func NewAWSClient(region string) (core.Provider, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
	)
	if err != nil {
		return nil, errors.New("failed to load AWS configuration: " + err.Error())
	}

	svc := autoscaling.NewFromConfig(cfg)

	return &AWSClient{
		svc:            svc,
		fallbackRegion: region,
	}, nil
}

// resolveRegion applies the last-resort fallback chain for cases where the
// caller didn't resolve a region (config priority is normally already
// resolved upstream by config.Asg.EffectiveRegion / the orchestrator).
// Priority here: explicit region argument > AWS_REGION > AWS_DEFAULT_REGION >
// region the client was constructed with.
func (c *AWSClient) resolveRegion(region string) (string, error) {
	if region != "" {
		return region, nil
	}
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r, nil
	}
	if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
		return r, nil
	}
	if c.fallbackRegion != "" {
		return c.fallbackRegion, nil
	}
	return "", errors.New("AWS region is not specified: set it for this ASG in config, " +
		"for the provider in config, or via AWS_REGION/AWS_DEFAULT_REGION")
}

func withRegion(region string) func(*autoscaling.Options) {
	return func(o *autoscaling.Options) {
		o.Region = region
	}
}

func (c *AWSClient) GetCurrentCapacity(asgName, region string) (int64, int64, error) {
	resolvedRegion, err := c.resolveRegion(region)
	if err != nil {
		return 0, 0, fmt.Errorf("ASG %s: %w", asgName, err)
	}

	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{asgName},
	}

	result, err := c.svc.DescribeAutoScalingGroups(context.TODO(), input, withRegion(resolvedRegion))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to describe ASG %s in region %s: %w", asgName, resolvedRegion, err)
	}

	if len(result.AutoScalingGroups) == 0 {
		return 0, 0, fmt.Errorf("ASG %s not found in region %s", asgName, resolvedRegion)
	}

	asg := result.AutoScalingGroups[0]
	var allocatedCount int64 = 0

	allocatedStates := map[string]bool{
		"InService":       true,
		"Pending":         true,
		"Pending:Wait":    true,
		"Pending:Proceed": true,
	}

	for _, inst := range asg.Instances {
		if inst.LifecycleState == "" {
			continue
		}
		state := string(inst.LifecycleState)
		if allocatedStates[state] {
			allocatedCount++
		}
	}

	desiredCapacity := int64(0)
	if asg.DesiredCapacity != nil && *asg.DesiredCapacity != 0 {
		desiredCapacity = int64(*asg.DesiredCapacity)
	}

	return allocatedCount, desiredCapacity, nil
}

func (c *AWSClient) UpdateASGCapacity(asgName string, capacity int64, region string) error {
	if capacity < minCapacity {
		return errors.New("cannot set capacity below " + fmt.Sprint(minCapacity))
	}

	resolvedRegion, err := c.resolveRegion(region)
	if err != nil {
		return fmt.Errorf("ASG %s: %w", asgName, err)
	}

	input := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(asgName),
		MinSize:              aws.Int32(int32(capacity)),
		MaxSize:              aws.Int32(int32(capacity)),
		DesiredCapacity:      aws.Int32(int32(capacity)),
	}

	_, err = c.svc.UpdateAutoScalingGroup(context.TODO(), input, withRegion(resolvedRegion))
	if err != nil {
		return fmt.Errorf("failed to update ASG %s in region %s: %w", asgName, resolvedRegion, err)
	}

	return nil
}
