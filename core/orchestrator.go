package core

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/shuliakovsky/gitlab-autoscaler/config"
	"github.com/shuliakovsky/gitlab-autoscaler/gitlab"
	"github.com/shuliakovsky/gitlab-autoscaler/utils"
)

// Orchestrator manages the scaling of auto-scaling groups based on job demand
type Orchestrator struct {
	providers     map[string]Provider
	asgToProvider map[string]string // Maps ASG name to provider name (aws, azure, etc.)
}

// NewOrchestrator creates a new orchestrator with providers and ASG-to-provider mapping
func NewOrchestrator(providers map[string]Provider, asgToProvider map[string]string) *Orchestrator {
	return &Orchestrator{
		providers:     providers,
		asgToProvider: asgToProvider,
	}
}

// ScaleASGs scales all auto-scaling groups according to current job demand
func (o *Orchestrator) ScaleASGs(cfg config.Config, state gitlab.ClusterState) {
	var wg sync.WaitGroup
	mu := &sync.Mutex{}
	totalCapacity := int64(0)

	// Получаем все ASG из всех провайдеров
	allAsgs := []config.Asg{}
	for _, providerConfig := range cfg.Providers {
		allAsgs = append(allAsgs, providerConfig.AsgNames...)
	}

	for _, asg := range allAsgs {
		wg.Add(1)
		go func(asg config.Asg) {
			defer wg.Done()
			o.scaleASG(asg, state, mu, &totalCapacity)
		}(asg)
	}
	wg.Wait()
}

// scaleASG scales a single auto-scaling group based on job demand
func (o *Orchestrator) scaleASG(asg config.Asg, state gitlab.ClusterState, mu *sync.Mutex, totalCapacity *int64) {
	// Determine provider by ASG name - not region!
	providerName := o.asgToProvider[asg.Name]
	if providerName == "" {
		providerName = "aws" // Default to AWS if not specified
	}

	provider, ok := o.providers[providerName]
	if !ok {
		log.Println(utils.Red, "Error: No provider found for ASG", asg.Name, utils.Reset)
		return
	}

	allocatedCount, desiredCapacity, err := provider.GetCurrentCapacity(asg.Name)
	if err != nil {
		log.Println(utils.Red, "Error:", err, utils.Reset)
		return
	}

	mu.Lock()
	*totalCapacity += allocatedCount
	mu.Unlock()

	log.Printf("Processing ASG: %s%s%s, Desired: %s%d%s, Allocated: %s%d%s, Tags:  %s%v%s",
		utils.LightGray, asg.Name, utils.Reset,
		utils.Green, desiredCapacity, utils.Reset,
		utils.Cyan, allocatedCount, utils.Reset,
		utils.Green, asg.Tags, utils.Reset)

	totalJobs := state.TotalPendingJobs + state.TotalRunningJobs

	pendingJobMatchingTags := false
	for _, tag := range asg.Tags {
		if count, exists := state.PendingJobsWithTags[tag]; exists && count > 0 {
			pendingJobMatchingTags = true
			break
		}
	}

	runningJobMatchingTags := false
	for _, tag := range asg.Tags {
		if count, exists := state.RunningJobsWithTags[tag]; exists && count > 0 {
			runningJobMatchingTags = true
			break
		}
	}

	if totalJobs > 0 && pendingJobMatchingTags {
		var pendingForASG int64
		for _, tag := range asg.Tags {
			pendingForASG += int64(state.PendingJobsWithTags[tag])
		}

		freeCapacity := allocatedCount - state.TotalRunningJobs
		if freeCapacity < 0 {
			freeCapacity = 0
		}

		additionalNeeded := pendingForASG - freeCapacity
		if additionalNeeded > 0 {
			proposed := desiredCapacity + additionalNeeded

			if proposed > asg.MaxAsgCapacity {
				proposed = asg.MaxAsgCapacity
			}

			if allocatedCount < proposed {
				err := provider.UpdateASGCapacity(asg.Name, proposed)
				if err != nil {
					log.Println(utils.Red, "Scale-up failed:", err, utils.Reset)
				} else {
					log.Printf("  → %sScaling up%s ASG: %s%s%s, Old desired: %d, New desired: %d",
						utils.Green, utils.Reset,
						utils.LightGray, asg.Name, utils.Reset,
						desiredCapacity, proposed)
				}
			}
		}
	}

	if !pendingJobMatchingTags && !runningJobMatchingTags {
		newCapacity := allocatedCount - 1
		minAllowed := int64(0)
		if !asg.ScaleToZero {
			minAllowed = 1
		}

		if newCapacity >= minAllowed {
			err := provider.UpdateASGCapacity(asg.Name, newCapacity)
			if err != nil {
				log.Println(utils.Red, "Scale-down failed:", err, utils.Reset)
			} else {
				log.Printf("  → %sScaling down%s ASG: %s%s%s, New capacity: %d",
					utils.Magenta, utils.Reset,
					utils.LightGray, asg.Name, utils.Reset,
					newCapacity)
			}
		}
	}
}

// Run starts the autoscaling process
func Run(cfg *config.Config, orchestrator *Orchestrator) {
	PrintSeparator()

	projects, err := gitlab.FetchProjects(cfg.GitLab.Token, cfg.GitLab.Group, cfg.GitLab.ExcludeProjects)
	if err != nil {
		log.Printf("%sError fetching projects: %s%s", utils.Red, err, utils.Reset)
		return
	}

	state := gitlab.CalculateClusterState(cfg.GitLab.Token, projects)
	orchestrator.ScaleASGs(*cfg, state)

	log.Printf("Total active capacity: %s%-4d%s", utils.Green, state.TotalCapacity, utils.Reset)

	PrintSeparator()
}

// PrintSeparator prints a visual separator in logs
func PrintSeparator() {
	border := "═"
	lineLength := 160
	separator := fmt.Sprintf("%s\n", strings.Repeat(string(border), lineLength))
	log.Print(separator)
}
