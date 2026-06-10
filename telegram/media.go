// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"errors"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

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
			_ = next.Reconnect(false)
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

// ReaderAtSource wraps an io.ReaderAt with a known size for parallel uploads.
// Use this when you have a custom source that supports random-access reads —
// it enables multi-worker parallel uploads without buffering the entire content.
//
//	src := &telegram.ReaderAtSource{Reader: myReaderAt, Size: fileSize, Name: "data.bin"}
//	client.UploadFile(src)
type ReaderAtSource struct {
	Reader io.ReaderAt
	Size   int64
	Name   string
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
	case *bytes.Reader:
		return src.Size(), ""
	case *bytes.Buffer:
		return int64(src.Len()), ""
	case ReaderAtSource:
		return src.Size, src.Name
	case *ReaderAtSource:
		return src.Size, src.Name
	case io.ReadSeeker:
		size, err := src.Seek(0, io.SeekEnd)
		if err != nil {
			return 0, ""
		}
		_, _ = src.Seek(0, io.SeekStart)
		return size, ""
	case io.Reader, *io.Reader:
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
	case ReaderAtSource:
		return src.Name
	case *ReaderAtSource:
		return src.Name
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
	case *bytes.Reader:
		return src
	case *bytes.Buffer:
		if src == nil {
			return bytes.NewReader(nil)
		}
		return bytes.NewReader(src.Bytes())
	case ReaderAtSource:
		return &readerAtToReader{r: src.Reader, off: 0}
	case *ReaderAtSource:
		return &readerAtToReader{r: src.Reader, off: 0}
	case io.ReadSeeker:
		if closer, ok := src.(io.Closer); ok {
			s.closer = closer
		}
		return src
	case io.Reader:
		return src
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
	case *bytes.Reader:
		return src, true
	case ReaderAtSource:
		return src.Reader, true
	case *ReaderAtSource:
		return src.Reader, true
	}
	return nil, false
}

func (s *Source) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}

type readerAtToReader struct {
	r   io.ReaderAt
	off int64
}

func (r *readerAtToReader) Read(p []byte) (n int, err error) {
	n, err = r.r.ReadAt(p, r.off)
	r.off += int64(n)
	return
}

func downloadRetryDelay(attempt int) time.Duration {
	const (
		base    = 300 * time.Millisecond
		maximum = 15 * time.Second
	)
	exp := min(attempt, 6)
	ceiling := min(time.Duration(1<<uint(exp))*base, maximum)
	floor := ceiling / 2
	jitter := time.Duration(rand.Int64N(int64(ceiling-floor) + 1))
	return floor + jitter
}

type uploadPart struct {
	index int
	data  []byte
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

	if size <= 0 {
		return c.uploadSequential(file, size, fileName, opts)
	}

	readerAt, canSeek := source.GetReaderAt()
	if !canSeek {
		return c.uploadSequential(file, size, fileName, opts)
	}

	partSize := uploadChunkSizeCalc(size)
	if opts.ChunkSize > 0 {
		partSize = int(opts.ChunkSize)
	}
	if err := validateUploadChunkSize(partSize); err != nil {
		return nil, err
	}

	fileId := GenerateRandomLong()
	isBigFile := size > 10*1024*1024

	totalParts := int((size + int64(partSize) - 1) / int64(partSize))

	numWorkers := countWorkers(int64(totalParts))
	if size < 5*1024*1024 {
		numWorkers = 1
	}
	if opts.Threads > 0 {
		numWorkers = opts.Threads
	}
	if numWorkers > totalParts {
		numWorkers = totalParts
	}
	if numWorkers > 16 {
		numWorkers = 16
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	w := NewWorkerPool(numWorkers)
	defer w.Close()

	if err := initializeWorkers(numWorkers, int32(c.GetDC()), c, w); err != nil {
		return nil, err
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if !w.WaitReady(initCtx) {
		initCancel()
		return nil, errors.New("failed to initialize upload workers: timeout")
	}
	initCancel()

	uploadLog := newPartLogAggregator("upload", totalParts, 3*time.Second, c.Log)
	uploadLog.setNumWorkers(numWorkers)
	defer uploadLog.Flush()

	c.Log.WithFields(map[string]any{
		"file_name":  source.GetName(),
		"file_size":  SizetoHuman(size),
		"parts":      totalParts,
		"workers":    numWorkers,
		"chunk_size": partSize,
	}).Info("starting file upload")

	var doneBytes atomic.Int64

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

	uploadCtx, uploadCancel := context.WithCancel(rootCtx(opts.Ctx))
	defer uploadCancel()

	var md5sum hash.Hash
	if !isBigFile {
		md5sum = md5.New()
	}

	var fatalErr atomic.Value
	var fatalOnce sync.Once
	setFatal := func(err error) {
		fatalOnce.Do(func() {
			fatalErr.Store(err)
			uploadCancel()
		})
	}

	partCh := make(chan uploadPart, numWorkers*2)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for part := range partCh {
				if err := uploadOnePart(uploadCtx, c, w, uploadLog, part, fileId, totalParts, isBigFile, opts); err != nil {
					setFatal(err)
					drainParts(partCh)
					return
				}
				doneBytes.Add(int64(len(part.data)))
			}
		}()
	}

	var dispatchErr error
	for p := 0; p < totalParts; p++ {
		if uploadCtx.Err() != nil {
			break
		}
		offset := int64(p) * int64(partSize)
		chunkLen := partSize
		if remaining := size - offset; remaining < int64(chunkLen) {
			chunkLen = int(remaining)
		}
		buf := make([]byte, chunkLen)
		n, err := readerAt.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			dispatchErr = fmt.Errorf("reading part %d at offset %d: %w", p, offset, err)
			break
		}
		buf = buf[:n]
		if md5sum != nil {
			md5sum.Write(buf)
		}
		select {
		case <-uploadCtx.Done():
		case partCh <- uploadPart{index: p, data: buf}:
		}
	}
	close(partCh)
	wg.Wait()

	if err, _ := fatalErr.Load().(error); err != nil {
		return nil, err
	}
	if dispatchErr != nil {
		return nil, dispatchErr
	}
	if opts.Ctx != nil && opts.Ctx.Err() != nil {
		return nil, opts.Ctx.Err()
	}

	if progressCallback != nil {
		progressCallback(&ProgressInfo{
			FileName:   source.GetName(),
			TotalSize:  size,
			Current:    size,
			Percentage: 100,
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
		Md5Checksum: hex.EncodeToString(md5sum.Sum(nil)),
		Name:        prettifyFileName(fileName),
		Parts:       int32(totalParts),
	}, nil
}

func rootCtx(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func drainParts(ch <-chan uploadPart) {
	for range ch {
	}
}

func uploadRetryDelay(attempt int) time.Duration {
	const (
		base    = 400 * time.Millisecond
		maximum = 15 * time.Second
	)
	exp := min(attempt, 6)
	ceiling := min(time.Duration(1<<uint(exp))*base, maximum)
	floor := ceiling / 2
	jitter := time.Duration(rand.Int64N(int64(ceiling-floor) + 1))
	return floor + jitter
}

func uploadRequestTimeout(chunkLen, attempt int) time.Duration {
	base := 60 * time.Second
	switch {
	case chunkLen >= 512*1024:
		base = 120 * time.Second
	case chunkLen >= 256*1024:
		base = 90 * time.Second
	}
	timeout := base + time.Duration(min(attempt, 10))*20*time.Second
	if timeout > 6*time.Minute {
		return 6 * time.Minute
	}
	return timeout
}

func isUploadFatal(err error) bool {
	if err == nil {
		return false
	}
	fatal := []string{
		"FILE_PARTS_INVALID",
		"FILE_PART_TOO_BIG",
		"FILE_PART_INVALID",
		"FILE_PART_EMPTY",
		"FILE_PART_SIZE_INVALID",
		"FILE_PART_SIZE_CHANGED",
		"FILE_ID_INVALID",
		"AUTH_BYTES_INVALID",
	}
	msg := err.Error()
	for _, marker := range fatal {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func uploadOnePart(ctx context.Context, c *Client, w *WorkerPool, log *partLogAggregator, part uploadPart, fileId int64, totalParts int, isBigFile bool, opts *UploadOptions) error {
	const maxAttempts = 20
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		reqTimeout := uploadRequestTimeout(len(part.data), attempt)
		reqCtx, cancel := context.WithTimeout(ctx, reqTimeout)
		sender := w.NextWithContext(reqCtx)
		if sender == nil {
			cancel()
			if err := ctx.Err(); err != nil {
				return err
			}
			lastErr = errors.New("no upload worker available")
			if err := sleepContext(ctx, uploadRetryDelay(attempt)); err != nil {
				return err
			}
			continue
		}

		var err error
		if isBigFile {
			_, err = sender.MakeRequestCtx(reqCtx, &UploadSaveBigFilePartParams{
				FileID:         fileId,
				FilePart:       int32(part.index),
				FileTotalParts: int32(totalParts),
				Bytes:          part.data,
			})
		} else {
			_, err = sender.MakeRequestCtx(reqCtx, &UploadSaveFilePartParams{
				FileID:   fileId,
				FilePart: int32(part.index),
				Bytes:    part.data,
			})
		}
		cancel()
		w.FreeWorker(sender)

		if opts.Delay > 0 {
			if sleepErr := sleepContext(ctx, time.Duration(opts.Delay)*time.Millisecond); sleepErr != nil {
				return sleepErr
			}
		}

		if err == nil {
			log.recordSuccess(part.index, sender)
			return nil
		}

		lastErr = err
		log.recordFailure(part.index, err, sender)

		if isUploadFatal(err) {
			return fmt.Errorf("part %d: %w", part.index, err)
		}

		msg := err.Error()
		switch {
		case !sender.MTProto.IsTcpActive():
			_ = sender.Reconnect(false)
		case strings.Contains(msg, "deadline exceeded"),
			strings.Contains(msg, "timeout"),
			strings.Contains(msg, "connection reset"),
			strings.Contains(msg, "broken pipe"),
			strings.Contains(msg, "EOF"):
			_ = sender.Redial()
		}

		if MatchError(err, "FLOOD_WAIT_") || MatchError(err, "FLOOD_PREMIUM_WAIT_") {
			wait := time.Duration(GetFloodWait(err)) * time.Second
			if wait <= 0 {
				wait = uploadRetryDelay(attempt)
			}
			c.Log.Debug(fmt.Sprintf("flood wait part %d: sleeping %v", part.index, wait))
			if err := sleepContext(ctx, wait); err != nil {
				return err
			}
			continue
		}

		if err := sleepContext(ctx, uploadRetryDelay(attempt)); err != nil {
			return err
		}
	}
	return fmt.Errorf("part %d failed after %d attempts: %w", part.index, maxAttempts, lastErr)
}

func validateUploadChunkSize(size int) error {
	if size <= 0 || size > 512*1024 {
		return fmt.Errorf("upload chunk size must be between 1 and 524288 bytes, got %d", size)
	}
	if size%1024 != 0 {
		return fmt.Errorf("upload chunk size must be a multiple of 1024, got %d", size)
	}
	return nil
}

func uploadChunkSizeCalc(size int64) int {
	switch {
	case size < 10*1024*1024:
		return 256 * 1024
	default:
		return 512 * 1024
	}
}

func (c *Client) uploadSequential(file io.Reader, size int64, fileName string, opts *UploadOptions) (InputFile, error) {
	partSize := 1024 * 512
	if size > 0 {
		partSize = uploadChunkSizeCalc(size)
	}
	if opts.ChunkSize > 0 {
		partSize = int(opts.ChunkSize)
	}
	if err := validateUploadChunkSize(partSize); err != nil {
		return nil, err
	}

	fileId := GenerateRandomLong()
	streaming := size <= 0
	isBigFile := streaming || size > 10*1024*1024

	totalParts := 0
	if !streaming {
		totalParts = int((size + int64(partSize) - 1) / int64(partSize))
	}

	var md5sum hash.Hash
	if !isBigFile {
		md5sum = md5.New()
		file = io.TeeReader(file, md5sum)
	}

	uploadCtx, uploadCancel := context.WithCancel(rootCtx(opts.Ctx))
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

	for p := 0; ; p++ {
		if err := uploadCtx.Err(); err != nil {
			return nil, err
		}
		if !streaming && p >= totalParts {
			break
		}

		nextPart := make([]byte, partSize)
		readBytes, err := io.ReadFull(file, nextPart)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return nil, err
		}
		nextPart = nextPart[:readBytes]

		if streaming && len(nextPart) == 0 {
			totalParts = p + 1
		}

		var uploadErr error
		const maxAttempts = 20
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if err := uploadCtx.Err(); err != nil {
				return nil, err
			}
			reqTimeout := uploadRequestTimeout(len(currentPart), attempt)
			ctx, cancel := context.WithTimeout(uploadCtx, reqTimeout)
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

			if isUploadFatal(uploadErr) {
				return nil, fmt.Errorf("part %d: %w", p, uploadErr)
			}

			if MatchError(uploadErr, "FLOOD_WAIT_") || MatchError(uploadErr, "FLOOD_PREMIUM_WAIT_") {
				wait := time.Duration(GetFloodWait(uploadErr)) * time.Second
				if wait <= 0 {
					wait = uploadRetryDelay(attempt)
				}
				if err := sleepContext(uploadCtx, wait); err != nil {
					return nil, err
				}
				continue
			}

			if err := sleepContext(uploadCtx, uploadRetryDelay(attempt)); err != nil {
				return nil, err
			}
		}
		if uploadErr != nil {
			return nil, fmt.Errorf("part %d failed: %w", p, uploadErr)
		}

		doneBytes.Add(int64(len(currentPart)))
		currentPart = nextPart

		if opts.Delay > 0 {
			if err := sleepContext(uploadCtx, time.Duration(opts.Delay)*time.Millisecond); err != nil {
				return nil, err
			}
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
		Md5Checksum: hex.EncodeToString(md5sum.Sum(nil)),
		Name:        prettifyFileName(fileName),
		Parts:       int32(totalParts),
	}, nil
}

func handleIfFlood(err error, c *Client) bool {
	if !MatchError(err, "FLOOD_WAIT_") && !MatchError(err, "FLOOD_PREMIUM_WAIT_") {
		return false
	}
	waitTime := GetFloodWait(err)
	if waitTime <= 0 {
		return false
	}
	c.Log.Debug("flood wait: sleeping %ds", waitTime)
	total := time.Duration(waitTime) * time.Second
	if c.clientData.sleepThresholdMs > 0 {
		total += time.Duration(c.clientData.sleepThresholdMs) * time.Millisecond
	}
	timer := time.NewTimer(total)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.stopCh:
		return false
	}
}

func prettifyFileName(file string) string {
	return filepath.Base(file)
}

func countWorkers(parts int64) int {
	switch {
	case parts <= 4:
		return 1
	case parts <= 10:
		return 2
	case parts <= 30:
		return 4
	case parts <= 80:
		return 6
	case parts <= 200:
		return 8
	case parts <= 500:
		return 12
	default:
		return 16
	}
}

func chunkSizeCalc(size int64) int {
	switch {
	case size < 50*1024*1024:
		return 512 * 1024
	case size < 500*1024*1024:
		return 1024 * 1024
	default:
		return 1024 * 1024
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
	TakeoutID        int64               // Download file using takeout api
	// Buffer is the download destination. Supported types: io.WriterAt or io.Writer.
	Buffer    any
	ThumbOnly bool            // Download thumbnail only
	ThumbSize PhotoSize       // Specific thumbnail size to download
	IsVideo   bool            // Download video version (for animated profiles)
	Ctx       context.Context // Context for cancellation
	// Resume picks up an interrupted download from a sidecar <dest>.partstate
	// file. Only supported for known-size file downloads (Buffer == nil).
	Resume bool
}

type downloadErrKind int

const (
	downloadErrRetry downloadErrKind = iota
	downloadErrFlood
	downloadErrContext
	downloadErrFatal
)

type downloadFailure struct {
	kind downloadErrKind
	err  error
	wait time.Duration
}

type downloadRange struct {
	index  int
	offset int64
	limit  int
}

type downloadResult struct {
	part downloadRange
	data []byte
}

type downloadDestination struct {
	name     string
	file     *os.File
	writerAt io.WriterAt
	writer   io.Writer
	written  int64
	mu       sync.Mutex
}

func newDownloadDestination(name string, size int64, buffer any, resume bool) (*downloadDestination, error) {
	d := &downloadDestination{name: name}
	if buffer == nil {
		flags := os.O_CREATE | os.O_RDWR
		if !resume {
			flags |= os.O_TRUNC
		}
		file, err := os.OpenFile(name, flags, 0666)
		if err != nil {
			return nil, err
		}
		if size > 0 {
			info, err := file.Stat()
			if err != nil {
				file.Close()
				return nil, err
			}
			if info.Size() != size {
				if err := file.Truncate(size); err != nil {
					file.Close()
					return nil, err
				}
			}
		}
		d.file = file
		return d, nil
	}

	switch dst := buffer.(type) {
	case io.WriterAt:
		d.writerAt = dst
	case io.Writer:
		d.writer = dst
	default:
		return nil, fmt.Errorf("unsupported Buffer type %T: must implement io.WriterAt or io.Writer", buffer)
	}
	return d, nil
}

func (d *downloadDestination) canWriteAt() bool {
	return d.file != nil || d.writerAt != nil
}

func (d *downloadDestination) displayName() string {
	if d.file != nil {
		return d.name
	}
	if d.writerAt != nil {
		return ":direct-writer:"
	}
	return ":stream-writer:"
}

func (d *downloadDestination) WriteAt(p []byte, off int64) (int, error) {
	if d.file != nil {
		return d.file.WriteAt(p, off)
	}
	if d.writerAt != nil {
		return d.writerAt.WriteAt(p, off)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if off != d.written {
		return 0, fmt.Errorf("sequential writer received offset %d after %d bytes", off, d.written)
	}
	n, err := d.writer.Write(p)
	d.written += int64(n)
	return n, err
}

func (d *downloadDestination) Close() error {
	if d.file != nil {
		return d.file.Close()
	}
	return nil
}

type downloadJob struct {
	client           *Client
	opts             *DownloadOptions
	location         InputFileLocation
	dc               int32
	size             int64
	knownSize        bool
	partSize         int
	workers          int
	destination      *downloadDestination
	ctx              context.Context
	doneBytes        atomic.Int64
	progressCallback func(*ProgressInfo)
	progressTracker  *progressTracker
	log              *partLogAggregator

	cdnMu    sync.Mutex
	cdn      *cdnRedirect
	cdnPools map[int32]*WorkerPool

	resume       *resumeState
	resumeStopCh chan struct{}
}

type cdnRedirect struct {
	dcID          int32
	fileToken     []byte
	encryptionKey []byte
	encryptionIv  []byte
}

func (c *Client) DownloadMedia(file any, Opts ...*DownloadOptions) (string, error) {
	opts := getVariadic(Opts, &DownloadOptions{})
	job, err := c.newDownloadJob(file, opts)
	if err != nil {
		return "", err
	}
	defer job.destination.Close()
	defer job.closeCDNPools()
	if job.progressTracker != nil {
		defer job.progressTracker.stop()
	}
	if job.log != nil {
		defer job.log.Flush()
	}
	if job.resume != nil {
		job.resumeStopCh = make(chan struct{})
		job.resume.startFlusher(job.resumeStopCh, 2*time.Second, c.Log)
		defer close(job.resumeStopCh)
	}

	if err := job.run(); err != nil {
		return "", err
	}
	if job.resume != nil {
		if err := job.resume.flush(); err != nil {
			c.Log.Debug("resume state final flush failed: %v", err)
		}
		job.resume.remove()
	}

	current := job.doneBytes.Load()
	total := job.size
	if total <= 0 {
		total = current
	}
	if job.progressCallback != nil {
		job.progressCallback(&ProgressInfo{
			FileName:   job.destination.displayName(),
			TotalSize:  total,
			Current:    current,
			Percentage: 100,
		})
	}

	job.client.Log.WithFields(map[string]any{
		"file_name": job.destination.displayName(),
		"file_size": SizetoHuman(total),
	}).Info("file download completed")

	return job.destination.name, nil
}

func (c *Client) newDownloadJob(file any, opts *DownloadOptions) (*downloadJob, error) {
	location, dc, size, fileName, err := GetFileLocation(file, FileLocationOptions{
		ThumbOnly: opts.ThumbOnly,
		ThumbSize: opts.ThumbSize,
		Video:     opts.IsVideo,
	})
	if err != nil {
		return nil, err
	}

	dc = getValue(dc, opts.DCId)
	if dc == 0 {
		dc = int32(c.GetDC())
	}

	dest := sanitizePath(getValue(opts.FileName, fileName), fileName)
	partSize := chunkSizeCalc(size)
	if size <= 0 {
		partSize = 512 * 1024
	}
	if opts.ChunkSize > 0 {
		if err := validateDownloadChunkSize(int(opts.ChunkSize)); err != nil {
			return nil, err
		}
		partSize = int(opts.ChunkSize)
	}

	resumeRequested := opts.Resume && size > 0 && opts.Buffer == nil
	var resume *resumeState
	if resumeRequested {
		locKey := locationKey(location)
		statePath := resumeStatePath(dest)
		if loaded, lerr := loadResumeState(statePath, size, partSize, locKey); lerr == nil {
			resume = loaded
		} else {
			if !os.IsNotExist(lerr) {
				c.Log.Debug("resume state ignored (%s): %v", statePath, lerr)
			}
			resume = newResumeState(statePath, size, partSize, locKey)
		}
	}

	destination, err := newDownloadDestination(dest, size, opts.Buffer, resume != nil)
	if err != nil {
		return nil, err
	}

	knownSize := size > 0
	parts := int64(0)
	if knownSize {
		parts = (size + int64(partSize) - 1) / int64(partSize)
	}
	workers := 1
	if knownSize && destination.canWriteAt() {
		workers = countWorkers(parts)
		if size < 5*1024*1024 {
			workers = 1
		}
		if opts.Threads > 0 {
			workers = opts.Threads
		}
		if workers > int(parts) {
			workers = int(parts)
		}
		if workers > 16 {
			workers = 16
		}
		if workers < 1 {
			workers = 1
		}
	}

	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	job := &downloadJob{
		client:      c,
		opts:        opts,
		location:    location,
		dc:          dc,
		size:        size,
		knownSize:   knownSize,
		partSize:    partSize,
		workers:     workers,
		destination: destination,
		ctx:         ctx,
		resume:      resume,
	}
	if resume != nil {
		job.doneBytes.Store(resume.completedBytes())
	}

	if opts.ProgressCallback != nil {
		job.progressCallback = opts.ProgressCallback
	} else if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(destination.displayName())
		opts.ProgressManager.SetTotalSize(size)
		job.progressCallback = opts.ProgressManager.getCallback()
	}
	if job.progressCallback != nil {
		job.progressTracker = newProgressTracker(destination.displayName(), size, job.progressCallback, opts.ProgressInterval)
		job.progressCallback(&ProgressInfo{FileName: destination.displayName(), TotalSize: size, Current: 0, Percentage: 0})
		job.progressTracker.start(&job.doneBytes)
	}

	totalParts := int(parts)
	if !knownSize {
		totalParts = 0
	}
	job.log = newPartLogAggregator("download", totalParts, 3*time.Second, c.Log)
	job.log.setNumWorkers(workers)

	c.Log.WithFields(map[string]any{
		"file_name":  destination.displayName(),
		"file_size":  SizetoHuman(size),
		"parts":      totalParts,
		"workers":    workers,
		"dc":         dc,
		"chunk_size": partSize,
	}).Info("starting file download")

	return job, nil
}

func validateDownloadChunkSize(size int) error {
	if size <= 0 || size > 1048576 || 1048576%size != 0 {
		return errors.New("chunk size must be a divisor of 1048576 (1MB)")
	}
	return nil
}

func (j *downloadJob) run() error {
	if j.knownSize && j.destination.canWriteAt() {
		return j.runKnownSize()
	}
	return j.runUnknownSize()
}

func (j *downloadJob) runKnownSize() error {
	totalParts := int((j.size + int64(j.partSize) - 1) / int64(j.partSize))
	pool := NewWorkerPool(j.workers)
	defer pool.Close()
	if err := initializeWorkers(j.workers, j.dc, j.client, pool, j.ctx); err != nil {
		return err
	}
	if !pool.WaitReady(j.ctx) {
		return contextErr(j.ctx, errors.New("failed to initialize download workers"))
	}

	work := make(chan downloadRange, j.workers*2)
	var firstErr error
	var firstErrMu sync.Mutex
	var cancelOnce sync.Once
	ctx, cancel := context.WithCancel(j.ctx)
	defer cancel()
	setErr := func(err error) {
		firstErrMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		firstErrMu.Unlock()
		cancelOnce.Do(cancel)
	}
	getErr := func() error {
		firstErrMu.Lock()
		defer firstErrMu.Unlock()
		return firstErr
	}

	var workers sync.WaitGroup
	for range j.workers {
		workers.Go(func() {
			for part := range work {
				if ctx.Err() != nil {
					return
				}
				result, err := j.fetchPartLoop(ctx, pool, part)
				if err != nil {
					setErr(err)
					return
				}
				data := result.data
				if remaining := j.size - result.part.offset; remaining > 0 && int64(len(data)) > remaining {
					data = data[:remaining]
				}
				if _, err := j.destination.WriteAt(data, result.part.offset); err != nil {
					tl.ReleaseLargeBuffer(result.data)
					setErr(err)
					return
				}
				n := len(data)
				tl.ReleaseLargeBuffer(result.data)
				j.doneBytes.Add(int64(n))
				if j.resume != nil {
					j.resume.mark(result.part.index)
				}
				j.log.recordSuccess(result.part.index, nil)
			}
		})
	}

	for p := range totalParts {
		if j.resume != nil && j.resume.has(p) {
			j.log.recordSuccess(p, nil)
			continue
		}
		offset := int64(p) * int64(j.partSize)
		select {
		case <-ctx.Done():
			close(work)
			workers.Wait()
			if err := getErr(); err != nil {
				return err
			}
			return ctx.Err()
		case work <- downloadRange{index: p, offset: offset, limit: j.partSize}:
		}
	}
	close(work)
	workers.Wait()

	if err := getErr(); err != nil {
		return err
	}
	if err := j.ctx.Err(); err != nil {
		return err
	}
	if got := j.doneBytes.Load(); got != j.size {
		return fmt.Errorf("download incomplete: got %d of %d bytes", got, j.size)
	}
	return nil
}

func (j *downloadJob) runUnknownSize() error {
	pool := NewWorkerPool(1)
	defer pool.Close()
	if err := initializeWorkers(1, j.dc, j.client, pool, j.ctx); err != nil {
		return err
	}
	if !pool.WaitReady(j.ctx) {
		return contextErr(j.ctx, errors.New("failed to initialize download worker"))
	}

	for index, offset := 0, int64(0); ; index++ {
		if j.knownSize && offset >= j.size {
			return nil
		}
		part := downloadRange{index: index, offset: offset, limit: j.partSize}
		result, err := j.fetchPartLoop(j.ctx, pool, part)
		if err != nil {
			return err
		}
		data := result.data
		if j.knownSize {
			if remaining := j.size - offset; remaining > 0 && int64(len(data)) > remaining {
				data = data[:remaining]
			}
		}
		if len(data) == 0 {
			tl.ReleaseLargeBuffer(result.data)
			return nil
		}
		if _, err := j.destination.WriteAt(data, offset); err != nil {
			tl.ReleaseLargeBuffer(result.data)
			return err
		}
		n := len(data)
		tl.ReleaseLargeBuffer(result.data)
		j.doneBytes.Add(int64(n))
		j.log.recordSuccess(index, nil)
		offset += int64(n)
		if n < part.limit {
			return nil
		}
		if j.opts.Delay > 0 {
			if err := sleepContext(j.ctx, time.Duration(j.opts.Delay)*time.Millisecond); err != nil {
				return err
			}
		}
	}
}

func (j *downloadJob) fetchPartLoop(ctx context.Context, pool *WorkerPool, part downloadRange) (downloadResult, error) {
	const maxAttempts = 20
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := j.fetchPart(ctx, pool, part, attempt)
		if err == nil {
			return result, nil
		}
		lastErr = err
		failure := j.classifyError(ctx, err)
		if failure.kind == downloadErrFatal || failure.kind == downloadErrContext {
			return downloadResult{}, failure.err
		}
		if err := j.sleepRetry(ctx, failure, attempt); err != nil {
			return downloadResult{}, err
		}
	}
	return downloadResult{}, fmt.Errorf("part %d failed after %d attempts: %w", part.index, maxAttempts, lastErr)
}

func (j *downloadJob) fetchPart(ctx context.Context, pool *WorkerPool, part downloadRange, attempt int) (downloadResult, error) {
	j.cdnMu.Lock()
	cdn := j.cdn
	j.cdnMu.Unlock()
	if cdn != nil {
		return j.fetchPartCDN(ctx, cdn, part, attempt)
	}

	reqCtx, cancel := context.WithTimeout(ctx, j.requestTimeout(part.limit, attempt))
	defer cancel()

	sender := pool.NextWithContext(reqCtx)
	if sender == nil {
		return downloadResult{}, contextErr(reqCtx, errors.New("failed to acquire download worker"))
	}
	defer pool.FreeWorker(sender)

	request := j.makeGetFileRequest(part)
	response, err := sender.MakeRequestCtx(reqCtx, request)
	if j.opts.Delay > 0 {
		if sleepErr := sleepContext(ctx, time.Duration(j.opts.Delay)*time.Millisecond); sleepErr != nil && err == nil {
			err = sleepErr
		}
	}
	if err != nil {
		msg := err.Error()
		switch {
		case !sender.MTProto.IsTcpActive():
			_ = sender.Reconnect(false)
		case strings.Contains(msg, "deadline exceeded"),
			strings.Contains(msg, "timeout"),
			strings.Contains(msg, "connection reset"),
			strings.Contains(msg, "broken pipe"),
			strings.Contains(msg, "EOF"):
			_ = sender.Redial()
		}
		j.log.recordFailure(part.index, err, sender)
		return downloadResult{}, err
	}

	switch v := response.(type) {
	case *UploadFileObj:
		return downloadResult{part: part, data: v.Bytes}, nil
	case *UploadFileCdnRedirect:
		if err := j.activateCDN(v); err != nil {
			return downloadResult{}, err
		}
		j.cdnMu.Lock()
		active := j.cdn
		j.cdnMu.Unlock()
		return j.fetchPartCDN(ctx, active, part, attempt)
	case nil:
		return downloadResult{}, errors.New("empty download response")
	default:
		return downloadResult{}, fmt.Errorf("unexpected download response %T", response)
	}
}

func (j *downloadJob) activateCDN(r *UploadFileCdnRedirect) error {
	j.cdnMu.Lock()
	defer j.cdnMu.Unlock()
	if j.cdn != nil && j.cdn.dcID == r.DcID {
		return nil
	}
	j.cdn = &cdnRedirect{
		dcID:          r.DcID,
		fileToken:     r.FileToken,
		encryptionKey: r.EncryptionKey,
		encryptionIv:  r.EncryptionIv,
	}
	j.client.Log.Info(fmt.Sprintf("cdn redirect: switching to CDN DC%d", r.DcID))
	return nil
}

func (j *downloadJob) cdnPool(_ context.Context, dc int32) (*WorkerPool, error) {
	j.cdnMu.Lock()
	if j.cdnPools == nil {
		j.cdnPools = make(map[int32]*WorkerPool)
	}
	if pool, ok := j.cdnPools[dc]; ok {
		j.cdnMu.Unlock()
		return pool, nil
	}
	j.cdnMu.Unlock()

	conn, err := j.client.CreateExportedSender(int(dc), true)
	if err != nil {
		return nil, fmt.Errorf("creating cdn sender: %w", err)
	}
	pool := NewWorkerPool(1)
	pool.AddWorker(NewExSender(conn))

	j.cdnMu.Lock()
	if existing, ok := j.cdnPools[dc]; ok {
		j.cdnMu.Unlock()
		return existing, nil
	}
	j.cdnPools[dc] = pool
	j.cdnMu.Unlock()
	return pool, nil
}

func (j *downloadJob) fetchPartCDN(ctx context.Context, cdn *cdnRedirect, part downloadRange, attempt int) (downloadResult, error) {
	pool, err := j.cdnPool(ctx, cdn.dcID)
	if err != nil {
		return downloadResult{}, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, j.requestTimeout(part.limit, attempt))
	defer cancel()

	sender := pool.NextWithContext(reqCtx)
	if sender == nil {
		return downloadResult{}, contextErr(reqCtx, errors.New("failed to acquire cdn worker"))
	}
	defer pool.FreeWorker(sender)

	response, err := sender.MakeRequestCtx(reqCtx, &UploadGetCdnFileParams{
		FileToken: cdn.fileToken,
		Offset:    part.offset,
		Limit:     int32(part.limit),
	})
	if err != nil {
		msg := err.Error()
		switch {
		case !sender.MTProto.IsTcpActive():
			_ = sender.Reconnect(false)
		case strings.Contains(msg, "deadline exceeded"),
			strings.Contains(msg, "timeout"),
			strings.Contains(msg, "connection reset"),
			strings.Contains(msg, "broken pipe"),
			strings.Contains(msg, "EOF"):
			_ = sender.Redial()
		}
		j.log.recordFailure(part.index, err, sender)
		return downloadResult{}, err
	}

	switch v := response.(type) {
	case *UploadCdnFileObj:
		decryptCDNBlock(v.Bytes, cdn.encryptionKey, cdn.encryptionIv, part.offset)
		return downloadResult{part: part, data: v.Bytes}, nil
	case *UploadCdnFileReuploadNeeded:
		if err := j.reuploadCDN(reqCtx, cdn, v.RequestToken); err != nil {
			return downloadResult{}, err
		}
		return downloadResult{}, errors.New("cdn reupload requested")
	case nil:
		return downloadResult{}, errors.New("empty cdn response")
	default:
		return downloadResult{}, fmt.Errorf("unexpected cdn response %T", response)
	}
}

func (j *downloadJob) reuploadCDN(ctx context.Context, cdn *cdnRedirect, requestToken []byte) error {
	_, err := j.client.MakeRequestCtx(ctx, &UploadReuploadCdnFileParams{
		FileToken:    cdn.fileToken,
		RequestToken: requestToken,
	})
	return err
}

func (j *downloadJob) closeCDNPools() {
	j.cdnMu.Lock()
	pools := j.cdnPools
	j.cdnPools = nil
	j.cdnMu.Unlock()
	for _, p := range pools {
		p.Close()
	}
}

func decryptCDNBlock(data, key, iv []byte, offset int64) {
	if len(data) == 0 || len(iv) != aes.BlockSize {
		return
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return
	}
	ivCopy := make([]byte, aes.BlockSize)
	copy(ivCopy, iv)
	counter := binary.BigEndian.Uint32(ivCopy[12:16]) + uint32(offset/aes.BlockSize)
	binary.BigEndian.PutUint32(ivCopy[12:16], counter)
	stream := cipher.NewCTR(block, ivCopy)
	stream.XORKeyStream(data, data)
}

func (j *downloadJob) makeGetFileRequest(part downloadRange) tl.Object {
	request := tl.Object(&UploadGetFileParams{
		Location:     j.location,
		Offset:       part.offset,
		Limit:        int32(part.limit),
		Precise:      false,
		CdnSupported: true,
	})
	if j.opts.TakeoutID != 0 {
		request = &InvokeWithTakeoutParams{
			TakeoutID: j.opts.TakeoutID,
			Query:     request,
		}
	}
	return request
}

func (j *downloadJob) classifyError(ctx context.Context, err error) downloadFailure {
	if err == nil {
		return downloadFailure{}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return downloadFailure{kind: downloadErrContext, err: ctxErr}
	}
	msg := err.Error()
	if MatchError(err, "FLOOD_WAIT_") || MatchError(err, "FLOOD_PREMIUM_WAIT_") {
		wait := time.Duration(GetFloodWait(err)) * time.Second
		if wait <= 0 {
			wait = downloadRetryDelay(1)
		}
		return downloadFailure{kind: downloadErrFlood, err: err, wait: wait}
	}
	fatal := []string{
		"FILE_REFERENCE_EXPIRED",
		"FILE_REFERENCE_INVALID",
		"TAKEOUT_INVALID",
		"LOCATION_INVALID",
		"OFFSET_INVALID",
		"FILE_ID_INVALID",
		"AUTH_BYTES_INVALID",
		"CDN_METHOD_INVALID",
	}
	for _, marker := range fatal {
		if strings.Contains(msg, marker) {
			return downloadFailure{kind: downloadErrFatal, err: err}
		}
	}
	return downloadFailure{kind: downloadErrRetry, err: err}
}

func (j *downloadJob) sleepRetry(ctx context.Context, failure downloadFailure, attempt int) error {
	wait := failure.wait
	if wait <= 0 {
		wait = downloadRetryDelay(attempt)
	}
	return sleepContext(ctx, wait)
}

func (j *downloadJob) requestTimeout(limit int, attempt int) time.Duration {
	base := 45 * time.Second
	switch {
	case limit >= 1024*1024:
		base = 120 * time.Second
	case limit >= 512*1024:
		base = 75 * time.Second
	case limit >= 128*1024:
		base = 60 * time.Second
	}
	timeout := base + time.Duration(min(attempt, 10))*20*time.Second
	if timeout > 6*time.Minute {
		return 6 * time.Minute
	}
	return timeout
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func contextErr(ctx context.Context, fallback error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fallback
}

func initializeWorkers(numWorkers int, dc int32, c *Client, w *WorkerPool, ctx ...context.Context) error {
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
		bgCtx := context.Background()
		if len(ctx) > 0 && ctx[0] != nil {
			bgCtx = ctx[0]
		}
		c.Log.Info(fmt.Sprintf("exporting senders: dc(%d) - workers(%d)", dc, numWorkers-numCreate))
		go func() {
			for i := numCreate; i < numWorkers; i++ {
				if bgCtx.Err() != nil {
					return
				}
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
	if err := validateDownloadChunkSize(chunkSize); err != nil {
		return nil, "", err
	}
	if start < 0 {
		return nil, "", errors.New("start offset must be non-negative")
	}
	if end <= start {
		_, _, _, name, err := GetFileLocation(media)
		return []byte{}, name, err
	}

	input, dc, size, name, err := GetFileLocation(media)
	if err != nil {
		return nil, "", err
	}
	if dc == 0 {
		dc = int32(c.GetDC())
	}
	if size > 0 && end > int(size) {
		end = int(size)
	}

	job := &downloadJob{
		client:    c,
		opts:      &DownloadOptions{},
		location:  input,
		dc:        dc,
		size:      size,
		knownSize: size > 0,
		partSize:  chunkSize,
		workers:   1,
		ctx:       context.Background(),
		log:       newPartLogAggregator("download_chunk", 0, 3*time.Second, c.Log),
	}
	defer job.log.Flush()

	pool := NewWorkerPool(1)
	defer pool.Close()
	if err := initializeWorkers(1, dc, c, pool); err != nil {
		return nil, "", err
	}
	if !pool.WaitReady(job.ctx) {
		return nil, "", errors.New("failed to initialize worker")
	}

	var buf []byte
	for index, offset := 0, int64(start); offset < int64(end); index++ {
		limit := chunkSize
		if remaining := int64(end) - offset; remaining < int64(limit) {
			limit = int(remaining)
		}
		result, err := job.fetchPartLoop(job.ctx, pool, downloadRange{index: index, offset: offset, limit: limit})
		if err != nil {
			return nil, "", err
		}
		if len(result.data) == 0 {
			tl.ReleaseLargeBuffer(result.data)
			break
		}
		buf = append(buf, result.data...)
		n := len(result.data)
		tl.ReleaseLargeBuffer(result.data)
		offset += int64(n)
		if n < limit {
			break
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

	totalProgress := 0.0
	if a.total > 0 {
		totalProgress = float64(a.successes) / float64(a.total) * 100
	}

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

				if current > 0 && (pt.totalSize <= 0 || current <= pt.totalSize) {
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

const (
	resumeMagic   uint32 = 0x444C5231
	resumeVersion uint16 = 1
)

type resumeState struct {
	mu         sync.Mutex
	path       string
	size       int64
	partSize   int
	totalParts int
	locKey     []byte
	bitmap     []byte
	dirty      bool
}

func resumeStatePath(dest string) string {
	return dest + ".partstate"
}

func locationKey(loc InputFileLocation) []byte {
	if loc == nil {
		return nil
	}
	buf := make([]byte, 4+8+8)
	binary.LittleEndian.PutUint32(buf[0:4], loc.CRC())
	switch v := loc.(type) {
	case *InputDocumentFileLocation:
		binary.LittleEndian.PutUint64(buf[4:12], uint64(v.ID))
		binary.LittleEndian.PutUint64(buf[12:20], uint64(v.AccessHash))
	case *InputPhotoFileLocation:
		binary.LittleEndian.PutUint64(buf[4:12], uint64(v.ID))
		binary.LittleEndian.PutUint64(buf[12:20], uint64(v.AccessHash))
	case *InputEncryptedFileLocation:
		binary.LittleEndian.PutUint64(buf[4:12], uint64(v.ID))
		binary.LittleEndian.PutUint64(buf[12:20], uint64(v.AccessHash))
	case *InputFileLocationObj:
		binary.LittleEndian.PutUint64(buf[4:12], uint64(v.VolumeID))
		binary.LittleEndian.PutUint64(buf[12:20], uint64(v.Secret))
	default:
		return buf[:4]
	}
	return buf
}

func newResumeState(path string, size int64, partSize int, locKey []byte) *resumeState {
	totalParts := int((size + int64(partSize) - 1) / int64(partSize))
	return &resumeState{
		path:       path,
		size:       size,
		partSize:   partSize,
		totalParts: totalParts,
		locKey:     locKey,
		bitmap:     make([]byte, (totalParts+7)/8),
	}
}

func loadResumeState(path string, size int64, partSize int, locKey []byte) (*resumeState, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	header := make([]byte, 32)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	if binary.LittleEndian.Uint32(header[0:4]) != resumeMagic {
		return nil, errors.New("not a resume state file")
	}
	if binary.LittleEndian.Uint16(header[4:6]) != resumeVersion {
		return nil, errors.New("unsupported resume state version")
	}
	storedSize := int64(binary.LittleEndian.Uint64(header[8:16]))
	storedPartSize := int(binary.LittleEndian.Uint32(header[16:20]))
	storedTotalParts := int(binary.LittleEndian.Uint32(header[20:24]))
	locKeyLen := int(binary.LittleEndian.Uint32(header[24:28]))

	if storedSize != size || storedPartSize != partSize {
		return nil, errors.New("size or partSize changed")
	}
	expectedTotal := int((size + int64(partSize) - 1) / int64(partSize))
	if storedTotalParts != expectedTotal {
		return nil, errors.New("totalParts mismatch")
	}

	storedLocKey := make([]byte, locKeyLen)
	if locKeyLen > 0 {
		if _, err := io.ReadFull(f, storedLocKey); err != nil {
			return nil, fmt.Errorf("read locKey: %w", err)
		}
	}
	if len(locKey) > 0 && len(storedLocKey) > 0 {
		if !bytes.Equal(locKey, storedLocKey) {
			return nil, errors.New("location key mismatch")
		}
	}

	bitmapLen := (storedTotalParts + 7) / 8
	bitmap := make([]byte, bitmapLen)
	if _, err := io.ReadFull(f, bitmap); err != nil {
		return nil, fmt.Errorf("read bitmap: %w", err)
	}

	return &resumeState{
		path:       path,
		size:       size,
		partSize:   partSize,
		totalParts: storedTotalParts,
		locKey:     locKey,
		bitmap:     bitmap,
	}, nil
}

func (s *resumeState) has(part int) bool {
	if part < 0 || part >= s.totalParts {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bitmap[part/8]&(1<<(part%8)) != 0
}

func (s *resumeState) mark(part int) {
	if part < 0 || part >= s.totalParts {
		return
	}
	s.mu.Lock()
	mask := byte(1 << (part % 8))
	if s.bitmap[part/8]&mask == 0 {
		s.bitmap[part/8] |= mask
		s.dirty = true
	}
	s.mu.Unlock()
}

func (s *resumeState) completedBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for p := 0; p < s.totalParts; p++ {
		if s.bitmap[p/8]&(1<<(p%8)) != 0 {
			partLen := int64(s.partSize)
			offset := int64(p) * int64(s.partSize)
			if remaining := s.size - offset; remaining < partLen {
				partLen = remaining
			}
			total += partLen
		}
	}
	return total
}

func (s *resumeState) flush() error {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	bitmap := make([]byte, len(s.bitmap))
	copy(bitmap, s.bitmap)
	s.dirty = false
	s.mu.Unlock()

	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	header := make([]byte, 32)
	binary.LittleEndian.PutUint32(header[0:4], resumeMagic)
	binary.LittleEndian.PutUint16(header[4:6], resumeVersion)
	binary.LittleEndian.PutUint64(header[8:16], uint64(s.size))
	binary.LittleEndian.PutUint32(header[16:20], uint32(s.partSize))
	binary.LittleEndian.PutUint32(header[20:24], uint32(s.totalParts))
	binary.LittleEndian.PutUint32(header[24:28], uint32(len(s.locKey)))
	if _, err := f.Write(header); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if len(s.locKey) > 0 {
		if _, err := f.Write(s.locKey); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if _, err := f.Write(bitmap); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *resumeState) remove() {
	_ = os.Remove(s.path)
}

func (s *resumeState) startFlusher(stop <-chan struct{}, interval time.Duration, log Logger) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				if err := s.flush(); err != nil && log != nil {
					log.Debug("resume state final flush failed: %v", err)
				}
				return
			case <-ticker.C:
				if err := s.flush(); err != nil && log != nil {
					log.Debug("resume state flush failed: %v", err)
				}
			}
		}
	}()
}
