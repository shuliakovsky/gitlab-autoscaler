package core

import (
	"github.com/shuliakovsky/gitlab-autoscaler/config"
	"github.com/shuliakovsky/gitlab-autoscaler/gitlab"
)

// CapacityCalculator defines the interface for capacity calculation strategies
type CapacityCalculator interface {
	Calculate(asg config.Asg, state gitlab.ClusterState) int64
}

// TagBasedCalculator calculates capacity based on job tags
type TagBasedCalculator struct{}

// NewTagBasedCalculator creates a new tag-based calculator
func NewTagBasedCalculator() *TagBasedCalculator {
	return &TagBasedCalculator{}
}

// Calculate computes the required capacity for an ASG based on pending jobs and tags
func (c *TagBasedCalculator) Calculate(asg config.Asg, state gitlab.ClusterState) int64 {
	var pendingCount int64 = 0
	for _, tag := range asg.Tags {
		pendingCount += int64(state.PendingJobsWithTags[tag])
	}

	return pendingCount
}
