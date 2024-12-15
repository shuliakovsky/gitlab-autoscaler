package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	gitlabAPIBaseTemplate = "https://gitlab.com/api/v4/groups/%s/projects"
	jobsAPIBaseTemplate   = "https://gitlab.com/api/v4/projects/%d/jobs?scope=%s"
	maxRetries            = 5
)

func FetchProjects(token, groupName string, excludeProjects []string) ([]Project, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(gitlabAPIBaseTemplate, groupName)+"?include_subgroups=true&per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	var allProjects []Project
	for attempt := 0; attempt < maxRetries; attempt++ {

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {

			}
		}(resp.Body)

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%serror fetching projects: %s%s", Red, resp.Status, Reset)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			waitDuration := time.Duration(2<<attempt) * time.Second // Exponential backoff
			log.Printf("Received 429 Too Many Requests. Retrying in %s...", waitDuration)
			time.Sleep(waitDuration) // Wait before retrying
			continue
		}

		var projects []Project

		if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
			return nil, err
		}
		for _, project := range projects {
			if !isExcluded(project.Name, excludeProjects) {
				allProjects = append(allProjects, project)
			}
		}
		return allProjects, nil

		// Handle specific error codes
	}
	return nil, fmt.Errorf("failed to fetch job counts after %d attempts", maxRetries)
}

func CountPendingJobsWithTags(token string, projects []Project) (map[string]int, map[string]int) {
	pendingJobsWithTags := make(map[string]int)
	runningJobsWithTags := make(map[string]int)

	var wg sync.WaitGroup
	results := make(chan struct {
		tags    []string
		pending int
		running int
		err     error
	}, len(projects))

	for _, project := range projects {
		wg.Add(1)
		go func(project Project) {
			defer wg.Done()
			pendingJobs, pendingTags, err := FetchJobsCount(token, project.ID, "pending")
			if err != nil {
				results <- struct {
					tags    []string
					pending int
					running int
					err     error
				}{nil, 0, 0, err}
				return
			}
			runningJobs, runningTags, err := FetchJobsCount(token, project.ID, "running")
			if err != nil {
				results <- struct {
					tags    []string
					pending int
					running int
					err     error
				}{nil, pendingJobs, 0, err}
				return
			}

			for _, tag := range pendingTags {
				pendingJobsWithTags[tag] += pendingJobs
			}
			for _, tag := range runningTags {
				runningJobsWithTags[tag] += runningJobs
			}
			results <- struct {
				tags    []string
				pending int
				running int
				err     error
			}{pendingTags, pendingJobs, runningJobs, nil}
		}(project)
	}
	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			log.Printf("Error: %s", result.err)
		}
	}
	return pendingJobsWithTags, runningJobsWithTags
}

func FetchJobCounts(token string, projects []Project) (int, int, int, int, error) {
	var totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags int
	var wg sync.WaitGroup

	results := make(chan struct {
		pending     int
		running     int
		pendingTags []string
		runningTags []string
		err         error
	}, len(projects))

	for _, project := range projects {
		wg.Add(1)
		go func(project Project) {
			defer wg.Done()

			pendingJobs, pendingTags, err := FetchJobsCount(token, project.ID, "pending")
			if err != nil {
				results <- struct {
					pending     int
					running     int
					pendingTags []string
					runningTags []string
					err         error
				}{0, 0, nil, nil, err}
				return
			}

			runningJobs, runningTags, err := FetchJobsCount(token, project.ID, "running")
			if err != nil {
				results <- struct {
					pending     int
					running     int
					pendingTags []string
					runningTags []string
					err         error
				}{0, 0, pendingTags, nil, err}
				return
			}

			log.Printf("Project: %-35s (ID: %-9d)  Pending jobs: %s%-3d%s tags: %s%v%s. Running jobs: %s%-3d%s tags: %s%v%s",
				project.Name, project.ID, Cyan, pendingJobs, Reset, Cyan, pendingTags, Reset, Green, runningJobs, Reset, Green, runningTags, Reset)

			results <- struct {
				pending     int
				running     int
				pendingTags []string
				runningTags []string
				err         error
			}{pendingJobs, runningJobs, pendingTags, runningTags, nil}
		}(project)
	}
	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			log.Printf("Error: %s", result.err)
			continue
		}
		totalPendingJobs += result.pending
		totalRunningJobs += result.running

		if len(result.pendingTags) == 0 {
			totalPendingWithoutTags += result.pending
		}
		if len(result.runningTags) == 0 {
			totalRunningWithoutTags += result.running
		}
	}

	return totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags, nil
}

func FetchJobsCount(token string, projectID int, scope string) (int, []string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(jobsAPIBaseTemplate, projectID, scope), nil)
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("PRIVATE-TOKEN", token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {

			}
		}(resp.Body)

		if resp.StatusCode != http.StatusOK {
			return 0, nil, fmt.Errorf("%serror fetching %s jobs for project ID %d: name: %s%s", Red, scope, projectID, resp.Status, Reset)
		}
		// Handle specific rate-limit
		if resp.StatusCode == http.StatusTooManyRequests {
			waitDuration := time.Duration(2<<attempt) * time.Second // Exponential backoff
			log.Printf("Received 429 Too Many Requests. Retrying in %s...", waitDuration)
			time.Sleep(waitDuration) // Wait before retrying
			continue
		}

		var jobs []struct {
			ID   int      `json:"id"`
			Tags []string `json:"tag_list"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
			return 0, nil, err
		}
		return len(jobs), extractTags(jobs), nil
	}
	return 0, nil, fmt.Errorf("%sfailed to fetch job counts after %d attempts%s", Red, maxRetries, Reset)
}

func extractTags(jobs []struct {
	ID   int      `json:"id"`
	Tags []string `json:"tag_list"`
}) []string {
	var tags []string
	for _, job := range jobs {
		tags = append(tags, job.Tags...)
	}
	return tags
}
func isExcluded(projectName string, excludeProjects []string) bool {
	for _, excluded := range excludeProjects {
		if projectName == excluded {
			return true
		}
	}
	return false
}
