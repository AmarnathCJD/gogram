package main

import (
	"fmt"
	"os"

	"github.com/AmarnathCJD/gogram/internal/config"
	"github.com/AmarnathCJD/gogram/internal/license"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// Check license before starting the application
	if err := checkLicense(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "License error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please enter a valid license key in your configuration.\n")
		os.Exit(1)
	}

	// ...existing code to start the application...
}

func checkLicense(cfg *config.Config) error {
	licenseInfo, err := license.ValidateLicense(cfg.LicenseKey)
	if err != nil {
		return err
	}

	fmt.Printf("License valid. Plan: %s, Expires: %s\n",
		licenseInfo.Plan,
		licenseInfo.ExpiryDate.Format("2006-01-02"))

	return nil
}

// ...existing functions and code...
