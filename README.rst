GoGram
========
.. epigraph::

  ⚒️ Under Development.



**gogram** is a **Pure Golang**
MTProto_ (Layer 144) library to interact with Telegram's API
as a user or through a bot account (bot API alternative).


What is this?
-------------

Telegram is a popular messaging application. This library is meant
to make it easy for you to write Golang programs that can interact
with Telegram. Think of it as a wrapper that has already done the
heavy job for you, so you can focus on developing an application.

Installing
----------

.. code-block:: sh

  go get -u github.com/amarnathcjd/gogram

    
SetUp Client
-----------------

.. code-block:: golang

    client, _ := telegram.TelegramClient(telegram.ClientConfig{
         AppID: 6,
         AppHash: "",
         DataCenter: 2,
    })
    client.LoginBot(botToken)
    // client.Login(phoneNumber)

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
    
    client.SendDice("username", "🎲")

TODO
----------

- ✔️ Basic MTProto implementation
- ✔️ Implement all Methods for latest layer (144)
- ✔️ Entity Cache + Friendly Methods
- ✔️ Add Update Handle System
- 📝 Make a reliable HTML Parser
- ✔️ Friendly Methods to Handle CallbackQuery, VoiceCalls
- 📝 Multiple tests
- 📝 Add more examples


.. _MTProto: https://core.telegram.org/mtproto
.. _chat: https://t.me/rosexchat
.. |image| image:: https://te.legra.ph/file/fe4dbc185ff2138cbdf45.jpg
  :width: 400
  :alt: Logo

Contributing
------------
    Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
    
