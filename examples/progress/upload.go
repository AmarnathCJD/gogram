package examples

import (
	"fmt"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
)

const (
	appID    = 6
	apiKey   = ""
	botToken = ""
)

func main() {
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   appID,
		AppHash: apiKey,
	})

	if err != nil {
		panic(err)
	}

	client.Conn()
	client.LoginBot(botToken)

	var pm *ProgressManager
	chat, _ := client.ResolvePeer("chatId")
	m, _ := client.SendMessage(chat, "Starting File Upload...")

	client.SendMedia(chat, "<file-name>", &telegram.MediaOptions{
		ProgressCallback: func(total, curr int32) {
			if pm == nil {
				pm = NewProgressManager(int(total), 5) // 5 seconds edit interval
			}
			if pm.shouldEdit() {
				client.EditMessage(chat, m.ID, pm.getStats(int(curr)))
			}
		},
	})
}

type ProgressManager struct {
	startTime    int64
	editInterval int
	lastEdit     int64
	totalSize    int
}

func NewProgressManager(totalSize int, editInterval int) *ProgressManager {
	return &ProgressManager{
		startTime:    time.Now().Unix(),
		editInterval: editInterval,
		totalSize:    totalSize,
		lastEdit:     time.Now().Unix(),
	}
}

func (pm *ProgressManager) shouldEdit() bool {
	if time.Now().Unix()-pm.lastEdit >= int64(pm.editInterval) {
		pm.lastEdit = time.Now().Unix()
		return true
	}
	return false
}

func (pm *ProgressManager) getProgress(currentSize int) float64 {
	return float64(currentSize) / float64(pm.totalSize) * 100
}

func (pm *ProgressManager) getETA(currentSize int) string {
	elapsed := time.Now().Unix() - pm.startTime
	remaining := float64(pm.totalSize-currentSize) / float64(currentSize) * float64(elapsed)
	return (time.Second * time.Duration(remaining)).String()
}

func (pm *ProgressManager) getSpeed(currentSize int) string {
	// partSize = 512 * 512: 512KB
	partSize := 512 * 512
	dataTransfered := partSize * currentSize
	elapsedTime := time.Since(time.Unix(pm.startTime, 0))
	if int(elapsedTime.Seconds()) == 0 {
		return "0 B/s"
	}
	speedBps := float64(dataTransfered) / elapsedTime.Seconds()
	if speedBps < 1024 {
		return fmt.Sprintf("%.2f B/s", speedBps)
	} else if speedBps < 1024*1024 {
		return fmt.Sprintf("%.2f KB/s", speedBps/1024)
	} else {
		return fmt.Sprintf("%.2f MB/s", speedBps/1024/1024)
	}
}

func (pm *ProgressManager) getStats(currentSize int) string {
	return fmt.Sprintf("Progress: %.2f%% | ETA: %s | Speed: %s", pm.getProgress(currentSize), pm.getETA(currentSize), pm.getSpeed(currentSize))
}
