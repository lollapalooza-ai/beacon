package main

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/api/cloudbilling/v1"
)

func main() {
	ctx := context.Background()
	billingService, err := cloudbilling.NewService(ctx)
	if err != nil {
		fmt.Println("Error creating billing service:", err)
		return
	}

	// Service ID for Compute Engine
	req := billingService.Services.Skus.List("services/6F81-5844-456A")
	
	fmt.Println("Fetching SKUs...")
	
	var a100Skus int
	err = req.Pages(ctx, func(page *cloudbilling.ListSkusResponse) error {
		for _, sku := range page.Skus {
			desc := strings.ToLower(sku.Description)
			if strings.Contains(desc, "a100") && strings.Contains(desc, "preemptible") {
				fmt.Printf("Found A100 Spot SKU: %s - %s\n", sku.SkuId, sku.Description)
				if len(sku.PricingInfo) > 0 && len(sku.PricingInfo[0].PricingExpression.TieredRates) > 0 {
				    rate := sku.PricingInfo[0].PricingExpression.TieredRates[0]
				    fmt.Printf("  Price: %v.%09d %s\n", rate.UnitPrice.Units, rate.UnitPrice.Nanos, rate.UnitPrice.CurrencyCode)
				}
				a100Skus++
			}
		}
		if a100Skus > 5 {
			// Stop pagination early once we find a few for testing
			return fmt.Errorf("found enough SKUs")
		}
		return nil
	})

	if err != nil && err.Error() != "found enough SKUs" {
		fmt.Println("Error listing SKUs:", err)
	}
}
