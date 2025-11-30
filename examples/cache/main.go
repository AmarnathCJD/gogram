package main

import (
	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	appHash  = "YOUR_APP_HASH"
	botToken = "YOUR_BOT_TOKEN"
)

func main() {
	// create a new client object
	client, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:    appID,
		AppHash:  appHash,
		LogLevel: telegram.LogInfo,
		Cache: telegram.NewCache("cache_file.db", &telegram.CacheConfig{
			MaxSize:  1000, // TODO
			LogLevel: telegram.LogInfo,
			Memory:   true,  // disable writing to disk
			Disabled: false, // to totally disable cache
		}), // if left empty, it will use the default cache 'cache.db', with default config
	})

	client.LoginBot(botToken)

	client.On(telegram.OnMessage, func(message *telegram.NewMessage) error {
		message.Respond(message)
		return nil
	}, telegram.FilterPrivate)

	client.On("message:/start", func(message *telegram.NewMessage) error {
		message.Reply("Hello, I am a bot!")
		return nil
	})

	// lock the main routine
	client.Idle()

	// ============================================================
	// Example: Using a custom external DB as CacheStorage
	// ============================================================
	// To use your own cache backend (e.g., Redis, SQL, etc.),
	// implement the telegram.CacheStorage interface:
	//
	// type MyCustomCacheStorage struct {}
	//
	// // Implement required methods:
	// func (c *MyCustomCacheStorage) Read() (*telegram.InputPeerCache, error) {
	//     // Load and decode InputPeerCache from your DB
	//     // return &telegram.InputPeerCache{...}, nil
	// }
	// func (c *MyCustomCacheStorage) Write(peers *telegram.InputPeerCache) error {
	//     // Encode and save InputPeerCache to your DB
	//     // return nil
	// }
	// func (c *MyCustomCacheStorage) Close() error {
	//     // Close DB connection if needed
	//     // return nil
	// }
	//
	// Then, pass your storage to the cache:
	//
	// customStorage := &MyCustomCacheStorage{/* ...init... */}
	// cache := telegram.NewCache("custom", &telegram.CacheConfig{
	//     Storage: customStorage, // use your custom storage
	//     MaxSize: 1000,
	//     Memory:  false,
	// })
	// client, _ := telegram.NewClient(telegram.ClientConfig{
	//     AppID:   appID,
	//     AppHash: appHash,
	//     Cache:   cache,
	// })
	//
	// Now all cache persistence will use your backend.
	//
	// Note: Do not import any DB driver here. This is just an interface example.
	// See telegram.CacheStorage for required methods.
	// ============================================================
}
