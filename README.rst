GoGRAM
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

    client, _ := telegram.NewClient(telegram.ClientConfig{
         AppID: 6,
         AppHash: "",
         DataCenter: 2,
         SessionFile: "", // optional
         ParseMode: "Markdown", //optional 
         AppVersion: "", // optional 
         DeviceModel: "", // optional 
    })

    client.Idle() // start infinity polling

Event handlers
--------------

.. code-block:: golang

    func Echo(c *telegram.Client, m *telegram.NewMessage) error {
         if !m.IsPrivate() {
             return nil
         }
         if m.Args() == nil {
             _, err := m.Reply("No echo text provided.")
             return err
         }
         _, err := m.Respond(m.Args())
         return err
    }

    client.AddEventHandler("(?i)[?!/.]echo", Echo)

Entity Cache
------------
   Entities are cached on memory for now.

Doing stuff
-----------

.. code-block:: golang

    fmt.Println(client.GetMe())

    message, _ := client.SendMessage("username", "Hello I'm talking to you from gogram!")
    client.EditMessage("username", message.ID, "Yep.")

    peer := client.ResolvePeer(777000)

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
    Mozilla 
