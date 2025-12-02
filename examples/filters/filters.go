package main

import (
	tg "github.com/amarnathcjd/gogram/telegram"
)

func main() {
	client, err := tg.NewClient(tg.ClientConfig{
		AppID:   6,
		AppHash: "YOUR_APP_HASH",
	})
	if err != nil {
		panic(err)
	}

	// ============================================
	// USING BITFLAGS DIRECTLY (fastest, cleanest)
	// ============================================

	// Single flag
	client.On("/start", startHandler, tg.F(tg.FPrivate))

	// Multiple flags with | (OR them together)
	client.On("message", mediaHandler, tg.F(tg.FGroup|tg.FMedia))
	client.On("/admin", adminHandler, tg.F(tg.FPrivate|tg.FCommand))

	// Photo OR Video in groups
	client.On("message", photoVideoHandler, tg.F(tg.FGroup), tg.Any(tg.IsPhoto, tg.IsVideo))

	// ============================================
	// SIMPLE FILTER VARIABLES (same as bitflags, just pre-wrapped)
	// ============================================

	client.On("/help", helpHandler, tg.IsPrivate)
	client.On("message", groupMediaHandler, tg.IsGroup, tg.IsMedia)

	// ============================================
	// PARAMETERIZED FILTERS
	// ============================================

	// Only from specific users
	client.On("message", vipHandler, tg.FromUser(123456, 789012))

	// Only in specific chats
	client.On("message", chatHandler, tg.InChat(-100123456789))

	// Text length constraints
	client.On("message", longTextHandler, tg.TextMinLen(10), tg.TextMaxLen(100))

	// ============================================
	// COMBINING FILTERS
	// ============================================

	// Any (OR logic)
	client.On("message", anyMediaHandler, tg.Any(tg.IsPhoto, tg.IsVideo, tg.FilterDocument))

	// Not - negate
	client.On("message", notForwardHandler, tg.Not(tg.IsForward))

	// Complex: Group + (Photo OR Video) + NOT forwarded
	client.On("message", complexHandler,
		tg.IsGroup,
		tg.Any(tg.IsPhoto, tg.IsVideo),
		tg.Not(tg.IsForward),
	)

	// ============================================
	// CUSTOM FILTER FUNCTIONS
	// ============================================

	// Inline custom
	client.On("message", customHandler, tg.Custom(func(m *tg.NewMessage) bool {
		return len(m.Text()) > 5 && m.Sender != nil && !m.Sender.Bot
	}))

	// Reusable custom filter
	var IsAdmin = tg.Custom(func(m *tg.NewMessage) bool {
		return m.SenderID() == 123456789 // your admin ID
	})
	client.On("/secret", secretHandler, IsAdmin)

	// ============================================
	// CHAINING (Builder pattern)
	// ============================================

	// Private + from specific users + min text length
	client.On("message", chainedHandler,
		tg.IsPrivate.FromUsers(123, 456).MinLen(10),
	)

	// Group + blacklist certain users
	client.On("message", blacklistHandler,
		tg.IsGroup.FromUsers(999, 888).Blacklist(),
	)

	client.Idle()
}

// Handler functions
func startHandler(m *tg.NewMessage) error {
	m.Reply("Welcome! This is a private message.")
	return nil
}

func helpHandler(m *tg.NewMessage) error {
	m.Reply("Help message")
	return nil
}

func mediaHandler(m *tg.NewMessage) error {
	m.Reply("Got media in group!")
	return nil
}

func adminHandler(m *tg.NewMessage) error {
	m.Reply("Admin command received")
	return nil
}

func photoVideoHandler(m *tg.NewMessage) error {
	m.Reply("Photo or video received!")
	return nil
}

func groupMediaHandler(m *tg.NewMessage) error {
	m.Reply("Media in group")
	return nil
}

func vipHandler(m *tg.NewMessage) error {
	m.Reply("Hello VIP user!")
	return nil
}

func chatHandler(m *tg.NewMessage) error {
	m.Reply("Message in specific chat")
	return nil
}

func longTextHandler(m *tg.NewMessage) error {
	m.Reply("Text between 10-100 chars")
	return nil
}

func anyMediaHandler(m *tg.NewMessage) error {
	m.Reply("Some media received")
	return nil
}

func notForwardHandler(m *tg.NewMessage) error {
	m.Reply("Not a forward")
	return nil
}

func complexHandler(m *tg.NewMessage) error {
	m.Reply("Group photo/video, not forwarded")
	return nil
}

func customHandler(m *tg.NewMessage) error {
	m.Reply("Custom filter passed!")
	return nil
}

func secretHandler(m *tg.NewMessage) error {
	m.Reply("Secret admin command")
	return nil
}

func chainedHandler(m *tg.NewMessage) error {
	m.Reply("Chained filter passed")
	return nil
}

func blacklistHandler(m *tg.NewMessage) error {
	m.Reply("You're not blacklisted!")
	return nil
}
