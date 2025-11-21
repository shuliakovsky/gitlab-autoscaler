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

// shared HTTP client with hard timeout to prevent hangs
var gitlabClient = &http.Client{
	Timeout: 25 * time.Second,
}

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
			return nil, err
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {

			}
		}(resp.Body)

		if resp.StatusCode == http.StatusTooManyRequests {
			waitDuration := time.Duration(2<<attempt) * time.Second // Exponential backoff
			log.Printf("Received 429 Too Many Requests. Retrying in %s...", waitDuration)
			time.Sleep(waitDuration) // Wait before retrying
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%serror fetching projects: %s%s", Red, resp.Status, Reset)
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
	type res struct {
		pendingTags []string
		runningTags []string
		pending     int
		running     int
		err         error
	}
	results := make(chan res, len(projects))

	for _, project := range projects {
		wg.Add(1)
		go func(project Project) {
			defer wg.Done()
			pendingJobs, pendingTags, err := FetchJobsCount(token, project.ID, "pending")
			if err != nil {
				results <- res{err: err}
				return
			}
			runningJobs, runningTags, err := FetchJobsCount(token, project.ID, "running")
			if err != nil {
				results <- res{pending: pendingJobs, err: err}
				return
			}
			results <- res{
				pendingTags: pendingTags,
				runningTags: runningTags,
				pending:     pendingJobs,
				running:     runningJobs,
				err:         nil,
			}
		}(project)
	}
	wg.Wait()
	close(results)

	for r := range results {
		if r.err != nil {
			log.Printf("Error: %s", r.err)
			continue
		}
		// Для каждого проекта считаем, сколько раз встречается каждый тег (количество джобов с этим тегом)
		// и добавляем именно это количество в глобальный агрегат.
		if len(r.pendingTags) > 0 {
			local := make(map[string]int)
			for _, t := range r.pendingTags {
				local[t]++
			}
			for tag, cnt := range local {
				pendingJobsWithTags[tag] += cnt
			}
		}
		if len(r.runningTags) > 0 {
			local := make(map[string]int)
			for _, t := range r.runningTags {
				local[t]++
			}
			for tag, cnt := range local {
				runningJobsWithTags[tag] += cnt
			}
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

		resp, err := gitlabClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {

			}
		}(resp.Body)

		// Handle specific rate-limit
		if resp.StatusCode == http.StatusTooManyRequests {
			waitDuration := time.Duration(2<<attempt) * time.Second // Exponential backoff
			log.Printf("Received 429 Too Many Requests. Retrying in %s...", waitDuration)
			time.Sleep(waitDuration) // Wait before retrying
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return 0, nil, fmt.Errorf("%serror fetching %s jobs for project ID %d: name: %s%s", Red, scope, projectID, resp.Status, Reset)
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
