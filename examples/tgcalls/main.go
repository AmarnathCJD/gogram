package examples

//#cgo LDFLAGS: -L . -lntgcalls -Wl,-rpath=./
import "C"
import (
	"fmt"

	"github.com/amarnathcjd/gogram/telegram"
)

// media.Video = &VideoDescription{
// InputMode: InputModeShell,
// Input:     fmt.Sprintf("ffmpeg -i %s -loglevel panic -f rawvideo -r 24 -pix_fmt yuv420p -vf scale=1280:720 pipe:1", video),
// Width:     1280,
// Height:    720,
// Fps:       24,
// }

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   6,
		AppHash: "<app_hash>",
	})

	client.AuthPrompt() // client.Login("<phone_number>")

	if err != nil {
		panic(err)
	}

	ntg := NTgCalls()
	defer ntg.Free()

	url := "https://envs.sh/trq.m4a" // audio file url

	media := MediaDescription{
		Audio: &AudioDescription{
			InputMode:     InputModeShell,
			SampleRate:    128000,
			BitsPerSample: 16,
			ChannelCount:  2,
			Input:         fmt.Sprintf("ffmpeg -i %s -loglevel panic -f s16le -ac 2 -ar 128k pipe:1", url), // ffmpeg command to convert audio to s16le format and pipe it to stdout
		},
	}

	joinGroupCall(client, ntg, "@rosexchat", media)

	client.Idle()
}

// join groupcall and start streaming audio
func joinGroupCall(client *telegram.Client, ntg *Client, chatId interface{}, media MediaDescription) {
	me, _ := client.GetMe() // get the current user for JoinAs

	call, err := client.GetGroupCall(chatId) // get the group call object
	if err != nil {
		panic(err)
	}

	rawChannel, _ := client.GetSendablePeer(chatId)
	channel := rawChannel.(*telegram.InputPeerChannel)
	jsonParams, err := ntg.CreateCall(channel.ChannelID, media) // create call object with media description
	if err != nil {
		panic(err)
	}

	callResRaw, err := client.PhoneJoinGroupCall(
		&telegram.PhoneJoinGroupCallParams{
			Muted:        false,
			VideoStopped: true, // false for video call
			Call:         call,
			Params: &telegram.DataJson{
				Data: jsonParams,
			},
			JoinAs: &telegram.InputPeerUser{
				UserID:     me.ID,
				AccessHash: me.AccessHash,
			},
		},
	)

	if err != nil {
		panic(err)
	}

	callRes := callResRaw.(*telegram.UpdatesObj)
	for _, update := range callRes.Updates {
		switch u := update.(type) {
		case *telegram.UpdateGroupCallConnection: // wait for connection params
			_ = ntg.Connect(channel.ChannelID, u.Params.Data)
		}
	}
}
