package examples

// ExampleMultiple demonstrates how to use multiple clients in a single application.

import (
	"fmt"

	tg "github.com/amarnathcjd/gogram/telegram"
)

func main() {
	// Create a new client with the default options
	client1, _ := tg.NewClient(tg.ClientConfig{
		Cache:   tg.NewCache("client1_cache"),
		Session: "client1_session",
	})

	// Create a new client with the default options
	client2, _ := tg.NewClient(tg.ClientConfig{
		Cache:   tg.NewCache("client2_cache"),
		Session: "client2_session",
	})

	client1.LoginBot("123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11")
	client2.LoginBot("123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11")

	fmt.Println(client1.GetMe())
	fmt.Println(client2.GetMe())

	// This way, their cache and session files are stored separately.
	// Now you can use both clients in your application.
}
