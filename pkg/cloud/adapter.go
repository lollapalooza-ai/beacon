package cloud

import "context"

// Adapter defines the interface for cloud provider interactions.
// Each cloud provider (AWS, GCP, etc.) implements this interface.
type Adapter interface {
	// Name returns the cloud provider name (e.g., "aws", "gcp").
	Name() string

	// QuerySpotPrices retrieves current spot instance pricing.
	QuerySpotPrices(ctx context.Context, req *SpotPriceRequest) ([]SpotPrice, error)

	// ProvisionInstance launches a new spot instance.
	ProvisionInstance(ctx context.Context, req *ProvisionRequest) (*Instance, error)

	// GetInstanceStatus retrieves the current status of an instance.
	GetInstanceStatus(ctx context.Context, instanceID string, region string) (*InstanceStatus, error)

	// TerminateInstance terminates (decommissions) a running instance.
	TerminateInstance(ctx context.Context, instanceID string, region string) error
}
