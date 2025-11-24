package main

import (
	"fmt"
	"os"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID   = 6
	appHash = "YOUR_APP_HASH"
)

func main() {
	// Create a new client
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
	})
	if err != nil {
		panic(err)
	}

	// Connect to the server
	if err := client.Connect(); err != nil {
		panic(err)
	}

	// Initiate QR code login
	qr, err := client.QRLogin(telegram.QrOptions{
		// Optional: Custom password callback for 2FA
		PasswordCallback: func() (string, error) {
			fmt.Print("Enter 2FA password: ")
			var password string
			fmt.Scanln(&password)
			return password, nil
		},
		// Optional: Called when wrong password is entered
		OnWrongPassword: func(attempt, maxRetries int) bool {
			fmt.Printf("Wrong password! Attempt %d of %d\n", attempt, maxRetries)
			return true // return false to abort
		},
		Timeout:    60, // QR login timeout in seconds
		MaxRetries: 3,  // Max password retry attempts
	})
	if err != nil {
		panic(err)
	}

	// Print QR code URL (can be used to generate QR elsewhere)
	fmt.Println("QR Code URL:", qr.Url())

	// Print QR code directly to console (ASCII art)
	qr.PrintToConsole()

	// Export QR code as PNG image
	png, err := qr.ExportAsPng()
	if err != nil {
		fmt.Println("Failed to export QR as PNG:", err)
	} else {
		if err := os.WriteFile("qr_login.png", png, 0644); err != nil {
			fmt.Println("Failed to save QR PNG:", err)
		} else {
			fmt.Println("QR code saved to qr_login.png")
		}
	}

	// Wait for the user to scan the QR code (timeout in seconds)
	fmt.Println("\nScan the QR code with your Telegram app...")
	if err := qr.WaitLogin(60); err != nil {
		panic(err)
	}

	fmt.Println("Login successful!")

	// Get user info
	me, err := client.GetMe()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Logged in as: %s (@%s)\n", me.FirstName, me.Username)
}
