GσGram
========

.. epigraph::

  ⚒️ Under Development.



Gogram is an open-source Telegram MTProto_ client written in the Go programming language (also known as Golang). It provides an easy-to-use API for building applications that interact with the Telegram API.

Gogram is designed to be fast and efficient, and it has no external dependencies, making it easy to use and integrate into your projects. It also supports native entity parser, inter DC requests, and friendlier methods, which can help simplify the process of interacting with Telegram's API.

*If you're looking to build a Telegram client or bot using the Go programming language, Gogram is definitely worth considering. Its open-source nature also means that you can contribute to its development and suggest new features or improvements.*


Layer - 170


What is this?
-------------

Telegram is a popular messaging application. This library is meant
to make it easy for you to write Golang programs that can interact
with Telegram. Think of it as a wrapper that has already done the
heavy job for you, so you can focus on developing an application.

Known Bugs
----------

• Find em:)

PRs and Issues are always welcome.

|imx| |imgo| |cma|



Features
--------

Light Weight compared to other go- mtproto clients. Fast compiling and execution, All commonly used methods are made more friendly,
Reliable updates handling system

Installing
----------

.. code-block:: sh

  go get -u github.com/amarnathcjd/gogram

    
Set-Up Client
-----------------

.. code-block:: golang

    client, _ := gogram.TelegramClient(gogram.ClientConfig{
         AppID: 0, 
         AppHash: "", 
    })
    client.ConnectBot(botToken) // client.Login(phoneNumber)
    client.Idle() // start infinite polling


Doing stuff
-----------

.. code-block:: golang

    var b = telegram.Button{}
    opts := &telegram.SendOptions{
        Caption: "Game of Thrones",
        ReplyMarkup: b.Keyboard(b.Row(b.URL("Imdb", "http://imdb.com/title/tt0944947/"))),
    })

    fmt.Println(client.GetMe())

    message, _ := client.SendMessage("username", "Hello I'm talking to you from gogram!")
    message.Edit("Yep!")

    album, _ := client.SendAlbum("username", []string{'file1.jpg', 'file2.jpg'})
    message.GetMediaGroup()

    message.ReplyMedia(url, opts)

    client.DeleteMessage("username", message.ID)

    message.ForwardTo(message.ChatID())
    peer := client.ResolvePeer("username")
    client.GetParticipant("chat", "user")

    client.EditAdmin(chatID, userID, &telegram.AdminOptions{
        AdminRights: &telegram.ChatAdminRights{
            AddAdmins: true,
        },
        Rank: "Admin",
    })

    client.GetMessages(chatID, &telegram.SearchOptions{Limit: 1})

    action, _ := client.SendAction(chat, "typing")
    defer action.Cancel()

    client.KickParticipant(chatID, userID)
    client.EditBanned(chatID, userID, &telegram.BannedOptions{Mute: true})
    client.DownloadMedia(message, "download.jpg")
    client.EditTitle("me", "MyNewAmazingName")

    client.UploadFile("file.txt")
    p := client.GetChatMember("chat", "user")

    p.CanChangeInfo()
    p.GetRank()
    client.InlineQuery("@pic", &telegram.InlineOptions{Query: "", Dialog: "@chat"})
    client.GetChatPhotos(chatID)
    client.GetDialogs()
    client.GetStats("channel")
    client.GetCustomEmoji("documentID")
    
    conv, _ = client.NewConversation("username")
    conv.GetResponse()
    
    client.CreateChannel("Title")
    
    albumHandle := client.AddAlbumHandler(func (a *telegram.Album) error {
           fmt.Println(a.GroupedID)
           a.Forward(chat_id)
           return nil
    }
    albumHandle.Remove()
    
    client.SendDice("username", "🎲")

TODO
----------

- ✔️ Basic MTProto implementation
- ✔️ Implement all Methods for latest layer (147)
- ✔️ Entity Cache + Friendly Methods
- ✔️ Add Update Handle System
- ✔️ Make a reliable HTML Parser
- ✔️ Friendly Methods to Handle CallbackQuery, VoiceCalls
- ✔️ Add Flag2.0 Parser (Then update to Layer-170)
- ✔️ Fix File handling
- 📝 Write beautiful Docs
- 📝 Multiple tests


.. _MTProto: https://core.telegram.org/mtproto
.. _chat: https://t.me/rosexchat
.. |image| image:: https://te.legra.ph/file/fe4dbc185ff2138cbdf45.jpg
  :width: 400
  :alt: Logo

.. |imx| image:: https://img.shields.io/github/issues/amarnathcjd/gogram
   :alt: GitHub issues

.. |imgo| image:: https://img.shields.io/github/go-mod/go-version/amarnathcjd/gogram/master
   :alt: GitHub go.mod Go version (branch & subdirectory of monorepo)

.. |cma| image:: https://img.shields.io/github/commit-activity/y/amarnathcjd/gogram
   :alt: GitHub commit activity

Contributing
------------
    Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
    
