package license

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidLicense = errors.New("invalid license key")
	ErrExpiredLicense = errors.New("license key has expired")
)

// LicenseInfo contains license information
type LicenseInfo struct {
	IsValid    bool
	ExpiryDate time.Time
	Plan       string
}

// ValidateLicense checks if the provided license key is valid
func ValidateLicense(licenseKey string) (*LicenseInfo, error) {
	if licenseKey == "" {
		return nil, ErrInvalidLicense
	}

	// For a real implementation, you'd want to do more sophisticated validation,
	// possibly involving cryptographic signatures or API calls to a license server
	// This is just a dummy implementation for demonstration purposes

	// Simple validation: check if the key has the expected format
	if len(licenseKey) < 32 {
		return nil, ErrInvalidLicense
	}

	// In a real system, you'd decode information from the license key
	// For this dummy version, we'll just extract some fake data

	// Let's say the first 8 chars represent an expiry date encoded somehow
	// Just a dummy check - in reality you'd have proper encoding/decoding
	expiryEncoded := licenseKey[:8]

	// Simple check: the key should start with "GOGRAM-"
	if licenseKey[:7] != "GOGRAM-" {
		return nil, ErrInvalidLicense
	}

	// Dummy implementation: derive expiry date from the key
	// In a real system, this would be properly encoded in the key
	now := time.Now()
	expiry := now.AddDate(1, 0, 0) // Default to one year from now

	// Check if license has expired based on the derived date
	if now.After(expiry) {
		return nil, ErrExpiredLicense
	}

	// Determine the plan from the key
	plan := "Standard"
	if len(licenseKey) > 40 {
		plan = "Professional"
	}

	return &LicenseInfo{
		IsValid:    true,
		ExpiryDate: expiry,
		Plan:       plan,
	}, nil
}

// GenerateLicenseKey creates a dummy license key (for admin use)
func GenerateLicenseKey(customerID string, planType string, validityMonths int) string {
	// This is a very basic implementation
	// In a real system, you'd want to use proper cryptographic signing

	timestamp := time.Now().Unix()
	expiryTime := time.Now().AddDate(0, validityMonths, 0)
	expiryStr := expiryTime.Format("20060102")

	// Create a basic hash combining customer info and expiry
	data := fmt.Sprintf("%s-%s-%d-%s", customerID, planType, timestamp, expiryStr)
	hash := sha256.Sum256([]byte(data))
	hashStr := hex.EncodeToString(hash[:])

	// Format: GOGRAM-<first 8 chars of hash>-<customer id>-<expiry>
	return fmt.Sprintf("GOGRAM-%s-%s-%s", hashStr[:8], customerID, expiryStr)
}
