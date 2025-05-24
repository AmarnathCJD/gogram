package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/AmarnathCJD/gogram/internal/license"
)

func main() {
	customerID := flag.String("customer", "", "Customer ID or name")
	plan := flag.String("plan", "Standard", "License plan (Standard/Professional)")
	months := flag.Int("months", 12, "Validity period in months")

	flag.Parse()

	if *customerID == "" {
		fmt.Println("Error: Customer ID is required")
		flag.Usage()
		os.Exit(1)
	}

	key := license.GenerateLicenseKey(*customerID, *plan, *months)

	fmt.Printf("Generated License Key:\n%s\n\n", key)
	fmt.Printf("Customer: %s\nPlan: %s\nValidity: %d months\n",
		*customerID, *plan, *months)
	fmt.Println("\nNote: This is a dummy implementation for demonstration purposes.")
	fmt.Println("In a production system, you would implement proper cryptographic validation.")
}
