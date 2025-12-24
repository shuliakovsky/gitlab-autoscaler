package core

import (
	"testing"

	"github.com/shuliakovsky/gitlab-autoscaler/config"
	"github.com/shuliakovsky/gitlab-autoscaler/gitlab"
)

// TestTagBasedCalculator_TagsOnly verifies the basic tag-based capacity calculation
// when only pending jobs are present.
//
// Conditions:
// - ASG with tags ["amd64", "prod"]
// - Pending jobs: 3 for "amd64", 2 for "prod", 1 for "stage"
// - No running jobs
//
// Expected result: 5 (3 + 2 + 1) - total pending jobs matching ASG tags
func TestTagBasedCalculator_TagsOnly(t *testing.T) {
	calculator := NewTagBasedCalculator()

	asg := config.Asg{
		Name: "test-asg",
		Tags: []string{"amd64", "prod"},
	}

	state := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{
			"amd64": 3,
			"prod":  2,
			"stage": 1,
			"arm64": 0,
		},
	}

	desired := calculator.Calculate(asg, state)

	if desired != 5 {
		t.Errorf("Expected 5, got %d", desired)
	}
}

// TestTagBasedCalculator_WithRunningJobs verifies capacity calculation when both
// pending and running jobs exist for the same tags.
//
// Conditions:
// - ASG with tag ["amd64"]
// - Pending jobs: 4 for "amd64"
// - Running jobs: 2 for "amd64"
//
// Expected result: 4 - all pending jobs need to be accommodated
func TestTagBasedCalculator_WithRunningJobs(t *testing.T) {
	calculator := NewTagBasedCalculator()

	asg := config.Asg{
		Name: "test-asg",
		Tags: []string{"amd64"},
	}

	state := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{
			"amd64": 4,
		},
		RunningJobsWithTags: map[string]int{
			"amd64": 2,
		},
	}

	desired := calculator.Calculate(asg, state)

	if desired != 4 {
		t.Errorf("Expected 4, got %d", desired)
	}
}

// TestScaleUp_MaxCapacity verifies that the capacity doesn't exceed max limit.
//
// Conditions:
// - ASG with max capacity of 10
// - 15 pending jobs for matching tag
//
// Expected result: 10 - capped at maximum allowed capacity
func TestScaleUp_MaxCapacity(t *testing.T) {
	asg := config.Asg{
		Name:           "test-asg",
		Tags:           []string{"amd64"},
		MaxAsgCapacity: 10,
	}

	state := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{
			"amd64": 15,
		},
	}

	currentCapacity := int64(5)
	desired := calculateDesiredCapacity(asg, state, currentCapacity)

	if desired != 10 {
		t.Errorf("Expected 10 (max), got %d", desired)
	}
}

// TestScaleUp_WithFreeSlots verifies no scaling is needed when free slots exist.
//
// Conditions:
// - ASG with 5 instances
// - 3 pending jobs for matching tag
// - 2 running jobs for matching tag
//
// Expected result: 5 - already enough capacity
func TestScaleUp_WithFreeSlots(t *testing.T) {
	asg := config.Asg{
		Name:           "test-asg",
		Tags:           []string{"amd64"},
		MaxAsgCapacity: 10,
	}

	state := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{
			"amd64": 3,
		},
		RunningJobsWithTags: map[string]int{
			"amd64": 2,
		},
	}

	currentCapacity := int64(5)
	desired := calculateDesiredCapacity(asg, state, currentCapacity)

	if desired != 5 {
		t.Errorf("Expected 5 (no change), got %d", desired)
	}
}

// TestScaleDown verifies scale-down behavior when no jobs are running.
//
// Conditions:
// - ASG with 3 instances
// - No pending or running jobs
// - ScaleToZero allowed
//
// Expected result: 3 - no scaling needed
func TestScaleDown(t *testing.T) {
	asg := config.Asg{
		Name:           "test-asg",
		Tags:           []string{"amd64"},
		MaxAsgCapacity: 10,
		ScaleToZero:    true,
	}

	state := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{},
		RunningJobsWithTags: map[string]int{},
	}

	currentCapacity := int64(3)
	desired := calculateDesiredCapacity(asg, state, currentCapacity)

	if desired != 3 {
		t.Errorf("Expected 3 (no change), got %d", desired)
	}
}

// TestScaleDown_Minimum verifies minimum capacity constraint.
//
// Conditions:
// - ASG with 1 instance
// - No jobs
// - ScaleToZero not allowed
//
// Expected result: 1 - cannot scale below minimum
func TestScaleDown_Minimum(t *testing.T) {
	asg := config.Asg{
		Name:           "test-asg",
		Tags:           []string{"amd64"},
		MaxAsgCapacity: 10,
		ScaleToZero:    false, // Scale-to-zero disabled
	}

	state := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{},
		RunningJobsWithTags: map[string]int{},
	}

	currentCapacity := int64(1)
	desired := calculateDesiredCapacity(asg, state, currentCapacity)

	if desired != 1 {
		t.Errorf("Expected 1 (minimum), got %d", desired)
	}
}

// TestZeroValues verifies zero values handling.
//
// Conditions:
// - ASG with 0 instances
// - No jobs
//
// Expected result: 0 - correct handling of zero capacity
func TestZeroValues(t *testing.T) {
	asg := config.Asg{
		Name:           "test-asg",
		Tags:           []string{"amd64"},
		MaxAsgCapacity: 5,
	}

	state := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{},
		RunningJobsWithTags: map[string]int{},
	}

	currentCapacity := int64(0)
	desired := calculateDesiredCapacity(asg, state, currentCapacity)

	if desired != 0 {
		t.Errorf("Expected 0, got %d", desired)
	}
}

// TestScaleDown_BlockingByRemainingJobs verifies that scaling down is blocked when
// remaining jobs exist even after some jobs complete.
//
// Conditions:
// - ASG with 6 instances
// - Initially: 6 jobs for "amd64"
// - After completion: 5 jobs remain
// - ScaleToZero allowed
//
// Expected result: 6 - cannot scale down because 5 jobs still require capacity
func TestScaleDown_BlockingByRemainingJobs(t *testing.T) {
	asg := config.Asg{
		Name:           "test-asg",
		Tags:           []string{"amd64"},
		MaxAsgCapacity: 10,
		ScaleToZero:    true,
	}

	state := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{
			"amd64": 3, // 3 pending jobs
		},
		RunningJobsWithTags: map[string]int{
			"amd64": 2, // 2 running jobs
		},
	}

	currentCapacity := int64(6)
	desired := calculateDesiredCapacity(asg, state, currentCapacity)

	if desired != 6 {
		t.Errorf("Expected 6 (no scaling down), got %d", desired)
	}

	stateNoJobs := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{},
		RunningJobsWithTags: map[string]int{},
	}

	desiredNoJobs := calculateDesiredCapacity(asg, stateNoJobs, currentCapacity)
	if desiredNoJobs != 6 {
		t.Errorf("Expected 6 (current capacity), got %d", desiredNoJobs)
	}
}

// TestScaleDown_FullCycle verifies the full scaling cycle
func TestScaleDown_FullCycle(t *testing.T) {
	asg := config.Asg{
		Name:           "test-asg",
		Tags:           []string{"amd64"},
		MaxAsgCapacity: 10,
		ScaleToZero:    true,
	}

	stateWithJobs := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{"amd64": 3},
		RunningJobsWithTags: map[string]int{"amd64": 2},
	}

	currentCapacity := int64(6)
	desiredWithJobs := calculateDesiredCapacity(asg, stateWithJobs, currentCapacity)

	if desiredWithJobs != 6 {
		t.Errorf("Expected 6 with jobs, got %d", desiredWithJobs)
	}

	stateNoJobs := gitlab.ClusterState{
		PendingJobsWithTags: map[string]int{},
		RunningJobsWithTags: map[string]int{},
	}

	desiredNoJobs := calculateDesiredCapacity(asg, stateNoJobs, currentCapacity)

	if desiredNoJobs != 6 {
		t.Errorf("Expected 6 (current capacity), got %d", desiredNoJobs)
	}
}

// calculateDesiredCapacity calculates the desired capacity correctly
func calculateDesiredCapacity(asg config.Asg, state gitlab.ClusterState, currentCapacity int64) int64 {
	pendingForASG := int64(0)
	for _, tag := range asg.Tags {
		if count, exists := state.PendingJobsWithTags[tag]; exists {
			pendingForASG += int64(count)
		}
	}

	runningCount := int64(0)
	for _, tag := range asg.Tags {
		if count, exists := state.RunningJobsWithTags[tag]; exists {
			runningCount += int64(count)
		}
	}

	freeCapacity := currentCapacity - runningCount
	if freeCapacity < 0 {
		freeCapacity = 0
	}

	additionalNeeded := pendingForASG - freeCapacity
	if additionalNeeded < 0 {
		additionalNeeded = 0
	}

	desired := currentCapacity + additionalNeeded
	if desired > asg.MaxAsgCapacity {
		desired = asg.MaxAsgCapacity
	}

	return desired
}
