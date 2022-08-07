# Go-Gram

Under Development.

Pure Go implementation of [Telegram Mtproto protocol](https://core.telegram.org/mtproto/description).



## Installation
    []: # Language: markdown

    go get -u github.com/amarnathcjd/gogram

    
## SettingUp Client
   client, _ := telegram.NewClient(telegram.ClientConfig{
         AppID: ,
         AppHash: ,
         DataCenter: ,
         SessionFile: , // optional
         AppVersion: , // optional 
         DeviceModel: , // optional 
   })
   client.Idle() // start infinity polling

