package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

const (
	minCapacity = 0 // Constant for ASG min size
)

func NewAWSClient(sess *session.Session) *AWSClient {
	return &AWSClient{
		svc: autoscaling.New(sess),
	}
}
func (c *AWSClient) DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return c.svc.DescribeAutoScalingGroups(input)
}

func NewAWSClients() *AWSClients {
	return &AWSClients{
		clients: make(map[string]AWSService),
	}
}
func (a *AWSClients) Get(region string) AWSService {
	if client, exists := a.clients[region]; exists {
		return client
	}
	client := InitializeAWS(region)
	a.clients[region] = client
	return client
}

func InitializeAWS(region string) AWSService {
	sess, _ := session.NewSession(&aws.Config{Region: aws.String(region)})
	return NewAWSClient(sess)
}

func GetCurrentCapacity(awsService *AWSClient, asgName string) (int64, error) {

	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{asgName}),
	}
	result, err := awsService.svc.DescribeAutoScalingGroups(input)
	if err != nil {
		return 0, fmt.Errorf("failed to describe ASG: %s%s%s. Error: %s%w%s", Red, asgName, Reset, Red, err, Reset)
	}
	if len(result.AutoScalingGroups) == 0 {
		return 0, fmt.Errorf("ASG: %s%s%s not found", Red, asgName, Reset)
	}
	return *result.AutoScalingGroups[0].DesiredCapacity, nil
}

func UpdateASGCapacity(awsService *AWSClient, asg AutoScalingGroup, capacity int64, maxCapacity int64) error {
	if capacity > maxCapacity {
		return fmt.Errorf("cannot update ASG %s%s%s: desired capacity  %s%d%s exceeds max capacity  %s%d%s",
			LightGray, asg.Name, Reset, Green, capacity, Reset, Green, maxCapacity, Reset)
	}

	input := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(asg.Name),
		MinSize:              aws.Int64(capacity),
		MaxSize:              aws.Int64(capacity),
		DesiredCapacity:      aws.Int64(capacity),
	}
	_, err := awsService.svc.UpdateAutoScalingGroup(input)
	if err != nil {
		return fmt.Errorf(" %sfailed to update ASG %s: %w%s", Red, asg.Name, err, Reset)
	}
	log.Printf("ASG %s updated with capacity: %s%d%s", asg.Name, Green, capacity, Reset)
	return nil
}

func HandleASG(awsService *AWSClient, asg Asg, totalPendingJobs, totalRunningJobs, totalPendingWithoutTags,
	totalRunningWithoutTags int64, allowScalingDownToZero bool, maxCapacity int64,
	pendingJobsWithTags map[string]int, runningJobsWithTags map[string]int, totalCapacity *int64, mu *sync.Mutex) {
	currentCapacity, err := GetCurrentCapacity(awsService, asg.Name)
	if err != nil {
		log.Println(err)
		return
	}

	mu.Lock()
	*totalCapacity += currentCapacity
	defer mu.Unlock()

	totalJobs := totalPendingJobs + totalRunningJobs
	log.Printf("Processing ASG: %s%s%s, Current capacity: %s%d%s, Tags:  %s%v%s",
		LightGray, asg.Name, Reset, Green, currentCapacity, Reset, Green, asg.Tags, Reset)

	pendingJobMatchingTags := false // pending job with matching tags flag - if true we can't scale down. must check possibility scale up ASG to serve this job
	pendingJobWithNoTags := false   // pending job with no any tags   flag - if true we can't scale down. must check possibility scale up ASG to serve this job
	runningJobMatchingTags := false // running job with matching tags flag - if true we can't scale down.
	runningJobWithNoTags := false   // running job with no any tags   flag - if true we can't scale down.

	// Check pending jobs for pendingJobMatchingTags
	if totalPendingJobs > 0 { // Only check if there are pending jobs
		for _, tag := range asg.Tags {
			if count, exists := pendingJobsWithTags[tag]; exists && count > 0 {
				pendingJobMatchingTags = true // if tags matching switch to true
				log.Printf("Found pending job with matching tag: %s, Check if needs to Scaling UP to serve it", tag)
				break
			}
		}
	}
	// Check running jobs for runningJobMatchingTags
	if totalRunningJobs > 0 { // Only check if there are pending jobs
		for _, tag := range asg.Tags {
			if count, exists := runningJobsWithTags[tag]; exists && count > 0 {
				runningJobMatchingTags = true
				log.Printf("Found runnning job with matching tag: %s, Skip Scaling Down", tag)
				break
			}
		}
	}
	// Check pending jobs for pendingJobWithNoTags
	if totalPendingWithoutTags > 0 {
		pendingJobWithNoTags = true // If there are pending jobs without any tag switch to true
		log.Printf("Found pending jobs without tag: %d,  Check if needs to Scaling UP to serve it ", totalPendingWithoutTags)
	}

	// Check pending jobs for runningJobWithNoTags
	if totalRunningWithoutTags > 0 {
		runningJobWithNoTags = true // If there are pending jobs without any tag switch to true
		log.Printf("Found running jobs without tag: %d, Skip Scaling Down ", totalRunningWithoutTags)
	}

	if totalJobs >= 0 {
		// in case of matching tags and current capacity is not enough -> try scaling up
		if pendingJobMatchingTags && currentCapacity < maxCapacity {
			newCapacity := currentCapacity + 1
			if newCapacity > maxCapacity {
				newCapacity = maxCapacity
			}
			if err := UpdateASGCapacity(awsService, AutoScalingGroup{Name: asg.Name}, newCapacity, maxCapacity); err != nil {
				log.Println(err)
			} else {
				log.Printf("Scaling up ASG: %s%s%s, New capacity:  %s%d %s, Reason:  %spending job with matching tags detected %s",
					LightGray, asg.Name, Reset, Green, newCapacity, Reset, Cyan, Reset)
			}
		}

		// in case of pending jobs and current capacity in not enough try scaling up
		if pendingJobWithNoTags && totalJobs > *totalCapacity {
			newCapacity := currentCapacity + 1
			if newCapacity > maxCapacity {
				newCapacity = maxCapacity
			}
			if err := UpdateASGCapacity(awsService, AutoScalingGroup{Name: asg.Name}, newCapacity, maxCapacity); err != nil {
				log.Println(err)
			} else {
				log.Printf("Scaling up ASG %s%s%s, New capacity:  %s%d%s, Reason:  %spending job with no tags detected %s",
					LightGray, asg.Name, Reset, Green, newCapacity, Reset, Cyan, Reset)
			}
		}
	}
	// in case if no serving task try scaling down
	if !pendingJobMatchingTags && !pendingJobWithNoTags && !runningJobMatchingTags && !runningJobWithNoTags {
		newCapacity := currentCapacity - 1
		if newCapacity > minCapacity || (newCapacity == minCapacity && allowScalingDownToZero) {
			if allowScalingDownToZero {
				log.Printf("Scaling down ASG: %s by %s1%s, Reason: there is no jobs to serve",
					asg.Name, Magenta, Reset)
				if err := UpdateASGCapacity(awsService, AutoScalingGroup{Name: asg.Name}, newCapacity, maxCapacity); err != nil {
					log.Println(err)
				} else {
					log.Printf("Scaled down ASG %s%s%s, Current capacity:  %s%d %s",
						LightGray, asg.Name, Reset, Green, newCapacity, Reset)
				}
			}
		}
	}
}

func ScaleAutoScalingGroups(awsClients *AWSClients, asgConfigs []Asg, totalPendingJobs, totalRunningJobs, totalPendingWithoutTags,
	totalRunningWithoutTags int64, pendingJobsWithTags map[string]int, runningJobsWithTags map[string]int, totalCapacity *int64) {

	var wg sync.WaitGroup
	mu := &sync.Mutex{}

	for _, asg := range asgConfigs {
		if len(asg.Tags) == 0 {
			log.Printf("%sASG %s has no tags%s", Red, asg.Name, Reset)
			continue
		}

		wg.Add(1)
		go func(asg Asg) {
			defer wg.Done()

			awsService := awsClients.Get(asg.Region)

			// Perform type assertion here
			if client, ok := awsService.(*AWSClient); ok {
				HandleASG(client, asg, totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags, bool(asg.ScaleToZero), int64(asg.MaxAsgCapacity), pendingJobsWithTags, runningJobsWithTags, totalCapacity, mu)
			} else {
				log.Printf("Error: awsService is not of type *AWSClient")
			}
		}(asg)
	}
	wg.Wait()
}
