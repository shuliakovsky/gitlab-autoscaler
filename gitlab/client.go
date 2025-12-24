package gitlab

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/shuliakovsky/gitlab-autoscaler/utils"
)

const (
	gitlabAPIBaseTemplate = "https://gitlab.com/api/v4/groups/%s/projects"
	jobsAPIBaseTemplate   = "https://gitlab.com/api/v4/projects/%d/jobs?scope=%s"
	maxRetries            = 5
)

var gitlabClient = &http.Client{
	Timeout: 25 * time.Second,
}

// ClusterState represents the current state of jobs across all projects
type ClusterState struct {
	TotalPendingJobs    int64
	TotalRunningJobs    int64
	PendingJobsWithTags map[string]int
	RunningJobsWithTags map[string]int
	Projects            []Project
	TotalCapacity       int64
}

// Project represents a GitLab project with job information
type Project struct {
	ID             int      `json:"id"`
	Name           string   `json:"name"`
	PendingTagList []string `json:"pending_tag_list"`
	RunningTagList []string `json:"running_tag_list"`
}

// FetchProjects fetches all projects in a GitLab group with proper error handling and retries
func FetchProjects(token, groupName string, excludeProjects []string) ([]Project, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(gitlabAPIBaseTemplate, groupName)+"?include_subgroups=true&per_page=100", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("PRIVATE-TOKEN", token)

	var allProjects []Project
	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, err := gitlabClient.Do(req)
		if err != nil {
			utils.LogRed(fmt.Sprintf("Error making request: %v", err))
			return nil, err
		}
		defer closeBody(resp.Body)

		if resp.StatusCode == http.StatusTooManyRequests {
			waitDuration := time.Duration(2<<attempt) * time.Second
			log.Printf("%sReceived 429 Too Many Requests. Retrying in %s...%s", utils.Yellow, waitDuration, utils.Reset)
			time.Sleep(waitDuration)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("error fetching projects: %s", resp.Status)
		}

		var projects []Project
		if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
			return nil, err
		}

		for _, project := range projects {
			if !isExcluded(project.Name, excludeProjects) {
				allProjects = append(allProjects, project)

				log.Printf("Project: %-35s (ID: %-9d)  Pending jobs: %s%-3d%s tags: %s%v%s. Running jobs: %s%-3d%s tags: %s%v%s",
					project.Name, project.ID,
					utils.Cyan, len(project.PendingTagList), utils.Reset,
					utils.Cyan, project.PendingTagList, utils.Reset,
					utils.Green, len(project.RunningTagList), utils.Reset,
					utils.Green, project.RunningTagList, utils.Reset)
			}
		}
		return allProjects, nil
	}
	return nil, fmt.Errorf("failed to fetch projects after %d attempts", maxRetries)
}

// FetchJobsCount fetches job counts for a specific scope (pending/running)
func FetchJobsCount(token string, projectID int, scope string) (int, []string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(jobsAPIBaseTemplate, projectID, scope), nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, err := gitlabClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer closeBody(resp.Body)

		if resp.StatusCode == http.StatusTooManyRequests {
			waitDuration := time.Duration(2<<attempt) * time.Second
			log.Printf("%sReceived 429 Too Many Requests. Retrying in %s...%s", utils.Yellow, waitDuration, utils.Reset)
			time.Sleep(waitDuration)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return 0, nil, fmt.Errorf("error fetching %s jobs for project ID %d: status=%s", scope, projectID, resp.Status)
		}

		var jobs []struct {
			ID   int      `json:"id"`
			Tags []string `json:"tag_list"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
			return 0, nil, err
		}

		tags := extractTags(jobs)
		return len(jobs), tags, nil
	}
	return 0, nil, fmt.Errorf("failed to fetch job counts after %d attempts", maxRetries)
}

// CalculateClusterState aggregates job information across all projects (exactly like in the old working version)
func CalculateClusterState(token string, projects []Project) ClusterState {
	pendingJobsWithTags := make(map[string]int)
	runningJobsWithTags := make(map[string]int)
	var totalPending, totalRunning int64 = 0, 0
	var totalPendingWithoutTags, totalRunningWithoutTags int

	var wg sync.WaitGroup
	results := make(chan struct {
		name        string
		id          int
		pending     int
		running     int
		pendingTags []string
		runningTags []string
		err         error
	}, len(projects))

	for _, project := range projects {
		wg.Add(1)
		go func(p Project) {
			defer wg.Done()
			pendingJobs, pendingTags, err := FetchJobsCount(token, p.ID, "pending")
			if err != nil {
				results <- struct {
					name        string
					id          int
					pending     int
					running     int
					pendingTags []string
					runningTags []string
					err         error
				}{name: p.Name, id: p.ID, pending: 0, running: 0, err: err}
				return
			}

			runningJobs, runningTags, err := FetchJobsCount(token, p.ID, "running")
			if err != nil {
				results <- struct {
					name        string
					id          int
					pending     int
					running     int
					pendingTags []string
					runningTags []string
					err         error
				}{name: p.Name, id: p.ID, pending: pendingJobs, running: 0, err: err}
				return
			}

			results <- struct {
				name        string
				id          int
				pending     int
				running     int
				pendingTags []string
				runningTags []string
				err         error
			}{
				name:        p.Name,
				id:          p.ID,
				pending:     pendingJobs,
				running:     runningJobs,
				pendingTags: pendingTags,
				runningTags: runningTags,
				err:         nil,
			}
		}(project)
	}

	wg.Wait()
	close(results)

	for r := range results {
		if r.err != nil {
			log.Printf("Error processing project: %s", r.err)
			continue
		}
		totalPending += int64(r.pending)
		totalRunning += int64(r.running)

		if len(r.pendingTags) == 0 {
			totalPendingWithoutTags += r.pending
		}
		if len(r.runningTags) == 0 {
			totalRunningWithoutTags += r.running
		}

		for _, tag := range r.pendingTags {
			pendingJobsWithTags[tag]++
		}

		for _, tag := range r.runningTags {
			runningJobsWithTags[tag]++
		}

		log.Printf("Project: %-35s (ID: %-9d)  Pending jobs: %s%-3d%s tags: %s%v%s. Running jobs: %s%-3d%s tags: %s%v%s",
			r.name, r.id,
			utils.Cyan, r.pending, utils.Reset,
			utils.Cyan, r.pendingTags, utils.Reset,
			utils.Green, r.running, utils.Reset,
			utils.Green, r.runningTags, utils.Reset)
	}

	return ClusterState{
		TotalPendingJobs:    totalPending,
		TotalRunningJobs:    totalRunning,
		PendingJobsWithTags: pendingJobsWithTags,
		RunningJobsWithTags: runningJobsWithTags,
		TotalCapacity:       totalPending + totalRunning,
	}
}

// extractTags extracts all tags from job list
func extractTags(jobs []struct {
	ID   int      `json:"id"`
	Tags []string `json:"tag_list"`
}) []string {
	var allTags []string
	for _, job := range jobs {
		allTags = append(allTags, job.Tags...)
	}
	return allTags
}

// closeBody closes HTTP response body safely
func closeBody(body io.Closer) {
	if err := body.Close(); err != nil {
		log.Printf("Error closing response body: %v", err)
	}
}

// isExcluded checks if a project should be excluded from processing
func isExcluded(projectName string, excludeProjects []string) bool {
	for _, excluded := range excludeProjects {
		if projectName == excluded {
			return true
		}
	}
	return false
}
