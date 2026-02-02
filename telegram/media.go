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

	mtproto "github.com/amarnathcjd/gogram"
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
	workers []*mtproto.MTProto
	free    chan *mtproto.MTProto
}

func NewWorkerPool(size int) *WorkerPool {
	return &WorkerPool{
		workers: make([]*mtproto.MTProto, 0, size),
		free:    make(chan *mtproto.MTProto, size),
	}
}

func (wp *WorkerPool) AddWorker(s *mtproto.MTProto) {
	wp.Lock()
	defer wp.Unlock()
	wp.workers = append(wp.workers, s)
	wp.free <- s // Mark the worker as free immediately
}

func (wp *WorkerPool) Next() *mtproto.MTProto {
	return <-wp.free
}

func (wp *WorkerPool) FreeWorker(s *mtproto.MTProto) {
	wp.free <- s
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

	c.Log.Info(fmt.Sprintf("file - upload: (%s) - (%d) - (%d)", source.GetName(), size, parts))

	doneBytes := atomic.Int64{}
	doneArray := sync.Map{}

	go initializeWorkers(numWorkers, int32(c.GetDC()), c, w)

	var progressTicker = make(chan struct{}, 1)

	if opts.ProgressManager != nil {
		opts.ProgressManager.SetTotalSize(size)
		go func() {
			ticker := time.NewTicker(time.Duration(opts.ProgressManager.editInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-progressTicker:
					return
				case <-ticker.C:
					opts.ProgressManager.editFunc(size, doneBytes.Load())
				}
			}
		}()
	}

	MAX_RETRIES := 3
	sem := make(chan struct{}, numWorkers)

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

			for i := 0; i < MAX_RETRIES; i++ {
				sender := w.Next()
				if !IsFsBig {
					_, err = sender.MakeRequestCtx(context.Background(), &UploadSaveFilePartParams{
						FileID:   fileId,
						FilePart: int32(p),
						Bytes:    part,
					})
				} else {
					_, err = sender.MakeRequestCtx(context.Background(), &UploadSaveBigFilePartParams{
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

		for i := 0; i < MAX_RETRIES; i++ {
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

	close(sem)
	close(progressTicker)

	if opts.ProgressManager != nil {
		opts.ProgressManager.editFunc(size, size)
	}

	// destroy created workers
	// for _, worker := range w.workers {
	// 	worker.Terminate()
	// }

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
	if matchError(err, "FLOOD_WAIT_") || matchError(err, "FLOOD_PREMIUM_WAIT_") {
		if waitTime := getFloodWait(err); waitTime > 0 {
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

	c.Log.Info(fmt.Sprintf("file - download: (%s) - (%s) - (%d)", dest, sizetoHuman(size), parts))
	c.Log.Info(fmt.Sprintf("exporting senders: dc(%d) - workers(%d)", dc, numWorkers))

	go initializeWorkers(numWorkers, dc, c, w)

	var sem = make(chan struct{}, numWorkers)
	var wg sync.WaitGroup
	var doneBytes atomic.Int64
	var doneArray sync.Map

	var progressTicker = make(chan struct{}, 1)

	if opts.ProgressManager != nil {
		opts.ProgressManager.SetTotalSize(size)
		go func() {
			ticker := time.NewTicker(time.Duration(opts.ProgressManager.editInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-progressTicker:
					return
				case <-ticker.C:
					opts.ProgressManager.editFunc(size, doneBytes.Load())
				}
			}
		}()
	}

	MAX_RETRIES := 3

	for p := int64(0); p < parts; p++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer func() {
				<-sem
				wg.Done()
			}()

			for i := 0; i < MAX_RETRIES; i++ {
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
					panic("cdn redirect not implemented") // TODO
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

retrySinglePart:
	for _, p := range getUndoneParts(&doneArray, int(totalParts)) {
		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer func() {
				<-sem
				wg.Done()
			}()

			for i := 0; i < MAX_RETRIES; i++ {
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
					c.Log.Debug("seq-downloaded part ", p, "/", totalParts, " len: ", len(v.Bytes)/1024, "KB")
					fs.WriteAt(v.Bytes, int64(p)*int64(partSize))
					doneBytes.Add(int64(len(v.Bytes)))
					doneArray.Store(p, true)
				case *UploadFileCdnRedirect:
					panic("cdn redirect not implemented") // TODO
				case nil:
					continue
				default:
					return
				}
				break
			}
		}(p)
	}

	if len(getUndoneParts(&doneArray, int(totalParts))) > 0 { // Loop through failed parts
		goto retrySinglePart
	}

	wg.Wait()
	close(sem)
	close(progressTicker)

	if opts.ProgressManager != nil {
		opts.ProgressManager.editFunc(size, size)
	}

	// destroy created workers
	// for _, worker := range w.workers {
	// 	worker.Terminate()
	// }

	return dest, nil
}

func getUndoneParts(doneMap *sync.Map, totalParts int) []int {
	undoneSet := make([]int, 0, totalParts)
	for i := 0; i < totalParts; i++ {
		if _, found := doneMap.Load(i); !found {
			undoneSet = append(undoneSet, i)
		}
	}
	return undoneSet
}

func initializeWorkers(numWorkers int, dc int32, c *Client, w *WorkerPool) {
	var authParams = ExportedAuthParams{}
	if dc != int32(c.GetDC()) {
		auth, err := c.AuthExportAuthorization(dc)
		if err != nil {
			c.Log.Error(err)
			return
		}

		authParams = ExportedAuthParams{
			ID:    auth.ID,
			Bytes: auth.Bytes,
		}
	}

	numCreate := 0
	for dcId, workers := range c.exSenders {
		if int(dc) == dcId {
			for _, worker := range workers {
				w.AddWorker(worker)
				numCreate++
			}
		}
	}

	for i := numCreate; i < numWorkers; i++ {
		conn, err := c.CreateExportedSender(int(dc), false, authParams)
		if conn != nil && err == nil {
			if c.exSenders == nil {
				c.exSenders = make(map[int][]*mtproto.MTProto)
			}
			c.exSenders[int(dc)] = append(c.exSenders[int(dc)], conn)
			w.AddWorker(conn)
		}
	}
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

	sender, err := c.CreateExportedSender(int(dc), false)
	if err != nil {
		return nil, "", err
	}

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
			panic("cdn redirect not implemented") // TODO
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
}

func NewProgressManager(editInterval int) *ProgressManager {
	return &ProgressManager{
		startTime:    time.Now().Unix(),
		editInterval: editInterval,
	}
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
