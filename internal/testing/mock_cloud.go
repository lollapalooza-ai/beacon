// Package testing provides mock implementations for use in tests.
package testing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lollapalooza-ai/beacon/pkg/cloud"
)

// MockCloudAdapter is an in-memory cloud adapter for testing.
// It simulates spot pricing, provisioning, and instance lifecycle.
type MockCloudAdapter struct {
	mu        sync.RWMutex
	name      string
	prices    []cloud.SpotPrice
	instances map[string]*cloud.Instance

	// Control behavior
	QueryError     error
	ProvisionError error
	StatusError    error
	TerminateError error

	// Counters for verifying call limits
	QueryCount     int
	ProvisionCount int
	StatusCount    int
	TerminateCount int
}

// NewMockCloudAdapter creates a new mock adapter with the given name and preset prices.
func NewMockCloudAdapter(name string, prices []cloud.SpotPrice) *MockCloudAdapter {
	return &MockCloudAdapter{
		name:      name,
		prices:    prices,
		instances: make(map[string]*cloud.Instance),
	}
}

// Name returns the adapter name.
func (m *MockCloudAdapter) Name() string {
	return m.name
}

// QuerySpotPrices returns preset prices or an error.
func (m *MockCloudAdapter) QuerySpotPrices(ctx context.Context, req *cloud.SpotPriceRequest) ([]cloud.SpotPrice, error) {
	m.mu.Lock()
	m.QueryCount++
	m.mu.Unlock()

	if m.QueryError != nil {
		return nil, m.QueryError
	}

	// Filter prices based on request
	var result []cloud.SpotPrice
	for _, p := range m.prices {
		if req.MaxPricePerHour > 0 && p.PricePerHour > req.MaxPricePerHour {
			continue
		}
		if req.GPUType != "" && p.GPUType != req.GPUType {
			continue
		}
		if req.MinGPUs > 0 && p.GPUCount < req.MinGPUs {
			continue
		}
		result = append(result, p)
	}

	return result, nil
}

// ProvisionInstance creates a mock instance.
func (m *MockCloudAdapter) ProvisionInstance(ctx context.Context, req *cloud.ProvisionRequest) (*cloud.Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ProvisionCount++

	if m.ProvisionError != nil {
		return nil, m.ProvisionError
	}

	id := fmt.Sprintf("i-mock-%d", m.ProvisionCount)
	instance := &cloud.Instance{
		ID:           id,
		Provider:     m.name,
		InstanceType: req.InstanceType,
		Region:       req.Region,
		Zone:         req.Zone,
		PublicIP:     "1.2.3.4",
		PrivateIP:    "10.0.0.1",
		State:        cloud.InstanceStateRunning,
		LaunchedAt:   time.Now(),
		Tags:         req.Tags,
		SpotPrice:    req.MaxPrice,
	}

	m.instances[id] = instance
	return instance, nil
}

// GetInstanceStatus returns the status of a mock instance.
func (m *MockCloudAdapter) GetInstanceStatus(ctx context.Context, instanceID string, region string) (*cloud.InstanceStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.StatusCount++

	if m.StatusError != nil {
		return nil, m.StatusError
	}

	inst, ok := m.instances[instanceID]
	if !ok {
		return nil, fmt.Errorf("instance %s not found", instanceID)
	}

	return &cloud.InstanceStatus{
		Instance:    inst,
		HealthCheck: inst.State == cloud.InstanceStateRunning,
		Uptime:      time.Since(inst.LaunchedAt).Hours(),
	}, nil
}

// TerminateInstance terminates a mock instance.
func (m *MockCloudAdapter) TerminateInstance(ctx context.Context, instanceID string, region string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TerminateCount++

	if m.TerminateError != nil {
		return m.TerminateError
	}

	inst, ok := m.instances[instanceID]
	if !ok {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	inst.State = cloud.InstanceStateTerminated
	return nil
}

// SetInstanceState changes the state of a mock instance (for test control).
func (m *MockCloudAdapter) SetInstanceState(instanceID string, state cloud.InstanceState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst, ok := m.instances[instanceID]; ok {
		inst.State = state
	}
}
