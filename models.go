package main

type AutoScalingGroup struct {
	Name             string
	MinSize          int64
	MaxSize          int64
	DesiredCapacity  int64
	MaxInstanceLimit int64
}
type Asg struct {
	Name           string   `yaml:"name"`
	Tags           []string `yaml:"tags"`
	MaxAsgCapacity int      `yaml:"max-asg-capacity"`
}
type GitLabConfig struct {
	Token           string   `yaml:"token"`
	Group           string   `yaml:"group"`
	ExcludeProjects []string `yaml:"exclude-projects"`
}
type AutoscalerConfig struct {
	CheckInterval int `yaml:"check-interval"`
}
type AWSConfig struct {
	ScaleToZero    bool  `yaml:"scale-to-zero"`
	MaxAsgCapacity int   `yaml:"max-asg-capacity"`
	AsgNames       []Asg `yaml:"asg-names"`
}
type Config struct {
	GitLab     GitLabConfig     `yaml:"gitlab"`
	Autoscaler AutoscalerConfig `yaml:"autoscaler"`
	AWS        AWSConfig        `yaml:"aws"`
}
type Project struct {
	ID             int      `json:"id"`
	Name           string   `json:"name"`
	PendingTagList []string `json:"pending_tag_list"`
	RunningTagList []string `json:"running_tag_list"`
}
