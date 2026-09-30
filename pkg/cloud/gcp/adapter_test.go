//go:build integration

package gcp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lollapalooza-ai/beacon/pkg/cloud"
	"github.com/rs/zerolog"
)

func TestGCPAdapterIntegration(t *testing.T) {
	projectID := os.Getenv("BEACON_GCP_PROJECT")
	if projectID == "" {
		t.Skip("Skipping GCP integration test; BEACON_GCP_PROJECT not set")
	}

	logger := zerolog.Nop()
	adapter := NewAdapter(projectID, logger)

	ctx := context.Background()

	// 1. Test QuerySpotPrices
	t.Run("QuerySpotPrices", func(t *testing.T) {
		req := &cloud.SpotPriceRequest{
			Regions: []string{"us-central1"},
		}
		prices, err := adapter.QuerySpotPrices(ctx, req)
		if err != nil {
			t.Fatalf("QuerySpotPrices failed: %v", err)
		}
		if len(prices) == 0 {
			t.Fatal("Expected at least 1 spot price returned")
		}
		
		found := false
		for _, p := range prices {
			if p.InstanceType == "g2-standard-4" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected g2-standard-4 in spot prices")
		}
	})

	// 2. Test ProvisionInstance (This creates real resources!)
	// To prevent costs during automated tests, we might want to skip the actual provision.
	// But since it's an integration test, we do a quick provision and terminate.
	t.Run("ProvisionAndTerminate", func(t *testing.T) {
		req := &cloud.ProvisionRequest{
			InstanceType: "g2-standard-4", // Smallest L4 instance
			Region:       "us-central1",
			Zone:         "us-central1-a",
			MaxPrice:     0.25,
			Tags:         map[string]string{"beacon-test": "true"},
		}

		instance, err := adapter.ProvisionInstance(ctx, req)
		if err != nil {
			t.Fatalf("ProvisionInstance failed: %v", err)
		}
		
		if instance.ID == "" {
			t.Fatal("Expected instance ID")
		}
		
		t.Logf("Provisioned instance %s", instance.ID)

		// Check status
		status, err := adapter.GetInstanceStatus(ctx, instance.ID, "us-central1")
		if err != nil {
			t.Fatalf("GetInstanceStatus failed: %v", err)
		}
		if status.Instance.State != cloud.InstanceStateRunning && status.Instance.State != cloud.InstanceStatePending {
			t.Errorf("Expected state Running or Pending, got %s", status.Instance.State)
		}

		// Terminate
		err = adapter.TerminateInstance(ctx, instance.ID, "us-central1")
		if err != nil {
			t.Fatalf("TerminateInstance failed: %v", err)
		}

		// Verify termination
		time.Sleep(5 * time.Second)
		status, err = adapter.GetInstanceStatus(ctx, instance.ID, "us-central1")
		if err == nil && (status.Instance.State == cloud.InstanceStateRunning || status.Instance.State == cloud.InstanceStatePending) {
			t.Errorf("Instance %s is still running after termination", instance.ID)
		}
	})
}
