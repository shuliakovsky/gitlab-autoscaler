package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

// Load loads the configuration from a YAML file
func Load(configPath string) (*Config, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var cfg Config
	err = yaml.NewDecoder(file).Decode(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	return &cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Autoscaler.CheckInterval <= 0 {
		return fmt.Errorf("check-interval must be positive")
	}

	for providerName, config := range c.Providers {
		for i, asg := range config.AsgNames {
			if err := asg.Validate(); err != nil {
				return fmt.Errorf("provider %s: asg[%d]: %w", providerName, i, err)
			}
		}
	}

	if len(c.GitLab.Token) == 0 {
		return fmt.Errorf("gitlab.token is required")
	}

	if len(c.GitLab.Group) == 0 {
		return fmt.Errorf("gitlab.group is required")
	}

	return nil
}

// Validate validates the ASG configuration
func (a *Asg) Validate() error {
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}
	if a.MaxAsgCapacity < 0 {
		return fmt.Errorf("max-asg-capacity must be non-negative")
	}

	return nil
}

// PrintConfiguration prints the configuration to standard output for debugging
func PrintConfiguration(cfg *Config, version string, commitHash string) {

	if len(cfg.GitLab.Token) > 0 {
		fmt.Printf("gitlab-autoscaler. version: %s commit hash: %s\n", version, commitHash)
	} else {
		fmt.Printf("gitlab-autoscaler. version: %s (unconfigured)\n", version)
	}

	fmt.Printf("configuration:\n")
	fmt.Printf("  gitlab private token: %s\n", "present")
	fmt.Printf("  gitlab group name: %s\n", cfg.GitLab.Group)
	fmt.Printf("  check interval: %d seconds\n", cfg.Autoscaler.CheckInterval)

	// Print ASGs from the AWS provider (if it exists in Providers)
	if awsConfig, ok := cfg.Providers["aws"]; ok {
		fmt.Println("\naws asg names:")
		for _, asg := range awsConfig.AsgNames {
			fmt.Printf("  - name: %-40s region: %-15s max capacity: %-3d scale to zero: %t  tags: %v\n",
				asg.Name, asg.Region, asg.MaxAsgCapacity, asg.ScaleToZero, asg.Tags)
		}
	} else {
		fmt.Println("\nNo AWS ASGs configured")
	}

	// Print other providers if needed
	for providerName, config := range cfg.Providers {
		if providerName == "aws" {
			continue // Already printed above
		}
		fmt.Printf("\n%s asg names:\n", providerName)
		for _, asg := range config.AsgNames {
			fmt.Printf("  - name: %-40s region: %-15s max capacity: %-3d scale to zero: %t  tags: %v\n",
				asg.Name, asg.Region, asg.MaxAsgCapacity, asg.ScaleToZero, asg.Tags)
		}
	}
}
