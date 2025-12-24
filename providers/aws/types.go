package aws

// AWSClient implements the AutoscalingAPI interface using AWS SDK.
type AWSClient struct {
	svc AutoscalingAPI
}
