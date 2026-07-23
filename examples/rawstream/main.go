// Raw MTProto update stream — for callers who want every update
// (including those piggybacking on RPC responses) delivered directly to
// the friendly handler API, without gogram's pts/qts gap-tracking.
//
// Typical use case: a proxy/adapter (e.g. Bot API server) that re-envelopes
// every update and doesn't need gogram to guarantee ordering. Under heavy
// concurrent RPC traffic, the default dispatcher can buffer non-contiguous
// pts updates until a gap resolves; RawUpdates=true bypasses that path.
//
// Two flavors below — pick one.
package main

import (
	"fmt"
	"os"

	"github.com/amarnathcjd/gogram/telegram"
)

func main() {
	// Flavor 1 — RawUpdates=true keeps the friendly handler API but skips
	// gap tracking. RPC-response updates (e.g. messages.sendMessage returning
	// an Updates envelope) are ALSO teed into the stream.
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:         2040,
		AppHash:       "b18441a1ff607e10a989891a5462e627",
		Session:       "rawstream.session",
		MemorySession: true,
		LogLevel:      telegram.LogInfo,
		RawUpdates:    true,
	})
	if err != nil {
		fmt.Println("NewClient:", err)
		os.Exit(1)
	}

	if err := client.Connect(); err != nil {
		fmt.Println("Connect:", err)
		os.Exit(1)
	}
	if authed, _ := client.IsAuthorized(); !authed {
		var phone string
		fmt.Print("Phone: ")
		fmt.Scanln(&phone)
		if _, err := client.Login(phone); err != nil {
			fmt.Println("Login:", err)
			os.Exit(1)
		}
	}

	client.OnRaw(nil, func(upd telegram.Update, c *telegram.Client) error {
		fmt.Printf("raw update: %T\n", upd)
		return nil
	})

	client.Idle()

	// Flavor 2 — the deeper escape hatch. Skip the dispatcher entirely and
	// tap the MTProto container stream directly. Use this if you don't want
	// gogram's per-type dispatch (OnMessage, OnCallback, etc.) at all.
	//
	//     client, _ := telegram.NewClient(telegram.ClientConfig{
	//         ..., NoUpdates: true,
	//     })
	//     client.MTProto.AddCustomServerRequestHandler(func(u any) bool {
	//         for _, upd := range telegram.UnpackContainer(u) {
	//             // handle upd — no dispatcher, no per-type routing
	//         }
	//         return false
	//     })
	//     // For RPC-response updates in this flavor, also register:
	//     client.MTProto.AddRPCResponseHandler(func(i any) {
	//         for _, upd := range telegram.UnpackContainer(i) {
	//             // same handling as above
	//         }
	//     })
}
