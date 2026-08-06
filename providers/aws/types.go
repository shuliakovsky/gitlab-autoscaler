package aws

// AWSClient implements the core.Provider interface using AWS SDK v2.
type AWSClient struct {
	svc AutoscalingAPI

	// fallbackRegion is used only when neither the caller-supplied region,
	// nor AWS_REGION/AWS_DEFAULT_REGION env vars are set. Populated from the
	// region NewAWSClient was constructed with.
	fallbackRegion string
}
