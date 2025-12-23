package aws

import (
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
)

type AutoScalingGroup struct {
	Name             string
	MinSize          int32
	MaxSize          int32
	DesiredCapacity  int32
	MaxInstanceLimit int32
}

type AWSClient struct {
	svc *autoscaling.Client
}
