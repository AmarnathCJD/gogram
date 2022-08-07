# <b>Go-Gram</b>

Under Development.

Pure Go implementation of [Telegram Mtproto protocol](https://core.telegram.org/mtproto/description).



## Installation
    []: # Language: markdown

    go get -u github.com/amarnathcjd/gogram

    
## SettingUp Client
    client, _ := telegram.NewClient(telegram.ClientConfig{
         AppID: 6,
         AppHash: "",
         DataCenter: 2,
         SessionFile: "", // optional
         AppVersion: "", // optional 
         DeviceModel: "", // optional 
    })
    client.Idle() // start infinity polling

## EventHandlers
    func Echo(c *telegram.Client, m *telegram.NewMessage) error {
         _, err := m.Respond(m.Args())
         return err
    }
    client.AddEventHandler("^(?i)[?!/.]echo", Echo)

## Common Methods
    client.GetMe() --> User

    client.SendMessage(Peer(int64/string), Text, &SendOptions{
           ReplyID: 0,
           Silent: true,
           NoWebPage: true,
           Entities: (autoset),
    }) --> MessageObj, error

    client.EditMessage(Peer(int64/string), MsgID, Text, &SendOptions{
           ReplyID: 0,
           Silent: true,
           NoWebPage: true,
           Entities: (autoset),
    }) --> MessageObj, error
    
    client.ResolvePeer(Peer (any)) --> (User/Chat/Channel)
    
    <-------- SOON ------->

## NewMessage
    m.ChatID() --> int64
    m.SenderID() --> int64
    m.PeerChat() --> PeerChat
    m.PeerUser() --> PeerUser
    m.PeerChannel() --> PeerChannel
    m.Message() --> string
    m.ChatType() --> string
    
    m.IsPrivate() --> bool
    m.IsGroup() --> bool
    m.IsChannel() --> bool
    m.IsReply() --> bool

    m.GetPeer() --> InputPeer
    m.GetSender() --> User/SenderChat
    m.GetChat() --> Chat
    m.GetSenderChat() --> Chat/Channel
    m.GetReplyMessage() --> Message
    m.ReplyID --> int64
    m.Respond() --> Message
    m.Reply() --> Message
    m.Edit() --> Message
    

## 2022 (RoseLoverX)
