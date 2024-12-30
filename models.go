package main

import (
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

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
	ScaleToZero    bool     `yaml:"scale-to-zero"`
	Region         string   `yaml:"region"`
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
	AsgNames []Asg `yaml:"asg-names"`
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

type AWSService interface {
	DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
}

type AWSClient struct {
	svc AutoScalingAPI
}

type AWSClients struct {
	clients map[string]AWSService
}
type AutoScalingAPI interface {
	DescribeAutoScalingGroups(*autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
	UpdateAutoScalingGroup(*autoscaling.UpdateAutoScalingGroupInput) (*autoscaling.UpdateAutoScalingGroupOutput, error)
}
