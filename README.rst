GσGɾαɱ
========
.. epigraph::

  ⚒️ Under Development.



**gogram** is a **Pure Golang**
MTProto_ (Layer 147) library to interact with Telegram's API
as a user or through a bot account (bot API alternative).


What is this?
-------------

Telegram is a popular messaging application. This library is meant
to make it easy for you to write Golang programs that can interact
with Telegram. Think of it as a wrapper that has already done the
heavy job for you, so you can focus on developing an application.

Known Bugs
----------

• HTML Parser (identical text only first one is detected)
• getFullChannel fails due to flag2.0

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
    client.Connect()
    client.LoginBot(botToken) // client.Login(phoneNumber)
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
    p := client.GetParticipant("chat", "user")

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
- 🔨 Add Flag2.0 Parser (Then update to Layer-151https://img.shields.io/github/issues/amarnathcjd/gogram)
- 📝 Fix File handling
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
    
