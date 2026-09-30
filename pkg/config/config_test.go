package config

import (
	"os"
	"testing"
)

func TestLoadFromEnv_GCP(t *testing.T) {
	// Save original env vars
	origProj := os.Getenv("BEACON_GCP_PROJECT")
	origReg := os.Getenv("BEACON_GCP_REGION")
	origZone := os.Getenv("BEACON_GCP_ZONE")
	defer func() {
		os.Setenv("BEACON_GCP_PROJECT", origProj)
		os.Setenv("BEACON_GCP_REGION", origReg)
		os.Setenv("BEACON_GCP_ZONE", origZone)
	}()

	// Set test values
	os.Setenv("BEACON_GCP_PROJECT", "test-project-123")
	os.Setenv("BEACON_GCP_REGION", "europe-west1")
	os.Setenv("BEACON_GCP_ZONE", "europe-west1-b")

	cfg := LoadFromEnv()

	if cfg.GCPProjectID != "test-project-123" {
		t.Errorf("Expected GCPProjectID test-project-123, got %s", cfg.GCPProjectID)
	}
	if cfg.GCPRegion != "europe-west1" {
		t.Errorf("Expected GCPRegion europe-west1, got %s", cfg.GCPRegion)
	}
	if cfg.GCPZone != "europe-west1-b" {
		t.Errorf("Expected GCPZone europe-west1-b, got %s", cfg.GCPZone)
	}
}

func TestDefaultConfig_GCP(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.GCPRegion != "us-central1" {
		t.Errorf("Expected default GCPRegion us-central1, got %s", cfg.GCPRegion)
	}
	if cfg.GCPZone != "us-central1-a" {
		t.Errorf("Expected default GCPZone us-central1-a, got %s", cfg.GCPZone)
	}
	if cfg.GCPProjectID != "" {
		t.Errorf("Expected default GCPProjectID to be empty, got %s", cfg.GCPProjectID)
	}
}
