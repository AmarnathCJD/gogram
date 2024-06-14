// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/errors"
)

const (
	DEFAULT_WORKERS = 4
	DEFAULT_PARTS   = 512 * 512
)

type UploadOptions struct {
	// Worker count for upload file.
	Threads int `json:"threads,omitempty"`
	//  Chunk size for upload file.
	ChunkSize int32 `json:"chunk_size,omitempty"`
	// File name for upload file.
	FileName string `json:"file_name,omitempty"`
	// output Callback for upload progress, total parts and uploaded parts.
	ProgressCallback func(totalParts int32, uploadedParts int32) `json:"-"`
}

type Sender struct {
	buzy bool
	c    *Client
}

type Source struct {
	Source interface{}
}

func (s *Source) GetSizeAndName() (int64, string) {
	switch src := s.Source.(type) {
	case string:
		file, err := os.Open(src)
		if err != nil {
			return 0, ""
		}
		stat, _ := file.Stat()
		return stat.Size(), file.Name()
	case *os.File:
		stat, _ := src.Stat()
		return stat.Size(), src.Name()
	case []byte:
		return int64(len(src)), ""
	case *io.Reader:
		return 0, ""
	}
	return 0, ""
}

func (s *Source) GetReader() io.Reader {
	switch src := s.Source.(type) {
	case string:
		file, err := os.Open(src)
		if err != nil {
			return nil
		}
		return file
	case *os.File:
		return src
	case []byte:
		return bytes.NewReader(src)
	case *bytes.Buffer:
		return bytes.NewReader(src.Bytes())
	case *io.Reader:
		return *src
	}
	return nil
}

func (c *Client) UploadFile(src interface{}, Opts ...*UploadOptions) (InputFile, error) {
	opts := getVariadic(Opts, &UploadOptions{}).(*UploadOptions)
	if src == nil {
		return nil, errors.New("file can not be nil")
	}

	source := &Source{Source: src}
	size, fileName := source.GetSizeAndName()

	file := source.GetReader()
	if file == nil {
		return nil, errors.New("failed to convert source to io.Reader")
	}

	partSize := 1024 * 512 // 512KB
	if opts.ChunkSize > 0 {
		partSize = int(opts.ChunkSize)
	}
	fileId := GenerateRandomLong()
	var hash hash.Hash

	IsFsBig := false
	if size > 10*1024*1024 { // 10MB
		IsFsBig = true
	}

	if !IsFsBig {
		hash = md5.New()
	}

	parts := size / int64(partSize)
	partOver := size % int64(partSize)

	totalParts := parts
	if partOver > 0 {
		totalParts++
	}

	wg := sync.WaitGroup{}

	numWorkers := countWorkers(parts)
	if opts.Threads > 0 {
		numWorkers = opts.Threads
	}
	sender := make([]Sender, numWorkers)
	sendersPreallocated := 0

	if pre := c.GetCachedExportedSenders(c.GetDC()); len(pre) > 0 {
		for i := 0; i < len(pre); i++ {
			if sendersPreallocated >= numWorkers {
				break
			}
			if pre[i] != nil {
				sender[i] = Sender{c: pre[i]}
				sendersPreallocated++
			}
		}
	}

	c.Logger.Info(fmt.Sprintf("file - upload: (%s) - (%d) - (%d)", source, size, parts))
	c.Logger.Info(fmt.Sprintf("expected workers: %d, preallocated workers: %d", numWorkers, sendersPreallocated))

	c.Logger.Debug(fmt.Sprintf("expected workers: %d, preallocated workers: %d", numWorkers, sendersPreallocated))
	var MAX_RETRIES = 3
	var retries = 0

	for i := sendersPreallocated; i < numWorkers; i++ {
		x, _ := c.CreateExportedSender(c.GetDC())
		if x == nil {
			if retries < MAX_RETRIES {
				retries++
				i--
				continue
			}
			c.Logger.Error("failed to create exported sender")
			continue
		}
		c.AddNewExportedSenderToMap(c.GetDC(), x)
		sender[i] = Sender{c: x}
	}

	for p := int64(0); p < parts; p++ {
		wg.Add(1)
		for {
			found := false
			for i := 0; i < numWorkers; i++ {
				if !sender[i].buzy && sender[i].c != nil {
					part := make([]byte, partSize)
					_, err := file.Read(part)
					if err != nil {
						c.Logger.Error(err)
						return nil, err
					}

					found = true
					sender[i].buzy = true
					go func(i int, part []byte, p int) {
						defer wg.Done()
					uploadStartPoint:
						c.Logger.Debug(fmt.Sprintf("uploading part %d/%d in chunks of %d", p, totalParts, len(part)/1024))
						if !IsFsBig {
							_, err = sender[i].c.UploadSaveFilePart(fileId, int32(p), part)
							hash.Write(part)
						} else {
							_, err = sender[i].c.UploadSaveBigFilePart(fileId, int32(p), int32(totalParts), part)
						}
						if err != nil {
							if handleIfFlood(err, c) {
								goto uploadStartPoint
							}
							c.Logger.Error(err)
						}
						if opts.ProgressCallback != nil {
							go opts.ProgressCallback(int32(totalParts), int32(p))
						}
						sender[i].buzy = false
					}(i, part, int(p))
					break
				}
			}

			if found {
				break
			}
		}
	}

	wg.Wait()

	if partOver > 0 {
		part := make([]byte, partOver)
		_, err := file.Read(part)
		if err != nil {
			c.Logger.Error(err)
		}

	lastPartUploadStartPoint:
		c.Logger.Debug(fmt.Sprintf("uploading last part %d/%d in chunks of %d", totalParts-1, totalParts, len(part)/1024))
		if !IsFsBig {
			_, err = c.UploadSaveFilePart(fileId, int32(totalParts)-1, part)
		} else {
			_, err = c.UploadSaveBigFilePart(fileId, int32(totalParts)-1, int32(totalParts), part)
		}

		if err != nil {
			if handleIfFlood(err, c) {
				goto lastPartUploadStartPoint
			}
			c.Logger.Error(err)
		}

		if opts.ProgressCallback != nil {
			go opts.ProgressCallback(int32(totalParts), int32(totalParts))
		}
	}

	if opts.FileName != "" {
		fileName = opts.FileName
	}

	if opts.ProgressCallback != nil {
		opts.ProgressCallback(int32(totalParts), int32(totalParts))
	}

	if !IsFsBig {
		return &InputFileObj{
			ID:          fileId,
			Md5Checksum: string(hash.Sum(nil)),
			Name:        prettifyFileName(fileName),
			Parts:       int32(totalParts),
		}, nil
	}

	return &InputFileBig{
		ID:    fileId,
		Parts: int32(totalParts),
		Name:  prettifyFileName(fileName),
	}, nil
}

func handleIfFlood(err error, c *Client) bool {
	if matchError(err, "FLOOD_WAIT_") {
		if waitTime := getFloodWait(err); waitTime > 0 {
			c.Logger.Warn("flood wait ", waitTime, "(s), waiting...")
			time.Sleep(time.Duration(waitTime) * time.Second)
			return true
		}
	}

	return false
}

func prettifyFileName(file string) string {
	return filepath.Base(file)
}

func countWorkers(parts int64) int {
	if parts <= 5 {
		return int(parts / 2)
	} else if parts > 100 {
		return 20
	} else if parts > 50 {
		return 10
	} else {
		return 5
	}
}

// ----------------------- Download Media -----------------------

type DownloadOptions struct {
	// Download path to save file
	FileName string `json:"file_name,omitempty"`
	// Worker count to download file
	Threads int `json:"threads,omitempty"`
	// Chunk size to download file
	ChunkSize int32 `json:"chunk_size,omitempty"`
	// output Callback for download progress, total parts and downloaded parts.
	ProgressCallback func(totalParts int32, downloadedParts int32) `json:"-"`
	// Datacenter ID of file
	DCId int32 `json:"dc_id,omitempty"`
	// Destination Writer
	Buffer *bytes.Buffer `json:"-"`
}

type Destination struct {
	data []byte
	mu   sync.Mutex
	file *os.File
}

func (mb *Destination) WriteAt(p []byte, off int64) (n int, err error) {
	if mb.file != nil {
		return mb.file.WriteAt(p, off)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if int(off)+len(p) > len(mb.data) {
		newData := make([]byte, int(off)+len(p))
		copy(newData, mb.data)
		mb.data = newData
	}

	copy(mb.data[off:], p)
	return len(p), nil
}

func (c *Client) DownloadMedia(file interface{}, Opts ...*DownloadOptions) (string, error) {
	opts := getVariadic(Opts, &DownloadOptions{}).(*DownloadOptions)
	location, dc, size, fileName, err := GetFileLocation(file)
	if err != nil {
		return "", err
	}

	dc = getValue(dc, opts.DCId).(int32)
	if dc == 0 {
		dc = int32(c.GetDC())
	}
	dest := getValue(opts.FileName, fileName).(string)

	partSize := 1024 * 512 // 512KB
	if opts.ChunkSize > 0 {
		partSize = int(opts.ChunkSize)
	}

	var fs Destination
	if opts.Buffer == nil {
		file, err := os.OpenFile(dest, os.O_CREATE|os.O_RDWR, 0666)
		if err != nil {
			return "", err
		}

		fs.file = file

		defer func() {
			fs.file.Close()
		}()
	}

	parts := size / int64(partSize)
	partOver := size % int64(partSize)

	totalParts := parts

	wg := sync.WaitGroup{}
	numWorkers := countWorkers(parts)
	if opts.Threads > 0 {
		numWorkers = opts.Threads
	}

	if numWorkers == 0 {
		numWorkers = 1
	}

	w := make([]Sender, numWorkers)
	wPreallocated := 0

	if pre := c.GetCachedExportedSenders(c.GetDC()); len(pre) > 0 {
		for i := 0; i < len(pre); i++ {
			if wPreallocated >= numWorkers {
				break
			}
			if pre[i] != nil {
				w[i] = Sender{c: pre[i]}
				wPreallocated++
			}
		}
	}

	if opts.Buffer != nil {
		dest = ":mem-buffer:"
	}

	c.Logger.Info(fmt.Sprintf("file - download: (%s) - (%d) - (%d)", dest, size, parts))
	c.Logger.Info(fmt.Sprintf("expected workers: %d, preallocated workers: %d", numWorkers, wPreallocated))

	c.Logger.Debug(fmt.Sprintf("expected workers: %d, preallocated workers: %d", numWorkers, wPreallocated))
	var MAX_RETRIES = 3
	var retries = 0

	for i := wPreallocated; i < numWorkers; i++ {
		x, _ := c.CreateExportedSender(int(dc))
		if x == nil {
			if retries < MAX_RETRIES {
				retries++
				i--
				continue
			}
			c.Logger.Error("failed to create exported sender")
			continue
		}
		c.AddNewExportedSenderToMap(int(dc), x)
		w[i] = Sender{c: x}
	}

	for p := int64(0); p < parts; p++ {
		wg.Add(1)
		for {
			found := false
			for i := 0; i < numWorkers; i++ {
				if !w[i].buzy && w[i].c != nil {
					found = true
					part := make([]byte, partSize)
					w[i].buzy = true
					go func(i int, part []byte, p int) {
						defer wg.Done()
					downloadStartPoint:
						c.Logger.Debug(fmt.Sprintf("downloading part %d/%d in chunks of %d", p, totalParts, len(part)/1024))
						buf, err := w[i].c.UploadGetFile(&UploadGetFileParams{
							Location:     location,
							Offset:       int64(p * partSize),
							Limit:        int32(partSize),
							CdnSupported: false,
						})

						if handleIfFlood(err, c) {
							goto downloadStartPoint
						}

						if err != nil || buf == nil {
							w[i].c.Logger.Warn(err)
							return
						}
						var buffer []byte
						switch v := buf.(type) {
						case *UploadFileObj:
							buffer = v.Bytes
						case *UploadFileCdnRedirect:
							return // TODO
						}
						_, err = fs.WriteAt(buffer, int64(p*partSize))
						if err != nil {
							panic(err)
						}
						if opts.ProgressCallback != nil {
							go opts.ProgressCallback(int32(totalParts), int32(p))
						}
						w[i].buzy = false
					}(i, part, int(p))
					break
				}
			}

			if found {
				break
			}
		}
	}

	wg.Wait()

	if partOver > 0 {
	downloadLastPartStartPoint:
		if w[0].c == nil {
			for i := 0; i < numWorkers; i++ {
				if w[i].c != nil {
					w[0].c = w[i].c
					break
				}
			}
		}

		c.Logger.Debug(fmt.Sprintf("downloading last part %d/%d in chunks of %d", totalParts-1, totalParts, partOver/1024))

		buf, err := w[0].c.UploadGetFile(&UploadGetFileParams{
			Location:     location,
			Offset:       int64(int(parts) * partSize),
			Limit:        int32(partSize),
			CdnSupported: false,
		})

		if err != nil || buf == nil {
			if handleIfFlood(err, c) {
				goto downloadLastPartStartPoint
			}
			w[0].c.Logger.Warn(err)
		}
		var buffer []byte
		switch v := buf.(type) {
		case *UploadFileObj:
			buffer = v.Bytes
			_, err = fs.WriteAt(buffer, int64(int(parts)*partSize))

			if err != nil {
				panic(err)
			}
		case *UploadFileCdnRedirect:
			return "", nil // TODO
		}

		if opts.ProgressCallback != nil {
			go opts.ProgressCallback(int32(totalParts), int32(parts))
		}
	}

	if opts.ProgressCallback != nil {
		opts.ProgressCallback(int32(totalParts), int32(totalParts))
	}

	if opts.Buffer != nil {
		c.Logger.Debug("writing to buffer, size: ", len(fs.data))

		opts.Buffer.Write(fs.data)
		return "", nil
	}

	return dest, nil
}

// ----------------------- Progress Manager -----------------------
type ProgressManager struct {
	startTime    int64
	editInterval int
	lastEdit     int64
	totalSize    int
	lastPerc     float64
}

func NewProgressManager(totalSize int, editInterval int) *ProgressManager {
	return &ProgressManager{
		startTime:    time.Now().Unix(),
		editInterval: editInterval,
		totalSize:    totalSize,
		lastEdit:     time.Now().Unix(),
	}
}

func (pm *ProgressManager) SetTotalSize(totalSize int) {
	pm.totalSize = totalSize
}

func (pm *ProgressManager) PrintFunc() func(a, b int32) {
	return func(a, b int32) {
		pm.SetTotalSize(int(a))
		if pm.ShouldEdit() {
			fmt.Println(pm.GetStats(int(b)))
		} else {
			fmt.Println(pm.GetStats(int(b)))
		}
	}
}

func (pm *ProgressManager) EditFunc(msg *NewMessage) func(a, b int32) {
	return func(a, b int32) {
		if pm.ShouldEdit() {
			_, _ = msg.Client.EditMessage(msg.Peer, msg.ID, pm.GetStats(int(b)))
		}
	}
}

func (pm *ProgressManager) ShouldEdit() bool {
	if time.Now().Unix()-pm.lastEdit >= int64(pm.editInterval) {
		pm.lastEdit = time.Now().Unix()
		return true
	}
	return false
}

func (pm *ProgressManager) GetProgress(currentSize int) float64 {
	if pm.totalSize == 0 {
		return 0
	}
	var currPerc = float64(currentSize) / float64(pm.totalSize) * 100
	if currPerc < pm.lastPerc {
		return pm.lastPerc
	}

	pm.lastPerc = currPerc
	return currPerc
}

func (pm *ProgressManager) GetETA(currentSize int) string {
	elapsed := time.Now().Unix() - pm.startTime
	remaining := float64(pm.totalSize-currentSize) / float64(currentSize) * float64(elapsed)
	return (time.Second * time.Duration(remaining)).String()
}

func (pm *ProgressManager) GetSpeed(currentSize int) string {
	// partSize = 512 * 512: 512KB
	partSize := 512 * 1024
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

func (pm *ProgressManager) GetStats(currentSize int) string {
	return fmt.Sprintf("Progress: %.2f%% | ETA: %s | Speed: %s\n%s", pm.GetProgress(currentSize), pm.GetETA(currentSize), pm.GetSpeed(currentSize), pm.GenProgressBar(currentSize))
}

func (pm *ProgressManager) GenProgressBar(b int) string {
	barLength := 50
	progress := int((pm.GetProgress(b) / 100) * float64(barLength))
	bar := "["

	for i := 0; i < barLength; i++ {
		if i < progress {
			bar += "="
		} else {
			bar += " "
		}
	}
	bar += "]"

	return fmt.Sprintf("\r%s %d%%", bar, int(pm.GetProgress(b)))
}
