package main

import (
	"context"
	"flag"
	"fmt"
	"gopkg.in/yaml.v2"
	"log"
	"os"
	"strings"
	"time"
)

// Version and CommitHash will be set during the build process
var Version string = "0.1.0" //Default Version value
var CommitHash string = ""

func printHelp() {
	fmt.Println("Usage:")
	fmt.Println("  --config <path to config file>     Specify the path to the configuration file.")
	fmt.Println("  --help                             Display this help message.")
	fmt.Println("  --version                          Display the current version of the application.")
}
func printConfiguration(config *Config) {
	border := "═"
	borderLine := fmt.Sprintf("%s\n", strings.Repeat(string(border), 160))
	fmt.Print(borderLine)
	log.Printf("Current Configuration:\n")
	log.Printf("  Token: %s\n", "hidden")
	log.Printf("  Group Name: %s\n", config.GitLab.Group)
	log.Printf("  Check Interval: %d\n", config.Autoscaler.CheckInterval)
	log.Printf("  Scale to Zero: %t\n", config.AWS.ScaleToZero)
	log.Printf("  ASG Names:\n")
	for _, asg := range config.AWS.AsgNames {
		log.Printf("    - Name: %s, Tags: %v, Max ASG Capacity: %d\n", asg.Name, asg.Tags, asg.MaxAsgCapacity)
	}
	fmt.Print(borderLine)
}

func loadConfig(configPath string) (*Config, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
		}
	}(file)

	var config Config
	if err := yaml.NewDecoder(file).Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

func main() {
	configPath := flag.String("config", "./config.yml", "Path to the configuration file. Default ./config.yml")
	help := flag.Bool("help", false, "Show help")
	version := flag.Bool("version", false, "Display the current version of the application")
	flag.Parse()
	borderLine := fmt.Sprintf("%s\n", strings.Repeat(string("="), 160))
	// version
	if *version {
		fmt.Printf("Version: %s\n", Version)
		if CommitHash != "" {
			fmt.Printf("Commit Hash: %s\n", CommitHash)
		}
		return
	}
	// help
	if *help {
		printHelp()
		return
	}
	if *configPath == "" {
		log.Fatal("Error: The configuration file path is required. \nFor usage instructions, please run: ./gitlab-autoscaler --help")
	}

	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Error loading configuration: %s", err)
	}

	// print started configuration
	printConfiguration(config)
	InitializeAWS()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				projects, err := FetchProjects(config.GitLab.Token, config.GitLab.Group, config.GitLab.ExcludeProjects)
				if err != nil {
					log.Printf("%sError fetching projects: %s%s", Red, err, Reset)
					time.Sleep(time.Duration(config.Autoscaler.CheckInterval) * time.Second)
					continue
				}
				pendingJobsWithTags, runningJobsWithTags := CountPendingJobsWithTags(config.GitLab.Token, projects)
				totalPendingJobs, totalRunningJobs, totalPendingWithoutTags, totalRunningWithoutTags,
					err := FetchJobCounts(config.GitLab.Token, projects)
				if err != nil {
					log.Printf("%sError fetching jobs: %s%s", Red, err, Reset)
					time.Sleep(time.Duration(config.Autoscaler.CheckInterval) * time.Second)
					continue
				}

				var totalCapacity int64 = 0
				ScaleAutoScalingGroups(config.AWS.AsgNames, int64(totalPendingJobs), int64(totalRunningJobs),
					int64(totalPendingWithoutTags), int64(totalRunningWithoutTags), config.AWS.ScaleToZero,
					pendingJobsWithTags, runningJobsWithTags, &totalCapacity)
				log.Printf("Total active capacity: %s%-4d%s", Green, totalCapacity, Reset)
				fmt.Print(borderLine)
				time.Sleep(time.Duration(config.Autoscaler.CheckInterval) * time.Second)
			}
		}
	}()

	<-ctx.Done()
	log.Println("Exit...")
}
