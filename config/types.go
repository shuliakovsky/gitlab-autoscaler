package config

// Config represents the application configuration structure
type Config struct {
	GitLab     GitLabConfig              `yaml:"gitlab"`     // GitLab settings for API access
	Autoscaler AutoscalerConfig          `yaml:"autoscaler"` // Autoscaling algorithm parameters
	Providers  map[string]ProviderConfig `yaml:",inline"`    // Map of providers (AWS, Azure etc.) with their specific configurations
}

// ProviderConfig contains settings specific to a cloud provider (e.g., AWS, Azure)
type ProviderConfig struct {
	Region      string `yaml:"region"`       // Cloud region where the ASGs are located
	AsgNames    []Asg  `yaml:"asg-names"`    // List of Auto Scaling Groups configured for this provider
	DefaultZone string `yaml:"default-zone"` // Default zone (used in some cloud providers)
}

// GitLabConfig contains the configuration for connecting to GitLab API
type GitLabConfig struct {
	Token           string   `yaml:"token"`            // Private access token with necessary permissions to read projects and jobs
	Group           string   `yaml:"group"`            // Name of the GitLab group containing all CI/CD enabled projects
	ExcludeProjects []string `yaml:"exclude-projects"` // List of project names to exclude from processing (e.g., "node-deployment")
}

// AutoscalerConfig contains settings for how often and how the autoscaler should operate
type AutoscalerConfig struct {
	CheckInterval int `yaml:"check-interval"` // Interval in seconds between scaling checks (must be positive)
}

// Asg represents a single Auto Scaling Group configuration
type Asg struct {
	Name           string   `yaml:"name"`             // Unique name of the ASG in cloud provider
	Tags           []string `yaml:"tags"`             // List of tags that this ASG should handle (e.g., ["amd64", "prod"])
	MaxAsgCapacity int64    `yaml:"max-asg-capacity"` // Maximum number of instances allowed in this ASG (prevents over-provisioning)
	ScaleToZero    bool     `yaml:"scale-to-zero"`    // Whether the ASG can be scaled down to zero instances
	Region         string   `yaml:"region"`           // Region where this specific ASG is located (overrides provider default if set)
}
