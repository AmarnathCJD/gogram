GoGram
========
.. epigraph::

  ⚒️ Under Development.

**gogram** is a **Pure Golang**
MTProto_ library to interact with Telegram's API
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

    
Creating a client
-----------------

.. code-block:: golang

    client, _ := telegram.TelegramClient(telegram.ClientConfig{
         AppID: 6,
         AppHash: "",
         DataCenter: 2,
         SessionFile: "", // optional
         ParseMode: "Markdown", //optional 
         AppVersion: "", // optional 
         DeviceModel: "", // optional 
         AllowUpdates: true, // optional
    })

    client.Idle() // start infinite polling

Event handlers
--------------

.. code-block:: golang

    func Echo(m *telegram.NewMessage) error {
         if !m.IsPrivate() {
             return nil
         }
         if m.Text() == nil {
             _, err := m.Reply("Nothing to echo.")
             return err
         }
         _, err := m.Respond(m.Text())
         return err
    }

    client.AddMessageHandler(telegram.OnNewMessage, Echo)

Entity Cache
------------

.. code-block:: golang

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

Next steps
----------

Support Chat_

.. _MTProto: https://core.telegram.org/mtproto
.. _chat: https://t.me/rosexchat

Contributing
------------
    Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.
    
License
-------
    Mozilla Public License 2.2

