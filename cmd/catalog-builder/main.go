package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/api/cloudbilling/v1"
)

// CatalogFile matches the struct in the GCP adapter
type CatalogFile struct {
	UpdatedAt time.Time               `json:"updated_at"`
	Instances map[string]instanceSpec `json:"instances"`
}

type instanceSpec struct {
	GPUType   string  `json:"gpu_type"`
	GPUCount  int     `json:"gpu_count"`
	VCPUs     int     `json:"vcpus"`
	MemoryGiB float64 `json:"memory_gib"`
	EstPrice  float64 `json:"spot_price"`
}

func main() {
	fmt.Println("GCP Spot Pricing Catalog Builder")
	fmt.Println("NOTE: This requires 'cloudbilling.googleapis.com' to be enabled on your GCP project.")
	
	ctx := context.Background()
	billingService, err := cloudbilling.NewService(ctx)
	if err != nil {
		fmt.Printf("Error creating billing service: %v\n", err)
		os.Exit(1)
	}

	req := billingService.Services.Skus.List("services/6F81-5844-456A") // Compute Engine Service ID
	
	fmt.Println("Fetching SKUs from GCP Cloud Billing API (this may take a few minutes)...")
	
	// In a complete implementation, this script would:
	// 1. Paginate through all 100,000+ Compute SKUs.
	// 2. Filter for UsageType = "Preemptible" (which covers Spot).
	// 3. Extract the TieredRates for CPU, RAM, and GPU.
	// 4. Construct the composite price for families like A2, G2, N1, etc.
	//
	// Because of the complexity and API quotas, this reference script provides the framework.
	// We simulate pulling the A100 GPU Preemptible rate as an example of SKU processing.

	var a100PreemptiblePrice float64
	var a2CorePrice float64
	var a2RamPrice float64

	err = req.Pages(ctx, func(page *cloudbilling.ListSkusResponse) error {
		for _, sku := range page.Skus {
			desc := strings.ToLower(sku.Description)
			
			if len(sku.PricingInfo) == 0 || len(sku.PricingInfo[0].PricingExpression.TieredRates) == 0 {
				continue
			}
			rate := sku.PricingInfo[0].PricingExpression.TieredRates[0]
			price := float64(rate.UnitPrice.Units) + float64(rate.UnitPrice.Nanos)/1e9

			if strings.Contains(desc, "a100") && strings.Contains(desc, "preemptible") && strings.Contains(desc, "americas") {
				a100PreemptiblePrice = price
			}
			if strings.Contains(desc, "a2 instance core") && strings.Contains(desc, "preemptible") && strings.Contains(desc, "americas") {
				a2CorePrice = price
			}
			if strings.Contains(desc, "a2 instance ram") && strings.Contains(desc, "preemptible") && strings.Contains(desc, "americas") {
				a2RamPrice = price
			}
		}
		
		// If we found the components, we can stop for this demo script
		if a100PreemptiblePrice > 0 && a2CorePrice > 0 && a2RamPrice > 0 {
			return fmt.Errorf("found components")
		}
		return nil
	})

	if err != nil && err.Error() != "found components" {
		if strings.Contains(err.Error(), "SERVICE_DISABLED") {
			fmt.Println("\n❌ Cloud Billing API is disabled. You must enable it in your GCP project.")
			fmt.Println("To enable: https://console.cloud.google.com/apis/api/cloudbilling.googleapis.com/overview")
			os.Exit(1)
		}
		fmt.Printf("Error fetching SKUs: %v\n", err)
	}

	// Calculate a2-highgpu-4g as an example if we successfully fetched the API rates
	var generatedCatalog CatalogFile
	if a100PreemptiblePrice > 0 {
		fmt.Printf("Successfully fetched live pricing components:\n")
		fmt.Printf("  A100 GPU: $%.4f/hr\n", a100PreemptiblePrice)
		fmt.Printf("  A2 vCPU:  $%.4f/hr\n", a2CorePrice)
		fmt.Printf("  A2 RAM:   $%.4f/hr/GB\n", a2RamPrice)
		
		a2HighGpu4gPrice := (a100PreemptiblePrice * 4) + (a2CorePrice * 48) + (a2RamPrice * 340)
		fmt.Printf("\nCalculated a2-highgpu-4g live spot price: $%.2f/hr\n", a2HighGpu4gPrice)
		
		// Build dynamic map
		generatedCatalog = CatalogFile{
			UpdatedAt: time.Now().UTC(),
			Instances: map[string]instanceSpec{
				"a2-highgpu-4g": {
					GPUType: "A100", GPUCount: 4, VCPUs: 48, MemoryGiB: 340, EstPrice: a2HighGpu4gPrice,
				},
			},
		}
	} else {
		fmt.Println("\nFalling back to default offline catalog generation...")
		// Fallback for demonstration if API fails or is disabled
		generatedCatalog = CatalogFile{
			UpdatedAt: time.Now().UTC(),
			Instances: map[string]instanceSpec{
				"a2-highgpu-1g": {GPUType: "A100", GPUCount: 1, VCPUs: 12, MemoryGiB: 85, EstPrice: 1.10},
				"a2-highgpu-2g": {GPUType: "A100", GPUCount: 2, VCPUs: 24, MemoryGiB: 170, EstPrice: 2.20},
				"a2-highgpu-4g": {GPUType: "A100", GPUCount: 4, VCPUs: 48, MemoryGiB: 340, EstPrice: 4.40},
				"a2-highgpu-8g": {GPUType: "A100", GPUCount: 8, VCPUs: 96, MemoryGiB: 680, EstPrice: 8.80},
				"g2-standard-4": {GPUType: "L4", GPUCount: 1, VCPUs: 4, MemoryGiB: 16, EstPrice: 0.25},
			},
		}
	}

	catalogPath := "pkg/cloud/gcp/catalog.json"
	
	fileData, err := json.MarshalIndent(generatedCatalog, "", "  ")
	if err != nil {
		fmt.Printf("Error marshalling catalog: %v\n", err)
		os.Exit(1)
	}

	err = os.WriteFile(catalogPath, fileData, 0644)
	if err != nil {
		fmt.Printf("Error writing %s: %v\n", catalogPath, err)
		os.Exit(1)
	}

	fmt.Printf("\nSuccessfully updated %s\n", catalogPath)
}
