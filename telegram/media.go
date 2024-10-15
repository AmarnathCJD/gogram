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
	"sync/atomic"
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
	ProgressCallback func(totalParts int64, uploadedParts int64) `json:"-"`
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

func (s *Source) GetName() string {
	switch src := s.Source.(type) {
	case string:
		file, err := os.Open(src)
		if err != nil {
			return ""
		}
		return file.Name()
	case *os.File:
		return src.Name()
	}
	return ""
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
	opts := getVariadic(Opts, &UploadOptions{})
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

	c.Logger.Info(fmt.Sprintf("file - upload: (%s) - (%d) - (%d)", source.GetName(), size, parts))
	c.Logger.Info(fmt.Sprintf("expected workers: %d, preallocated workers: %d", numWorkers, sendersPreallocated))

	c.Logger.Debug(fmt.Sprintf("expected workers: %d, preallocated workers: %d", numWorkers, sendersPreallocated))

	nW := numWorkers
	numWorkers = sendersPreallocated

	doneBytes := atomic.Int64{}

	createAndAppendSender := func(dcId int, senders []Sender, senderIndex int) {
		conn, _ := c.CreateExportedSender(dcId)
		if conn != nil {
			senders[senderIndex] = Sender{c: conn}
			go c.AddNewExportedSenderToMap(dcId, conn)
			numWorkers++
		}
	}

	go func() {
		for i := sendersPreallocated; i < nW; i++ {
			createAndAppendSender(c.GetDC(), sender, i)
		}
	}()

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
					partUploadStartPoint:
						c.Logger.Debug(fmt.Sprintf("uploading part %d/%d in chunks of %d", p, totalParts, len(part)/1024))
						if !IsFsBig {
							_, err = sender[i].c.UploadSaveFilePart(fileId, int32(p), part)
						} else {
							_, err = sender[i].c.UploadSaveBigFilePart(fileId, int32(p), int32(totalParts), part)
						}
						if err != nil {
							if handleIfFlood(err, c) {
								goto partUploadStartPoint
							}
							c.Logger.Error(err)
						}
						doneBytes.Add(int64(len(part)))

						if opts.ProgressCallback != nil {
							go opts.ProgressCallback(size, doneBytes.Load())
						}
						if !IsFsBig {
							hash.Write(part)
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

		doneBytes.Add(int64(len(part)))
		if opts.ProgressCallback != nil {
			go opts.ProgressCallback(size, doneBytes.Load())
		}
	}

	if opts.FileName != "" {
		fileName = opts.FileName
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
	if matchError(err, "FLOOD_WAIT_") || matchError(err, "FLOOD_PREMIUM_WAIT_") {
		if waitTime := getFloodWait(err); waitTime > 0 {
			c.Logger.Debug("flood wait ", waitTime, "(s), waiting...")
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
		return 1
	} else if parts <= 10 {
		return 2
	} else if parts <= 50 {
		return 3
	} else if parts <= 100 {
		return 6
	} else if parts <= 200 {
		return 7
	} else if parts <= 400 {
		return 8
	} else if parts <= 500 {
		return 10
	} else {
		return 12 // not recommended to use more than 15 workers
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
	// output Callback for download progress in bytes.
	ProgressCallback func(totalBytes int64, downloadedBytes int64) `json:"-"`
	// Datacenter ID of file
	DCId int32 `json:"dc_id,omitempty"`
	// Destination Writer
	Buffer io.Writer `json:"-"`
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

func (mb *Destination) Close() error {
	if mb.file != nil {
		return mb.file.Close()
	}
	return nil
}

func (c *Client) DownloadMedia(file interface{}, Opts ...*DownloadOptions) (string, error) {
	opts := getVariadic(Opts, &DownloadOptions{})
	location, dc, size, fileName, err := GetFileLocation(file)
	if err != nil {
		return "", err
	}

	dc = getValue(dc, opts.DCId)
	if dc == 0 {
		dc = int32(c.GetDC())
	}
	dest := getValue(opts.FileName, fileName)

	partSize := 2048 * 512 // 1MB
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
	}
	defer fs.Close()

	parts := size / int64(partSize)
	partOver := size % int64(partSize)
	totalParts := parts
	if partOver > 0 {
		totalParts++
	}

	numWorkers := countWorkers(parts)
	if opts.Threads > 0 {
		numWorkers = opts.Threads
	}

	w, wPreallocated := c.initializeWorkers(numWorkers, dc)

	if opts.Buffer != nil {
		dest = ":mem-buffer:"
		c.Logger.Warn("downloading to buffer (memory) - use with caution")
	}

	c.Logger.Info(fmt.Sprintf("file - download: (%s) - (%d) - (%d)", dest, size, parts))
	c.Logger.Info(fmt.Sprintf("expected workers: %d, preallocated workers: %d", numWorkers, wPreallocated))
	c.Logger.Debug(fmt.Sprintf("expected workers: %d, preallocated workers: %d", numWorkers, wPreallocated))

	go c.allocateRemainingWorkers(dc, w, numWorkers, wPreallocated)

	mu := sync.Mutex{}
	wg := sync.WaitGroup{}
	var doneArray = make([]bool, totalParts+1)
	var doneBytes = atomic.Int64{}

	c.downloadParts(&wg, &mu, w, partSize, doneArray, numWorkers, location, &fs, opts, parts, &doneBytes)

	wg.Wait()

	if partOver > 0 {
		c.downloadLastPart(dc, w, location, partSize, totalParts, parts, &fs, opts, doneArray, partOver, &doneBytes)
	}

	if opts.ProgressCallback != nil {
		opts.ProgressCallback(size, doneBytes.Load())
	}

	if opts.Buffer != nil {
		c.Logger.Debug("writing to buffer, size: ", len(fs.data))
		opts.Buffer.Write(fs.data)
		return "", nil
	}

	c.retryFailedParts(totalParts, dc, w, doneArray, &fs, opts, location, partSize, &doneBytes)
	return dest, nil
}

func (c *Client) initializeWorkers(numWorkers int, dc int32) ([]Sender, int) {
	w := make([]Sender, numWorkers)
	wPreallocated := 0

	if pre := c.GetCachedExportedSenders(int(dc)); len(pre) > 0 {
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

	return w, wPreallocated
}

func (c *Client) allocateRemainingWorkers(dc int32, w []Sender, numWorkers, wPreallocated int) {
	for i := wPreallocated; i < numWorkers; i++ {
		c.createAndAppendSender(int(dc), w, i)
	}
}

func (c *Client) createAndAppendSender(dcId int, senders []Sender, senderIndex int) {
	conn, err := c.CreateExportedSender(dcId)
	if conn != nil && err == nil {
		senders[senderIndex] = Sender{c: conn}
		go c.AddNewExportedSenderToMap(dcId, conn)
	}
}

func (c *Client) downloadParts(wg *sync.WaitGroup, mu *sync.Mutex, w []Sender, partSize int, doneArray []bool, numWorkers int, location InputFileLocation, fs *Destination, opts *DownloadOptions, parts int64, doneBytes *atomic.Int64) {
	taskCh := make(chan int64, parts)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerIndex int) {
			defer wg.Done()
			for part := range taskCh {
				var success bool
				for !success {
					mu.Lock()
					if w[workerIndex].c == nil || w[workerIndex].buzy {
						mu.Unlock()
						time.Sleep(10 * time.Millisecond)
						continue
					}
					w[workerIndex].buzy = true
					mu.Unlock()
					c.downloadPart(wg, mu, w, workerIndex, int(part), partSize, doneArray, location, fs, opts, doneBytes, parts*int64(partSize))
					success = true
				}
			}
		}(i)
	}
	for p := int64(0); p < parts; p++ {
		wg.Add(1)
		taskCh <- p
	}
	close(taskCh)
}

func (c *Client) downloadPart(wg *sync.WaitGroup, mu *sync.Mutex, w []Sender, workerIndex, p int, partSize int, doneArray []bool, location InputFileLocation, fs *Destination, opts *DownloadOptions, doneBytes *atomic.Int64, totalBytes int64) {
	defer wg.Done()
	defer func() {
		mu.Lock()
		w[workerIndex].buzy = false
		mu.Unlock()
	}()

	retryCount := 0
	reqTimeout := 4 * time.Second

partDownloadStartPoint:
	c.Logger.Debug(fmt.Sprintf("download part %d in chunks of %d", p, partSize/1024))

	resultChan := make(chan UploadFile, 1)
	errorChan := make(chan error, 1)

	go func() {
		upl, err := w[workerIndex].c.UploadGetFile(&UploadGetFileParams{
			Location:     location,
			Offset:       int64(p * partSize),
			Limit:        int32(partSize),
			Precise:      true,
			CdnSupported: false,
		})
		if err != nil {
			errorChan <- err
			return
		}
		resultChan <- upl
	}()

	select {
	case upl := <-resultChan:
		if upl == nil {
			goto partDownloadStartPoint
		}

		c.processDownloadedPart(upl, p, doneArray, fs, partSize, opts, doneBytes, totalBytes)
	case err := <-errorChan:
		if handleIfFlood(err, c) {
			goto partDownloadStartPoint
		}
		c.Logger.Error(err)
	case <-time.After(reqTimeout):
		retryCount++
		if retryCount > 2 {
			c.Logger.Debug(fmt.Errorf("upload part %d timed out - queuing for seq", p))
			return
		}
		goto partDownloadStartPoint
	}
}

func (c *Client) processDownloadedPart(upl UploadFile, p int, doneArray []bool, fs *Destination, partSize int, opts *DownloadOptions, doneBytes *atomic.Int64, size int64) {
	var buffer []byte
	switch v := upl.(type) {
	case *UploadFileObj:
		buffer = v.Bytes
		doneArray[p] = true
	case *UploadFileCdnRedirect:
		panic("CDN redirect not implemented") // TODO
	}

	_, err := fs.WriteAt(buffer, int64(p*partSize))
	if err != nil {
		c.Logger.Error(err)
	}

	doneBytes.Add(int64(len(buffer)))
	if opts.ProgressCallback != nil {
		go opts.ProgressCallback(size, doneBytes.Load())
	}
}

func (c *Client) downloadLastPart(dc int32, w []Sender, location InputFileLocation, partSize int, totalParts int64, parts int64, fs *Destination, opts *DownloadOptions, doneArray []bool, partOver int64, doneBytes *atomic.Int64) {
downloadLastPartStartPoint:
	if w[0].c == nil {
		for i := 0; i < len(w); i++ {
			if w[i].c != nil {
				w[0].c = w[i].c
				break
			}
		}
	}

	if w[0].c == nil {
		c.createAndAppendSender(int(dc), w, 0)
	}

	if w[0].c == nil {
		c.Logger.Warn(errors.Wrap(errors.New("failed to create sender for dc "+fmt.Sprint(dc)), "download failed"))
		return
	}

	c.Logger.Debug(fmt.Sprintf("downloading last part %d in chunks of %d", totalParts-1, partOver/1024))

	retryCount := 0
	reqTimeout := 4 * time.Second

	resultChan := make(chan UploadFile, 1)
	errorChan := make(chan error, 1)

	go func() {
		upl, err := c.UploadGetFile(&UploadGetFileParams{
			Location:     location,
			Offset:       int64(int(parts) * partSize),
			Limit:        int32(partSize),
			Precise:      true,
			CdnSupported: false,
		})
		if err != nil {
			errorChan <- err
			return
		}
		resultChan <- upl
	}()

	select {
	case upl := <-resultChan:
		if upl == nil {
			goto downloadLastPartStartPoint
		}

		c.processDownloadedPart(upl, int(parts), doneArray, fs, partSize, opts, doneBytes, parts*int64(partSize))
	case err := <-errorChan:
		if handleIfFlood(err, c) {
			goto downloadLastPartStartPoint
		}
		c.Logger.Error(err)
	case <-time.After(reqTimeout):
		retryCount++
		if retryCount > 2 {
			c.Logger.Debug(fmt.Errorf("upload part %d timed out - queuing for seq", parts))
			return
		}
		goto downloadLastPartStartPoint
	}
}

func (c *Client) retryFailedParts(totalParts int64, dc int32, w []Sender, doneArray []bool, fs *Destination, opts *DownloadOptions, location InputFileLocation, partSize int, doneBytes *atomic.Int64) {
	if w[0].c == nil {
		for i := 0; i < len(w); i++ {
			if w[i].c != nil {
				w[0].c = w[i].c
				break
			}
		}
	}

	if w[0].c == nil {
		c.createAndAppendSender(int(dc), w, 0)
	}

	if w[0].c == nil {
		c.Logger.Warn(errors.Wrap(errors.New("failed to create sender for dc "+fmt.Sprint(dc)), "download failed"))
		return
	}

	for i, v := range doneArray {
		if !v && i < int(totalParts) {
			c.Logger.Debug("seq retrying part ", i, " of ", totalParts)
			upl, err := w[0].c.UploadGetFile(&UploadGetFileParams{
				Location:     location,
				Offset:       int64(i * partSize),
				Limit:        int32(partSize),
				Precise:      true,
				CdnSupported: false,
			})

			if err != nil {
				c.Logger.Error(err)
				continue
			}

			c.processDownloadedPart(upl, i, doneArray, fs, partSize, opts, doneBytes, totalParts*int64(partSize))
		}
	}
}

// ----------------------- Progress Manager -----------------------
type ProgressManager struct {
	startTime    int64
	editInterval int
	lastEdit     int64
	totalSize    int64
	lastPerc     float64
}

func NewProgressManager(totalBytes int64, editInterval int) *ProgressManager {
	return &ProgressManager{
		startTime:    time.Now().Unix(),
		editInterval: editInterval,
		totalSize:    totalBytes,
		lastEdit:     time.Now().Unix(),
	}
}

func (pm *ProgressManager) SetTotalSize(totalSize int64) {
	pm.totalSize = totalSize
}

func (pm *ProgressManager) PrintFunc() func(a, b int64) {
	return func(a, b int64) {
		pm.SetTotalSize(a)
		if pm.ShouldEdit() {
			fmt.Println(pm.GetStats(b))
		} else {
			fmt.Println(pm.GetStats(b))
		}
	}
}

func (pm *ProgressManager) EditFunc(msg *NewMessage) func(a, b int64) {
	return func(a, b int64) {
		if pm.ShouldEdit() {
			_, _ = msg.Client.EditMessage(msg.Peer, msg.ID, pm.GetStats(b))
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

func (pm *ProgressManager) GetProgress(currentBytes int64) float64 {
	if pm.totalSize == 0 {
		return 0
	}
	var currPerc = float64(currentBytes) / float64(pm.totalSize) * 100
	if currPerc < pm.lastPerc {
		return pm.lastPerc
	}

	pm.lastPerc = currPerc
	return currPerc
}

func (pm *ProgressManager) GetETA(currentBytes int64) string {
	elapsed := time.Now().Unix() - pm.startTime
	remaining := float64(pm.totalSize-currentBytes) / float64(currentBytes) * float64(elapsed)
	return (time.Second * time.Duration(remaining)).String()
}

func (pm *ProgressManager) GetSpeed(currentBytes int64) string {
	elapsedTime := time.Since(time.Unix(pm.startTime, 0))
	if int(elapsedTime.Seconds()) == 0 {
		return "0 B/s"
	}
	speedBps := float64(currentBytes) / elapsedTime.Seconds()
	if speedBps < 1024 {
		return fmt.Sprintf("%.2f B/s", speedBps)
	} else if speedBps < 1024*1024 {
		return fmt.Sprintf("%.2f KB/s", speedBps/1024)
	} else {
		return fmt.Sprintf("%.2f MB/s", speedBps/1024/1024)
	}
}

func (pm *ProgressManager) GetStats(currentBytes int64) string {
	return fmt.Sprintf("Progress: %.2f%% | ETA: %s | Speed: %s\n%s", pm.GetProgress(currentBytes), pm.GetETA(currentBytes), pm.GetSpeed(currentBytes), pm.GenProgressBar(currentBytes))
}

func (pm *ProgressManager) GenProgressBar(b int64) string {
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
