package main

import (
	"context"
	"flag"
	"fmt"
	"gopkg.in/yaml.v3"
	"log"
	"os"
	"strings"
	"time"
)

// Version and CommitHash will be set during the build process
var Version string = "0.1.0" //Default Version value
var CommitHash string = ""

func printVersion() {
	fmt.Printf("gitlab-autoscaler version: %s\n", Version)
	if CommitHash != "" {
		fmt.Printf("commit hash: %s\n", CommitHash)
	}
}
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
	log.Printf("gitlab-autoscaler. version: %s commit hash: %s\n", Version, CommitHash)
	log.Printf("configuration:\n")
	if len(config.GitLab.Token) > 0 {
		log.Printf("  gitlab private token: %s\n", "present")
	} else {
		log.Printf("  gitlab private token: %s%s%s\n", Red, "empty", Reset)
	}
	if len(config.GitLab.Group) > 0 {
		log.Printf("  gitlab group name: %s\n", config.GitLab.Group)
	} else {
		log.Printf("  gitlab group name: %s%s%s\n", Red, "empty", Reset)
	}
	log.Printf("  check interval: %d\n", config.Autoscaler.CheckInterval)
	log.Printf("  aws asg names:\n")
	for _, asg := range config.AWS.AsgNames {
		log.Printf("    - name: %s, tags: %v, max asg capacity: %d, scale to zero: %t\n", asg.Name, asg.Tags, asg.MaxAsgCapacity, asg.ScaleToZero)
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
func setDefaultConfig(config *Config) {
	if config.Autoscaler.CheckInterval == 0 {
		config.Autoscaler.CheckInterval = 10
	}
	for i := range config.AWS.AsgNames {
		if config.AWS.AsgNames[i].MaxAsgCapacity == 0 {
			config.AWS.AsgNames[i].MaxAsgCapacity = 1
		}
		if config.AWS.AsgNames[i].Region == "" {
			config.AWS.AsgNames[i].Region = os.Getenv("AWS_REGION")
			if config.AWS.AsgNames[i].Region == "" {
				config.AWS.AsgNames[i].Region = os.Getenv("AWS_DEFAULT_REGION")
			}
		}
	}
}
func main() {
	configPath := flag.String("config", "./config.yml", "Path to the configuration file. Default ./config.yml")
	help := flag.Bool("help", false, "Show help")
	version := flag.Bool("version", false, "Display the current version of the application")
	flag.Parse()
	borderLine := fmt.Sprintf("%s\n", strings.Repeat(string("="), 160))
	// version
	if *version {
		printVersion()
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
	setDefaultConfig(config)
	printConfiguration(config)
	awsClients := NewAWSClients()

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

				ScaleAutoScalingGroups(awsClients, config.AWS.AsgNames, int64(totalPendingJobs), int64(totalRunningJobs),
					int64(totalPendingWithoutTags), int64(totalRunningWithoutTags),
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
