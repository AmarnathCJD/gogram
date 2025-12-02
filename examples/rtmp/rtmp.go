// RTMP Streaming Example for Telegram Group Calls
//
// This example demonstrates how to stream media to Telegram group calls using RTMP.
// It supports playing files, pausing, resuming, seeking, and stopping streams.
//
// Requirements:
// - FFmpeg must be installed and available in PATH
// - User account (userbot) is required for FetchRTMPURL() - bots cannot fetch RTMP URLs
// - The chat must have an active RTMP livestream enabled
//
// Usage:
//   .play <file_path>  - Start streaming a file
//   .pause             - Pause the stream
//   .resume            - Resume the stream
//   .stop              - Stop the stream
//   .seek <seconds>    - Seek to position
//   .pos               - Show current position
//   .rtmpurl           - Show RTMP URL and key

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
)

var streams = make(map[int64]*telegram.RTMPStream)

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:    6,
		AppHash:  "your_app_hash",
		LogLevel: telegram.LogInfo,
	})
	if err != nil {
		panic(err)
	}

	client.Conn()

	// Play command: .play <file_path>
	client.On("message:.play", func(m *telegram.NewMessage) error {
		args := strings.TrimPrefix(m.Text(), ".play ")
		if args == "" || args == ".play" {
			m.Reply("Usage: `.play <file_path>`")
			return nil
		}

		chatID := m.ChatID()
		stream, exists := streams[chatID]

		if !exists {
			var err error
			stream, err = client.NewRTMPStream(chatID)
			if err != nil {
				if errors.Is(err, telegram.ErrFFmpegNotFound) {
					_, err := m.Reply("❌ FFmpeg is not installed. Please install FFmpeg first.")
					return err
				}
				_, err := m.Reply(fmt.Sprintf("❌ Failed to create stream: %v", err))
				return err
			}

			// NOTE: FetchRTMPURL only works with user accounts, not bots
			if err := stream.FetchRTMPURL(); err != nil {
				_, replyErr := m.Reply(fmt.Sprintf("❌ Failed to fetch RTMP URL: %v\n\nNote: Bots cannot fetch RTMP URLs. Use a user account or set URL manually.", err))
				return replyErr
			}

			// Set error callback
			stream.OnError(func(err error) {
				client.SendMessage(chatID, fmt.Sprintf("⚠️ Stream error: %v", err))
			})

			streams[chatID] = stream
		}

		if err := stream.Play(args); err != nil {
			if errors.Is(err, telegram.ErrFileNotFound) {
				_, replyErr := m.Reply(fmt.Sprintf("❌ File not found: `%s`", args))
				return replyErr
			}
			if errors.Is(err, telegram.ErrStreamPlaying) {
				_, err := m.Reply("⚠️ Stream already playing. Use `.stop` first.")
				return err
			}
			if errors.Is(err, telegram.ErrNoRTMPURL) {
				_, err := m.Reply("❌ RTMP URL not set. Make sure the chat has RTMP streaming enabled.")
				return err
			}
			_, replyErr := m.Reply(fmt.Sprintf("❌ Failed to play: %v", err))
			return replyErr
		}

		m.Reply(fmt.Sprintf("▶️ Now playing: `%s`", args))
		return nil
	})

	// Pause command
	client.On("message:.pause", func(m *telegram.NewMessage) error {
		stream, exists := streams[m.ChatID()]
		if !exists {
			_, err := m.Reply("❌ No active stream in this chat")
			return err
		}

		if err := stream.Pause(); err != nil {
			_, replyErr := m.Reply(fmt.Sprintf("❌ %v", err))
			return replyErr
		}

		pos := stream.CurrentPosition()
		m.Reply(fmt.Sprintf("⏸️ Paused at %s", formatDuration(pos)))
		return nil
	})

	// Resume command
	client.On("message:.resume", func(m *telegram.NewMessage) error {
		stream, exists := streams[m.ChatID()]
		if !exists {
			_, err := m.Reply("❌ No active stream in this chat")
			return err
		}

		if err := stream.Resume(); err != nil {
			if errors.Is(err, telegram.ErrStreamNotPaused) {
				_, replyErr := m.Reply("⚠️ Stream is not paused")
				return replyErr
			}
			_, replyErr := m.Reply(fmt.Sprintf("❌ %v", err))
			return replyErr
		}

		m.Reply("▶️ Resumed playback")
		return nil
	})

	// Stop command
	client.On("message:.stop", func(m *telegram.NewMessage) error {
		stream, exists := streams[m.ChatID()]
		if !exists {
			_, err := m.Reply("❌ No active stream in this chat")
			return err
		}

		if err := stream.Stop(); err != nil {
			_, replyErr := m.Reply(fmt.Sprintf("❌ %v", err))
			return replyErr
		}

		delete(streams, m.ChatID())
		m.Reply("⏹️ Stream stopped")
		return nil
	})

	// Seek command: .seek <seconds>
	client.On("message:.seek", func(m *telegram.NewMessage) error {
		stream, exists := streams[m.ChatID()]
		if !exists {
			_, err := m.Reply("❌ No active stream in this chat")
			return err
		}

		args := strings.TrimPrefix(m.Text(), ".seek ")
		pos, err := parseDuration(args)
		if err != nil {
			_, replyErr := m.Reply("Usage: `.seek <seconds>` or `.seek mm:ss`")
			return replyErr
		}

		if err := stream.Seek(pos); err != nil {
			_, replyErr := m.Reply(fmt.Sprintf("❌ %v", err))
			return replyErr
		}

		m.Reply(fmt.Sprintf("⏩ Seeked to %s", formatDuration(pos)))
		return nil
	})

	// Position command
	client.On("message:.pos", func(m *telegram.NewMessage) error {
		stream, exists := streams[m.ChatID()]
		if !exists {
			_, err := m.Reply("❌ No active stream in this chat")
			return err
		}

		pos := stream.CurrentPosition()
		state := stream.State()

		m.Reply(fmt.Sprintf("📍 Position: %s\n📊 State: %s", formatDuration(pos), state))
		return nil
	})

	// Get RTMP URL command
	client.On("message:.rtmpurl", func(m *telegram.NewMessage) error {
		chatID := m.ChatID()
		stream, exists := streams[chatID]

		if !exists {
			var err error
			stream, err = client.NewRTMPStream(chatID)
			if err != nil {
				_, replyErr := m.Reply(fmt.Sprintf("❌ %v", err))
				return replyErr
			}

			if err := stream.FetchRTMPURL(); err != nil {
				_, replyErr := m.Reply(fmt.Sprintf("❌ Failed to fetch RTMP URL: %v", err))
				return replyErr
			}
			streams[chatID] = stream
		}

		m.Reply(fmt.Sprintf("🔗 **RTMP URL:**\n`%s`\n\n🔑 **Stream Key:**\n`%s`", stream.GetURL(), stream.GetKey()))
		return nil
	})

	// Set RTMP URL manually (for bots or custom URLs)
	// Usage: .setrtmp <url> <key>
	client.On("message:.setrtmp", func(m *telegram.NewMessage) error {
		args := strings.TrimPrefix(m.Text(), ".setrtmp ")
		parts := strings.Fields(args)

		if len(parts) != 2 {
			_, err := m.Reply("Usage: `.setrtmp <rtmp_url> <stream_key>`")
			return err
		}

		chatID := m.ChatID()
		stream, exists := streams[chatID]

		if !exists {
			var err error
			stream, err = client.NewRTMPStream(chatID)
			if err != nil {
				_, replyErr := m.Reply(fmt.Sprintf("❌ %v", err))
				return replyErr
			}
			streams[chatID] = stream
		}

		stream.SetURL(parts[0])
		stream.SetKey(parts[1])

		m.Reply("✅ RTMP URL and key set successfully")
		return nil
	})

	// Help command
	client.On("message:.rtmphelp", func(m *telegram.NewMessage) error {
		help := `**🎬 RTMP Stream Commands**

▶️ **Playback:**
• ` + "`.play <file>`" + ` - Start streaming
• ` + "`.pause`" + ` - Pause stream
• ` + "`.resume`" + ` - Resume stream
• ` + "`.stop`" + ` - Stop stream
• ` + "`.seek <time>`" + ` - Seek to position
• ` + "`.pos`" + ` - Show current position

🔗 **RTMP Settings:**
• ` + "`.rtmpurl`" + ` - Get RTMP URL and key
• ` + "`.setrtmp <url> <key>`" + ` - Set URL manually

⚠️ **Note:** Fetching RTMP URL only works with user accounts, not bots.`

		m.Reply(help)
		return nil
	})

	fmt.Println("RTMP streaming bot started!")
	client.Idle()
}

func formatDuration(d time.Duration) string {
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)

	// Try mm:ss format
	if strings.Contains(s, ":") {
		var mins, secs int
		if _, err := fmt.Sscanf(s, "%d:%d", &mins, &secs); err == nil {
			return time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second, nil
		}
	}

	// Try seconds
	var secs float64
	if _, err := fmt.Sscanf(s, "%f", &secs); err == nil {
		return time.Duration(secs * float64(time.Second)), nil
	}

	return 0, fmt.Errorf("invalid duration format")
}
