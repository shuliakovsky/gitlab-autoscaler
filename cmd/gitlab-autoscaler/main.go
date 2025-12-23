package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/shuliakovsky/gitlab-autoscaler/config"
	"github.com/shuliakovsky/gitlab-autoscaler/core"
	"github.com/shuliakovsky/gitlab-autoscaler/providers/aws"
)

// Version and CommitHash will be set during the build process
var Version string = "0.1.0" // Default Version value
var CommitHash string = ""   // Default Commit Hash empty

func main() {
	configPath := flag.String("config", "./config.yml", "Path to the configuration file")
	versionFlag := flag.Bool("version", false, "Display application version")
	helpFlag := flag.Bool("help", false, "Show help message")

	flag.Parse()

	if *versionFlag {
		fmt.Printf("gitlab-autoscaler version: %s\n", Version)
		if CommitHash != "" {
			fmt.Printf("commit hash: %s\n", CommitHash)
		}
		return
	}

	if *helpFlag {
		printHelp()
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	config.PrintConfiguration(cfg, Version, CommitHash)

	// Create all available providers
	providers := make(map[string]core.Provider)
	asgToProvider := make(map[string]string) // Maps ASG name to provider

	// Process ALL providers (not just AWS)
	for providerName, config := range cfg.Providers {
		if len(config.AsgNames) == 0 {
			continue // Skip empty providers
		}

		defaultRegion := config.Region
		if defaultRegion == "" {
			defaultRegion = os.Getenv("AWS_REGION")
			if defaultRegion == "" {
				defaultRegion = "us-east-1"
			}
		}

		var providerClient core.Provider
		switch strings.ToLower(providerName) {
		case "aws":
			client, err := aws.NewAWSClient(defaultRegion)
			if err != nil {
				log.Fatalf("Failed to initialize %s client: %v", providerName, err)
			}
			providerClient = client
		default:
			log.Fatalf("Unsupported provider '%s'", providerName)
		}

		providers[providerName] = providerClient

		// Link each ASG name to its provider
		for _, asg := range config.AsgNames {
			asgToProvider[asg.Name] = providerName
		}
	}

	orchestrator := core.NewOrchestrator(providers, asgToProvider)

	ticker := time.NewTicker(time.Duration(cfg.Autoscaler.CheckInterval) * time.Second)
	defer ticker.Stop()

	core.Run(cfg, orchestrator)

	for range ticker.C {
		core.Run(cfg, orchestrator)
	}
}

func printHelp() {
	fmt.Println("Usage:")
	fmt.Println("  --config <path to config file>     Specify the path to the configuration file.")
	fmt.Println("  --version                          Display application version.")
	fmt.Println("  --help                             Show help message.")
}
