package gcp

import (
	"context"
	"fmt"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"github.com/lollapalooza-ai/beacon/pkg/cloud"
	"github.com/rs/zerolog"
	"google.golang.org/api/option"
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed catalog.json
var catalogJSON []byte

type CatalogFile struct {
	UpdatedAt time.Time               `json:"updated_at"`
	Instances map[string]instanceSpec `json:"instances"`
}

// instanceSpec holds hardware specifications and estimated spot prices for GCP instances.
type instanceSpec struct {
	GPUType   string  `json:"gpu_type"`
	GPUCount  int     `json:"gpu_count"`
	VCPUs     int     `json:"vcpus"`
	MemoryGiB float64 `json:"memory_gib"`
	EstPrice  float64 `json:"spot_price"` // spot price per hour in USD
}

var (
	instanceSpecs map[string]instanceSpec
	catalogOnce   sync.Once
)

func loadCatalog() {
	catalogOnce.Do(func() {
		var cat CatalogFile
		if err := json.Unmarshal(catalogJSON, &cat); err != nil {
			panic(fmt.Sprintf("Failed to load embedded GCP catalog: %v", err))
		}
		instanceSpecs = cat.Instances
	})
}

// Adapter implements the cloud.Adapter interface for GCP Compute Engine Spot VMs.
type Adapter struct {
	projectID string
	logger    zerolog.Logger
}

// NewAdapter creates a new GCP cloud adapter.
func NewAdapter(projectID string, logger zerolog.Logger) *Adapter {
	return &Adapter{
		projectID: projectID,
		logger:    logger,
	}
}

// Name returns the provider name.
func (a *Adapter) Name() string {
	return "gcp"
}

// QuerySpotPrices returns available GCP instance types and their estimated spot prices.
func (a *Adapter) QuerySpotPrices(ctx context.Context, req *cloud.SpotPriceRequest) ([]cloud.SpotPrice, error) {
	loadCatalog()
	var prices []cloud.SpotPrice
	now := time.Now()

	regions := req.Regions
	if len(regions) == 0 {
		regions = []string{"us-central1"} // Default fallback if none specified
	}

	for _, region := range regions {
		zone := fmt.Sprintf("%s-a", region) // Defaulting to zone 'a' for the region

		for instanceType, spec := range instanceSpecs {
			// Apply filters
			if req.MaxPricePerHour > 0 && spec.EstPrice > req.MaxPricePerHour {
				continue
			}
			if req.GPUType != "" && spec.GPUType != req.GPUType {
				continue
			}
			if req.MinGPUs > 0 && spec.GPUCount < req.MinGPUs {
				continue
			}
			if req.MinVCPUs > 0 && spec.VCPUs < req.MinVCPUs {
				continue
			}
			if req.MinMemoryGiB > 0 && spec.MemoryGiB < req.MinMemoryGiB {
				continue
			}

			prices = append(prices, cloud.SpotPrice{
				Provider:     a.Name(),
				Region:       region,
				Zone:         zone,
				InstanceType: instanceType,
				PricePerHour: spec.EstPrice,
				GPUType:      spec.GPUType,
				GPUCount:     spec.GPUCount,
				VCPUs:        spec.VCPUs,
				MemoryGiB:    spec.MemoryGiB,
				Timestamp:    now,
			})
		}
	}

	return prices, nil
}

// ProvisionInstance launches a new GCP Spot VM.
func (a *Adapter) ProvisionInstance(ctx context.Context, req *cloud.ProvisionRequest) (*cloud.Instance, error) {
	instancesClient, err := compute.NewInstancesRESTClient(ctx, option.WithScopes("https://www.googleapis.com/auth/compute"))
	if err != nil {
		return nil, fmt.Errorf("failed to create instances client: %w", err)
	}
	defer instancesClient.Close()

	// Use provided zone or default to region-a
	zone := req.Zone
	if zone == "" {
		zone = fmt.Sprintf("%s-a", req.Region)
	}

	// For MVP, we'll use a standard Debian or Ubuntu image
	sourceImage := req.ImageID
	if sourceImage == "" {
		sourceImage = "projects/debian-cloud/global/images/family/debian-12"
	}

	instanceName := fmt.Sprintf("beacon-spot-%d", time.Now().UnixNano())

	// Build Labels — GCP requires lowercase keys with only letters, digits, underscores, dashes
	labels := make(map[string]string)
	for k, v := range req.Tags {
		labels[sanitizeLabelKey(k)] = v
	}

	storageGiB := int64(req.StorageGiB)
	if storageGiB == 0 {
		storageGiB = 50 // default
	}

	insertReq := &computepb.InsertInstanceRequest{
		Project: a.projectID,
		Zone:    zone,
		InstanceResource: &computepb.Instance{
			Name:        &instanceName,
			MachineType: strPtr(fmt.Sprintf("zones/%s/machineTypes/%s", zone, req.InstanceType)),
			Labels:      labels,
			Scheduling: &computepb.Scheduling{
				ProvisioningModel: strPtr("SPOT"),
				InstanceTerminationAction: strPtr("DELETE"), // Delete when preempted
			},
			Disks: []*computepb.AttachedDisk{
				{
					Boot:       boolPtr(true),
					AutoDelete: boolPtr(true),
					InitializeParams: &computepb.AttachedDiskInitializeParams{
						SourceImage: &sourceImage,
						DiskSizeGb:  &storageGiB,
					},
				},
			},
			NetworkInterfaces: []*computepb.NetworkInterface{
				{
					Network: strPtr("global/networks/default"),
					AccessConfigs: []*computepb.AccessConfig{
						{
							Type: strPtr("ONE_TO_ONE_NAT"),
							Name: strPtr("External NAT"),
						},
					},
				},
			},
		},
	}

	a.logger.Info().Str("instance_name", instanceName).Msg("Provisioning GCP Spot VM")
	
	op, err := instancesClient.Insert(ctx, insertReq)
	if err != nil {
		return nil, fmt.Errorf("failed to insert GCP instance: %w", err)
	}

	err = op.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for GCP instance creation failed: %w", err)
	}

	// Fetch status to get the details
	return a.waitForRunning(ctx, instancesClient, a.projectID, zone, instanceName)
}

func (a *Adapter) waitForRunning(ctx context.Context, client *compute.InstancesClient, project, zone, name string) (*cloud.Instance, error) {
	for i := 0; i < 10; i++ {
		status, err := a.GetInstanceStatusWithClient(ctx, client, project, zone, name)
		if err == nil && status.Instance.State == cloud.InstanceStateRunning {
			return status.Instance, nil
		}
		time.Sleep(5 * time.Second)
	}
	// Fallback to returning what we have
	status, err := a.GetInstanceStatusWithClient(ctx, client, project, zone, name)
	if err != nil {
		return nil, err
	}
	return status.Instance, nil
}

// GetInstanceStatus retrieves the current status of a GCP instance.
func (a *Adapter) GetInstanceStatus(ctx context.Context, instanceID string, region string) (*cloud.InstanceStatus, error) {
	instancesClient, err := compute.NewInstancesRESTClient(ctx, option.WithScopes("https://www.googleapis.com/auth/compute"))
	if err != nil {
		return nil, fmt.Errorf("failed to create instances client: %w", err)
	}
	defer instancesClient.Close()
	
	zone := fmt.Sprintf("%s-a", region) // we need zone to query; assuming we used region-a if not strictly tracked

	return a.GetInstanceStatusWithClient(ctx, instancesClient, a.projectID, zone, instanceID)
}

func (a *Adapter) GetInstanceStatusWithClient(ctx context.Context, client *compute.InstancesClient, project, zone, instanceName string) (*cloud.InstanceStatus, error) {
	req := &computepb.GetInstanceRequest{
		Project:  project,
		Zone:     zone,
		Instance: instanceName,
	}

	inst, err := client.Get(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get GCP instance %s: %w", instanceName, err)
	}

	state := cloud.InstanceStateUnknown
	if inst.Status != nil {
		switch *inst.Status {
		case "PROVISIONING", "STAGING":
			state = cloud.InstanceStatePending
		case "RUNNING":
			state = cloud.InstanceStateRunning
		case "STOPPING", "SUSPENDING":
			state = cloud.InstanceStateTerminating
		case "TERMINATED", "STOPPED", "SUSPENDED":
			state = cloud.InstanceStateTerminated
		}
	}

	publicIP := ""
	privateIP := ""
	if len(inst.NetworkInterfaces) > 0 {
		if inst.NetworkInterfaces[0].NetworkIP != nil {
			privateIP = *inst.NetworkInterfaces[0].NetworkIP
		}
		if len(inst.NetworkInterfaces[0].AccessConfigs) > 0 && inst.NetworkInterfaces[0].AccessConfigs[0].NatIP != nil {
			publicIP = *inst.NetworkInterfaces[0].AccessConfigs[0].NatIP
		}
	}

	var launchedAt time.Time
	if inst.CreationTimestamp != nil {
		launchedAt, _ = time.Parse(time.RFC3339, *inst.CreationTimestamp)
	}

	uptime := 0.0
	if !launchedAt.IsZero() && state == cloud.InstanceStateRunning {
		uptime = time.Since(launchedAt).Hours()
	}

	labels := make(map[string]string)
	for k, v := range inst.Labels {
		labels[k] = v
	}

	// Parse machine type from full URL (e.g. zones/.../machineTypes/n1-standard-1)
	mt := ""
	if inst.MachineType != nil {
		parts := parseURLParts(*inst.MachineType)
		if len(parts) > 0 {
			mt = parts[len(parts)-1]
		}
	}

	region := zone
	if len(zone) > 2 {
		region = zone[:len(zone)-2] // strip '-a'
	}

	return &cloud.InstanceStatus{
		Instance: &cloud.Instance{
			ID:           instanceName,
			Provider:     a.Name(),
			InstanceType: mt,
			Region:       region,
			Zone:         zone,
			PublicIP:     publicIP,
			PrivateIP:    privateIP,
			State:        state,
			LaunchedAt:   launchedAt,
			Tags:         labels,
		},
		HealthCheck: state == cloud.InstanceStateRunning,
		Uptime:      uptime,
	}, nil
}

// TerminateInstance deletes a GCP instance.
func (a *Adapter) TerminateInstance(ctx context.Context, instanceID string, region string) error {
	instancesClient, err := compute.NewInstancesRESTClient(ctx, option.WithScopes("https://www.googleapis.com/auth/compute"))
	if err != nil {
		return fmt.Errorf("failed to create instances client: %w", err)
	}
	defer instancesClient.Close()

	zone := fmt.Sprintf("%s-a", region)

	req := &computepb.DeleteInstanceRequest{
		Project:  a.projectID,
		Zone:     zone,
		Instance: instanceID,
	}

	a.logger.Info().Str("instance_id", instanceID).Msg("Terminating GCP instance")
	
	op, err := instancesClient.Delete(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete GCP instance %s: %w", instanceID, err)
	}
	
	err = op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("wait for GCP instance deletion failed: %w", err)
	}

	return nil
}

// Utility functions for GCP pointers
func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// sanitizeLabelKey converts a tag key to a GCP-compliant label key.
// GCP labels must start with a lowercase letter and contain only lowercase letters,
// digits, underscores, and dashes.
func sanitizeLabelKey(key string) string {
	var result []byte
	for i, ch := range key {
		if ch >= 'A' && ch <= 'Z' {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, byte(ch-'A'+'a'))
		} else if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			result = append(result, byte(ch))
		}
	}
	if len(result) == 0 {
		return "label"
	}
	// Ensure it starts with a letter
	if result[0] >= '0' && result[0] <= '9' {
		result = append([]byte{'l', '_'}, result...)
	}
	return string(result)
}

func parseURLParts(url string) []string {
	var res []string
	for _, s := range split(url, '/') {
		if s != "" {
			res = append(res, s)
		}
	}
	return res
}

func split(s string, sep byte) []string {
	var parts []string
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[last:i])
			last = i + 1
		}
	}
	parts = append(parts, s[last:])
	return parts
}
