package gitlab

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	TotalCapacity       *int64
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

// CalculateClusterState aggregates job information across all projects
func CalculateClusterState(projects []Project) ClusterState {
	state := ClusterState{
		PendingJobsWithTags: make(map[string]int),
		RunningJobsWithTags: make(map[string]int),
		TotalCapacity:       new(int64),
	}

	for _, project := range projects {
		state.TotalPendingJobs += int64(len(project.PendingTagList))
		state.TotalRunningJobs += int64(len(project.RunningTagList))

		for _, tag := range project.PendingTagList {
			state.PendingJobsWithTags[tag]++
		}

		for _, tag := range project.RunningTagList {
			state.RunningJobsWithTags[tag]++
		}
	}

	return state
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
