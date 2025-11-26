package main

import (
	"fmt"
	"strings"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	// create a new client object
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
	})

	client.LoginBot(botToken)

	// Handle /start command with conversation wizard
	client.On("command:register", registrationHandler)

	// Simple conversation example
	client.On("command:ask", simpleConversationHandler)

	client.Idle()
}

// Simple conversation example
func simpleConversationHandler(m *telegram.NewMessage) error {
	conv, err := m.Client.NewConversation(m.ChatID(), &telegram.ConversationOptions{
		Timeout: 60,
		Private: true,
	})
	if err != nil {
		return err
	}
	defer conv.Close()

	// Ask and wait for response
	response, err := conv.Ask("What's your name?")
	if err != nil {
		m.Reply("Conversation timed out!")
		return err
	}

	m.Reply(fmt.Sprintf("Nice to meet you, %s!", response.Text()))
	return nil
}

// Registration wizard using Conversation Wizard
func registrationHandler(m *telegram.NewMessage) error {
	conv, err := m.Client.NewConversation(m.ChatID(), &telegram.ConversationOptions{
		Timeout:         120, // 2 minutes per step
		Private:         true,
		StopPropagation: true,
	})
	if err != nil {
		return err
	}
	defer conv.Close()

	// Create a wizard with multiple steps
	wizard := conv.Wizard()

	// Step 1: Name (no validation)
	wizard.Step("name", "👤 What's your full name?")

	// Step 2: Age (with number validation)
	wizard.Step("age", "🎂 How old are you?",
		telegram.WithValidator(func(m *telegram.NewMessage) bool {
			_, err := fmt.Sscanf(m.Text(), "%d", new(int))
			return err == nil
		}),
		telegram.WithRetryMessage("❌ Please enter a valid number for your age."),
		telegram.WithMaxRetries(3),
	)

	// Step 3: Email (with validation)
	wizard.Step("email", "📧 What's your email address?",
		telegram.WithValidator(func(m *telegram.NewMessage) bool {
			return strings.Contains(m.Text(), "@") && strings.Contains(m.Text(), ".")
		}),
		telegram.WithRetryMessage("❌ Please enter a valid email address."),
	)

	// Step 4: Bio (optional, no validation)
	wizard.Step("bio", "📝 Tell us a bit about yourself (short bio):")

	// Run the wizard
	answers, err := wizard.Run()
	if err != nil {
		m.Reply(fmt.Sprintf("Registration failed: %v", err))
		return err
	}

	// Get all answers
	name := wizard.GetAnswerText("name")
	age := wizard.GetAnswerText("age")
	email := wizard.GetAnswerText("email")
	bio := wizard.GetAnswerText("bio")

	// Send confirmation
	summary := fmt.Sprintf(`✅ **Registration Complete!**

👤 **Name:** %s
🎂 **Age:** %s
📧 **Email:** %s
📝 **Bio:** %s

Thank you for registering!`, name, age, email, bio)

	conv.Respond(summary, &telegram.SendOptions{ParseMode: "markdown"})

	// You can also access the original message objects
	_ = answers // map[string]*telegram.NewMessage

	return nil
}

// Example: Using Choice buttons
func choiceExample(m *telegram.NewMessage) error {
	conv, _ := m.Client.NewConversation(m.ChatID())
	defer conv.Close()

	// Send buttons and wait for click
	callback, err := conv.Choice("What's your favorite color?", []string{
		"🔴 Red",
		"🟢 Green",
		"🔵 Blue",
	})
	if err != nil {
		return err
	}

	conv.Respond(fmt.Sprintf("You chose: %s", string(callback.Data)))
	return nil
}

// Example: Ask for specific media
func mediaExample(m *telegram.NewMessage) error {
	conv, _ := m.Client.NewConversation(m.ChatID())
	defer conv.Close()

	// Ask for a photo
	photo, err := conv.AskPhoto("Please send me your profile picture 📷")
	if err != nil {
		m.Reply("No photo received!")
		return err
	}

	conv.Respond("Got your photo! 👍")

	// Download the photo
	_ = photo.Photo()

	return nil
}

// Example: Yes/No question
func confirmExample(m *telegram.NewMessage) error {
	conv, _ := m.Client.NewConversation(m.ChatID())
	defer conv.Close()

	confirmed, err := conv.AskYesNo("Do you want to proceed? (yes/no)")
	if err != nil {
		return err
	}

	if confirmed {
		conv.Respond("Great! Proceeding...")
	} else {
		conv.Respond("Cancelled.")
	}

	return nil
}
