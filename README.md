<p align="center">
    <a href="https://github.com/amarnathcjd/gogram">
        <img src="https://i.imgur.com/RE1M0sM.png" alt="Gogram" width="256">
    </a>
    <br>
    <b>modern golang library for mtproto</b>
    <br>
    <b>
    <a href="https://gogramdoc.vercel.app">documentation</a>
    &nbsp;•&nbsp;
    <a href="https://github.com/amarnathcjd/gogram/releases">releases</a>
    &nbsp;•&nbsp;
    <a href="https://t.me/rosexchat">telegram chat</a>
    </b>
</p>

<div align='center'>
	
[![GoDoc](https://godoc.org/github.com/amarnathcjd/gogram?status.svg)](https://godoc.org/github.com/amarnathcjd/gogram)
[![Go Report Card](https://goreportcard.com/badge/github.com/amarnathcjd/gogram)](https://goreportcard.com/report/github.com/amarnathcjd/gogram)
[![License](https://img.shields.io/github/license/amarnathcjd/gogram.svg)](https://img.shields.io/github/license/amarnathcjd/gogram.svg)
[![GitHub stars](https://img.shields.io/github/stars/amarnathcjd/gogram.svg?style=social&label=Stars)](https://img.shields.io/github/stars/amarnathcjd/gogram.svg?style=social&label=Stars)
[![GitHub forks](https://img.shields.io/github/forks/amarnathcjd/gogram.svg?style=social&label=Fork)](https://img.shields.io/github/forks/amarnathcjd/gogram.svg?style=social&label=Fork)

</div>

<div align='center'>
	<img src="https://count.getloli.com/get/@gogram-amarnathcdj?theme=moebooru" alt="Counter" />
</div>


<br>

<p>⭐️ <b>Gogram</b> is a modern, elegant and concurrent <b><a href='https://core.telegram.org/api'>MTProto API</a></b>
framework. It enables you to easily interact with the main Telegram API through a user account (custom client) or a bot
identity (bot API alternative) using Go.</p>
<br>

> Gogram is currently in its stable release stage. While there may still be a few bugs, feel free to use it and provide feedback if you encounter any issues or rough edges. 😊

## setup

<p>please note that gogram requires Go <b>1.18</b> or later to support go-generics</p>

```bash
go get -u github.com/amarnathcjd/gogram/telegram
```

## quick start

```golang
package main

import "github.com/amarnathcjd/gogram/telegram"

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID: 6, AppHash: "<app-hash>",
	})

	if err != nil {
		log.Fatal(err)
	}

	client.Conn()

	client.LoginBot("<bot-token>") // or client.Login("<phone-number>") for user account, or client.AuthPrompt() for interactive login

	client.On(telegram.OnMessage, func(message *telegram.NewMessage) error { // client.AddMessageHandler
			message.Reply("Hello from Gogram!")
        		return nil
	}, telegram.FilterPrivate) // waits for private messages only

	client.Idle() // block main goroutine until client is closed
}
```

- **[sample modular bot](https://github.com/AmarnathCJD/JuliaBot.git)**: a simple modular bot built using gogram with plugins support.
- **Try out Live Demo** at **[JuliaBot](https://t.me/rustyDbot)**. 

## support dev

If you'd like to support Gogram, you can consider:

- <b><a href="https://github.com/sponsors/amarnathcjd" style="text-decoration: none; color: green;">become a github sponsor</a></b>
- <b>star this repo :) ⭐</b>

## key features

<ul>
  <li><strong>ready</strong>: 🚀 install gogram with <code>go get</code> and you are ready to go!</li>
  <li><strong>easy</strong>: 😊 makes the telegram api simple and intuitive, while still allowing advanced usages.</li>
  <li><strong>elegant</strong>: 💎 low-level details are abstracted and re-presented in a more convenient way.</li>
  <li><strong>fast</strong>: ⚡ backed by a powerful and concurrent library, gogram can handle even the heaviest workloads.</li>
  <li><strong>zero dependencies</strong>: 🛠️ no need to install anything else than gogram itself.</li>
  <li><strong>powerful</strong>: 💪 full access to telegram's api to execute any official client action and more.</li>
  <li><strong>feature-rich</strong>: 🌟 built-in support for file uploading, formatting, custom keyboards, message editing, moderation tools and more.</li>
  <li><strong>up-to-date</strong>: 🔄 gogram is always in sync with the latest telegram api changes and additions (<code>tl-parser</code> is used to generate the api layer).</li>
</ul>

#### Current Layer - **223** (Updated on 2026-01-27)

## doing stuff

```golang
// sending a message

client.SendMessage("username", "Hello from Gogram!")

client.SendDice("username", "🎲")

client.On("message:/start", func(m *telegram.NewMessage) error {
    m.Reply("Hello from Gogram!") // m.Respond("...")
    return nil
})
```

```golang
// sending media

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

```golang
// inline queries

client.On("inline:<pattern>", func(iq *telegram.InlineQuery) error { // client.AddInlineHandler
	builder := iq.Builder()
	builder.Article("<title>", "<description>", "<text>", &telegram.ArticleOptions{
			LinkPreview: true,
	})

	return nil
})
```

```golang
// callback queries

client.On("callback:<pattern>", func(cb *telegram.CallbackQuery) error { // client.AddCallbackHandler
    cb.Answer("This is a callback response", &CallbackOptions{
		Alert: true,
	})
    return nil
})
```

For more examples, check the **[examples](examples)** directory.

## features

- [x] basic mtproto implementation (layer 184)
- [x] updates handling system + cache
- [x] html, markdown parsing, friendly methods
- [x] support for flag2.0, layer 147
- [x] webrtc calls support
- [x] documentation for all methods
- [x] stabilize file uploading
- [x] stabilize file downloading
- [ ] secret chats support
- [x] cdn dc support
- [x] reply markup builder helpers
- [x] reimplement file downloads (more speed + less cpu usage)

## known issues

- [x] ~ file download, is cpu intensive
- [x] ~ open issues if found :)
- [x] ~ enhance peer caching (Fixed: improved locking, username persistence, debounced writes, min entity handling)

## contributing

Gogram is an open-source project and your contribution is very much appreciated. If you'd like to contribute, simply fork the repository, commit your changes and send a pull request. If you have any questions, feel free to ask.

## License

This library is provided under the terms of the [GPL-3.0 License](LICENSE).
