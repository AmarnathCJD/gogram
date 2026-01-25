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

var chunkPool = sync.Pool{
	New: func() any {
		b := make([]byte, 1024*1024)
		return &b
	},
}

type UploadOptions struct {
	Threads          int                 // Number of concurrent upload workers
	ChunkSize        int32               // Size of each upload chunk in bytes
	FileName         string              // Custom filename for the upload
	ProgressCallback func(*ProgressInfo) // Callback for upload progress updates
	ProgressManager  *ProgressManager    // Progress manager (legacy support)
	ProgressInterval int                 // Progress callback interval in seconds (default: 5)
	Delay            int                 // Delay between chunks in milliseconds
	Ctx              context.Context     // Context for cancellation
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
		if src == nil {
			return nil
		}
		return src
	case []byte:
		return bytes.NewReader(src)
	case *bytes.Buffer:
		if src == nil {
			return bytes.NewReader(nil)
		}
		return bytes.NewReader(src.Bytes())
	case *io.Reader:
		if src == nil {
			return nil
		}
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

		bufPtr := chunkPool.Get().(*[]byte)
		defer chunkPool.Put(bufPtr)

		if cap(*bufPtr) < int(chunkSize) {
			*bufPtr = make([]byte, chunkSize)
		}
		data := (*bufPtr)[:chunkSize]

		n, err := readerAt.ReadAt(data, offset)
		if err != nil && err != io.EOF {
			return false
		}
		data = data[:n]

		ctx, cancel := context.WithTimeout(uploadCtx, 10*time.Second)
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

			if MatchError(err, "timeout") {
				return false
			}

			return false
		}

		writeToHash(partNum, data)

		doneBytes.Add(int64(len(data)))
		completedParts[partNum].Store(true)
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

	if size == 0 {
		totalParts = -1
		isBigFile = true
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

	currentPart := make([]byte, partSize)
	readBytes, err := io.ReadFull(file, currentPart)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	currentPart = currentPart[:readBytes]

	for p := 0; p < totalParts || totalParts == -1; p++ {
		select {
		case <-uploadCtx.Done():
			return nil, uploadCtx.Err()
		default:
		}

		nextPart := make([]byte, partSize)
		readBytes, err := io.ReadFull(file, nextPart)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return nil, err
		}
		nextPart = nextPart[:readBytes]

		if len(nextPart) == 0 {
			totalParts = p + 1
		}

		var uploadErr error
		for retry := range 5 {
			ctx, cancel := context.WithTimeout(uploadCtx, 10*time.Second)
			if isBigFile {
				_, uploadErr = c.MakeRequestCtx(ctx, &UploadSaveBigFilePartParams{
					FileID:         fileId,
					FilePart:       int32(p),
					FileTotalParts: int32(totalParts),
					Bytes:          currentPart,
				})
			} else {
				_, uploadErr = c.MakeRequestCtx(ctx, &UploadSaveFilePartParams{
					FileID:   fileId,
					FilePart: int32(p),
					Bytes:    currentPart,
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

			if MatchError(uploadErr, "timeout") {
				select {
				case <-uploadCtx.Done():
					return nil, uploadCtx.Err()
				default:
					continue
				}
			}

			break
		}
		if uploadErr != nil {
			return nil, uploadErr
		}

		doneBytes.Add(int64(len(currentPart)))
		currentPart = nextPart

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

type DownloadOptions struct {
	FileName         string              // Path to save the downloaded file
	Threads          int                 // Number of concurrent download workers
	ChunkSize        int32               // Size of each download chunk in bytes
	ProgressCallback func(*ProgressInfo) // Callback for download progress updates
	ProgressManager  *ProgressManager    // Progress manager (legacy support)
	ProgressInterval int                 // Progress callback interval in seconds (default: 5)
	Delay            int                 // Delay between chunks in milliseconds
	DCId             int32               // Datacenter ID where file is stored
	Buffer           io.Writer           // Custom writer for download output
	ThumbOnly        bool                // Download thumbnail only
	ThumbSize        PhotoSize           // Specific thumbnail size to download
	IsVideo          bool                // Download video version (for animated profiles)
	Ctx              context.Context     // Context for cancellation
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
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}

	location, dc, size, fileName, err := GetFileLocation(file, FileLocationOptions{
		ThumbOnly: opts.ThumbOnly,
		ThumbSize: opts.ThumbSize,
		Video:     opts.IsVideo,
	})
	if err != nil {
		return "", err
	}

	if opts.DCId != 0 {
		dc = opts.DCId
	}
	if dc == 0 {
		dc = int32(c.GetDC())
	}

	dest := sanitizePath(getValue(opts.FileName, fileName), fileName)

	const (
		windowSize = 1 << 20
		align      = 1024
	)

	threads := opts.Threads
	if threads <= 0 {
		if size < 20<<20 {
			threads = 2
		} else {
			threads = 4
		}
	}
	if threads > 8 {
		threads = 8
	}

	totalWindows := int((size + windowSize - 1) / windowSize)

	c.Log.WithFields(map[string]any{
		"file": dest, "size": SizetoHuman(size),
		"dc": dc, "threads": threads,
	}).Info("starting download")

	var outFile *os.File
	var memBuf []byte

	if opts.Buffer != nil {
		memBuf = make([]byte, size)
		dest = ":mem:"
	} else {
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
		if err != nil {
			return "", err
		}
		if err := f.Truncate(size); err != nil {
			f.Close()
			return "", err
		}
		outFile = f
		defer outFile.Close()
	}

	var done atomic.Int64

	var progressCB func(*ProgressInfo)
	if opts.ProgressCallback != nil {
		progressCB = opts.ProgressCallback
	} else if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(dest)
		opts.ProgressManager.SetTotalSize(size)
		progressCB = opts.ProgressManager.getCallback()
	}

	var tracker *progressTracker
	if progressCB != nil {
		tracker = newProgressTracker(dest, size, progressCB, opts.ProgressInterval)
		tracker.start(&done)
		defer tracker.stop()
	}

	startTime := time.Now()

	senders, err := c.createSenders(dc, threads)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithCancel(opts.Ctx)
	defer cancel()

	var nextWindow atomic.Int64
	var fatal atomic.Value
	var wg sync.WaitGroup
	wg.Add(len(senders))

	for _, sender := range senders {
		go func(s *ExSender) {
			defer wg.Done()

			for {
				w := int(nextWindow.Add(1) - 1)
				if w >= totalWindows || ctx.Err() != nil {
					return
				}

				base := int64(w * windowSize)
				remaining := size - base

				readLen := windowSize
				if remaining < windowSize {
					readLen = int(remaining)
				}
				readLen = max((readLen/align)*align, align)
				if base+int64(readLen) > int64((w+1)*windowSize) {
					readLen = windowSize - int(base%windowSize)
				}

				req := &UploadGetFileParams{
					Location: location,
					Offset:   base,
					Limit:    int32(readLen),
					Precise:  true,
				}

				rctx, cancelReq := context.WithTimeout(ctx, 60*time.Second)
				res, err := s.MakeRequestCtx(rctx, req)
				cancelReq()

				if err != nil {
					if MatchError(err, "FLOOD_WAIT_") || MatchError(err, "FLOOD_PREMIUM_WAIT_") {
						wait := GetFloodWait(err)
						time.Sleep(time.Duration(wait) * time.Second)
						nextWindow.Add(-1)
						continue
					}
					fatal.Store(err)
					cancel()
					return
				}

				data := res.(*UploadFileObj).Bytes

				if outFile != nil {
					if _, err := outFile.WriteAt(data, base); err != nil {
						fatal.Store(err)
						cancel()
						return
					}
				} else {
					copy(memBuf[base:], data)
				}

				written := int64(len(data))
				done.Add(written)
			}
		}(sender)
	}

	wg.Wait()

	if err := fatal.Load(); err != nil {
		return "", err.(error)
	}

	done.Store(size)

	c.Log.WithFields(map[string]any{
		"file": dest,
		"time": time.Since(startTime).String(),
	}).Info("download completed")

	if opts.Buffer != nil {
		_, _ = opts.Buffer.Write(memBuf)
	}

	return dest, nil
}

func (c *Client) createSenders(dc int32, n int) ([]*ExSender, error) {
	if dc == int32(c.GetDC()) {
		out := make([]*ExSender, 0, n)

		existing := c.exSenders.GetSenders(int(dc))
		for _, s := range existing {
			out = append(out, s)
			if len(out) == n {
				return out, nil
			}
		}

		for len(out) < n {
			conn, err := c.CreateExportedSender(int(dc), false)
			if err != nil {
				return nil, err
			}
			out = append(out, NewExSender(conn))
		}
		return out, nil
	}

	var auth *AuthExportedAuthorization

	c.exportedKeysMu.Lock()
	if c.exportedKeys == nil {
		c.exportedKeys = make(map[int]*AuthExportedAuthorization)
	}

	if cached, ok := c.exportedKeys[int(dc)]; ok {
		auth = cached
		c.exportedKeysMu.Unlock()
	} else {
		c.exportedKeysMu.Unlock()

		exp, err := c.AuthExportAuthorization(dc)
		if err != nil {
			return nil, err
		}

		auth = &AuthExportedAuthorization{
			ID:    exp.ID,
			Bytes: exp.Bytes,
		}

		c.exportedKeysMu.Lock()
		c.exportedKeys[int(dc)] = auth
		c.exportedKeysMu.Unlock()
	}

	out := make([]*ExSender, 0, n)
	existing := c.exSenders.GetSenders(int(dc))
	for _, s := range existing {
		out = append(out, s)
		if len(out) == n {
			return out, nil
		}
	}

	for len(out) < n {
		conn, err := c.CreateExportedSender(int(dc), false, auth)
		if err != nil {
			return nil, err
		}

		sender := NewExSender(conn)
		c.exSenders.AddSender(int(dc), sender)
		out = append(out, sender)
	}

	return out, nil
}

func initializeWorkers(numWorkers int, dc int32, c *Client, w *WorkerPool) error {
	if numWorkers == 1 && dc == int32(c.GetDC()) {
		w.AddWorker(NewExSender(c.MTProto))
		return nil
	}

	var authParams = &AuthExportedAuthorization{}
	if dc != int32(c.GetDC()) {
		c.exportedKeysMu.Lock()
		if c.exportedKeys == nil {
			c.exportedKeys = make(map[int]*AuthExportedAuthorization)
		}

		if exportedKey, ok := c.exportedKeys[int(dc)]; ok {
			authParams = exportedKey
			c.exportedKeysMu.Unlock()
		} else {
			c.exportedKeysMu.Unlock()
			auth, err := c.AuthExportAuthorization(dc)
			if err != nil {
				return err
			}

			authParams = &AuthExportedAuthorization{
				ID:    auth.ID,
				Bytes: auth.Bytes,
			}

			c.exportedKeysMu.Lock()
			c.exportedKeys[int(dc)] = authParams
			c.exportedKeysMu.Unlock()
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
