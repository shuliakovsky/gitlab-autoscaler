package core

// Provider defines the interface for cloud provider implementations
type Provider interface {
	GetCurrentCapacity(asgName string) (int64, int64, error)
	UpdateASGCapacity(asgName string, capacity int64) error
}
