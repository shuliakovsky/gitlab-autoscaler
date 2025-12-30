package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shuliakovsky/gitlab-autoscaler/config"
	"github.com/shuliakovsky/gitlab-autoscaler/core"
	"github.com/shuliakovsky/gitlab-autoscaler/providers/aws"
)

// Version and CommitHash will be set during the build process
var Version string = "0.1.0" // Default Version value
var CommitHash string = ""   // Default Commit Hash empty

const (
	systemConfigPath = "/etc/gitlab-autoscaler/config.yml"
	systemPidPath    = "/var/run/gitlab-autoscaler.pid"
	localConfigPath  = "./config.yml"
	localPidPath     = "./gitlab-autoscaler.pid"
)

func main() {
	// Flags: allow explicit override; resolution happens after parsing
	configFlag := flag.String("config", "", "Path to the configuration file (explicit overrides discovery)")
	helpFlag := flag.Bool("help", false, "Show help message")
	pidFileFlag := flag.String("pid-file", "", "Path to pidfile (explicit overrides discovery)")
	reloadFlag := flag.Bool("r", false, "Validate config and send SIGHUP to running process (or self)")
	versionFlag := flag.Bool("version", false, "Display application version")

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

	// Resolve config and pidfile paths by priority:
	// 1) explicit flag
	// 2) system path if exists
	// 3) local path fallback
	configPath := resolveConfigPath(*configFlag)
	pidFile := resolvePidFilePath(*pidFileFlag)

	// If -r: validate config first, then send SIGHUP to pidfile (or self)
	if *reloadFlag {
		cfg, err := config.Load(configPath)
		if err != nil {
			log.Fatalf("Failed to load config (%s): %v", configPath, err)
		}
		if err := cfg.Validate(); err != nil {
			log.Fatalf("Config validation failed: %v", err)
		}

		pid, err := readPidFile(pidFile)
		if err != nil {
			// pidfile not found — send SIGHUP to self
			log.Printf("pidfile not found (%s), sending SIGHUP to self", pidFile)
			pid = os.Getpid()
		} else {
			log.Printf("Sending SIGHUP to pid %d (pidfile: %s)", pid, pidFile)
		}

		if err := sendHUPToPID(pid); err != nil {
			log.Fatalf("Failed to send SIGHUP to pid %d: %v", pid, err)
		}
		log.Printf("Reload signal sent successfully")
		return
	}

	// Normal start: write pidfile
	if err := writePidFile(pidFile); err != nil {
		log.Fatalf("Failed to write pidfile: %v", err)
	}
	defer func() {
		_ = os.Remove(pidFile)
	}()

	// Load and validate config
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config (%s): %v", configPath, err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Build initial providers and asg mapping (keeps original behavior)
	providers, asgToProvider, err := buildProvidersFromConfig(cfg)
	if err != nil {
		log.Fatalf("Failed to build providers: %v", err)
	}

	orchestrator := core.NewOrchestrator(providers, asgToProvider)

	// Context and signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		// debounce: not more often than once per second
		var lastReload time.Time
		minInterval := time.Second
		for {
			select {
			case s := <-sigCh:
				switch s {
				case syscall.SIGHUP:
					if time.Since(lastReload) < minInterval {
						log.Printf("Reload suppressed (debounce)")
						continue
					}
					lastReload = time.Now()
					log.Printf("Received SIGHUP: reloading config")
					newCfg, err := config.Load(configPath)
					if err != nil {
						log.Printf("Config load failed: %v", err)
						continue
					}
					if err := newCfg.Validate(); err != nil {
						log.Printf("Config validation failed: %v", err)
						continue
					}

					// Build new providers (initialization happens here)
					newProviders, newAsgToProvider, err := buildProvidersFromConfig(newCfg)
					if err != nil {
						log.Printf("Failed to initialize providers for new config: %v", err)
						continue
					}

					// Atomically swap providers in orchestrator
					orchestrator.SetProviders(newProviders, newAsgToProvider)
					// Update cfg used by ticker loop below
					cfg = newCfg

					log.Printf("Config reloaded successfully")
				case syscall.SIGINT, syscall.SIGTERM:
					log.Printf("Shutdown signal received")
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Main loop
	ticker := time.NewTicker(time.Duration(cfg.Autoscaler.CheckInterval) * time.Second)
	defer ticker.Stop()

	core.Run(cfg, orchestrator)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Exiting")
			return
		case <-ticker.C:
			core.Run(cfg, orchestrator)
		}
	}
}

func printHelp() {
	fmt.Println("Usage:")
	fmt.Println("  --config <path to config file>     Specify the path to the configuration file (explicit overrides discovery).")
	fmt.Println("  -r                                 Validate config and send SIGHUP to running process (or self).")
	fmt.Println("  --pid-file <path>                  Path to pidfile (explicit overrides discovery).")
	fmt.Println("  --version                          Display application version.")
	fmt.Println("  --help                             Show help message.")
}

// resolveConfigPath chooses config path by priority: explicit -> system if exists -> local
func resolveConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if fileExists(systemConfigPath) {
		return systemConfigPath
	}
	return localConfigPath
}

// resolvePidFilePath chooses pidfile path by priority: explicit -> system if exists -> local
func resolvePidFilePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if fileExists(systemPidPath) {
		return systemPidPath
	}
	return localPidPath
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func writePidFile(path string) error {
	pid := os.Getpid()
	return ioutil.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func readPidFile(path string) (int, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func sendHUPToPID(pid int) error {
	return syscall.Kill(pid, syscall.SIGHUP)
}

func buildProvidersFromConfig(cfg *config.Config) (map[string]core.Provider, map[string]string, error) {
	providers := make(map[string]core.Provider)
	asgToProvider := make(map[string]string)

	for providerName, providerCfg := range cfg.Providers {
		if len(providerCfg.AsgNames) == 0 {
			continue
		}

		defaultRegion := providerCfg.Region
		if defaultRegion == "" {
			defaultRegion = os.Getenv("AWS_REGION")
			if defaultRegion == "" {
				defaultRegion = "us-east-1"
			}
		}

		switch strings.ToLower(providerName) {
		case "aws":
			client, err := aws.NewAWSClient(defaultRegion)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to initialize %s client: %w", providerName, err)
			}
			providers[providerName] = client
		default:
			return nil, nil, fmt.Errorf("unsupported provider '%s'", providerName)
		}

		for _, asg := range providerCfg.AsgNames {
			asgToProvider[asg.Name] = providerName
		}
	}

	return providers, asgToProvider, nil
}
