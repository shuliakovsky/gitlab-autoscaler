package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"

	"github.com/shuliakovsky/gitlab-autoscaler/utils"
)

const minCapacity = 0

func UpdateASGCapacity(awsClient *AWSClient, asgName string, capacity int64) error {
	if capacity < minCapacity {
		return errors.New("cannot set capacity below " + fmt.Sprint(minCapacity))
	}

	input := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(asgName),
		MinSize:              aws.Int32(int32(capacity)),
		MaxSize:              aws.Int32(int32(capacity)),
		DesiredCapacity:      aws.Int32(int32(capacity)),
	}

	_, err := awsClient.svc.UpdateAutoScalingGroup(context.TODO(), input)
	if err != nil {
		return fmt.Errorf("failed to update ASG %s: %w", asgName, err)
	}

	utils.LogGreen(fmt.Sprintf("ASG %s updated with capacity: %d", asgName, capacity))
	return nil
}

func GetCurrentCapacity(awsClient *AWSClient, asgName string) (int64, int64, error) {
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{asgName},
	}

	result, err := awsClient.svc.DescribeAutoScalingGroups(context.TODO(), input)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to describe ASG %s: %w", asgName, err)
	}

	if len(result.AutoScalingGroups) == 0 {
		return 0, 0, fmt.Errorf("ASG %s not found", asgName)
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
		if inst.LifecycleState == "" { // Проверка на пустую строку
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
