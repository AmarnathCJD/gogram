// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"math"
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
	// Progress callback for upload file.
	ProgressManager *ProgressManager `json:"-"`
	// ProgressHandler allows providing a lightweight callback instead of a full manager.
	ProgressHandler func(totalSize, currentSize int64) `json:"-"`
	// ProgressInterval overrides the default progress tick duration.
	ProgressInterval time.Duration `json:"-"`
}

type WorkerPool struct {
	sync.Mutex
	workers   []*ExSender
	free      chan *ExSender
	closeOnce sync.Once
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
	if next == nil {
		return nil
	}
	if next.MTProto != nil && !next.MTProto.IsTcpActive() {
		next.MTProto.Reconnect(false)
	}
	next.lastUsedMu.Lock()
	next.lastUsed = time.Now()
	next.lastUsedMu.Unlock()
	return next
}

func (wp *WorkerPool) FreeWorker(s *ExSender) {
	select {
	case wp.free <- s:
	default:
	}
}

func (wp *WorkerPool) Close() {
	wp.closeOnce.Do(func() {
		wp.Lock()
		workers := append([]*ExSender(nil), wp.workers...)
		wp.Unlock()
		for _, worker := range workers {
			if worker != nil {
				worker.Release()
			}
		}
	})
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
		if err := s.closer.Close(); err != nil {
			return err
		}
		s.closer = nil
	}
	return nil
}

func (s *Source) ReadChunkAt(offset int64, length int) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	reader, cleanup, err := s.readerAt()
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	buf := make([]byte, length)
	n, err := reader.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

func (s *Source) readerAt() (io.ReaderAt, func(), error) {
	switch src := s.Source.(type) {
	case string:
		if f, ok := s.closer.(io.ReaderAt); ok {
			return f, nil, nil
		}
		f, err := os.Open(src)
		if err != nil {
			return nil, nil, err
		}
		return f, func() { f.Close() }, nil
	case *os.File:
		return src, nil, nil
	case []byte:
		return bytes.NewReader(src), nil, nil
	case *bytes.Buffer:
		return bytes.NewReader(src.Bytes()), nil, nil
	case io.ReaderAt:
		return src, nil, nil
	case *io.Reader:
		if src != nil {
			if ra, ok := (*src).(io.ReaderAt); ok {
				return ra, nil, nil
			}
		}
	}
	return nil, nil, errors.New("source does not support random access retries")
}

func (c *Client) UploadFile(src any, Opts ...*UploadOptions) (InputFile, error) {
	opts := getVariadic(Opts, &UploadOptions{})
	if src == nil {
		return nil, errors.New("file can not be nil")
	}
	opts.ProgressManager = ensureProgressManager(opts.ProgressManager, opts.ProgressHandler, opts.ProgressInterval)

	source := &Source{Source: src}
	defer source.Close()
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

	IsFsBig := size > 10*1024*1024 // 10MB

	parts := size / int64(partSize)
	partOver := size % int64(partSize)

	totalParts := parts
	if partOver > 0 {
		totalParts++
	}
	totalPartsInt := int(totalParts)

	wg := sync.WaitGroup{}
	numWorkers := countWorkers(int64(totalParts))
	if opts.Threads > 0 {
		numWorkers = opts.Threads
	}
	w := NewWorkerPool(numWorkers)
	defer w.Close()

	c.Log.WithFields(map[string]any{
		"file_name": source.GetName(),
		"file_size": SizetoHuman(size),
		"parts":     parts,
	}).Info("starting file upload")

	doneBytes := atomic.Int64{}
	doneArray := sync.Map{}
	doneParts := atomic.Int64{}
	finishProgress := func() {
		if opts.ProgressManager != nil && opts.ProgressManager.editFunc != nil {
			doneBytes.Store(size)
			opts.ProgressManager.editFunc(size, size)
		}
	}

	markPartComplete := func(partIndex int, chunkSize int) {
		if _, loaded := doneArray.LoadOrStore(partIndex, true); !loaded {
			doneParts.Add(1)
			doneBytes.Add(int64(chunkSize))
		}
	}

	if err := initializeWorkers(numWorkers, int32(c.GetDC()), c, w); err != nil {
		return nil, err
	}

	stopProgress := make(chan struct{})
	defer close(stopProgress)

	if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(source.GetName())
		opts.ProgressManager.IncCount()
		opts.ProgressManager.SetTotalSize(size)
		opts.ProgressManager.SetMeta(c.GetDC(), numWorkers)

		opts.ProgressManager.editFunc(size, 0) // Initial edit

		go func() {
			ticker := time.NewTicker(opts.ProgressManager.Interval())
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

	MaxRetries := 3
	sem := make(chan struct{}, numWorkers)
	defer close(sem)

	retryUploadParts := func(undone []int, timeout time.Duration) error {
		if len(undone) == 0 {
			return nil
		}

		c.Log.WithFields(map[string]any{
			"missing_count": len(undone),
			"sample":        sampleParts(undone, 10),
			"timeout":       timeout,
		}).Debug("retrying upload parts")

		var retryWG sync.WaitGroup
		var readErr error

		for _, partIndex := range undone {
			offset := int64(partIndex) * int64(partSize)
			chunk, err := source.ReadChunkAt(offset, partSize)
			if err != nil {
				readErr = fmt.Errorf("retry upload: failed to read part %d: %w", partIndex, err)
				break
			}
			if len(chunk) == 0 {
				readErr = fmt.Errorf("retry upload: empty chunk for part %d", partIndex)
				break
			}

			data := make([]byte, len(chunk))
			copy(data, chunk)

			retryWG.Add(1)
			sem <- struct{}{}
			go func(partIdx int, body []byte) {
				defer func() {
					<-sem
					retryWG.Done()
				}()

				for r := 0; r < MaxRetries; r++ {
					sender := w.Next()
					if sender == nil {
						time.Sleep(50 * time.Millisecond)
						continue
					}
					ctx, cancel := context.WithTimeout(context.Background(), timeout)

					var reqErr error
					if IsFsBig {
						_, reqErr = sender.MakeRequestCtx(ctx, &UploadSaveBigFilePartParams{
							FileID:         fileId,
							FilePart:       int32(partIdx),
							FileTotalParts: int32(totalParts),
							Bytes:          body,
						})
					} else {
						_, reqErr = sender.MakeRequestCtx(ctx, &UploadSaveFilePartParams{
							FileID:   fileId,
							FilePart: int32(partIdx),
							Bytes:    body,
						})
					}
					w.FreeWorker(sender)
					cancel()

					if reqErr != nil {
						if handleIfFlood(reqErr, c) {
							continue
						}
						c.Log.Debug("retry upload part %d error: %v", partIdx, reqErr)
						continue
					}

					c.Log.Debug("retry uploaded part %d/%d len: %d KB", partIdx, totalParts, len(body)/1024)
					markPartComplete(partIdx, len(body))
					return
				}

				c.Log.Warn("retry upload part %d exhausted retries", partIdx)
			}(partIndex, data)
		}

		retryWG.Wait()
		return readErr
	}

	ensureUploadCompletion := func() error {
		if doneParts.Load() == int64(totalPartsInt) {
			return nil
		}

		const maxUploadRetryAttempts = 10
		retryAttempt := 0

		for doneParts.Load() != int64(totalPartsInt) {
			undone := getUndoneParts(&doneArray, totalPartsInt)
			if len(undone) == 0 {
				break
			}

			c.Log.WithFields(map[string]any{
				"attempt":       retryAttempt,
				"missing_parts": len(undone),
				"sample":        sampleParts(undone, 10),
			}).Debug("upload completion retry triggered")

			if retryAttempt >= maxUploadRetryAttempts {
				err := fmt.Errorf("upload incomplete: %d parts failed after %d retries", len(undone), maxUploadRetryAttempts)
				c.Log.WithError(err).WithFields(map[string]any{
					"file_name":    source.GetName(),
					"undone_parts": undone,
					"done_parts":   doneParts.Load(),
					"total_parts":  totalPartsInt,
				}).Error("upload retry limit reached")
				return err
			}
			retryAttempt++

			timeout := 6 * time.Second
			if retryAttempt > 1 {
				timeout = 8 * time.Second
			}

			if err := retryUploadParts(undone, timeout); err != nil {
				return err
			}

			c.Log.WithFields(map[string]any{
				"attempt": retryAttempt,
				"done":    doneParts.Load(),
			}).Debug("upload retry batch finished")
		}

		if doneParts.Load() != int64(totalPartsInt) {
			undone := getUndoneParts(&doneArray, totalPartsInt)
			err := fmt.Errorf("upload incomplete: %d parts missing", len(undone))
			c.Log.WithError(err).WithFields(map[string]any{
				"file_name":    source.GetName(),
				"undone_parts": undone,
				"done_parts":   doneParts.Load(),
				"total_parts":  totalPartsInt,
			}).Error("upload incomplete after retries")
			return err
		}

		return nil
	}

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
				c.Log.WithError(err).Error("reading file part")
				return nil, err
			}
			part = part[:readBytes]

			wg.Add(1)
			go func(p int64, part []byte) {
				defer func() { <-sem; wg.Done() }()

				for range MaxRetries {
					sender := w.Next()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

					_, err := sender.MakeRequestCtx(ctx, &UploadSaveFilePartParams{
						FileID:   fileId,
						FilePart: int32(p),
						Bytes:    part,
					})
					w.FreeWorker(sender)
					cancel()

					if err != nil {
						if handleIfFlood(err, c) {
							continue
						}
						c.Log.Debug("upload part %d error: %v", p, err)
						continue
					}

					c.Log.Debug("uploaded part %d/%d in chunks of %d KB", p, totalParts, len(part)/1024)
					markPartComplete(int(p), len(part))
					break
				}
			}(p, part)
		}

		wg.Wait()

		if err := ensureUploadCompletion(); err != nil {
			return nil, err
		}

		if opts.FileName != "" {
			fileName = opts.FileName
		}

		finishProgress()

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
			c.Log.WithError(err).Error("reading file part")
			return nil, err
		}
		part = part[:readBytes]

		wg.Add(1)
		go func(p int64, part []byte) {
			defer func() { <-sem; wg.Done() }()

			for range MaxRetries {
				sender := w.Next()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

				_, err := sender.MakeRequestCtx(ctx, &UploadSaveBigFilePartParams{
					FileID:         fileId,
					FilePart:       int32(p),
					FileTotalParts: int32(totalParts),
					Bytes:          part,
				})
				w.FreeWorker(sender)
				cancel()

				if err != nil {
					if handleIfFlood(err, c) {
						continue
					}
					c.Log.Debug("upload part %d error: %v", p, err)
					continue
				}

				c.Log.Debug("uploaded part %d/%d in chunks of %d KB", p, totalParts, len(part)/1024)
				markPartComplete(int(p), len(part))
				break
			}
		}(p, part)
	}

	wg.Wait()

	if err := ensureUploadCompletion(); err != nil {
		return nil, err
	}

	if opts.FileName != "" {
		fileName = opts.FileName
	}

	finishProgress()

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
			c.Log.Debug("flood wait detected, sleeping for", waitTime, "seconds")
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
	// ProgressHandler allows providing a callback without wiring a ProgressManager manually.
	ProgressHandler func(totalSize, currentSize int64) `json:"-"`
	// ProgressInterval overrides the default edit interval.
	ProgressInterval time.Duration `json:"-"`
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
	// Context for cancellation
	Ctx context.Context `json:"-"`
	// Timeout for download operation to seize out
	Timeout time.Duration `json:"-"`
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
	ops := getVariadic(Opts, &DownloadOptions{})
	ops.ProgressManager = ensureProgressManager(ops.ProgressManager, ops.ProgressHandler, ops.ProgressInterval)

	ctx := ops.Ctx
	if ctx == nil {
		if ops.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), ops.Timeout)
			defer cancel()
		} else {
			ctx = context.Background()
		}
	}

	location, dc, size, fileName, err := GetFileLocation(file, FileLocationOptions{
		ThumbOnly: ops.ThumbOnly,
		ThumbSize: ops.ThumbSize,
		Video:     ops.IsVideo,
	})
	if err != nil {
		return "", err
	}

	dc = getValue(dc, ops.DCId)
	if dc == 0 {
		dc = int32(c.GetDC())
	}
	dest := getValue(ops.FileName, fileName)

	partSize := chunkSizeCalc(size)
	if ops.ChunkSize > 0 {
		if ops.ChunkSize > 1048576 || (1048576%ops.ChunkSize) != 0 {
			return "", errors.New("chunk size must be a multiple of 1048576 (1MB)")
		}
		partSize = int(ops.ChunkSize)
	}

	dest = sanitizePath(dest, fileName)

	var fs Destination
	if ops.Buffer == nil {
		file, err := os.OpenFile(dest, os.O_CREATE|os.O_RDWR, 0666)
		if err != nil {
			return "", err
		}
		fs.file = file
	} else {
		fs.data = make([]byte, size)
	}

	parts := size / int64(partSize)
	partOver := size % int64(partSize)
	totalParts := parts
	if partOver > 0 {
		totalParts++
	}
	totalPartsInt := int(totalParts)

	numWorkers := countWorkers(parts)
	if ops.Threads > 0 {
		numWorkers = ops.Threads
	}

	var w = NewWorkerPool(numWorkers)
	defer w.Close()

	if ops.Buffer != nil {
		dest = ":mem-buffer:"
		c.Log.Warn("downloading to buffer (memory) - use with caution (memory usage)")
	}

	c.Log.WithFields(map[string]any{
		"file_name": dest,
		"file_size": SizetoHuman(size),
		"parts":     parts,
	}).Info("starting file download")

	if err := initializeWorkers(numWorkers, dc, c, w); err != nil {
		return "", err
	}

	c.Log.WithFields(map[string]any{
		"dc":      dc,
		"workers": numWorkers,
		"parts":   totalPartsInt,
		"chunk":   partSize,
	}).Debug("download workers ready")

	var sem = make(chan struct{}, numWorkers)
	var wg sync.WaitGroup
	var doneBytes atomic.Int64
	var doneArray sync.Map
	var doneParts atomic.Int64
	var cancelled atomic.Bool
	var downloadErr atomic.Value
	var cleanupOnce sync.Once

	stopProgress := make(chan struct{})

	cleanup := func() {
		cleanupOnce.Do(func() {
			close(stopProgress)
			fs.Close()
			if cancelled.Load() && ops.Buffer == nil && fs.file != nil {
				os.Remove(dest)
			}
		})
	}
	defer cleanup()

	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancelled.Store(true)
			downloadErr.Store(ctx.Err())
			c.Log.WithError(ctx.Err()).WithFields(map[string]any{
				"file": dest,
				"dc":   dc,
			}).Warn("download context cancelled")
		case <-ctxDone:
			return
		}
	}()
	defer close(ctxDone)

	if ops.ProgressManager != nil {
		ops.ProgressManager.SetFileName(dest)
		ops.ProgressManager.IncCount()
		ops.ProgressManager.SetTotalSize(size)
		ops.ProgressManager.SetMeta(int(dc), numWorkers)
		if ops.ProgressManager.editFunc != nil {
			ops.ProgressManager.editFunc(size, 0)
		}

		go func() {
			ticker := time.NewTicker(ops.ProgressManager.Interval())
			defer ticker.Stop()

			for {
				select {
				case <-stopProgress:
					return
				case <-ticker.C:
					if ops.ProgressManager.editFunc != nil {
						current := min(doneBytes.Load(), size)
						ops.ProgressManager.editFunc(size, current)
					}
				}
			}
		}()
	}

	MaxRetries := 3
	var cdnRedirect atomic.Bool
	c.Log.WithFields(map[string]any{
		"parts":       totalPartsInt,
		"workers":     numWorkers,
		"chunk_bytes": partSize,
		"dc":          dc,
	}).Debug("dispatching download workers")

	for p := int64(0); p < totalParts; p++ {
		if cancelled.Load() {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer func() {
				<-sem
				wg.Done()
			}()

			for r := 0; r < MaxRetries; r++ {
				if cancelled.Load() || cdnRedirect.Load() {
					return
				}
				waitStart := time.Now()
				sender := w.Next()
				if waitDelay := time.Since(waitStart); waitDelay > 500*time.Millisecond {
					logFields := map[string]any{
						"part":        p,
						"attempt":     r + 1,
						"wait_ms":     waitDelay.Milliseconds(),
						"worker_pool": numWorkers,
					}
					if waitDelay > 2*time.Second {
						c.Log.WithFields(logFields).Warn("slow worker acquisition")
					} else {
						c.Log.WithFields(logFields).Debug("worker acquisition delay")
					}
				}
				if sender == nil {
					c.Log.WithFields(map[string]any{
						"part":    p,
						"attempt": r + 1,
					}).Warn("no sender available for download part")
					time.Sleep(50 * time.Millisecond)
					continue
				}
				offset := int64(p * partSize)
				c.Log.WithFields(map[string]any{
					"part":    p,
					"attempt": r + 1,
					"offset":  offset,
					"limit":   partSize,
				}).Debug("requesting download part")
				reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)

				part, err := sender.MakeRequestCtx(reqCtx, &UploadGetFileParams{
					Location:     location,
					Offset:       offset,
					Limit:        int32(partSize),
					Precise:      true,
					CdnSupported: false,
				})
				w.FreeWorker(sender)
				cancel()

				if err != nil {
					if handleIfFlood(err, c) {
						continue
					}

					if MatchError(err, "FILE_REFERENCE_EXPIRED") {
						c.Log.WithError(err).Debug("[FILE_REFERENCE_EXPIRED]")
						cancelled.Store(true)
						downloadErr.Store(err)
						return // file reference expired, need to refetch ref
					}

					c.Log.WithError(err).WithFields(map[string]any{
						"part":    p,
						"attempt": r + 1,
					}).Debug("download part failed, retrying")
					continue
				}

				switch v := part.(type) {
				case *UploadFileObj:
					c.Log.Debug("downloaded part %d/%d len: %d KB", p, totalParts, len(v.Bytes)/1024)
					fs.WriteAt(v.Bytes, int64(p)*int64(partSize))
					doneBytes.Add(int64(len(v.Bytes)))
					if _, loaded := doneArray.LoadOrStore(p, true); !loaded {
						doneParts.Add(1)
					}
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
	c.Log.WithFields(map[string]any{
		"file":       dest,
		"done_parts": doneParts.Load(),
		"total":      totalPartsInt,
	}).Debug("initial download pass completed")

	if cancelled.Load() {
		if err := downloadErr.Load(); err != nil {
			return "", err.(error)
		}
		return "", context.Canceled
	}

	if cdnRedirect.Load() {
		return "", errors.New("cdn redirect not implemented")
	}

	retryDownload := func(undone []int, ctxTimeout time.Duration) error {
		if len(undone) == 0 {
			c.Log.WithField("file", dest).Debug("retryDownload invoked with no parts; skipping")
			return nil
		}
		c.Log.WithFields(map[string]any{
			"file":        dest,
			"dc":          dc,
			"parts":       len(undone),
			"timeout":     ctxTimeout,
			"sample":      sampleParts(undone, 8),
			"done_parts":  doneParts.Load(),
			"total_parts": totalPartsInt,
		}).Debug("retryDownload scheduling parts")
		for _, p := range undone {
			if cancelled.Load() || cdnRedirect.Load() {
				return nil
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(p int) {
				defer func() {
					<-sem
					wg.Done()
				}()

				for r := range MaxRetries {
					if cancelled.Load() || cdnRedirect.Load() {
						return
					}
					waitStart := time.Now()
					sender := w.Next()
					if waitDelay := time.Since(waitStart); waitDelay > 500*time.Millisecond {
						logFields := map[string]any{
							"part":        p,
							"attempt":     r + 1,
							"wait_ms":     waitDelay.Milliseconds(),
							"worker_pool": numWorkers,
							"timeout":     ctxTimeout,
						}
						if waitDelay > 2*time.Second {
							c.Log.WithFields(logFields).Warn("slow worker acquisition")
						} else {
							c.Log.WithFields(logFields).Debug("worker acquisition delay")
						}
					}
					if sender == nil {
						c.Log.WithFields(map[string]any{
							"part":    p,
							"attempt": r + 1,
						}).Warn("no sender available for retry part")
						time.Sleep(50 * time.Millisecond)
						continue
					}
					offset := int64(p * partSize)
					c.Log.WithFields(map[string]any{
						"part":    p,
						"attempt": r + 1,
						"offset":  offset,
						"limit":   partSize,
						"timeout": ctxTimeout,
					}).Debug("retry requesting download part")
					reqCtx, cancel := context.WithTimeout(ctx, ctxTimeout)

					part, err := sender.MakeRequestCtx(reqCtx, &UploadGetFileParams{
						Location:     location,
						Offset:       offset,
						Limit:        int32(partSize),
						Precise:      true,
						CdnSupported: false,
					})
					w.FreeWorker(sender)
					cancel()

					if err != nil {
						if handleIfFlood(err, c) {
							continue
						}
						if MatchError(err, "FILE_REFERENCE_EXPIRED") {
							c.Log.WithError(err).Debug("[FILE_REFERENCE_EXPIRED]")
							cancelled.Store(true)
							downloadErr.Store(err)
							return
						}

						c.Log.WithError(err).WithFields(map[string]any{
							"part":    p,
							"attempt": r + 1,
							"timeout": ctxTimeout,
						}).Debug("retry download part failed, retrying")
						continue
					}

					switch v := part.(type) {
					case *UploadFileObj:
						c.Log.Debug("downloaded part %d/%d len: %d KB", p, totalParts, len(v.Bytes)/1024)
						fs.WriteAt(v.Bytes, int64(p)*int64(partSize))
						doneBytes.Add(int64(len(v.Bytes)))
						if _, loaded := doneArray.LoadOrStore(p, true); !loaded {
							doneParts.Add(1)
						}
					case *UploadFileCdnRedirect:
						cdnRedirect.Store(true)
					case nil:
						continue
					default:
						return
					}
					break
				}
			}(p)
		}
		wg.Wait()
		c.Log.WithFields(map[string]any{
			"file":       dest,
			"dc":         dc,
			"parts":      len(undone),
			"done_parts": doneParts.Load(),
		}).Debug("retryDownload batch complete")
		return nil
	}

	if doneParts.Load() != int64(totalPartsInt) {
		const maxRetryAttempts = 10
		retryAttempt := 0
		for {
			if cancelled.Load() || cdnRedirect.Load() {
				break
			}
			undone := getUndoneParts(&doneArray, totalPartsInt)
			if len(undone) == 0 {
				break
			}

			c.Log.WithFields(map[string]any{
				"attempt":       retryAttempt,
				"missing_parts": len(undone),
				"sample":        sampleParts(undone, 10),
			}).Debug("download completion retry triggered")

			if retryAttempt >= maxRetryAttempts {
				err := fmt.Errorf("download incomplete: %d parts failed after %d retries", len(undone), maxRetryAttempts)
				c.Log.WithError(err).WithFields(map[string]any{
					"undone_parts": undone,
					"attempt":      retryAttempt,
					"total_parts":  totalPartsInt,
					"done_parts":   doneParts.Load(),
				}).Error("download retry limit reached")
				return "", err
			}
			retryAttempt++

			timeout := 6 * time.Second
			if retryAttempt > 1 {
				timeout = 8 * time.Second
			}
			if err := retryDownload(undone, timeout); err != nil {
				return "", err
			}

			c.Log.WithFields(map[string]any{
				"attempt": retryAttempt,
				"done":    doneParts.Load(),
			}).Debug("download retry batch finished")
		}
	}

	if cancelled.Load() {
		if err := downloadErr.Load(); err != nil {
			return "", err.(error)
		}
		return "", context.Canceled
	}

	if cdnRedirect.Load() {
		return "", errors.New("cdn redirect not implemented")
	}

	if doneParts.Load() != int64(totalPartsInt) {
		if doneBytes.Load() >= size {
			c.Log.WithFields(map[string]any{
				"file":        dest,
				"total_parts": totalPartsInt,
				"done_parts":  doneParts.Load(),
			}).Warn("download bytes complete but part tracker inconsistent; continuing")
		} else {
			undone := getUndoneParts(&doneArray, totalPartsInt)
			missing := totalPartsInt - int(doneParts.Load())
			err := fmt.Errorf("download incomplete: %d parts missing", missing)
			c.Log.WithError(err).WithFields(map[string]any{
				"undone_parts": undone,
				"done_parts":   doneParts.Load(),
				"total_parts":  totalPartsInt,
			}).Error("download incomplete after retries")
			return "", err
		}
	}

	c.Log.WithFields(map[string]any{
		"file":       dest,
		"size":       SizetoHuman(size),
		"done_bytes": doneBytes.Load(),
	}).Debug("download complete")

	if ops.ProgressManager != nil && ops.ProgressManager.editFunc != nil {
		ops.ProgressManager.editFunc(size, size)
	}

	if ops.Buffer != nil {
		io.Copy(ops.Buffer, bytes.NewReader(fs.data))
	}

	c.Log.Debug("finalizing download to %s", dest)

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

func sampleParts(parts []int, limit int) []int {
	if limit <= 0 || len(parts) <= limit {
		return append([]int(nil), parts...)
	}
	return append([]int(nil), parts[:limit]...)
}

func initializeWorkers(numWorkers int, dc int32, c *Client, w *WorkerPool) error {
	if numWorkers <= 0 {
		return errors.New("number of workers must be greater than 0")
	}

	if numWorkers == 1 && dc == int32(c.GetDC()) {
		sender := NewExSender(c.MTProto)
		if !sender.TryAcquire() {
			return errors.New("failed to reserve local sender")
		}
		w.AddWorker(sender)
		return nil
	}

	var authParams *AuthExportedAuthorization
	if dc != int32(c.GetDC()) {
		var err error
		authParams, err = c.ensureExportedAuth(dc)
		if err != nil {
			return err
		}
	}

	c.exSenders.Lock()
	existing := append([]*ExSender(nil), c.exSenders.senders[int(dc)]...)
	c.exSenders.Unlock()

	numCreate := 0
	for _, worker := range existing {
		if worker == nil {
			continue
		}
		if !worker.TryAcquire() {
			continue
		}
		w.AddWorker(worker)
		numCreate++
		if numCreate >= numWorkers {
			return nil
		}
	}

	needed := numWorkers - numCreate
	if needed <= 0 {
		return nil
	}

	c.Log.WithFields(map[string]any{
		"dc":      dc,
		"workers": needed,
	}).Info("exporting senders")

	var (
		lastErr error
		errMu   sync.Mutex
	)

	var readyOnce sync.Once
	var readyCh chan struct{}
	var doneCh chan struct{}
	if numCreate == 0 {
		readyCh = make(chan struct{})
		doneCh = make(chan struct{})
	}

	var wg sync.WaitGroup
	wg.Add(needed)
	for range needed {
		go func() {
			defer wg.Done()
			conn, err := c.CreateExportedSender(int(dc), false, authParams)
			if err != nil || conn == nil {
				errMu.Lock()
				if err != nil {
					lastErr = err
				}
				errMu.Unlock()
				if err != nil {
					c.Log.WithError(err).Warn("failed to export sender")
				}
				return
			}
			sender := NewExSender(conn)
			if !sender.TryAcquire() {
				c.Log.WithField("dc", dc).Warn("newly exported sender busy before use")
				return
			}
			c.exSenders.Lock()
			c.exSenders.senders[int(dc)] = append(c.exSenders.senders[int(dc)], sender)
			c.exSenders.Unlock()
			w.AddWorker(sender)
			if readyCh != nil {
				readyOnce.Do(func() { close(readyCh) })
			}
		}()
	}

	if numCreate > 0 {
		return nil
	}

	go func() {
		wg.Wait()
		if doneCh != nil {
			close(doneCh)
		}
	}()

	timer := time.NewTimer(120 * time.Second)
	defer timer.Stop()

	select {
	case <-readyCh:
		return nil
	case <-doneCh:
		if lastErr != nil {
			return lastErr
		}
		return errors.New("failed to initialize workers")
	case <-timer.C:
		return errors.New("timed out initializing workers")
	}
}

// DownloadChunk downloads a file in chunks, useful for downloading specific parts of a file.
//
// start and end are the byte offsets to download.
// chunkSize is the size of each chunk to download.
// callback is an optional function that receives each chunk as it's downloaded.
//
// Note: chunkSize must be a multiple of 1048576 (1MB)
func (c *Client) DownloadChunk(media any, start int, end int, chunkSize int, callback ...func([]byte)) ([]byte, string, error) {
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
	defer w.Close()
	if err := initializeWorkers(1, int32(dc), c, w); err != nil {
		return nil, "", err
	}

	sender := w.Next()
	defer w.FreeWorker(sender)

	var onChunk func([]byte)
	if len(callback) > 0 {
		onChunk = callback[0]
	}

	for curr := start; curr < end; curr += chunkSize {
		part, err := sender.MakeRequest(&UploadGetFileParams{
			Location:     input,
			Limit:        int32(chunkSize),
			Offset:       int64(curr),
			CdnSupported: false,
		})

		if err != nil {
			if handleIfFlood(err, c) {
				continue
			}
			c.Log.WithError(err).Error("downloading chunk")
		}

		switch v := part.(type) {
		case *UploadFileObj:
			buf = append(buf, v.Bytes...)
			if onChunk != nil {
				onChunk(v.Bytes)
			}
		case *UploadFileCdnRedirect:
			return nil, "", errors.New("cdn redirect not implemented")
		}
	}

	return buf, name, nil
}

// ----------------------- Progress Manager -----------------------
type ProgressManager struct {
	mu           sync.Mutex
	startTime    time.Time
	editInterval time.Duration
	editFunc     func(totalSize, currentSize int64)
	totalSize    int64
	lastPerc     float64
	fileName     string
	fileCount    int
	meta         struct {
		dataCenter int
		numWorkers int
	}
	lastSampleBytes int64
	lastSampleTime  time.Time
	smoothedSpeed   float64
	lastSnapshot    ProgressSnapshot
}

type ProgressSnapshot struct {
	FileName       string
	TotalSize      int64
	CurrentSize    int64
	Percentage     float64
	SpeedBps       float64
	Speed          string
	ETA            time.Duration
	ETAString      string
	ProgressBar    string
	Status         string
	RemainingBytes int64
	StartedAt      time.Time
	UpdatedAt      time.Time
	Meta           struct {
		DataCenter int
		NumWorkers int
	}
}

func NewProgressManager(editInterval int, editFunc ...func(totalSize, currentSize int64)) *ProgressManager {
	if editInterval <= 0 {
		editInterval = 2
	}
	pm := &ProgressManager{
		startTime:    time.Now(),
		editInterval: normalizeInterval(time.Duration(editInterval) * time.Second),
	}
	if len(editFunc) > 0 {
		pm.editFunc = editFunc[0]
	}
	return pm
}

func (pm *ProgressManager) Interval() time.Duration {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return normalizeInterval(pm.editInterval)
}

func (pm *ProgressManager) SetInterval(interval time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.editInterval = normalizeInterval(interval)
}

func normalizeInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 2 * time.Second
	}
	if interval < 200*time.Millisecond {
		return 200 * time.Millisecond
	}
	return interval
}

func ensureProgressManager(base *ProgressManager, handler func(totalSize, currentSize int64), interval time.Duration) *ProgressManager {
	if base == nil && handler == nil {
		return nil
	}
	if base == nil {
		secs := int(normalizeInterval(interval) / time.Second)
		if secs <= 0 {
			secs = 2
		}
		base = NewProgressManager(secs)
	}
	if interval > 0 {
		base.SetInterval(interval)
	}
	if handler != nil {
		base.Edit(handler)
	}
	return base
}

func (pm *ProgressManager) SetMessage(msg *NewMessage) *ProgressManager {
	pm.editFunc = MediaDownloadProgress(msg, pm)
	return pm
}

func (pm *ProgressManager) SetInlineMessage(client *Client, inline *InputBotInlineMessageID) *ProgressManager {
	pm.editFunc = MediaDownloadProgress(&NewMessage{Client: client}, pm, inline)
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
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.totalSize = totalSize
	pm.resetTrackingLocked()
}

func (pm *ProgressManager) resetTrackingLocked() {
	pm.startTime = time.Now()
	pm.lastPerc = 0
	pm.lastSampleBytes = 0
	pm.lastSampleTime = time.Time{}
	pm.smoothedSpeed = 0
	pm.lastSnapshot = ProgressSnapshot{}
}

func (pm *ProgressManager) SetMeta(dataCenter, numWorkers int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.meta.dataCenter = dataCenter
	pm.meta.numWorkers = numWorkers
}

func (pm *ProgressManager) PrintFunc() func(a, b int64) {
	return func(_, current int64) {
		fmt.Println(pm.GetStats(current))
	}
}

// specify the message to edit
func (pm *ProgressManager) WithMessage(msg *NewMessage) func(a, b int64) {
	return func(_, current int64) {
		msg.Edit(pm.GetStats(current))
	}
}

func (pm *ProgressManager) SetFileName(fileName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.fileName = fileName
}

func (pm *ProgressManager) GetFileName() string {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.fileName
}

func (pm *ProgressManager) IncCount() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.fileCount++
}

func (pm *ProgressManager) GetCount() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.fileCount
}

func (pm *ProgressManager) Snapshot(currentBytes int64) ProgressSnapshot {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now()
	if pm.startTime.IsZero() {
		pm.startTime = now
	}
	current := currentBytes
	if pm.totalSize > 0 && current > pm.totalSize {
		current = pm.totalSize
	}
	if current < 0 {
		current = 0
	}
	percent := 0.0
	if pm.totalSize > 0 {
		percent = (float64(current) / float64(pm.totalSize)) * 100
	}
	if percent > 100 {
		percent = 100
	}
	if percent < pm.lastPerc {
		percent = pm.lastPerc
	}
	pm.lastPerc = percent

	if pm.lastSampleTime.IsZero() {
		pm.lastSampleTime = now
		pm.lastSampleBytes = currentBytes
	}

	elapsed := now.Sub(pm.lastSampleTime)
	if elapsed >= 350*time.Millisecond {
		deltaBytes := currentBytes - pm.lastSampleBytes
		if deltaBytes < 0 {
			deltaBytes = 0
		}
		instantSpeed := 0.0
		if elapsed > 0 {
			instantSpeed = float64(deltaBytes) / elapsed.Seconds()
		}
		const smoothing = 0.55
		if pm.smoothedSpeed == 0 {
			pm.smoothedSpeed = instantSpeed
		} else {
			pm.smoothedSpeed = smoothing*instantSpeed + (1-smoothing)*pm.smoothedSpeed
		}
		pm.lastSampleBytes = currentBytes
		pm.lastSampleTime = now
	}

	remaining := pm.totalSize - current
	if remaining < 0 {
		remaining = 0
	}
	etaDuration := time.Duration(-1)
	if pm.smoothedSpeed > 0 && remaining > 0 {
		etaDuration = time.Duration(float64(remaining)/pm.smoothedSpeed) * time.Second
	} else if remaining == 0 && pm.totalSize > 0 {
		etaDuration = 0
	}

	status := "Downloading"
	if remaining == 0 && pm.totalSize > 0 {
		status = "✅ Completed"
		percent = 100
	}

	snapshot := ProgressSnapshot{
		FileName:       pm.fileName,
		TotalSize:      pm.totalSize,
		CurrentSize:    current,
		Percentage:     percent,
		SpeedBps:       pm.smoothedSpeed,
		Speed:          humanizeSpeed(pm.smoothedSpeed),
		ETA:            etaDuration,
		ETAString:      formatETAString(etaDuration),
		ProgressBar:    buildProgressBar(percent, 10),
		Status:         status,
		RemainingBytes: remaining,
		StartedAt:      pm.startTime,
		UpdatedAt:      now,
	}
	snapshot.Meta.DataCenter = pm.meta.dataCenter
	snapshot.Meta.NumWorkers = pm.meta.numWorkers
	pm.lastSnapshot = snapshot
	return snapshot
}

func (pm *ProgressManager) RenderSnapshot(currentBytes int64) string {
	snap := pm.Snapshot(currentBytes)
	sizeMiB := float64(snap.TotalSize) / (1024 * 1024)
	var builder strings.Builder
	fmt.Fprintf(&builder, "<b>📄 Name:</b> <code>%s</code>\n", snap.FileName)
	fmt.Fprintf(&builder, "<b>🈂️ DC ID:</b> <code>%d</code> <b>|</b> <b>⚡ Workers:</b> <code>%d</code>\n\n", snap.Meta.DataCenter, snap.Meta.NumWorkers)
	fmt.Fprintf(&builder, "<b>💾 File Size:</b> <code>%.2f MiB</code>\n", sizeMiB)
	fmt.Fprintf(&builder, "<b>⚙️ Progress:</b> %s <code>%.2f%%</code>\n", snap.ProgressBar, snap.Percentage)
	fmt.Fprintf(&builder, "<b>⏱ Speed:</b> <code>%s</code>\n", snap.Speed)
	fmt.Fprintf(&builder, "<b>⌛️ ETA:</b> <code>%s</code>\n", snap.ETAString)
	fmt.Fprintf(&builder, "<b>Status:</b> <code>%s</code>", snap.Status)
	return builder.String()
}

func (pm *ProgressManager) GetProgress(currentBytes int64) float64 {
	return pm.Snapshot(currentBytes).Percentage
}

func (pm *ProgressManager) GetETA(currentBytes int64) string {
	return pm.Snapshot(currentBytes).ETAString
}

func (pm *ProgressManager) GetSpeed(currentBytes int64) string {
	return pm.Snapshot(currentBytes).Speed
}

func (pm *ProgressManager) GetStats(currentBytes int64) string {
	snap := pm.Snapshot(currentBytes)
	return fmt.Sprintf("Progress: %.2f%% | ETA: %s | Speed: %s\n%s", snap.Percentage, snap.ETAString, snap.Speed, pm.GenProgressBar(currentBytes))
}

func (pm *ProgressManager) GenProgressBar(b int64) string {
	snap := pm.Snapshot(b)
	bar := buildProgressBar(snap.Percentage, 20)
	return fmt.Sprintf("\r[%s] %d%%", strings.ReplaceAll(bar, "■", "="), int(snap.Percentage))
}

func MediaDownloadProgress(editMsg *NewMessage, pm *ProgressManager, inline ...*InputBotInlineMessageID) func(totalBytes, currentBytes int64) {
	return func(totalBytes int64, currentBytes int64) {
		if pm.totalSize == 0 && totalBytes > 0 {
			pm.SetTotalSize(totalBytes)
		}
		message := pm.RenderSnapshot(currentBytes)
		if len(inline) > 0 {
			editMsg.Client.EditMessage(inline[0], 0, message)
		} else {
			editMsg.Edit(message)
		}
	}
}

func humanizeSpeed(bps float64) string {
	switch {
	case bps <= 0:
		return "0 B/s"
	case bps >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GB/s", bps/1024/1024/1024)
	case bps >= 1024*1024:
		return fmt.Sprintf("%.2f MB/s", bps/1024/1024)
	case bps >= 1024:
		return fmt.Sprintf("%.2f KB/s", bps/1024)
	default:
		return fmt.Sprintf("%.2f B/s", bps)
	}
}

func formatETAString(d time.Duration) string {
	if d < 0 {
		return "calculating..."
	}
	if d == 0 {
		return "done"
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	s := int((d % time.Minute) / time.Second)
	if h > 0 {
		return fmt.Sprintf("%dh %02dm %02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func buildProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = 10
	}
	filled := min(int(math.Round(percent/100*float64(width))), width)
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("■", filled) + strings.Repeat("□", width-filled)
}
