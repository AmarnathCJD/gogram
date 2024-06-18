<p align="center">
    <a href="https://github.com/amarnathcjd/gogram">
        <img src="https://i.imgur.com/RE1M0sM.png" alt="Gogram" width="256">
    </a>
    <br>
    <b>Telegram MTProto API Framework for Golang</b>
    <br>
    <b>
    <a href="/">
        HOME
    </a>
    •
    <a href="/examples/">
        DOCS
    </a>
    •
    <a href="https://github.com/amarnathcjd/gogram/releases">
        RELEASES
    </a>
    •
    <a href="https://t.me/rosexchat">
        SUPPORT
    </a>
    </b>
</p>

## <b>GoGram</b>

<p>Light Weight, Fast, Elegant Telegram <b><a href="https://core.telegram.org/api">MTProto API</a></b> framework in <b><a href="https://golang.org/">Golang</a></b> for building Telegram clients and bots.</p>

## Status

[![GoDoc](https://godoc.org/github.com/amarnathcjd/gogram?status.svg)](https://godoc.org/github.com/amarnathcjd/gogram)
[![Go Report Card](https://goreportcard.com/badge/github.com/amarnathcjd/gogram)](https://goreportcard.com/report/github.com/amarnathcjd/gogram)
[![License](https://img.shields.io/github/license/amarnathcjd/gogram.svg)](https://img.shields.io/github/license/amarnathcjd/gogram.svg)
[![GitHub stars](https://img.shields.io/github/stars/amarnathcjd/gogram.svg?style=social&label=Stars)](https://img.shields.io/github/license/amarnathcjd/gogram.svg?style=social&label=Stars)
[![GitHub forks](https://img.shields.io/github/forks/amarnathcjd/gogram.svg?style=social&label=Fork)](https://img.shields.io/github/license/amarnathcjd/gogram.svg?style=social&label=Fork)
[![GitHub issues](https://img.shields.io/github/issues/amarnathcjd/gogram.svg)](https://img.shields.io/github/license/amarnathcjd/gogram.svg)
[![GitHub pull requests](https://img.shields.io/github/issues-pr/amarnathcjd/gogram.svg)](https://img.shields.io/github/license/amarnathcjd/gogram.svg)

<p>⭐️ <b>Gogram</b> is a modern, elegant and concurrent <b><a href='https://core.telegram.org/api'>MTProto API</a></b>
framework. It enables you to easily interact with the main Telegram API through a user account (custom client) or a bot
identity (bot API alternative) using Go.</p>

## Setup

<p>Please note that Gogram requires Go <b>1.18</b> or later.</p>

```bash
go get -u github.com/amarnathcjd/gogram/telegram
```

## Getting Started

```golang
package main

import "github.com/amarnathcjd/gogram/telegram"

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID: 6, AppHash: "<app-hash>",
		// StringSession: "<string-session>",
	})

	if err != nil {
		log.Fatal(err)
	}

	client.ConnectBot("<bot-token>") // or client.Login("<phone-number>") for user account, or client.AuthPrompt() for interactive login

	client.AddMessageHandler(telegram.OnNewMessage, func(message *telegram.NewMessage) error {
		if message.IsPrivate() {
			message.Reply("Hello from Gogram!")
			return nil
		}

        return nil
	})

	client.Idle() // block main goroutine until client is closed
}
```

## Support

If you'd like to support Gogram, you can consider:

- [Become a GitHub sponsor](https://github.com/sponsors/amarnathcjd).

## Key Features

- **Ready**: Install Gogram with go get and you are ready to go!
- **Easy**: Makes the Telegram API simple and intuitive, while still allowing advanced usages.
- **Elegant**: Low-level details are abstracted and re-presented in a more convenient way.
- **Fast**: Backed by a powerful and concurrent library, Gogram can handle even the heaviest workloads.
- **Zero Dependencies**: No need to install anything else than Gogram itself.
- **Powerful**: Full access to Telegram's API to execute any official client action and more.
- **Feature-Rich**: Built-in support for file uploading, formatting, custom keyboards, message editing, moderation tools and more.
- **Up-to-date**: Gogram is always in sync with the latest Telegram API changes and additions (`tl-parser` is used to generate the API layer).

#### Current Layer - **182** (Updated on 2024-06-18)

## Doing Stuff

#### Sending a Message

```golang
client.SendMessage("username", "Hello from Gogram!")

client.SendDice("username", "🎲")

client.AddMessageHandler("/start", func(m *telegram.Message) error {
    m.Reply("Hello from Gogram!") // m.Respond("<text>")
    return nil
})
```

#### Sending Media

```golang
client.SendMedia("username", "<file-name>", &telegram.MediaOptions{ // filename/inputmedia,...
    Caption: "Hello from Gogram!",
    TTL: int32((math.Pow(2, 31) - 1)), //  TTL For OneTimeMedia
})

client.SendAlbum("username", []string{"<file-name>", "<file-name>"}, &telegram.MediaOptions{ // Array of filenames/inputmedia,...
    Caption: "Hello from Gogram!",
})

// with progress
var pm *telegram.ProgressManager
client.SendMedia("username", "<file-name>", &telegram.MediaOptions{
    Progress: func(a,b int) {
        if pm == nil {
            pm = telegram.NewProgressManager(a, 3) // 3 is edit interval
        }

        if pm.ShouldEdit(b) {
            fmt.Println(pm.GetStats(b)) // client.EditMessage("<chat-id>", "<message-id>", pm.GetStats())
        }
    },
})
```

#### Inline Queries

```golang
client.AddInlineHandler("<pattern>", func(iq *telegram.InlineQuery) error {
	builder := iq.Builder()
	builder.Article("<title>", "<description>", "<text>", &telegram.ArticleOptions{
			LinkPreview: true,
	})

	return nil
})
```

For more examples, check the [examples](examples) directory.

## Features TODO

- [x] Basic MTProto implementation (LAYER 179)
- [x] Updates handling system + Cache
- [x] HTML, Markdown Parsing, Friendly Methods
- [x] Support for Flag2.0, Layer 147
- [x] WebRTC Calls Support
- [ ] Documentation for all methods
- [x] Stabilize File Uploading
- [x] Stabilize File Downloading
- [ ] Secret Chats Support

## Known Issues

- [x] File Uploading/Downloading is not stable
- [x] MessageMediaPoll, UserFull Decode Fails
- [x] invokeWithLayer channel missing while bad Salt
- [x] tcp.io.Reader.Read unstable
- [x] Perfect HTML Parser
- [x] Session File some issues
- [x] Unidentified RPCError decoding fails
- [x] File downloading is not stable

## Contributing

Gogram is an open-source project and your contribution is very much appreciated. If you'd like to contribute, simply fork the repository, commit your changes and send a pull request. If you have any questions, feel free to ask.

## Resources

- Documentation: Work in Progress
- Support: [@rosexchat](https://t.me/rosexchat), [@EvieSupport](https://t.me/EvieSupport)

## License

This library is provided under the terms of the [GPL-3.0 License](LICENSE).
