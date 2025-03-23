// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
)

type UploadOptions struct {
	// Worker count for upload file.
	Threads int `json:"threads,omitempty"`
	//  Chunk size for upload file.
	ChunkSize int32 `json:"chunk_size,omitempty"`
	// File name for upload file.
	FileName string `json:"file_name,omitempty"`
	// Progress callback for upload file.
	ProgressManager *ProgressManager `json:"-"`
}

type WorkerPool struct {
	sync.Mutex
	workers []*ExSender
	free    chan *ExSender
}

func NewWorkerPool(size int) *WorkerPool {
	return &WorkerPool{
		workers: make([]*ExSender, 0, size),
		free:    make(chan *ExSender, size),
	}
}

func (wp *WorkerPool) AddWorker(s *ExSender) {
	wp.Lock()
	defer wp.Unlock()
	wp.workers = append(wp.workers, s)
	wp.free <- s // Mark the worker as free immediately
}

func (wp *WorkerPool) Next() *ExSender {
	next := <-wp.free
	if !next.MTProto.IsTcpActive() {
		next.MTProto.Reconnect(false)
	}
	next.lastUsed = time.Now()
	return next
}

func (wp *WorkerPool) FreeWorker(s *ExSender) {
	select {
	case wp.free <- s:
	default:
	}
}

type Source struct {
	Source any
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

func (c *Client) UploadFile(src any, Opts ...*UploadOptions) (InputFile, error) {
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

	w := NewWorkerPool(numWorkers)

	c.Log.Info(fmt.Sprintf("file - upload: (%s) - (%s) - (%d)", source.GetName(), SizetoHuman(size), parts))

	doneBytes := atomic.Int64{}
	doneArray := sync.Map{}

	if err := initializeWorkers(numWorkers, int32(c.GetDC()), c, w); err != nil {
		return nil, err
	}

	stopProgress := make(chan struct{})
	defer close(stopProgress)

	if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(source.GetName())
		opts.ProgressManager.lastPerc = 0
		opts.ProgressManager.IncCount()
		opts.ProgressManager.SetTotalSize(size)
		opts.ProgressManager.SetMeta(c.GetDC(), numWorkers)

		opts.ProgressManager.editFunc(size, 0) // Initial edit

		go func() {
			ticker := time.NewTicker(time.Duration(opts.ProgressManager.editInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-stopProgress:
					return
				case <-ticker.C:
					current := min(doneBytes.Load(), size)
					opts.ProgressManager.editFunc(size, current)
				}
			}
		}()
	}

	MAX_RETRIES := 3
	sem := make(chan struct{}, numWorkers)
	defer close(sem)

	for p := int64(0); p < parts; p++ {
		sem <- struct{}{}
		part := make([]byte, partSize)
		_, err := file.Read(part)
		if err != nil {
			c.Log.Error(err)
			return nil, err
		}

		wg.Add(1)
		go func(p int, part []byte) {
			defer func() {
				<-sem
				wg.Done()
			}()

			for range MAX_RETRIES {
				sender := w.Next()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if !IsFsBig {
					_, err = sender.MakeRequestCtx(ctx, &UploadSaveFilePartParams{
						FileID:   fileId,
						FilePart: int32(p),
						Bytes:    part,
					})
				} else {
					_, err = sender.MakeRequestCtx(ctx, &UploadSaveBigFilePartParams{
						FileID:         fileId,
						FilePart:       int32(p),
						FileTotalParts: int32(totalParts),
						Bytes:          part,
					})
				}
				w.FreeWorker(sender)

				if err != nil {
					if handleIfFlood(err, c) {
						continue
					}
					c.Log.Debug(err)
					continue
				}

				c.Log.Debug(fmt.Sprintf("uploaded part %d/%d in chunks of %d KB", p, totalParts, len(part)/1024))

				doneBytes.Add(int64(len(part)))
				if !IsFsBig {
					hash.Write(part)
				}
				doneArray.Store(p, true)
				break
			}
		}(int(p), part)
	}

	wg.Wait()

	if partOver > 0 {
		part := make([]byte, partOver)
		_, err := file.Read(part)
		if err != nil {
			c.Log.Error(err)
			return nil, err
		}

		for range MAX_RETRIES {
			sender := w.Next()
			if !IsFsBig {
				_, err = sender.MakeRequestCtx(context.Background(), &UploadSaveFilePartParams{
					FileID:   fileId,
					FilePart: int32(totalParts - 1),
					Bytes:    part,
				})
			} else {
				_, err = sender.MakeRequestCtx(context.Background(), &UploadSaveBigFilePartParams{
					FileID:         fileId,
					FilePart:       int32(totalParts - 1),
					FileTotalParts: int32(totalParts),
					Bytes:          part,
				})
			}
			w.FreeWorker(sender)
			if err != nil {
				if handleIfFlood(err, c) {
					continue
				}
				c.Log.Debug(err)
				continue
			}

			doneBytes.Add(int64(len(part)))
			if !IsFsBig {
				hash.Write(part)
			}

			doneArray.Store(int(totalParts-1), true)
			break
		}
	}

	if opts.ProgressManager != nil {
		opts.ProgressManager.editFunc(size, size)
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

// Internal flood sleep handler
func handleIfFlood(err error, c *Client) bool {
	if MatchError(err, "FLOOD_WAIT_") || MatchError(err, "FLOOD_PREMIUM_WAIT_") {
		if waitTime := GetFloodWait(err); waitTime > 0 {
			c.Log.Debug("flood wait ", waitTime, "(s), waiting...")
			time.Sleep(time.Duration(waitTime) * time.Second)

			if c.clientData.sleepThresholdMs > 0 {
				time.Sleep(time.Duration(c.clientData.sleepThresholdMs) * time.Millisecond)
			}
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

func chunkSizeCalc(size int64) int {
	if size < 200*1024*1024 { // 200MB
		return 256 * 1024 // 256KB
	} else if size < 1024*1024*1024 { // 1GB
		return 512 * 1024 // 512KB
	}
	return 1024 * 1024 // 1MB
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
	ProgressManager *ProgressManager `json:"-"`
	// Datacenter ID of file
	DCId int32 `json:"dc_id,omitempty"`
	// Destination Writer
	Buffer io.Writer `json:"-"`
	// Weather to download the thumb only
	ThumbOnly bool `json:"thumb_only,omitempty"`
	// Thumb size to download
	ThumbSize PhotoSize `json:"thumb_size,omitempty"`
	// Weather to download video file (profile photo, etc)
	IsVideo bool `json:"is_video,omitempty"`
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

func (c *Client) DownloadMedia(file any, Opts ...*DownloadOptions) (string, error) {
	opts := getVariadic(Opts, &DownloadOptions{})

	location, dc, size, fileName, err := GetFileLocation(file, FileLocationOptions{
		ThumbOnly: opts.ThumbOnly,
		ThumbSize: opts.ThumbSize,
		Video:     opts.IsVideo,
	})
	if err != nil {
		return "", err
	}

	dc = getValue(dc, opts.DCId)
	if dc == 0 {
		dc = int32(c.GetDC())
	}
	dest := getValue(opts.FileName, fileName)

	partSize := chunkSizeCalc(size)
	if opts.ChunkSize > 0 {
		if opts.ChunkSize > 1048576 || (1048576%opts.ChunkSize) != 0 {
			return "", errors.New("chunk size must be a multiple of 1048576 (1MB)")
		}
		partSize = int(opts.ChunkSize)
	}

	dest = sanitizePath(dest, fileName)

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

	var w = NewWorkerPool(numWorkers)

	if opts.Buffer != nil {
		dest = ":mem-buffer:"
		c.Log.Warn("downloading to buffer (memory) - use with caution (memory usage)")
	}

	c.Log.Info(fmt.Sprintf("file - download: (%s) - (%s) - (%d)", dest, SizetoHuman(size), parts))

	if err := initializeWorkers(numWorkers, dc, c, w); err != nil {
		return "", err
	}

	var sem = make(chan struct{}, numWorkers)
	defer close(sem)

	var wg sync.WaitGroup
	var doneBytes atomic.Int64
	var doneArray sync.Map

	stopProgress := make(chan struct{})
	defer close(stopProgress)

	if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(dest)
		opts.ProgressManager.lastPerc = 0
		opts.ProgressManager.IncCount()
		opts.ProgressManager.SetTotalSize(size)
		opts.ProgressManager.SetMeta(int(dc), numWorkers)

		opts.ProgressManager.editFunc(size, 0) // Initial edit

		go func() {
			ticker := time.NewTicker(time.Duration(opts.ProgressManager.editInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-stopProgress:
					return
				case <-ticker.C:
					current := min(doneBytes.Load(), size)
					opts.ProgressManager.editFunc(size, current)
				}
			}
		}()
	}

	MAX_RETRIES := 3
	var cdnRedirect atomic.Bool
	for p := int64(0); p < totalParts; p++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer func() {
				<-sem
				wg.Done()
			}()

			for range MAX_RETRIES {
				if cdnRedirect.Load() {
					return
				}
				sender := w.Next()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				part, err := sender.MakeRequestCtx(ctx, &UploadGetFileParams{
					Location:     location,
					Offset:       int64(p * partSize),
					Limit:        int32(partSize),
					Precise:      true,
					CdnSupported: false,
				})
				w.FreeWorker(sender)
				if err != nil {
					if handleIfFlood(err, c) {
						continue
					}

					if strings.Contains(err.Error(), "FILE_REFERENCE_EXPIRED") {
						c.Log.Debug(err)
						return // File reference expired
					}

					c.Log.Debug(errors.Wrap(err, fmt.Sprintf("part - (%d) - retrying...", p)))
					continue
				}

				switch v := part.(type) {
				case *UploadFileObj:
					c.Log.Debug("downloaded part ", p, "/", totalParts, " len: ", len(v.Bytes)/1024, "KB")
					fs.WriteAt(v.Bytes, int64(p)*int64(partSize))
					doneBytes.Add(int64(len(v.Bytes)))
					doneArray.Store(p, true)
				case *UploadFileCdnRedirect:
					cdnRedirect.Store(true)
				case nil:
					continue
				default:
					return
				}
				break
			}
		}(int(p))
	}
	wg.Wait()
	if cdnRedirect.Load() {
		return "", errors.New("cdn redirect not implemented")
	}

retrySinglePart:
	for _, p := range getUndoneParts(&doneArray, int(totalParts)) {
		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer func() {
				<-sem
				wg.Done()
			}()

			for range MAX_RETRIES {
				sender := w.Next()
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()

				part, err := sender.MakeRequestCtx(ctx, &UploadGetFileParams{
					Location:     location,
					Offset:       int64(p * partSize),
					Limit:        int32(partSize),
					Precise:      true,
					CdnSupported: false,
				})
				w.FreeWorker(sender)

				if err != nil {
					if handleIfFlood(err, c) {
						continue
					}
					if strings.Contains(err.Error(), "FILE_REFERENCE_EXPIRED") {
						c.Log.Debug(err)
						return // File reference expired
					}

					c.Log.Debug(errors.Wrap(err, fmt.Sprintf("part - (%d) - retrying...", p)))
					continue
				}

				switch v := part.(type) {
				case *UploadFileObj:
					c.Log.Debug("downloaded part ", p, "/", totalParts, " len: ", len(v.Bytes)/1024, "KB")
					fs.WriteAt(v.Bytes, int64(p)*int64(partSize))
					doneBytes.Add(int64(len(v.Bytes)))
					doneArray.Store(p, true)
				case *UploadFileCdnRedirect:
					cdnRedirect.Store(true) // TODO
				case nil:
					continue
				default:
					return
				}
				break
			}
		}(p)
	}

	if !cdnRedirect.Load() && len(getUndoneParts(&doneArray, int(totalParts))) > 0 { // Loop through failed parts
		goto retrySinglePart
	}

	wg.Wait()
	if cdnRedirect.Load() {
		return "", errors.New("cdn redirect not implemented")
	}

	if opts.ProgressManager != nil {
		opts.ProgressManager.editFunc(size, size)
	}

	return dest, nil
}

func getUndoneParts(doneMap *sync.Map, totalParts int) []int {
	undoneSet := make([]int, 0, totalParts)
	for i := range totalParts {
		if _, found := doneMap.Load(i); !found {
			undoneSet = append(undoneSet, i)
		}
	}
	return undoneSet
}

func initializeWorkers(numWorkers int, dc int32, c *Client, w *WorkerPool) error {
	if numWorkers == 1 && dc == int32(c.GetDC()) {
		w.AddWorker(&ExSender{c.MTProto, time.Now()})
		return nil
	}

	var authParams = &AuthExportedAuthorization{}
	if dc != int32(c.GetDC()) {
		if c.exportedKeys == nil {
			c.exportedKeys = make(map[int]*AuthExportedAuthorization)
		}

		if exportedKey, ok := c.exportedKeys[int(dc)]; ok {
			authParams = exportedKey
		} else {
			auth, err := c.AuthExportAuthorization(dc)
			if err != nil {
				return err
			}

			authParams = &AuthExportedAuthorization{
				ID:    auth.ID,
				Bytes: auth.Bytes,
			}

			c.exportedKeys[int(dc)] = authParams
		}
	}

	go func() {
		numCreate := 0
		for dcId, workers := range c.exSenders.senders {
			if int(dc) == dcId {
				for _, worker := range workers {
					w.AddWorker(worker)
					numCreate++
				}
			}
		}

		if numCreate < numWorkers {
			c.Log.Info(fmt.Sprintf("exporting senders: dc(%d) - workers(%d)", dc, numWorkers-numCreate))
		}

		for i := numCreate; i < numWorkers; i++ {
			conn, err := c.CreateExportedSender(int(dc), false, authParams)
			if conn != nil && err == nil {
				sender := &ExSender{conn, time.Now()}
				c.exSenders.senders[int(dc)] = append(c.exSenders.senders[int(dc)], sender)
				w.AddWorker(sender)
			}
		}
	}()

	return nil
}

// DownloadChunk downloads a file in chunks, useful for downloading specific parts of a file.
//
// start and end are the byte offsets to download.
// chunkSize is the size of each chunk to download.
//
// Note: chunkSize must be a multiple of 1048576 (1MB)
func (c *Client) DownloadChunk(media any, start int, end int, chunkSize int) ([]byte, string, error) {
	var buf []byte
	input, dc, size, name, err := GetFileLocation(media)
	if err != nil {
		return nil, "", err
	}

	if chunkSize > 1048576 || (1048576%chunkSize) != 0 {
		return nil, "", errors.New("chunk size must be a multiple of 1048576 (1MB)")
	}

	if end > int(size) {
		end = int(size)
	}

	w := NewWorkerPool(1)
	if err := initializeWorkers(1, int32(dc), c, w); err != nil {
		return nil, "", err
	}

	sender := w.Next()
	defer w.FreeWorker(sender)

	for curr := start; curr < end; curr += chunkSize {
		part, err := sender.MakeRequest(&UploadGetFileParams{
			Location:     input,
			Limit:        int32(chunkSize),
			Offset:       int64(curr),
			CdnSupported: false,
		})

		if err != nil {
			c.Log.Error(err)
		}

		switch v := part.(type) {
		case *UploadFileObj:
			buf = append(buf, v.Bytes...)
		case *UploadFileCdnRedirect:
			return nil, "", errors.New("cdn redirect not implemented")
		}
	}

	return buf, name, nil
}

// ----------------------- Progress Manager -----------------------
type ProgressManager struct {
	startTime    int64
	editInterval int
	editFunc     func(totalSize, currentSize int64)
	totalSize    int64
	lastPerc     float64
	fileName     string
	fileCount    int
	meta         struct {
		dataCenter int
		numWorkers int
	}
}

func NewProgressManager(editInterval int, editFunc ...func(totalSize, currentSize int64)) *ProgressManager {
	var pm = &ProgressManager{
		startTime:    time.Now().Unix(),
		editInterval: editInterval,
	}

	if len(editFunc) > 0 {
		pm.editFunc = editFunc[0]
	}

	return pm
}

func (pm *ProgressManager) SetMessage(msg *NewMessage) *ProgressManager {
	pm.editFunc = MediaDownloadProgress(msg, pm)
	return pm
}

func (pm *ProgressManager) SetInlineMessage(client *Client, inline *InputBotInlineMessageID) *ProgressManager {
	pm.editFunc = MediaDownloadProgress(&NewMessage{
		Client: client,
	}, pm, inline)
	return pm
}

// WithEdit sets the edit function for the progress manager.
func (pm *ProgressManager) WithEdit(editFunc func(totalSize, currentSize int64)) *ProgressManager {
	pm.editFunc = editFunc
	return pm
}

func (pm *ProgressManager) Edit(editFunc func(totalSize, currentSize int64)) {
	pm.editFunc = editFunc
}

func (pm *ProgressManager) SetTotalSize(totalSize int64) {
	pm.totalSize = totalSize
}

func (pm *ProgressManager) SetMeta(dataCenter, numWorkers int) {
	pm.meta.dataCenter = dataCenter
	pm.meta.numWorkers = numWorkers
}

func (pm *ProgressManager) PrintFunc() func(a, b int64) {
	return func(a, b int64) {
		fmt.Println(pm.GetStats(b))
	}
}

// specify the message to edit
func (pm *ProgressManager) WithMessage(msg *NewMessage) func(a, b int64) {
	return func(a, b int64) {
		msg.Edit(pm.GetStats(b))
	}
}

func (pm *ProgressManager) SetFileName(fileName string) {
	pm.fileName = fileName
}

func (pm *ProgressManager) GetFileName() string {
	return pm.fileName
}

func (pm *ProgressManager) IncCount() {
	pm.fileCount++
}

func (pm *ProgressManager) GetCount() int {
	return pm.fileCount
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
	barLength := 20
	progress := int((pm.GetProgress(b) / 100) * float64(barLength))
	bar := "["

	for i := range barLength {
		if i < progress {
			bar += "="
		} else {
			bar += " "
		}
	}
	bar += "]"

	return fmt.Sprintf("\r%s %d%%", bar, int(pm.GetProgress(b)))
}

func MediaDownloadProgress(editMsg *NewMessage, pm *ProgressManager, inline ...*InputBotInlineMessageID) func(totalBytes, currentBytes int64) {
	return func(totalBytes int64, currentBytes int64) {
		text := ""
		text += "<b>📄 Name:</b> <code>%s</code>\n"
		text += "<b>🈂️ DC ID:</b> <code>%d</code> <b>|</b> <b>⚡Workers:</b> <code>%d</code>\n\n"
		text += "<b>💾 File Size:</b> <code>%.2f MiB</code>\n"
		text += "<b>⌛️ ETA:</b> <code>%s</code>\n"
		text += "<b>⏱ Speed:</b> <code>%s</code>\n"
		text += "<b>⚙️ Progress:</b> %s <code>%.2f%%</code>"

		size := float64(totalBytes) / 1024 / 1024
		eta := pm.GetETA(currentBytes)
		speed := pm.GetSpeed(currentBytes)
		percent := pm.GetProgress(currentBytes)

		progressbar := strings.Repeat("■", int(percent/10)) + strings.Repeat("□", 10-int(percent/10))

		message := fmt.Sprintf(text, pm.GetFileName(), pm.meta.dataCenter, pm.meta.numWorkers, size, eta, speed, progressbar, percent)
		if len(inline) > 0 {
			editMsg.Client.EditMessage(inline[0], 0, message)
		} else {
			editMsg.Edit(message)
		}
	}
}
