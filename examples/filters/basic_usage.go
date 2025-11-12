package main

import (
	"fmt"
	"strings"

	"github.com/amarnathcjd/gogram/telegram"
)

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   6,
		AppHash: "YOUR_APP_HASH",
	})
	if err != nil {
		panic(err)
	}

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Message from private chat!")
		return nil
	}, telegram.FilterPrivate)

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Incoming photo from group!")
		return nil
	}, telegram.NewFilter().IsGroup().WithPhoto().IsIncoming())

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Photo from private incoming!")
		return nil
	}, telegram.FilterPrivate, telegram.FilterPhoto, telegram.FilterIncoming)

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Hello, VIP user!")
		return nil
	}, telegram.NewFilter().FromUsers(123456789, 987654321))

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Perfect message length!")
		return nil
	}, telegram.NewFilter().MinLen(10).MaxLen(500))

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply(fmt.Sprintf("You sent: %s", m.MediaType()))
		return nil
	}, telegram.NewFilter().WithMediaTypes("photo", "video", "animation"))

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("I see you edited your message! ✏️")
		return nil
	}, telegram.NewFilter().IsEdited())

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Welcome!")
		return nil
	}, telegram.NewFilter().FromUsers(111111111, 222222222).AsBlacklist())

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Help keyword detected!")
		return nil
	}, telegram.NewFilter().Custom(func(m *telegram.NewMessage) bool {
		text := strings.ToLower(m.Text())
		return strings.Contains(text, "help") || strings.Contains(text, "support")
	}))

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("All conditions matched!")
		return nil
	}, telegram.NewFilter().
		IsPrivate().
		IsIncoming().
		WithText().
		MinLen(5).
		MaxLen(200).
		Custom(func(m *telegram.NewMessage) bool {
			return m.Sender == nil || !m.Sender.Bot
		}))

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("From public chat!")
		return nil
	}, telegram.FilterOr(
		telegram.NewFilter().IsGroup(),
		telegram.NewFilter().IsChannel(),
	))

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Not a sticker!")
		return nil
	}, telegram.FilterNot(telegram.NewFilter().WithSticker()))

	client.AddCallbackHandler(telegram.OnCallbackQuery, func(c *telegram.CallbackQuery) error {
		c.Answer("Button clicked in private!", &telegram.CallbackOptions{Alert: true})
		return nil
	}, telegram.NewFilter().IsPrivate())

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Audio content! 🎵")
		return nil
	}, telegram.FilterOr(
		telegram.NewFilter().WithVoice(),
		telegram.NewFilter().WithAudio(),
	))

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Forwarded message detected")
		return nil
	}, telegram.NewFilter().IsForward())

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Thanks for replying!")
		return nil
	}, telegram.NewFilter().IsReply().WithText().MinLen(5))

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		fmt.Println("I sent:", m.Text())
		return nil
	}, telegram.NewFilter().IsOutgoing())

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Contact received! 👤")
		return nil
	}, telegram.NewFilter().WithContact())

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Poll detected! 📊")
		return nil
	}, telegram.NewFilter().IsGroup().WithPoll())

	client.AddMessageHandler(telegram.OnNewMessage, func(m *telegram.NewMessage) error {
		m.Reply("Message from our special chat!")
		return nil
	}, telegram.NewFilter().FromChats(-1001234567890))

	client.Idle()
}
