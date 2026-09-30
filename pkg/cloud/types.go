// Package cloud provides interfaces and types for interacting with cloud
// compute providers. Each provider (AWS, GCP, etc.) implements the Adapter
// interface to enable spot instance discovery, provisioning, and teardown.
package cloud

import "time"

// SpotPrice represents a spot instance pricing data point.
type SpotPrice struct {
	Provider     string    `json:"provider"`      // "aws", "gcp"
	Region       string    `json:"region"`        // e.g., "us-east-1"
	Zone         string    `json:"zone"`          // e.g., "us-east-1a"
	InstanceType string    `json:"instance_type"` // e.g., "p4d.24xlarge"
	PricePerHour float64   `json:"price_per_hour"`
	GPUType      string    `json:"gpu_type"`  // e.g., "A100"
	GPUCount     int       `json:"gpu_count"` // number of GPUs
	VCPUs        int       `json:"vcpus"`
	MemoryGiB    float64   `json:"memory_gib"`
	Timestamp    time.Time `json:"timestamp"` // when this price was observed
}

// SpotPriceRequest defines the parameters for querying spot prices.
type SpotPriceRequest struct {
	// Filters
	InstanceTypes []string `json:"instance_types,omitempty"`
	GPUType       string   `json:"gpu_type,omitempty"`
	MinGPUs       int      `json:"min_gpus,omitempty"`
	MinVCPUs      int      `json:"min_vcpus,omitempty"`
	MinMemoryGiB  float64  `json:"min_memory_gib,omitempty"`

	// Regions to query (empty = all available)
	Regions []string `json:"regions,omitempty"`

	// Budget constraint
	MaxPricePerHour float64 `json:"max_price_per_hour,omitempty"`
}

// ProvisionRequest defines the parameters for launching a spot instance.
type ProvisionRequest struct {
	InstanceType string            `json:"instance_type"`
	Region       string            `json:"region"`
	Zone         string            `json:"zone,omitempty"`
	ImageID      string            `json:"image_id,omitempty"`  // AMI ID or equivalent
	KeyName      string            `json:"key_name,omitempty"`  // SSH key pair name
	MaxPrice     float64           `json:"max_price"`           // max spot price bid
	Tags         map[string]string `json:"tags,omitempty"`
	UserData     string            `json:"user_data,omitempty"` // startup script (base64)
	StorageGiB   int               `json:"storage_gib,omitempty"`
}

// Instance represents a provisioned compute instance.
type Instance struct {
	ID           string            `json:"id"` // provider-specific instance ID
	Provider     string            `json:"provider"`
	InstanceType string            `json:"instance_type"`
	Region       string            `json:"region"`
	Zone         string            `json:"zone"`
	PublicIP     string            `json:"public_ip,omitempty"`
	PrivateIP    string            `json:"private_ip,omitempty"`
	State        InstanceState     `json:"state"`
	LaunchedAt   time.Time         `json:"launched_at"`
	Tags         map[string]string `json:"tags,omitempty"`
	SpotPrice    float64           `json:"spot_price"`
}

// InstanceState represents the lifecycle state of an instance.
type InstanceState string

const (
	InstanceStatePending     InstanceState = "pending"
	InstanceStateRunning     InstanceState = "running"
	InstanceStateTerminating InstanceState = "terminating"
	InstanceStateTerminated  InstanceState = "terminated"
	InstanceStateStopped     InstanceState = "stopped"
	InstanceStateUnknown     InstanceState = "unknown"
)

// InstanceStatus provides current status information about an instance.
type InstanceStatus struct {
	Instance    *Instance `json:"instance"`
	HealthCheck bool      `json:"health_check"` // true if healthy
	Uptime      float64   `json:"uptime_hours"`
}
