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
	// fast path: read lock
	a.mu.RLock()
	client, exists := a.clients[region]
	a.mu.RUnlock()
	if exists {
		return client
	}
	// slow path: write lock + double-check
	a.mu.Lock()
	defer a.mu.Unlock()
	if client, exists := a.clients[region]; exists {
		return client
	}
	client = InitializeAWS(region)
	a.clients[region] = client
	return client
}

func InitializeAWS(region string) AWSService {
	sess, _ := session.NewSession(&aws.Config{Region: aws.String(region)})
	return NewAWSClient(sess)
}

// GetCurrentCapacity возвращает allocatedCount (InService + Pending*) и desiredCapacity
func GetCurrentCapacity(awsService *AWSClient, asgName string) (int64, int64, error) {
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{asgName}),
	}
	result, err := awsService.svc.DescribeAutoScalingGroups(input)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to describe ASG: %s%s%s. Error: %s%w%s", Red, asgName, Reset, Red, err, Reset)
	}
	if len(result.AutoScalingGroups) == 0 {
		return 0, 0, fmt.Errorf("ASG: %s%s%s not found", Red, asgName, Reset)
	}

	asg := result.AutoScalingGroups[0]

	allocatedStates := map[string]bool{
		"InService":       true,
		"Pending":         true,
		"Pending:Wait":    true,
		"Pending:Proceed": true,
	}

	var allocatedCount int64 = 0
	for _, inst := range asg.Instances {
		if inst == nil || inst.LifecycleState == nil {
			continue
		}
		if allocatedStates[aws.StringValue(inst.LifecycleState)] {
			allocatedCount++
		}
	}

	var desired int64 = 0
	if asg.DesiredCapacity != nil {
		desired = aws.Int64Value(asg.DesiredCapacity)
	}

	return allocatedCount, desired, nil
}

func UpdateASGCapacity(awsService *AWSClient, asg AutoScalingGroup, capacity int64, maxCapacity int64) error {
	if capacity > maxCapacity {
		return fmt.Errorf("cannot update ASG %s%s%s: desired capacity  %s%d%s exceeds max capacity  %s%d%s",
			LightGray, asg.Name, Reset, Green, capacity, Reset, Green, maxCapacity, Reset)
	}
	if capacity < minCapacity {
		return fmt.Errorf("cannot update ASG %s%s%s: desired capacity  %s%d%s exceeds min capacity  %s%d%s",
			LightGray, asg.Name, Reset, Green, capacity, Reset, Green, minCapacity, Reset)
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

	allocatedCount, desiredCapacity, err := GetCurrentCapacity(awsService, asg.Name)
	if err != nil {
		log.Println(err)
		return
	}

	// Добавляем allocatedCount к общей capacity (allocated учитывает Pending/InService)
	mu.Lock()
	*totalCapacity += allocatedCount
	mu.Unlock()

	log.Printf("Processing ASG: %s%s%s, Desired: %s%d%s, Allocated: %s%d%s, Tags:  %s%v%s",
		LightGray, asg.Name, Reset, Green, desiredCapacity, Reset, Cyan, allocatedCount, Reset, Green, asg.Tags, Reset)

	totalJobs := totalPendingJobs + totalRunningJobs

	pendingJobMatchingTags := false
	pendingJobWithNoTags := false
	runningJobMatchingTags := false
	runningJobWithNoTags := false

	// Check pending jobs for pendingJobMatchingTags
	if totalPendingJobs > 0 {
		for _, tag := range asg.Tags {
			if count, exists := pendingJobsWithTags[tag]; exists && count > 0 {
				pendingJobMatchingTags = true
				log.Printf("Found pending job with matching tag: %s, Check if needs to Scaling UP to serve it", tag)
				break
			}
		}
	}

	// Check running jobs for runningJobMatchingTags
	if totalRunningJobs > 0 {
		for _, tag := range asg.Tags {
			if count, exists := runningJobsWithTags[tag]; exists && count > 0 {
				runningJobMatchingTags = true
				log.Printf("Found runnning job with matching tag: %s, Skip Scaling Down", tag)
				break
			}
		}
	}

	// Pending without tags
	if totalPendingWithoutTags > 0 {
		pendingJobWithNoTags = true
		log.Printf("Found pending jobs without tag: %d,  Check if needs to Scaling UP to serve it ", totalPendingWithoutTags)
	}

	// Running without tags
	if totalRunningWithoutTags > 0 {
		runningJobWithNoTags = true
		log.Printf("Found running jobs without tag: %d, Skip Scaling Down ", totalRunningWithoutTags)
	}

	// Если есть задачи — рассматриваем scale-up варианты
	if totalJobs > 0 {
		// Scale-up for matching tags
		if pendingJobMatchingTags && allocatedCount < maxCapacity {
			var pendingForASG, runningForASG int64
			for _, tag := range asg.Tags {
				if c, ok := pendingJobsWithTags[tag]; ok {
					pendingForASG += int64(c)
				}
				if c, ok := runningJobsWithTags[tag]; ok {
					runningForASG += int64(c)
				}
			}

			// свободные слоты = allocatedCount - runningForASG
			freeCapacity := allocatedCount - runningForASG
			if freeCapacity < 0 {
				freeCapacity = 0
			}

			var additionalNeeded int64
			if pendingForASG > freeCapacity {
				additionalNeeded = pendingForASG - freeCapacity
			}

			if additionalNeeded > 0 {
				proposed := desiredCapacity + additionalNeeded
				if proposed > maxCapacity {
					proposed = maxCapacity
				}
				if allocatedCount >= proposed {
					log.Printf("ASG %s: allocated (%d) >= proposed desired (%d), skipping", asg.Name, allocatedCount, proposed)
				} else {
					if err := UpdateASGCapacity(awsService, AutoScalingGroup{Name: asg.Name}, proposed, maxCapacity); err != nil {
						log.Println(err)
					} else {
						log.Printf("Scaling up ASG: %s%s%s, Old desired: %s%d%s, New desired:  %s%d %s, Reason: pending=%d running=%d allocated=%d",
							LightGray, asg.Name, Reset, Green, desiredCapacity, Reset, Green, proposed, Reset, pendingForASG, runningForASG, allocatedCount)
					}
				}
			} else {
				log.Printf("ASG %s: pendingForASG=%d freeCapacity=%d => nothing to increase", asg.Name, pendingForASG, freeCapacity)
			}
		}
	}

	// Scale-down when нет задач
	if !pendingJobMatchingTags && !pendingJobWithNoTags && !runningJobMatchingTags && !runningJobWithNoTags {
		// уменьшаем относительно allocatedCount (видимых инстансов)
		newCapacity := allocatedCount - 1
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
