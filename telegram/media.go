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
	close(wp.free)

	for range wp.free {
		// Just drain
	}

	wp.workers = nil
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

func (s *Source) GetReaderAt() (io.ReaderAt, bool) {
	switch src := s.Source.(type) {
	case string:
		file, err := os.Open(src)
		if err != nil {
			return nil, false
		}
		s.closer = file
		return file, true
	case *os.File:
		return src, true
	case []byte:
		return bytes.NewReader(src), true
	}
	return nil, false
}

func (s *Source) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}

// retryWithBackoff executes a function with retry logic and exponential backoff
func retryWithBackoff(ctx context.Context, maxRetries int, baseDelay time.Duration, fn func(retryCount int) bool) {
	for retry := 0; retry < maxRetries; retry++ {
		if fn(retry) {
			return
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(baseDelay * time.Duration(retry+1)):
		}
	}
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

	readerAt, canSeek := source.GetReaderAt()
	if !canSeek {
		return c.uploadSequential(file, size, fileName, opts)
	}

	partSize := 1024 * 512
	if opts.ChunkSize > 0 {
		partSize = int(opts.ChunkSize)
	}
	fileId := GenerateRandomLong()
	isBigFile := size > 10*1024*1024

	parts := size / int64(partSize)
	partOver := size % int64(partSize)
	totalParts := int(parts)
	if partOver > 0 {
		totalParts++
	}

	// For small files (<10MB), use only main client (pool of 1)
	numWorkers := countWorkers(int64(totalParts))
	if size < 10*1024*1024 {
		numWorkers = 1
	}
	if opts.Threads > 0 {
		numWorkers = opts.Threads
	}

	w := NewWorkerPool(numWorkers)
	defer w.Close()

	if err := initializeWorkers(numWorkers, int32(c.GetDC()), c, w); err != nil {
		return nil, err
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if !w.WaitReady(initCtx) {
		initCancel()
		return nil, errors.New("failed to initialize upload workers: timeout")
	}
	initCancel()

	uploadLog := newPartLogAggregator("upload", totalParts, 3*time.Second, c.Log)
	uploadLog.setNumWorkers(numWorkers)
	defer uploadLog.Flush()

	c.Log.WithFields(map[string]any{
		"file_name": source.GetName(),
		"file_size": SizetoHuman(size),
		"parts":     totalParts,
		"workers":   numWorkers,
	}).Info("starting file upload")

	var (
		doneBytes      atomic.Int64
		completedParts = make([]atomic.Bool, totalParts)
		globalErr      atomic.Value
	)

	var progressCallback func(*ProgressInfo)
	if opts.ProgressCallback != nil {
		progressCallback = opts.ProgressCallback
	} else if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(source.GetName())
		opts.ProgressManager.SetTotalSize(size)
		progressCallback = opts.ProgressManager.getCallback()
	}

	var progressTracker *progressTracker
	if progressCallback != nil {
		progressTracker = newProgressTracker(source.GetName(), size, progressCallback, opts.ProgressInterval)
		defer progressTracker.stop()
		progressCallback(&ProgressInfo{
			FileName:   source.GetName(),
			TotalSize:  size,
			Current:    0,
			Percentage: 0,
		})
		progressTracker.start(&doneBytes)
	}

	var uploadCtx context.Context
	var uploadCancel context.CancelFunc
	if opts.Ctx != nil {
		uploadCtx, uploadCancel = context.WithCancel(opts.Ctx)
	} else {
		uploadCtx, uploadCancel = context.WithCancel(context.Background())
	}
	defer uploadCancel()

	var hash hash.Hash
	var hashMu sync.Mutex
	var nextHashPart atomic.Int32
	var hashPending = make(map[int][]byte)
	if !isBigFile {
		hash = md5.New()
	}

	writeToHash := func(partNum int, data []byte) {
		if isBigFile || hash == nil {
			return
		}

		hashMu.Lock()
		defer hashMu.Unlock()
		hashPending[partNum] = data

		next := int(nextHashPart.Load())
		for {
			if partData, ok := hashPending[next]; ok {
				hash.Write(partData)
				delete(hashPending, next)
				next++
				nextHashPart.Store(int32(next))
			} else {
				break
			}
		}
	}

	uploadPart := func(partNum int, retryCount int) bool {
		select {
		case <-uploadCtx.Done():
			return false
		default:
		}

		if globalErr.Load() != nil {
			return false
		}

		if completedParts[partNum].Load() {
			return true
		}

		offset := int64(partNum) * int64(partSize)
		chunkSize := partSize
		if offset+int64(partSize) > size {
			chunkSize = int(size - offset)
		}

		data := make([]byte, chunkSize)
		n, err := readerAt.ReadAt(data, offset)
		if err != nil && err != io.EOF {
			return false
		}
		data = data[:n]

		ctx, cancel := context.WithTimeout(uploadCtx, 5*time.Second)
		defer cancel()

		sender := w.NextWithContext(ctx)
		if sender == nil {
			return false
		}

		if isBigFile {
			_, err = sender.MakeRequestCtx(ctx, &UploadSaveBigFilePartParams{
				FileID:         fileId,
				FilePart:       int32(partNum),
				FileTotalParts: int32(totalParts),
				Bytes:          data,
			})
		} else {
			_, err = sender.MakeRequestCtx(ctx, &UploadSaveFilePartParams{
				FileID:   fileId,
				FilePart: int32(partNum),
				Bytes:    data,
			})
		}

		if opts.Delay > 0 {
			time.Sleep(time.Duration(opts.Delay) * time.Millisecond)
		}

		w.FreeWorker(sender)

		if err != nil {
			if MatchError(err, "FLOOD_WAIT_") || MatchError(err, "FLOOD_PREMIUM_WAIT_") {
				if waitTime := GetFloodWait(err); waitTime > 0 {
					backoff := time.Duration(waitTime+retryCount*retryCount) * time.Second
					c.Log.Debug(fmt.Sprintf("flood wait: sleeping %v (retry %d)", backoff, retryCount))
					select {
					case <-uploadCtx.Done():
						return false
					case <-time.After(backoff):
						// After sleeping, return false to trigger retry
						return false
					}
				}
			}
			uploadLog.recordFailure(partNum, err, sender)
			return false
		}

		writeToHash(partNum, data)

		doneBytes.Add(int64(len(data)))
		completedParts[partNum].Store(true)
		uploadLog.recordSuccess(partNum, sender)
		return true
	}

	var wg sync.WaitGroup

	partQueue := make(chan int, totalParts)

	for p := 0; p < totalParts; p++ {
		partQueue <- p
	}
	close(partQueue)

	for i := 0; i < numWorkers; i++ {
		wg.Go(func() {
			for partNum := range partQueue {
				if uploadCtx.Err() != nil || globalErr.Load() != nil {
					return
				}

				retryWithBackoff(uploadCtx, 5, 100*time.Millisecond, func(retry int) bool {
					if uploadPart(partNum, retry) {
						return true
					}
					return uploadCtx.Err() != nil || globalErr.Load() != nil
				})
			}
		})
	}

	wg.Wait()

	if err := globalErr.Load(); err != nil {
		return nil, err.(error)
	}

	for round := range 3 {
		var failedParts []int
		for p := 0; p < totalParts; p++ {
			if !completedParts[p].Load() {
				failedParts = append(failedParts, p)
			}
		}

		if len(failedParts) == 0 {
			break
		}

		c.Log.WithFields(map[string]any{
			"round":        round + 1,
			"failed_parts": len(failedParts),
		}).Debug("retrying failed parts")

		retryQueue := make(chan int, len(failedParts))
		for _, p := range failedParts {
			retryQueue <- p
		}
		close(retryQueue)

		for i := 0; i < numWorkers && i < len(failedParts); i++ {
			wg.Add(1)
			go func(retryRound int) {
				defer wg.Done()
				for partNum := range retryQueue {
					if uploadCtx.Err() != nil || globalErr.Load() != nil {
						return
					}

					retryWithBackoff(uploadCtx, 3, 200*time.Millisecond, func(retry int) bool {
						if uploadPart(partNum, retryRound*3+retry) {
							return true
						}
						return uploadCtx.Err() != nil || globalErr.Load() != nil
					})
				}
			}(round)
		}

		wg.Wait()

		if err := globalErr.Load(); err != nil {
			return nil, err.(error)
		}
	}

	var missingParts []int
	for p := 0; p < totalParts; p++ {
		if !completedParts[p].Load() {
			missingParts = append(missingParts, p)
		}
	}

	if len(missingParts) > 0 {
		return nil, fmt.Errorf("upload incomplete: %d parts failed (parts: %v)", len(missingParts), missingParts)
	}

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
		"parts":     totalParts,
	}).Info("file upload completed")

	if isBigFile {
		return &InputFileBig{
			ID:    fileId,
			Parts: int32(totalParts),
			Name:  prettifyFileName(fileName),
		}, nil
	}

	return &InputFileObj{
		ID:          fileId,
		Md5Checksum: hex.EncodeToString(hash.Sum(nil)),
		Name:        prettifyFileName(fileName),
		Parts:       int32(totalParts),
	}, nil
}

func (c *Client) uploadSequential(file io.Reader, size int64, fileName string, opts *UploadOptions) (InputFile, error) {
	partSize := 1024 * 512
	if opts.ChunkSize > 0 {
		partSize = int(opts.ChunkSize)
	}
	fileId := GenerateRandomLong()
	isBigFile := size > 10*1024*1024

	parts := size / int64(partSize)
	partOver := size % int64(partSize)
	totalParts := int(parts)
	if partOver > 0 {
		totalParts++
	}

	var hash hash.Hash
	if !isBigFile {
		hash = md5.New()
		file = io.TeeReader(file, hash)
	}

	var uploadCtx context.Context
	var uploadCancel context.CancelFunc
	if opts.Ctx != nil {
		uploadCtx, uploadCancel = context.WithCancel(opts.Ctx)
	} else {
		uploadCtx, uploadCancel = context.WithCancel(context.Background())
	}
	defer uploadCancel()

	var doneBytes atomic.Int64
	var progressCallback func(*ProgressInfo)
	if opts.ProgressCallback != nil {
		progressCallback = opts.ProgressCallback
	} else if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(fileName)
		opts.ProgressManager.SetTotalSize(size)
		progressCallback = opts.ProgressManager.getCallback()
	}

	var progressTracker *progressTracker
	if progressCallback != nil {
		progressTracker = newProgressTracker(fileName, size, progressCallback, opts.ProgressInterval)
		defer progressTracker.stop()
		// Mark start of operation
		progressCallback(&ProgressInfo{
			FileName:   fileName,
			TotalSize:  size,
			Current:    0,
			Percentage: 0,
		})
		progressTracker.start(&doneBytes)
	}

	for p := 0; p < totalParts; p++ {
		select {
		case <-uploadCtx.Done():
			return nil, uploadCtx.Err()
		default:
		}

		part := make([]byte, partSize)
		readBytes, err := io.ReadFull(file, part)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return nil, err
		}
		part = part[:readBytes]

		var uploadErr error
		for retry := range 5 {
			ctx, cancel := context.WithTimeout(uploadCtx, 5*time.Second)
			if isBigFile {
				_, uploadErr = c.MakeRequestCtx(ctx, &UploadSaveBigFilePartParams{
					FileID:         fileId,
					FilePart:       int32(p),
					FileTotalParts: int32(totalParts),
					Bytes:          part,
				})
			} else {
				_, uploadErr = c.MakeRequestCtx(ctx, &UploadSaveFilePartParams{
					FileID:   fileId,
					FilePart: int32(p),
					Bytes:    part,
				})
			}
			cancel()

			if uploadErr == nil {
				break
			}

			if MatchError(uploadErr, "FLOOD_WAIT_") || MatchError(uploadErr, "FLOOD_PREMIUM_WAIT_") {
				if waitTime := GetFloodWait(uploadErr); waitTime > 0 {
					select {
					case <-uploadCtx.Done():
						return nil, uploadCtx.Err()
					case <-time.After(time.Duration(waitTime+retry*retry) * time.Second):
						continue
					}
				}
			}
			break
		}
		if uploadErr != nil {
			return nil, uploadErr
		}

		doneBytes.Add(int64(len(part)))

		if opts.Delay > 0 {
			time.Sleep(time.Duration(opts.Delay) * time.Millisecond)
		}
	}

	if progressCallback != nil {
		progressCallback(&ProgressInfo{
			FileName:   fileName,
			TotalSize:  size,
			Current:    size,
			Percentage: 100,
		})
	}

	if opts.FileName != "" {
		fileName = opts.FileName
	}

	if isBigFile {
		return &InputFileBig{
			ID:    fileId,
			Parts: int32(totalParts),
			Name:  prettifyFileName(fileName),
		}, nil
	}

	return &InputFileObj{
		ID:          fileId,
		Md5Checksum: hex.EncodeToString(hash.Sum(nil)),
		Name:        prettifyFileName(fileName),
		Parts:       int32(totalParts),
	}, nil
}

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
		return 12
	}
}

func chunkSizeCalc(size int64) int {
	if size < 200*1024*1024 {
		return 256 * 1024
	} else if size < 1024*1024*1024 {
		return 512 * 1024
	}
	return 1024 * 1024
}

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
	file *os.File
}

func (mb *Destination) WriteAt(p []byte, off int64) (n int, err error) {
	if mb.file != nil {
		return mb.file.WriteAt(p, off)
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
	totalParts := int(parts)
	if partOver > 0 {
		totalParts++
	}

	// For small files (<10MB), use only main client (pool of 1)
	numWorkers := countWorkers(parts)
	if size < 10*1024*1024 {
		numWorkers = 1
	}
	if opts.Threads > 0 {
		numWorkers = opts.Threads
	}

	w := NewWorkerPool(numWorkers)
	defer w.Close()

	if err := initializeWorkers(numWorkers, dc, c, w); err != nil {
		return "", err
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if !w.WaitReady(initCtx) {
		initCancel()
		return "", errors.New("failed to initialize download workers: timeout")
	}
	initCancel()

	downloadLog := newPartLogAggregator("download", totalParts, 3*time.Second, c.Log)
	downloadLog.setNumWorkers(numWorkers)
	defer downloadLog.Flush()

	if opts.Buffer != nil {
		dest = ":mem-buffer:"
		c.Log.WithField("size", size).Warn("downloading to buffer (memory) - use with caution (memory usage)")
	}

	c.Log.WithFields(map[string]any{
		"file_name": dest,
		"file_size": SizetoHuman(size),
		"parts":     totalParts,
		"workers":   numWorkers,
	}).Info("starting file download")

	var (
		doneBytes      atomic.Int64
		completedParts = make([]atomic.Bool, totalParts)
		globalErr      atomic.Value
		cdnRedirect    atomic.Bool
	)

	var progressCallback func(*ProgressInfo)
	if opts.ProgressCallback != nil {
		progressCallback = opts.ProgressCallback
	} else if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(dest)
		opts.ProgressManager.SetTotalSize(size)
		progressCallback = opts.ProgressManager.getCallback()
	}

	var progressTracker *progressTracker
	if progressCallback != nil {
		progressTracker = newProgressTracker(dest, size, progressCallback, opts.ProgressInterval)
		defer progressTracker.stop()
		// Mark start of operation
		progressCallback(&ProgressInfo{
			FileName:   dest,
			TotalSize:  size,
			Current:    0,
			Percentage: 0,
		})
		progressTracker.start(&doneBytes)
	}

	c.Log.WithFields(map[string]any{
		"parts":   totalParts,
		"workers": numWorkers,
		"dc":      dc,
	}).Debug("dispatching download workers")

	var downloadCtx context.Context
	var downloadCancel context.CancelFunc
	if opts.Ctx != nil {
		downloadCtx, downloadCancel = context.WithCancel(opts.Ctx)
	} else {
		downloadCtx, downloadCancel = context.WithCancel(context.Background())
	}
	defer downloadCancel()

	downloadPart := func(partNum int, retryCount int) bool {
		select {
		case <-downloadCtx.Done():
			return false
		default:
		}

		if globalErr.Load() != nil || cdnRedirect.Load() {
			return false
		}

		if completedParts[partNum].Load() {
			return true
		}

		ctx, cancel := context.WithTimeout(downloadCtx, 5*time.Second)
		defer cancel()

		sender := w.NextWithContext(ctx)
		if sender == nil {
			return false
		}

		part, err := sender.MakeRequestCtx(ctx, &UploadGetFileParams{
			Location:     location,
			Offset:       int64(partNum * partSize),
			Limit:        int32(partSize),
			Precise:      true,
			CdnSupported: false,
		})

		if opts.Delay > 0 {
			time.Sleep(time.Duration(opts.Delay) * time.Millisecond)
		}

		w.FreeWorker(sender)

		if err != nil {
			if MatchError(err, "FLOOD_WAIT_") || MatchError(err, "FLOOD_PREMIUM_WAIT_") {
				if waitTime := GetFloodWait(err); waitTime > 0 {
					backoff := time.Duration(waitTime+retryCount*retryCount) * time.Second
					c.Log.Debug(fmt.Sprintf("flood wait: sleeping %v (retry %d)", backoff, retryCount))
					select {
					case <-downloadCtx.Done():
						return false
					case <-time.After(backoff):
						// After sleeping, return false to trigger retry
						return false
					}
				}
			}

			if strings.Contains(err.Error(), "FILE_REFERENCE_EXPIRED") {
				globalErr.Store(errors.New("file reference expired"))
				downloadCancel()
				return false
			}

			downloadLog.recordFailure(partNum, err, sender)
			return false
		}

		switch v := part.(type) {
		case *UploadFileObj:
			if _, writeErr := fs.WriteAt(v.Bytes, int64(partNum)*int64(partSize)); writeErr != nil {
				downloadLog.recordFailure(partNum, writeErr, sender)
				return false
			}
			doneBytes.Add(int64(len(v.Bytes)))
			completedParts[partNum].Store(true)
			downloadLog.recordSuccess(partNum, sender)
			return true

		case *UploadFileCdnRedirect:
			cdnRedirect.Store(true)
			return false

		case nil:
			return false

		default:
			return false
		}
	}

	var wg sync.WaitGroup

	partQueue := make(chan int, totalParts)

	for p := 0; p < totalParts; p++ {
		partQueue <- p
	}
	close(partQueue)

	for i := 0; i < numWorkers; i++ {
		wg.Go(func() {
			for partNum := range partQueue {
				if downloadCtx.Err() != nil || globalErr.Load() != nil || cdnRedirect.Load() {
					return
				}

				retryWithBackoff(downloadCtx, 5, 100*time.Millisecond, func(retry int) bool {
					if downloadPart(partNum, retry) {
						return true
					}
					return downloadCtx.Err() != nil || globalErr.Load() != nil || cdnRedirect.Load()
				})
			}
		})
	}

	wg.Wait()

	if cdnRedirect.Load() {
		return "", fmt.Errorf("cdn redirect not implemented")
	}

	if err := globalErr.Load(); err != nil {
		return "", err.(error)
	}

	const maxRetryRounds = 3
	for round := range maxRetryRounds {
		var failedParts []int
		for p := 0; p < totalParts; p++ {
			if !completedParts[p].Load() {
				failedParts = append(failedParts, p)
			}
		}

		if len(failedParts) == 0 {
			break
		}

		c.Log.WithFields(map[string]any{
			"round":        round + 1,
			"failed_parts": len(failedParts),
		}).Debug("retrying failed parts")

		retryQueue := make(chan int, len(failedParts))
		for _, p := range failedParts {
			retryQueue <- p
		}
		close(retryQueue)

		for i := 0; i < numWorkers && i < len(failedParts); i++ {
			wg.Add(1)
			go func(retryRound int) {
				defer wg.Done()
				for partNum := range retryQueue {
					if downloadCtx.Err() != nil || globalErr.Load() != nil || cdnRedirect.Load() {
						return
					}

					retryWithBackoff(downloadCtx, 3, 200*time.Millisecond, func(retry int) bool {
						if downloadPart(partNum, retryRound*3+retry) {
							return true
						}
						return downloadCtx.Err() != nil || globalErr.Load() != nil || cdnRedirect.Load()
					})
				}
			}(round)
		}

		wg.Wait()

		if cdnRedirect.Load() {
			return "", fmt.Errorf("cdn redirect not implemented")
		}

		if err := globalErr.Load(); err != nil {
			return "", err.(error)
		}
	}

	var missingParts []int
	for p := 0; p < totalParts; p++ {
		if !completedParts[p].Load() {
			missingParts = append(missingParts, p)
		}
	}

	if len(missingParts) > 0 {
		return "", fmt.Errorf("download incomplete: %d parts failed after all retries (parts: %v)", len(missingParts), missingParts)
	}

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
		if _, err := io.Copy(opts.Buffer, bytes.NewReader(fs.data)); err != nil {
			return "", fmt.Errorf("failed to copy to buffer: %w", err)
		}
	}

	c.Log.WithFields(map[string]any{
		"file_name": dest,
		"file_size": SizetoHuman(size),
	}).Info("file download completed")

	return dest, nil
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
	defer w.Close()

	if err := initializeWorkers(1, int32(dc), c, w); err != nil {
		return nil, "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if !w.WaitReady(ctx) {
		return nil, "", errors.New("failed to initialize worker: timeout")
	}

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
	logger      Logger
}

type senderStats struct {
	successes int
	failures  int
	lastSeen  time.Time
}

func newPartLogAggregator(ctx string, total int, interval time.Duration, logger Logger) *partLogAggregator {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	return &partLogAggregator{
		ctx:         ctx,
		total:       total,
		interval:    interval,
		lastLog:     time.Now(),
		senderStats: make(map[*ExSender]*senderStats),
		logger:      logger,
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
	a.maybeLogLocked()
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
	a.maybeLogLocked()
	a.mu.Unlock()
}

func (a *partLogAggregator) maybeLogLocked() {
	if time.Since(a.lastLog) < a.interval {
		return
	}
	a.logLocked()
}

func (a *partLogAggregator) logLocked() {
	if a.successes == 0 && a.failures == 0 {
		return
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

	if a.logger != nil {
		a.logger.Debug(logMsg)
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
				"<b>Speed:</b> <code>%s</code>\n"+
				"<b>ETA:</b> <code>%s</code> | <b>Elapsed:</b> <code>%s</code>\n\n"+
				"%s <code>%.2f%%</code>",
			info.FileName,
			float64(info.TotalSize)/1024/1024,
			info.SpeedString(),
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
