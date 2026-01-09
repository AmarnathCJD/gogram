package main

import (
	"fmt"
	"time"

	tg "github.com/amarnathcjd/gogram/telegram"
)

func main() {
	client, _ := tg.NewClient(tg.ClientConfig{
		AppID:   6,
		AppHash: "",
	})

	// --- Basic Get/Set ---
	client.Data.Set("bot_status", "running")
	fmt.Println(client.Data.GetString("bot_status", "unknown"))

	// --- TTL-based expiration ---
	client.Data.SetWithTTL("temp_token", "abc123", 5*time.Minute)

	// --- Scoped by chat/user ID ---
	chatID := int64(123456789)
	client.Data.SetScoped(chatID, "step", 1)
	client.Data.SetScoped(chatID, "awaiting_reply", true)

	step := client.Data.GetScoped(chatID, "step")
	fmt.Printf("Chat %d is on step: %v\n", chatID, step)

	// --- Atomic counter ---
	count := client.Data.IncrementScoped(chatID, "message_count")
	fmt.Printf("Message count: %d\n", count)

	// --- Type-safe generic access ---
	client.Data.Set("user_settings", map[string]any{"theme": "dark", "lang": "en"})
	if settings, ok := tg.GetTyped[map[string]any](client.Data, "user_settings"); ok {
		fmt.Printf("Theme: %s\n", settings["theme"])
	}

	// --- Clean up all data for a chat ---
	client.Data.DeleteByScope(chatID)

	// --- Example in an update handler ---
	client.On(tg.OnMessage, func(m *tg.NewMessage) error {
		// Track conversation state per chat
		if m.Text() == "/start" {
			client.Data.SetScoped(m.ChatID(), "state", "awaiting_name")
			m.Reply("What's your name?")
			return nil
		}

		state := client.Data.GetString(fmt.Sprintf("%d:state", m.ChatID()), "")
		if state == "awaiting_name" {
			client.Data.SetScoped(m.ChatID(), "name", m.Text())
			client.Data.DeleteScoped(m.ChatID(), "state")
			m.Reply("Nice to meet you, " + m.Text() + "!")
		}
		return nil
	})

	client.Idle()
}
