package aws

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/lollapalooza-ai/beacon/pkg/cloud"
	"github.com/rs/zerolog"
)

// instanceSpec holds hardware specifications for known EC2 instance types.
type instanceSpec struct {
	GPUType   string
	GPUCount  int
	VCPUs     int
	MemoryGiB float64
}

// instanceSpecs contains hardcoded specs for common GPU instances.
var instanceSpecs = map[string]instanceSpec{
	"p4d.24xlarge":  {GPUType: "A100", GPUCount: 8, VCPUs: 96, MemoryGiB: 1152},
	"p3.2xlarge":    {GPUType: "V100", GPUCount: 1, VCPUs: 8, MemoryGiB: 61},
	"p3.8xlarge":    {GPUType: "V100", GPUCount: 4, VCPUs: 32, MemoryGiB: 244},
	"p3.16xlarge":   {GPUType: "V100", GPUCount: 8, VCPUs: 64, MemoryGiB: 488},
	"g4dn.xlarge":   {GPUType: "T4", GPUCount: 1, VCPUs: 4, MemoryGiB: 16},
	"g4dn.12xlarge": {GPUType: "T4", GPUCount: 4, VCPUs: 48, MemoryGiB: 192},
	"g5.xlarge":     {GPUType: "A10G", GPUCount: 1, VCPUs: 4, MemoryGiB: 16},
	"g5.12xlarge":   {GPUType: "A10G", GPUCount: 4, VCPUs: 48, MemoryGiB: 192},
	"g5.48xlarge":   {GPUType: "A10G", GPUCount: 8, VCPUs: 192, MemoryGiB: 768},
}

// Adapter implements the cloud.Adapter interface for AWS EC2.
type Adapter struct {
	cfg    aws.Config
	logger zerolog.Logger
}

// NewAdapter creates a new AWS EC2 cloud adapter.
func NewAdapter(cfg aws.Config, logger zerolog.Logger) *Adapter {
	return &Adapter{
		cfg:    cfg,
		logger: logger,
	}
}

// Name returns the provider name.
func (a *Adapter) Name() string {
	return "aws"
}

// QuerySpotPrices retrieves current spot instance pricing across specified regions.
func (a *Adapter) QuerySpotPrices(ctx context.Context, req *cloud.SpotPriceRequest) ([]cloud.SpotPrice, error) {
	var prices []cloud.SpotPrice
	now := time.Now()

	regions := req.Regions
	if len(regions) == 0 {
		regions = []string{a.cfg.Region}
	}

	for _, region := range regions {
		client := ec2.NewFromConfig(a.cfg, func(o *ec2.Options) { o.Region = region })

		var instanceTypes []types.InstanceType
		for _, it := range req.InstanceTypes {
			instanceTypes = append(instanceTypes, types.InstanceType(it))
		}

		input := &ec2.DescribeSpotPriceHistoryInput{
			InstanceTypes:       instanceTypes,
			ProductDescriptions: []string{"Linux/UNIX"},
			StartTime:           aws.Time(now),
		}

		paginator := ec2.NewDescribeSpotPriceHistoryPaginator(client, input)
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to describe spot prices in %s: %w", region, err)
			}
			for _, sp := range page.SpotPriceHistory {
				priceStr := aws.ToString(sp.SpotPrice)
				price, err := strconv.ParseFloat(priceStr, 64)
				if err != nil {
					continue
				}

				spec, ok := instanceSpecs[string(sp.InstanceType)]
				var gpuType string
				var gpuCount, vcpus int
				var memory float64
				if ok {
					gpuType = spec.GPUType
					gpuCount = spec.GPUCount
					vcpus = spec.VCPUs
					memory = spec.MemoryGiB
				}

				prices = append(prices, cloud.SpotPrice{
					Provider:     "aws",
					Region:       region,
					Zone:         aws.ToString(sp.AvailabilityZone),
					InstanceType: string(sp.InstanceType),
					PricePerHour: price,
					GPUType:      gpuType,
					GPUCount:     gpuCount,
					VCPUs:        vcpus,
					MemoryGiB:    memory,
					Timestamp:    aws.ToTime(sp.Timestamp),
				})
			}
		}
	}

	return prices, nil
}

// ProvisionInstance launches a new spot instance.
func (a *Adapter) ProvisionInstance(ctx context.Context, req *cloud.ProvisionRequest) (*cloud.Instance, error) {
	client := ec2.NewFromConfig(a.cfg, func(o *ec2.Options) { o.Region = req.Region })

	marketOptions := &types.InstanceMarketOptionsRequest{
		MarketType: types.MarketTypeSpot,
		SpotOptions: &types.SpotMarketOptions{
			MaxPrice: aws.String(fmt.Sprintf("%f", req.MaxPrice)),
		},
	}

	var tags []types.Tag
	for k, v := range req.Tags {
		tags = append(tags, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	var tagSpecs []types.TagSpecification
	if len(tags) > 0 {
		tagSpecs = append(tagSpecs, types.TagSpecification{
			ResourceType: types.ResourceTypeInstance,
			Tags:         tags,
		})
	}

	input := &ec2.RunInstancesInput{
		ImageId:               aws.String(req.ImageID),
		InstanceType:          types.InstanceType(req.InstanceType),
		MinCount:              aws.Int32(1),
		MaxCount:              aws.Int32(1),
		InstanceMarketOptions: marketOptions,
		TagSpecifications:     tagSpecs,
	}

	if req.KeyName != "" {
		input.KeyName = aws.String(req.KeyName)
	}
	if req.UserData != "" {
		input.UserData = aws.String(req.UserData) // assuming base64 encoded based on comments
	}
	if req.Zone != "" {
		input.Placement = &types.Placement{
			AvailabilityZone: aws.String(req.Zone),
		}
	}
	if req.StorageGiB > 0 {
		input.BlockDeviceMappings = []types.BlockDeviceMapping{
			{
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &types.EbsBlockDevice{
					VolumeSize: aws.Int32(int32(req.StorageGiB)),
					VolumeType: types.VolumeTypeGp3,
				},
			},
		}
	}

	out, err := client.RunInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to run instances: %w", err)
	}
	if len(out.Instances) == 0 {
		return nil, fmt.Errorf("no instances returned by RunInstances")
	}

	instanceID := aws.ToString(out.Instances[0].InstanceId)
	a.logger.Info().Str("instance_id", instanceID).Msg("Waiting for instance to reach running state")

	waiter := ec2.NewInstanceRunningWaiter(client)
	waitErr := waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}, 5*time.Minute)

	if waitErr != nil {
		a.logger.Warn().Err(waitErr).Str("instance_id", instanceID).Msg("Wait for instance running failed or timed out")
	}

	status, err := a.GetInstanceStatus(ctx, instanceID, req.Region)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance status post-provision: %w", err)
	}

	// Attach spot price requested
	status.Instance.SpotPrice = req.MaxPrice

	return status.Instance, nil
}

// GetInstanceStatus retrieves the current status of an EC2 instance.
func (a *Adapter) GetInstanceStatus(ctx context.Context, instanceID string, region string) (*cloud.InstanceStatus, error) {
	client := ec2.NewFromConfig(a.cfg, func(o *ec2.Options) { o.Region = region })
	out, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe instance %s: %w", instanceID, err)
	}

	if len(out.Reservations) == 0 || len(out.Reservations[0].Instances) == 0 {
		return nil, fmt.Errorf("instance %s not found", instanceID)
	}

	inst := out.Reservations[0].Instances[0]

	state := cloud.InstanceStateUnknown
	switch inst.State.Name {
	case types.InstanceStateNamePending:
		state = cloud.InstanceStatePending
	case types.InstanceStateNameRunning:
		state = cloud.InstanceStateRunning
	case types.InstanceStateNameShuttingDown, types.InstanceStateNameStopping:
		state = cloud.InstanceStateTerminating
	case types.InstanceStateNameTerminated:
		state = cloud.InstanceStateTerminated
	case types.InstanceStateNameStopped:
		state = cloud.InstanceStateStopped
	}

	tags := make(map[string]string)
	for _, t := range inst.Tags {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}

	var uptime float64
	launchedAt := aws.ToTime(inst.LaunchTime)
	if !launchedAt.IsZero() && state == cloud.InstanceStateRunning {
		uptime = time.Since(launchedAt).Hours()
	}

	zone := ""
	if inst.Placement != nil {
		zone = aws.ToString(inst.Placement.AvailabilityZone)
	}

	return &cloud.InstanceStatus{
		Instance: &cloud.Instance{
			ID:           aws.ToString(inst.InstanceId),
			Provider:     "aws",
			InstanceType: string(inst.InstanceType),
			Region:       region,
			Zone:         zone,
			PublicIP:     aws.ToString(inst.PublicIpAddress),
			PrivateIP:    aws.ToString(inst.PrivateIpAddress),
			State:        state,
			LaunchedAt:   launchedAt,
			Tags:         tags,
		},
		HealthCheck: state == cloud.InstanceStateRunning,
		Uptime:      uptime,
	}, nil
}

// TerminateInstance terminates a running instance.
func (a *Adapter) TerminateInstance(ctx context.Context, instanceID string, region string) error {
	client := ec2.NewFromConfig(a.cfg, func(o *ec2.Options) { o.Region = region })
	_, err := client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return fmt.Errorf("failed to terminate instance %s: %w", instanceID, err)
	}
	return nil
}
