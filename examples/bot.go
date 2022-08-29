// Example of using gogram for bot.

package examples

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/amarnathcjd/gogram/telegram"
	"github.com/showwin/speedtest-go/speedtest"
)

const (
	appID    = 6
	appHash  = ""
	botToken = ""
)

func main() {
	client, err := telegram.TelegramClient(telegram.ClientConfig{
		AppID:      appID,
		AppHash:    appHash,
		DataCenter: 5, // Working on a fix for this
		ParseMode:  "HTML",
	})
	if err != nil {
		panic(err)
	}
	client.LoginBot(botToken)
	if err != nil {
		panic(err)
	}
	me, _ := client.GetMe()
	fmt.Printf("Logged in as %s\n", me.Username)

	client.AddEventHandler(telegram.Command{Cmd: "start", Prefix: "/?."}, Start)
	client.AddEventHandler("/ping", Ping)
	client.AddEventHandler("[/!?]js|json", Jsonify)
	client.AddEventHandler(telegram.OnNewMessage, Echo)
	client.AddEventHandler("/info", Info)
	client.AddEventHandler("/fwd", fwd)
	client.AddEventHandler("/go", GoroutinesNum)
	client.AddEventHandler("/id", ID)
	client.AddEventHandler("/speed", SpeedTest)
	client.AddEventHandler("/sh", Exec)
	client.AddEventHandler("/ul", Upload)
	client.Idle()
	goquery.NewDocument("https://www.google.com/")
}

func Start(m *telegram.NewMessage) error {
	_, err := m.Client.SendMessage(m.ChatID(), "Hello, I'm a bot!")
	return err
}

func Ping(m *telegram.NewMessage) error {
	msg, _ := m.Reply("Pinging...")
	_, err := msg.Edit(fmt.Sprintf("<b>Pong!!</b> %s", m.Client.Ping()))
	return err
}

func Jsonify(m *telegram.NewMessage) error {
	if m.IsReply() {
		r, errr := m.GetReplyMessage()
		if errr != nil {
			return errr
		}
		_, err := m.Reply(r.Marshal())
		return err
	}
	_, err := m.Reply(fmt.Sprint("<code>", m.Marshal(), "</code>"))
	return err
}

func Echo(m *telegram.NewMessage) error {
	if (m.Text() == "" && !m.IsMedia()) || !m.IsPrivate() || m.IsCommand() {
		return nil
	}
	if m.IsMedia() {
		_, err := m.Respond(m.Media())
		return err
	}
	_, err := m.Respond(m.Text())
	return err
}

func Info(m *telegram.NewMessage) error {
	var peer telegram.InputPeer
	if m.IsReply() {
		r, err := m.GetReplyMessage()
		if err != nil {
			return err
		}
		peer, err = m.Client.GetSendablePeer(r.SenderID())
		if err != nil {
			return err
		}
	} else {
		if m.Args() == "" {
			peer, _ = m.Client.GetSendablePeer(m.SenderID())
		} else {
			peer, _ = m.Client.GetSendablePeer(m.Args())
		}
	}
	switch peer := peer.(type) {
	case *telegram.InputPeerUser:
		user, err := m.Client.Cache.GetUser(peer.UserID)
		if err != nil {
			_, err := m.Reply("User not found")
			return err
		}
		INFO := "<b>User Info</b>\n<code>First Name:</code> <b>%s</b>\n<code>Last Name:</code> <b>%s</b>\n<code>ID:</code> <b>%d</b>\n<code>DC ID:</code> <>%s</b>\n<code>Username:</code> <b>@%s</b>\n<code>Premium:</code> <b>%v</b>\n<code>Bot:</code> <b>%v</b>"
		var DC string
		if user.Photo != nil {
			DC = fmt.Sprint(user.Photo.(*telegram.UserProfilePhotoObj).DcID)
		}
		x := fmt.Sprintf(INFO, user.FirstName, user.LastName, user.ID, DC, user.Username, user.Premium, user.Bot)
		fmt.Println(x)
		_, err = m.Reply(x)

		return err
	case *telegram.InputPeerChannel:
		channel, err := m.Client.Cache.GetChannel(peer.ChannelID)
		if err != nil {
			_, err := m.Reply("Channel not found")
			return err
		}
		INFO := "<b>Channel Info</b>\n<code>Title:</code> <b>%s</b>\n<code>ID:</code> <b>%d</b>\n<code>DC ID:</code> <b>%s</b>\n<code>Username:</code> <b>@%s</b>\n<code>Public:</code> <b>%v</b>\n<code>Verified:</code> <b>%v</b>"
		var DC string
		if channel.Photo != nil {
			if p, ok := channel.Photo.(*telegram.ChatPhotoObj); ok {
				DC = fmt.Sprint(p.DcID)
			}
		}
		x := fmt.Sprintf(INFO, channel.Title, channel.ID, DC, channel.Username, channel.Broadcast, channel.Verified)
		fmt.Println(x)
		_, err = m.Reply(x)
		return err
	default:
		_, err := m.Reply(fmt.Sprintf("Peer not found, peer type: %T", peer))
		return err
	}
}

func fwd(m *telegram.NewMessage) error {
	if !m.IsReply() {
		_, err := m.Reply("Reply to a message to forward it")
		return err
	}
	r, err := m.GetReplyMessage()
	if err != nil {
		return err
	}
	r.ForwardTo(m.ChatID())
	return nil
}

func GoroutinesNum(m *telegram.NewMessage) error {
	_, err := m.Reply(fmt.Sprintf("<b>Goroutines: %d\nGo Version: %s\nOS: %s\nArch: %s</b>", runtime.NumGoroutine(), runtime.Version(), runtime.GOOS, runtime.GOARCH))
	return err
}

func ID(m *telegram.NewMessage) error {
	if m.IsReply() {
		r, err := m.GetReplyMessage()
		if err != nil {
			return err
		}
		_, err = m.Reply(fmt.Sprintf("Replied User ID: `%d`\nReplied Message ID: `%d`", r.SenderID(), r.ID))
		return err
	} else {
		_, err := m.Reply(fmt.Sprintf("User ID: `%d`\nMessage ID: `%d`\nChat ID: `%d`", m.SenderID(), m.ID, m.ChatID()))
		return err
	}
}

func SpeedTest(m *telegram.NewMessage) error {
	msg, err := m.Reply("Running Speedtest...")
	if err != nil {
		return err
	}
	userf, _ := speedtest.FetchUserInfo()

	serverList, _ := speedtest.FetchServers(userf)
	targets, _ := serverList.FindServer([]int{})
	server := targets[0]
	server.PingTest()
	server.DownloadTest(false)
	server.UploadTest(false)
	_, Editerr := msg.Edit(fmt.Sprintf("<b>SPEEDTEST RESULTS\n\nDownload: %v mbps\nUpload: %v mbps\nPing: %s\nDistance: %vkm\nHost: %s</b>", int(server.DLSpeed), int(server.ULSpeed), server.Latency.String(), server.Distance, server.Host))
	return Editerr
}

func Exec(m *telegram.NewMessage) error {
	var err error
	msg, _ := m.Reply("<code>Processing...</code>")
	if m.Args() == "" {
		_, err := msg.Edit("No code provided!")
		return err
	} else {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		proc := exec.Command("bash", "-c", m.Args())
		proc.Stdout = &stdout
		proc.Stderr = &stderr
		err = proc.Run()
		var result string
		if stdout.String() != string("") {
			result = stdout.String()
		} else if stderr.String() != string("") {
			result = stderr.String()
		} else if err != nil {
			result = err.Error()
		} else {
			result = "No output"
		}
		if len(result) > 4096 {
			fs, _ := os.OpenFile("sh.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			defer fs.Close()
			fs.WriteString(result)
			f, _ := m.Client.UploadFile("sh.txt")
			m.Client.SendMedia(m.ChatID(), f)
			m.Delete()
		}
		_, err = msg.Edit(fmt.Sprintf("<b>BASH:</b> <code>%s</code>", result))
	}
	return err
}

func Upload(m *telegram.NewMessage) error {
	if m.Args() == "" {
		_, err := m.Reply("No Args Provided!")
		return err
	}
	message, err := m.Reply("Uploading...")
	if err != nil {
		return err
	}
	if _, err := os.Stat(m.Args()); os.IsNotExist(err) {
		if strings.Contains(m.Args(), "http") {
			_, err := m.Client.SendMedia(m.ChatID(), m.Args())
			return err
		} else {
			_, err := message.Edit("File not found!")
			return err
		}
	} else {
		f, err := m.Client.UploadFile(m.Args())
		if err != nil {
			return err
		}
		m.Client.SendMedia(m.ChatID(), f)
		return message.Delete()
	}
}
