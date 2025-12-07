// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"errors"
)

type UploadOptions struct {
	// Worker count for upload file.
	Threads int `json:"threads,omitempty"`
	//  Chunk size for upload file.
	ChunkSize int32 `json:"chunk_size,omitempty"`
	// File name for upload file.
	FileName string `json:"file_name,omitempty"`
	// Progress callback: receives ProgressInfo with all transfer details
	ProgressCallback func(*ProgressInfo) `json:"-"`
	// Progress manager (legacy support)
	ProgressManager *ProgressManager `json:"-"`
	// Progress callback interval in seconds (default: 5)
	ProgressInterval int `json:"progress_interval,omitempty"`
	// Delay between chunks in milliseconds (default: 0)
	Delay int `json:"delay,omitempty"`
	// Context for cancellation.
	Ctx context.Context `json:"-"`
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
	wp.workers = append(wp.workers, s)
	wp.Unlock()

	select {
	case wp.free <- s:
	default:
	}
}

func (wp *WorkerPool) Next() *ExSender {
	return wp.NextWithContext(context.Background())
}

func (wp *WorkerPool) NextWithContext(ctx context.Context) *ExSender {
	select {
	case next := <-wp.free:
		if !next.MTProto.IsTcpActive() {
			go func(mt *ExSender) {
				_ = mt.Reconnect(false)
			}(next)
		}

		next.lastUsedMu.Lock()
		next.lastUsed = time.Now()
		next.lastUsedMu.Unlock()
		return next
	case <-ctx.Done():
		return nil
	}
}

func (wp *WorkerPool) WaitReady(ctx context.Context) bool {
	for {
		wp.Lock()
		count := len(wp.workers)
		wp.Unlock()
		if count > 0 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (wp *WorkerPool) FreeWorker(s *ExSender) {
	select {
	case wp.free <- s:
	default:
	}
}

func (wp *WorkerPool) Close() {
	wp.Lock()
	defer wp.Unlock()

	// Drain the free channel
	for {
		select {
		case <-wp.free:
		default:
			wp.workers = nil
			return
		}
	}
}

type Source struct {
	Source any
	closer io.Closer
}

func (s *Source) GetSizeAndName() (int64, string) {
	switch src := s.Source.(type) {
	case string:
		file, err := os.Open(src)
		if err != nil {
			return 0, ""
		}
		defer file.Close()
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
		defer file.Close()
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
		s.closer = file
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

func (s *Source) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}

func (c *Client) UploadFile(src any, Opts ...*UploadOptions) (InputFile, error) {
	opts := getVariadic(Opts, &UploadOptions{})
	if src == nil {
		return nil, errors.New("you must provide a valid file source")
	}

	source := &Source{Source: src}
	defer source.Close()
	size, fileName := source.GetSizeAndName()

	file := source.GetReader()
	if file == nil {
		return nil, errors.New("could not get reader from source")
	}

	partSize := 1024 * 512 // 512KB
	if opts.ChunkSize > 0 {
		partSize = int(opts.ChunkSize)
	}
	fileId := GenerateRandomLong()

	IsFsBig := size > 10*1024*1024 // 10MB

	parts := size / int64(partSize)
	partOver := size % int64(partSize)

	totalParts := parts
	if partOver > 0 {
		totalParts++
	}

	wg := sync.WaitGroup{}
	numWorkers := countWorkers(int64(totalParts))
	if opts.Threads > 0 {
		numWorkers = opts.Threads
	}
	w := NewWorkerPool(numWorkers)
	uploadLog := newPartLogAggregator("upload", int(totalParts), 3*time.Second)
	uploadLog.setNumWorkers(numWorkers)
	defer uploadLog.Flush()

	c.Log.WithFields(map[string]any{
		"file_name": source.GetName(),
		"file_size": SizetoHuman(size),
		"parts":     parts,
	}).Info("starting file upload")

	doneBytes := atomic.Int64{}
	doneArray := sync.Map{}

	if err := initializeWorkers(numWorkers, int32(c.GetDC()), c, w); err != nil {
		return nil, err
	}

	var progressTracker *progressTracker
	var progressCallback func(*ProgressInfo)

	if opts.ProgressCallback != nil {
		progressCallback = opts.ProgressCallback
	} else if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(source.GetName())
		opts.ProgressManager.SetTotalSize(size)
		progressCallback = opts.ProgressManager.getCallback()
	}

	if progressCallback != nil {
		progressTracker = newProgressTracker(source.GetName(), size, progressCallback, opts.ProgressInterval)
		defer progressTracker.stop()
		progressTracker.start(&doneBytes)
	}

	MaxRetries := 15
	sem := make(chan struct{}, numWorkers)
	defer close(sem)

	// Small file < 10MB
	var hash hash.Hash
	if !IsFsBig {
		hash = md5.New()
		file = io.TeeReader(file, hash)

		for p := int64(0); p < totalParts; p++ {
			sem <- struct{}{}
			part := make([]byte, partSize)
			readBytes, err := io.ReadFull(file, part)
			if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
				c.Log.Error(err)
				return nil, err
			}
			part = part[:readBytes]

			wg.Add(1)
			go func(p int64, part []byte) {
				defer func() { <-sem; wg.Done() }()

				for range MaxRetries {
					var ctx context.Context
					var cancel context.CancelFunc
					if opts.Ctx != nil {
						select {
						case <-opts.Ctx.Done():
							return
						default:
						}
						ctx, cancel = context.WithTimeout(opts.Ctx, 30*time.Second)
					} else {
						ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
					}

					sender := w.NextWithContext(ctx)
					if sender == nil {
						cancel()
						return
					}

					_, err := sender.MakeRequestCtx(ctx, &UploadSaveFilePartParams{
						FileID:   fileId,
						FilePart: int32(p),
						Bytes:    part,
					})
					if opts.Delay > 0 {
						time.Sleep(time.Duration(opts.Delay) * time.Millisecond)
					}
					w.FreeWorker(sender)
					cancel()

					if err != nil {
						if handleIfFlood(err, c) {
							continue
						}
						uploadLog.recordFailure(int(p), err, sender)
						continue
					}

					uploadLog.recordSuccess(int(p), sender)
					doneBytes.Add(int64(len(part)))
					doneArray.Store(p, true)
					break
				}
			}(p, part)
		}

		wg.Wait()

		if progressCallback != nil {
			progressCallback(&ProgressInfo{
				FileName:     source.GetName(),
				TotalSize:    size,
				Current:      size,
				CurrentSpeed: 0,
				AverageSpeed: 0,
				ETA:          0,
				Elapsed:      0,
				Percentage:   100,
			})
		}

		if opts.FileName != "" {
			fileName = opts.FileName
		}

		c.Log.WithFields(map[string]any{
			"file_name": source.GetName(),
			"file_size": SizetoHuman(size),
			"parts":     parts,
		}).Info("file upload completed")

		return &InputFileObj{
			ID:          fileId,
			Md5Checksum: hex.EncodeToString(hash.Sum(nil)),
			Name:        prettifyFileName(fileName),
			Parts:       int32(totalParts),
		}, nil
	}

	for p := int64(0); p < totalParts; p++ { // Big file > 10MB
		sem <- struct{}{}
		part := make([]byte, partSize)
		readBytes, err := io.ReadFull(file, part)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			c.Log.Error(err)
			return nil, err
		}
		part = part[:readBytes]

		wg.Add(1)
		go func(p int64, part []byte) {
			defer func() { <-sem; wg.Done() }()

			for range MaxRetries {
				var ctx context.Context
				var cancel context.CancelFunc
				if opts.Ctx != nil {
					select {
					case <-opts.Ctx.Done():
						return
					default:
					}
					ctx, cancel = context.WithTimeout(opts.Ctx, 30*time.Second)
				} else {
					ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
				}

				sender := w.NextWithContext(ctx)
				if sender == nil {
					cancel()
					return
				}

				_, err := sender.MakeRequestCtx(ctx, &UploadSaveBigFilePartParams{
					FileID:         fileId,
					FilePart:       int32(p),
					FileTotalParts: int32(totalParts),
					Bytes:          part,
				})
				if opts.Delay > 0 {
					time.Sleep(time.Duration(opts.Delay) * time.Millisecond)
				}
				w.FreeWorker(sender)
				cancel()

				if err != nil {
					if handleIfFlood(err, c) {
						continue
					}
					uploadLog.recordFailure(int(p), err, sender)
					continue
				}

				uploadLog.recordSuccess(int(p), sender)
				doneBytes.Add(int64(len(part)))
				doneArray.Store(p, true)
				break
			}
		}(p, part)
	}

	wg.Wait()

	if progressCallback != nil {
		progressCallback(&ProgressInfo{
			FileName:     source.GetName(),
			TotalSize:    size,
			Current:      size,
			CurrentSpeed: 0,
			AverageSpeed: 0,
			ETA:          0,
			Elapsed:      0,
			Percentage:   100,
		})
	}

	if opts.FileName != "" {
		fileName = opts.FileName
	}

	c.Log.WithFields(map[string]any{
		"file_name": source.GetName(),
		"file_size": SizetoHuman(size),
		"parts":     parts,
	}).Info("file upload completed")

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
			c.Log.Debug("sleeping for flood wait %s", waitTime, "(s)...", waitTime)
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
	// Progress callback: receives ProgressInfo with all transfer details
	ProgressCallback func(*ProgressInfo) `json:"-"`
	// Progress manager (legacy support)
	ProgressManager *ProgressManager `json:"-"`
	// Progress callback interval in seconds (default: 5)
	ProgressInterval int `json:"progress_interval,omitempty"`
	// Delay between chunks in milliseconds (default: 0)
	Delay int `json:"delay,omitempty"`
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
	// Context for cancellation.
	Ctx context.Context `json:"-"`
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
			return "", fmt.Errorf("chunk size must be a multiple of 1048576 (1MB)")
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
	} else {
		fs.data = make([]byte, size)
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
	downloadLog := newPartLogAggregator("download", int(totalParts), 3*time.Second)
	downloadLog.setNumWorkers(numWorkers)
	defer downloadLog.Flush()

	if opts.Buffer != nil {
		dest = ":mem-buffer:"
		c.Log.WithField("size", size).Warn("downloading to buffer (memory) - use with caution (memory usage)")
	}

	c.Log.WithFields(map[string]any{
		"file_name": dest,
		"file_size": SizetoHuman(size),
		"parts":     parts,
	}).Info("starting file download")

	if err := initializeWorkers(numWorkers, dc, c, w); err != nil {
		return "", err
	}

	var sem = make(chan struct{}, numWorkers)
	defer close(sem)

	var wg sync.WaitGroup
	var doneBytes atomic.Int64
	var doneArray sync.Map

	var progressTracker *progressTracker
	var progressCallback func(*ProgressInfo)

	if opts.ProgressCallback != nil {
		progressCallback = opts.ProgressCallback
	} else if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(dest)
		opts.ProgressManager.SetTotalSize(size)
		progressCallback = opts.ProgressManager.getCallback()
	}

	if progressCallback != nil {
		progressTracker = newProgressTracker(dest, size, progressCallback, opts.ProgressInterval)
		defer progressTracker.stop()
		progressTracker.start(&doneBytes)
	}

	MaxRetries := 3
	c.Log.WithFields(map[string]any{
		"parts":   totalParts,
		"workers": numWorkers,
		"dc":      dc,
	}).Debug("dispatching download workers")

	var cdnRedirect atomic.Bool
	for p := int64(0); p < totalParts; p++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer func() {
				<-sem
				wg.Done()
			}()

			for range MaxRetries {
				if cdnRedirect.Load() {
					return
				}
				var ctx context.Context
				var cancel context.CancelFunc
				if opts.Ctx != nil {
					select {
					case <-opts.Ctx.Done():
						return
					default:
					}
					ctx, cancel = context.WithTimeout(opts.Ctx, 30*time.Second)
				} else {
					ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
				}

				sender := w.NextWithContext(ctx)
				if sender == nil {
					cancel()
					return
				}

				part, err := sender.MakeRequestCtx(ctx, &UploadGetFileParams{
					Location:     location,
					Offset:       int64(p * partSize),
					Limit:        int32(partSize),
					Precise:      true,
					CdnSupported: false,
				})
				if opts.Delay > 0 {
					time.Sleep(time.Duration(opts.Delay) * time.Millisecond)
				}
				w.FreeWorker(sender)
				cancel()

				if err != nil {
					if handleIfFlood(err, c) {
						continue
					} else if strings.Contains(err.Error(), "FILE_REFERENCE_EXPIRED") {
						c.Log.WithError(err).Debug("while downloading file")
						return // file reference expired
					}

					downloadLog.recordFailure(p, err, sender)
					continue
				}

				switch v := part.(type) {
				case *UploadFileObj:
					fs.WriteAt(v.Bytes, int64(p)*int64(partSize))
					doneBytes.Add(int64(len(v.Bytes)))
					doneArray.Store(p, true)
					downloadLog.recordSuccess(p, sender)
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
		return "", fmt.Errorf("cdn redirect not implemented")
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

			for range MaxRetries {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

				sender := w.NextWithContext(ctx)
				if sender == nil {
					cancel()
					return
				}

				part, err := sender.MakeRequestCtx(ctx, &UploadGetFileParams{
					Location:     location,
					Offset:       int64(p * partSize),
					Limit:        int32(partSize),
					Precise:      true,
					CdnSupported: false,
				})
				if opts.Delay > 0 {
					time.Sleep(time.Duration(opts.Delay) * time.Millisecond)
				}
				w.FreeWorker(sender)
				cancel()

				if err != nil {
					if handleIfFlood(err, c) {
						downloadLog.recordFailure(p, err, sender)
					} else if strings.Contains(err.Error(), "FILE_REFERENCE_EXPIRED") {
						c.Log.WithError(err).Debug("while downloading file")
						return // file reference expired
					}

					downloadLog.recordFailure(p, err, sender)
					continue
				}

				switch v := part.(type) {
				case *UploadFileObj:
					fs.WriteAt(v.Bytes, int64(p)*int64(partSize))
					doneBytes.Add(int64(len(v.Bytes)))
					doneArray.Store(p, true)
					downloadLog.recordSuccess(p, sender)
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
		return "", fmt.Errorf("cdn redirect not implemented")
	}

	// Final progress callback to show 100%
	if progressCallback != nil {
		progressCallback(&ProgressInfo{
			FileName:     dest,
			TotalSize:    size,
			Current:      size,
			CurrentSpeed: 0,
			AverageSpeed: 0,
			ETA:          0,
			Elapsed:      0,
			Percentage:   100,
		})
	}

	if opts.Buffer != nil {
		io.Copy(opts.Buffer, bytes.NewReader(fs.data))
	}

	c.Log.WithFields(map[string]any{
		"file_name": dest,
		"file_size": SizetoHuman(size),
	}).Info("file download completed")

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
		w.AddWorker(NewExSender(c.MTProto))
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

	numCreate := 0
	existingSenders := c.exSenders.GetSenders(int(dc))
	for _, worker := range existingSenders {
		w.AddWorker(worker)
		numCreate++
	}

	// set first worker synchronously
	if numCreate == 0 {
		conn, err := c.CreateExportedSender(int(dc), false, authParams)
		if err != nil {
			return fmt.Errorf("creating initial sender: %w", err)
		}
		if conn != nil {
			sender := NewExSender(conn)
			c.exSenders.AddSender(int(dc), sender)
			w.AddWorker(sender)
			numCreate++
		}
	}

	if numCreate < numWorkers {
		c.Log.Info(fmt.Sprintf("exporting senders: dc(%d) - workers(%d)", dc, numWorkers-numCreate))
		go func() {
			for i := numCreate; i < numWorkers; i++ {
				conn, err := c.CreateExportedSender(int(dc), false, authParams)
				if conn != nil && err == nil {
					sender := NewExSender(conn)
					c.exSenders.AddSender(int(dc), sender)
					w.AddWorker(sender)
				}
			}
		}()
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sender := w.NextWithContext(ctx)
	if sender == nil {
		return nil, "", errors.New("failed to get worker: timeout")
	}
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
			return nil, "", fmt.Errorf("cdn redirect not implemented")
		}
	}

	return buf, name, nil
}

// ----------------------- Helper Functions -----------------------

type partLogAggregator struct {
	mu          sync.Mutex
	ctx         string
	total       int
	interval    time.Duration
	lastLog     time.Time
	successes   int
	failures    int
	lastPart    int
	lastErr     error
	senderStats map[*ExSender]*senderStats
	numWorkers  int
}

type senderStats struct {
	successes int
	failures  int
	lastSeen  time.Time
}

func newPartLogAggregator(ctx string, total int, interval time.Duration) *partLogAggregator {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	return &partLogAggregator{
		ctx:         ctx,
		total:       total,
		interval:    interval,
		lastLog:     time.Now(),
		senderStats: make(map[*ExSender]*senderStats),
	}
}

func (a *partLogAggregator) setNumWorkers(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.numWorkers = n
}

func (a *partLogAggregator) recordSuccess(part int, sender *ExSender) {
	a.mu.Lock()
	a.successes++
	a.lastPart = part
	if sender != nil {
		if _, ok := a.senderStats[sender]; !ok {
			a.senderStats[sender] = &senderStats{}
		}
		a.senderStats[sender].successes++
		a.senderStats[sender].lastSeen = time.Now()
	}
	a.maybeLogLocked(sender)
	a.mu.Unlock()
}

func (a *partLogAggregator) recordFailure(part int, err error, sender *ExSender) {
	a.mu.Lock()
	a.failures++
	a.lastPart = part
	a.lastErr = err
	if sender != nil {
		if _, ok := a.senderStats[sender]; !ok {
			a.senderStats[sender] = &senderStats{}
		}
		a.senderStats[sender].failures++
		a.senderStats[sender].lastSeen = time.Now()
	}
	a.maybeLogLocked(sender)
	a.mu.Unlock()
}

func (a *partLogAggregator) maybeLogLocked(sender *ExSender) {
	if time.Since(a.lastLog) < a.interval {
		return
	}
	a.logLocked(sender)
}

func (a *partLogAggregator) logLocked(senderVar ...*ExSender) {
	if a.successes == 0 && a.failures == 0 {
		return
	}

	var sender *ExSender
	if len(senderVar) > 0 {
		sender = senderVar[0]
	}

	var workerStats []string
	if len(a.senderStats) > 0 {
		type senderInfo struct {
			sender *ExSender
			stats  *senderStats
		}
		var senders []senderInfo
		for sender, stats := range a.senderStats {
			senders = append(senders, senderInfo{sender: sender, stats: stats})
		}

		for i := 0; i < len(senders); i++ {
			for j := i + 1; j < len(senders); j++ {
				if senders[j].stats.successes > senders[i].stats.successes {
					senders[i], senders[j] = senders[j], senders[i]
				}
			}
		}

		for i, s := range senders {
			total := s.stats.successes + s.stats.failures
			if total == 0 {
				continue
			}

			successRate := float64(s.stats.successes) / float64(total) * 100
			var stat string
			if s.stats.failures == 0 {
				stat = fmt.Sprintf("W%d:%d", i+1, s.stats.successes)
			} else if successRate < 80 {
				stat = fmt.Sprintf("W%d:%d/%d!", i+1, s.stats.successes, s.stats.failures)
			} else {
				stat = fmt.Sprintf("W%d:%d/%d", i+1, s.stats.successes, s.stats.failures)
			}
			workerStats = append(workerStats, stat)
		}
	}

	totalProgress := float64(a.successes) / float64(a.total) * 100

	logMsg := fmt.Sprintf("[%s] %d/%d (%.1f%%) ✓%d ✗%d",
		a.ctx, a.successes, a.total, totalProgress, a.successes, a.failures)

	if len(workerStats) > 0 {
		logMsg += " | " + strings.Join(workerStats, " ")
	}

	if a.lastErr != nil && a.failures > 0 {
		errMsg := a.lastErr.Error()
		if strings.Contains(errMsg, "context deadline exceeded") {
			errMsg = "timeout"
		} else if strings.Contains(errMsg, "FLOOD_WAIT") {
			errMsg = "flood"
		} else if len(errMsg) > 30 {
			errMsg = errMsg[:30] + "..."
		}
		logMsg += " | err:" + errMsg
	}

	if sender != nil {
		sender.Logger.Debug(logMsg)
	}
	a.lastLog = time.Now()
}

func (a *partLogAggregator) Flush() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logLocked()
}

// ----------------------- Progress Manager -----------------------

// ProgressInfo contains all information about upload/download progress
type ProgressInfo struct {
	// File name being transferred
	FileName string
	// Total file size in bytes
	TotalSize int64
	// Current bytes transferred
	Current int64
	// Current transfer speed in bytes/second (speed in last interval)
	CurrentSpeed float64
	// Average transfer speed in bytes/second (overall)
	AverageSpeed float64
	// Estimated time remaining in seconds
	ETA float64
	// Time elapsed since start in seconds
	Elapsed float64
	// Progress percentage (0-100)
	Percentage float64
}

// SpeedString returns current speed as human-readable string
func (p *ProgressInfo) SpeedString() string {
	speed := p.CurrentSpeed
	if speed < 1024 {
		return fmt.Sprintf("%.2f B/s", speed)
	} else if speed < 1024*1024 {
		return fmt.Sprintf("%.2f KB/s", speed/1024)
	}
	return fmt.Sprintf("%.2f MB/s", speed/1024/1024)
}

// AvgSpeedString returns average speed as human-readable string
func (p *ProgressInfo) AvgSpeedString() string {
	speed := p.AverageSpeed
	if speed < 1024 {
		return fmt.Sprintf("%.2f B/s", speed)
	} else if speed < 1024*1024 {
		return fmt.Sprintf("%.2f KB/s", speed/1024)
	}
	return fmt.Sprintf("%.2f MB/s", speed/1024/1024)
}

// ETAString returns ETA as human-readable string
func (p *ProgressInfo) ETAString() string {
	if p.ETA <= 0 {
		return "--:--"
	}
	duration := time.Duration(p.ETA) * time.Second
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm%ds", int(duration.Minutes()), int(duration.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(duration.Hours()), int(duration.Minutes())%60)
}

// ElapsedString returns elapsed time as human-readable string
func (p *ProgressInfo) ElapsedString() string {
	duration := time.Duration(p.Elapsed) * time.Second
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm%ds", int(duration.Minutes()), int(duration.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(duration.Hours()), int(duration.Minutes())%60)
}

type progressTracker struct {
	fileName  string
	totalSize int64
	callback  func(*ProgressInfo)
	interval  time.Duration
	lastBytes int64
	lastTime  time.Time
	startTime time.Time
	stopChan  chan struct{}
	mu        sync.Mutex
}

func newProgressTracker(fileName string, totalSize int64, callback func(*ProgressInfo), intervalSec int) *progressTracker {
	now := time.Now()
	if intervalSec <= 0 {
		intervalSec = 5
	}
	return &progressTracker{
		fileName:  fileName,
		totalSize: totalSize,
		callback:  callback,
		interval:  time.Duration(intervalSec) * time.Second,
		lastTime:  now,
		startTime: now,
		stopChan:  make(chan struct{}),
	}
}

func (pt *progressTracker) start(doneBytes *atomic.Int64) {
	go func() {
		ticker := time.NewTicker(pt.interval)
		defer ticker.Stop()

		for {
			select {
			case <-pt.stopChan:
				return
			case <-ticker.C:
				pt.mu.Lock()
				current := doneBytes.Load()
				now := time.Now()
				intervalElapsed := now.Sub(pt.lastTime).Seconds()
				totalElapsed := now.Sub(pt.startTime).Seconds()

				var currentSpeed float64
				if intervalElapsed > 0 {
					bytesDiff := current - pt.lastBytes
					currentSpeed = float64(bytesDiff) / intervalElapsed
				}

				var averageSpeed float64
				if totalElapsed > 0 {
					averageSpeed = float64(current) / totalElapsed
				}

				var eta float64
				if currentSpeed > 0 && current < pt.totalSize {
					remaining := pt.totalSize - current
					eta = float64(remaining) / currentSpeed
				}

				var percentage float64
				if pt.totalSize > 0 {
					percentage = float64(current) / float64(pt.totalSize) * 100
				}

				pt.lastBytes = current
				pt.lastTime = now
				pt.mu.Unlock()

				if current > 0 && current <= pt.totalSize {
					pt.callback(&ProgressInfo{
						FileName:     pt.fileName,
						TotalSize:    pt.totalSize,
						Current:      current,
						CurrentSpeed: currentSpeed,
						AverageSpeed: averageSpeed,
						ETA:          eta,
						Elapsed:      totalElapsed,
						Percentage:   percentage,
					})
				}
			}
		}
	}()
}

func (pt *progressTracker) stop() {
	close(pt.stopChan)
}

// ProgressManager provides progress tracking for uploads and downloads
type ProgressManager struct {
	progressCallback func(*ProgressInfo)
	legacyCallback   func(totalSize, currentSize int64)
	fileName         string
	totalSize        int64
}

func NewProgressManager(editInterval int, editFunc ...func(totalSize, currentSize int64)) *ProgressManager {
	pm := &ProgressManager{}
	if len(editFunc) > 0 {
		pm.legacyCallback = editFunc[0]
	}
	return pm
}

func (pm *ProgressManager) SetFileName(fileName string) {
	pm.fileName = fileName
}

func (pm *ProgressManager) SetTotalSize(totalSize int64) {
	pm.totalSize = totalSize
}

// WithEdit sets the edit function
func (pm *ProgressManager) WithEdit(editFunc func(totalSize, currentSize int64)) *ProgressManager {
	pm.legacyCallback = editFunc
	return pm
}

// WithCallback sets the callback function
func (pm *ProgressManager) WithCallback(callback func(*ProgressInfo)) *ProgressManager {
	pm.progressCallback = callback
	return pm
}

// SetMessage sets up progress reporting to edit a telegram message
func (pm *ProgressManager) SetMessage(msg *NewMessage) *ProgressManager {
	pm.progressCallback = MediaDownloadProgress(msg)
	return pm
}

func (pm *ProgressManager) SetInlineMessage(client *Client, inline *InputBotInlineMessageID) *ProgressManager {
	pm.progressCallback = MediaDownloadProgress(&NewMessage{Client: client}, inline)
	return pm
}

func (pm *ProgressManager) getCallback() func(*ProgressInfo) {
	if pm.progressCallback != nil {
		return pm.progressCallback
	}
	if pm.legacyCallback != nil {
		return func(info *ProgressInfo) {
			pm.legacyCallback(info.TotalSize, info.Current)
		}
	}
	return nil
}

// MediaDownloadProgress creates a progress callback that edits a telegram message with download progress
func MediaDownloadProgress(editMsg *NewMessage, inline ...*InputBotInlineMessageID) func(*ProgressInfo) {
	return func(info *ProgressInfo) {
		progressbar := strings.Repeat("■", int(info.Percentage/10)) + strings.Repeat("□", 10-int(info.Percentage/10))

		message := fmt.Sprintf(
			"<b>Name:</b> <code>%s</code>\n\n"+
				"<b>Size:</b> <code>%.2f MiB</code>\n"+
				"<b>Speed:</b> <code>%s</code> (avg: <code>%s</code>)\n"+
				"<b>ETA:</b> <code>%s</code> | <b>Elapsed:</b> <code>%s</code>\n\n"+
				"%s <code>%.2f%%</code>",
			info.FileName,
			float64(info.TotalSize)/1024/1024,
			info.SpeedString(),
			info.AvgSpeedString(),
			info.ETAString(),
			info.ElapsedString(),
			progressbar,
			info.Percentage,
		)

		if len(inline) > 0 {
			editMsg.Client.EditMessage(inline[0], 0, message)
		} else {
			editMsg.Edit(message)
		}
	}
}
