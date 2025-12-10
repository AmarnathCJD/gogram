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
	// Choice buttons example
	client.On("command:choice", choiceExample)
	// Media request example
	client.On("command:media", mediaExample)
	// Yes/No question example
	client.On("command:confirm", confirmExample)
	// Advanced wizard with all features
	client.On("command:advanced", advancedWizardExample)
	// Abort example
	client.On("command:profile", profileWithAbortExample)

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
		AbortKeywords:   []string{"cancel", "quit"}, // Allow user to cancel
	})
	if err != nil {
		return err
	}
	defer conv.Close()

	// Create a wizard with multiple steps
	wizard := conv.Wizard()

	// Step 1: Name (no validation)
	wizard.Step("name", "👤 What's your full name?")

	// Step 2: Age (with number validation and transformation)
	wizard.Step("age", "🎂 How old are you?",
		telegram.WithValidator(func(m *telegram.NewMessage) bool {
			_, err := fmt.Sscanf(m.Text(), "%d", new(int))
			return err == nil
		}),
		telegram.WithRetryMessage("❌ Please enter a valid number for your age."),
		telegram.WithMaxRetries(3),
		telegram.WithTransform(func(m *telegram.NewMessage) any {
			var age int
			fmt.Sscanf(m.Text(), "%d", &age)
			return age
		}),
	)

	// Step 3: Email (with validation)
	wizard.Step("email", "📧 What's your email address?",
		telegram.WithValidator(func(m *telegram.NewMessage) bool {
			return strings.Contains(m.Text(), "@") && strings.Contains(m.Text(), ".")
		}),
		telegram.WithRetryMessage("❌ Please enter a valid email address."),
	)

	// Step 4: Bio (optional - skippable)
	wizard.Step("bio", "📝 Tell us a bit about yourself",
		telegram.WithSkip("skip", "pass"),
	)

	// Run the wizard
	answers, err := wizard.Run()
	if err != nil {
		if err == telegram.ErrConversationAborted {
			m.Reply("❌ Registration cancelled.")
			return nil
		}
		m.Reply(fmt.Sprintf("Registration failed: %v", err))
		return err
	}

	// Get answers (text)
	name := wizard.GetAnswerText("name")
	email := wizard.GetAnswerText("email")
	bio := wizard.GetAnswerText("bio")

	// Get transformed answer (as int)
	age := wizard.GetTransformed("age").(int)

	// Send confirmation
	bioText := bio
	if bioText == "" {
		bioText = "(not provided)"
	}

	summary := fmt.Sprintf(`✅ **Registration Complete!**

👤 **Name:** %s
🎂 **Age:** %d
📧 **Email:** %s
📝 **Bio:** %s

Thank you for registering!`, name, age, email, bioText)

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

// Advanced wizard example with progress, conditions, and media
func advancedWizardExample(m *telegram.NewMessage) error {
	conv, _ := m.Client.NewConversation(m.ChatID(), &telegram.ConversationOptions{
		Timeout:       180,
		AbortKeywords: []string{"cancel", "stop"},
	})
	defer conv.Close()

	wizard := conv.Wizard().
		WithProgress("📝 Step %d of %d").
		OnStepComplete(func(stepName string, stepNum int) {
			conv.Respond(fmt.Sprintf("✓ %s completed", stepName))
		})

	// Step 1: Ask if user wants to upload profile picture
	wizard.Step("upload_photo", "Do you want to upload a profile picture?",
		telegram.WithValidator(func(m *telegram.NewMessage) bool {
			text := strings.ToLower(m.Text())
			return text == "yes" || text == "no"
		}),
		telegram.WithTransform(func(m *telegram.NewMessage) any {
			return strings.ToLower(m.Text()) == "yes"
		}),
	)

	// Step 2: Conditional - only ask for photo if user said yes
	wizard.Step("photo", "📷 Please send your profile picture",
		telegram.ExpectPhoto(),
		telegram.WithCondition(func(answers map[string]*telegram.NewMessage) bool {
			return wizard.GetTransformed("upload_photo").(bool)
		}),
	)

	// Step 3: Ask for favorite color
	wizard.Step("color", "🎨 What's your favorite color?")

	// Step 4: Optional interests
	wizard.Step("interests", "Tell us your interests (or skip)",
		telegram.WithSkip(),
	)

	answers, err := wizard.Run()
	if err != nil {
		if err == telegram.ErrConversationAborted {
			m.Reply("Operation cancelled by user")
			return nil
		}
		return err
	}

	summary := "✅ Profile setup complete!\n\n"
	if wizard.HasAnswer("photo") {
		summary += "✓ Profile picture uploaded\n"
	}
	summary += fmt.Sprintf("✓ Favorite color: %s\n", wizard.GetAnswerText("color"))
	if wizard.HasAnswer("interests") {
		summary += fmt.Sprintf("✓ Interests: %s\n", wizard.GetAnswerText("interests"))
	}

	conv.Respond(summary)
	_ = answers
	return nil
}

// Profile creation with abort example
func profileWithAbortExample(m *telegram.NewMessage) error {
	conv, _ := m.Client.NewConversation(m.ChatID(), &telegram.ConversationOptions{
		AbortKeywords: []string{"cancel", "quit", "exit"},
	})
	defer conv.Close()

	m.Reply("Creating your profile... (type 'cancel' at any time to abort)")

	name, err := conv.Ask("What's your name?")
	if err != nil {
		if err == telegram.ErrConversationAborted {
			m.Reply("❌ Profile creation cancelled")
			return nil
		}
		return err
	}

	age, err := conv.Ask("How old are you?")
	if err != nil {
		if err == telegram.ErrConversationAborted {
			m.Reply("❌ Profile creation cancelled")
			return nil
		}
		return err
	}

	conv.Respond(fmt.Sprintf("✅ Profile created for %s, age %s", name.Text(), age.Text()))
	return nil
}
