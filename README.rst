GoGram
========
.. epigraph::

  ⚒️ Under Development.



**gogram** is a **Pure Golang**
MTProto_ (Layer 144) library to interact with Telegram's API
as a user or through a bot account (bot API alternative).

|image|

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

    
Creating a client
-----------------

.. code-block:: golang

    client, _ := telegram.TelegramClient(telegram.ClientConfig{
         AppID: 6,
         AppHash: "",
         DataCenter: 2,
    })

    client.Idle() // start infinite polling

Event handlers
--------------

.. code-block:: golang

    func Start(m *telegram.NewMessage) error {
        _, err := m.Reply("Hello World!")
        return err
    }

    client.AddMessageHandler("[/!]start$", Start)

Entity Cache
------------

    Entities are cached on memory for now.

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
    client.EditMessage("username", message.ID, "Yep.")
    client.SendMedia("username", "https://m.media-amazon.com/images/M/MV5BYTRiNDQwYzAtMzVlZS00NTI5LWJjYjUtMzkwNTUzMWMxZTllXkEyXkFqcGdeQXVyNDIzMzcwNjc@._V1_FMjpg_UX1000_.jpg", opts)
    client.DeleteMessage("username", message.ID)
    message.ForwardTo(message.ChatID())
    peer := client.ResolvePeer("username")
    client.GetParticipant("chat", "user")
    client.EditAdmin(chatID, userID, &telegram.AdminOptions{
        AdminRights: &telegram.AdminRights{
            ChangeInfo: true,
            DeleteMessages: true,
            BanUsers: true,
            InviteUsers: true,
            PinMessages: true,
            AddAdmins: true,
        },
        Rank: "Admin",
    })
    client.SendDice("username", "🎲")

TODO
----------

- [ x ] Basic MTProto implementation
- [ x ] Implement all Methods for latest layer (144)
- [ x ] Entity Cache + Friendly Methods
- [ x ] Add Update Handle System
- [   ] Make a reliable HTML Parser
- [   ] Friendly Methods to Handle CallbackQuery, VoiceCalls
- [   ] Multiple tests
- [   ] Add more examples


.. _MTProto: https://core.telegram.org/mtproto
.. _chat: https://t.me/rosexchat
.. |image| image:: https://te.legra.ph/file/fe4dbc185ff2138cbdf45.jpg
  :width: 400
  :alt: Logo

Contributing
------------
    Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
    
