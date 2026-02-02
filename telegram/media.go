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
	Threads          int                 // Number of concurrent upload workers
	ChunkSize        int32               // Size of each upload chunk in bytes
	FileName         string              // Custom filename for the upload
	ProgressCallback func(*ProgressInfo) // Callback for upload progress updates
	ProgressManager  *ProgressManager    // Progress manager (legacy support)
	ProgressInterval int                 // Progress callback interval in seconds (default: 5)
	Delay            int                 // Delay between chunks in milliseconds
	Ctx              context.Context     // Context for cancellation
}

type Source struct {
	Source any
	closer io.Closer
}

type BoxedError struct {
	error
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

func (c *Client) UploadFile(src any, Opts ...*UploadOptions) (InputFile, error) {
	opts := getVariadic(Opts, &UploadOptions{})
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}

	source := &Source{Source: src}
	defer source.Close()

	size, fileName := source.GetSizeAndName()
	if opts.FileName != "" {
		fileName = opts.FileName
	}
	fileName = filepath.Base(fileName)
	if fileName == "." || fileName == "/" {
		fileName = "file"
	}

	readerAt, isSeekable := source.GetReaderAt()

	if isSeekable && size > 0 {
		return c.uploadParallel(readerAt, size, fileName, opts)
	}

	return c.uploadSequential(source.GetReader(), size, fileName, opts)
}

func (c *Client) uploadParallel(reader io.ReaderAt, size int64, fileName string, opts *UploadOptions) (InputFile, error) {
	partSize := int32(512 * 1024)
	if opts.ChunkSize > 0 {
		if opts.ChunkSize%1024 != 0 {
			return nil, fmt.Errorf("chunk size must be a multiple of 1KB")
		}
		partSize = opts.ChunkSize
	}

	totalParts := int(size / int64(partSize))
	if size%int64(partSize) != 0 {
		totalParts++
	}

	isBigFile := size > 10*1024*1024
	fileID := GenerateRandomLong()

	var fileMD5 string
	if !isBigFile {
		h := md5.New()
		if _, err := io.Copy(h, io.NewSectionReader(reader, 0, size)); err != nil {
			return nil, fmt.Errorf("compute md5: %w", err)
		}
		fileMD5 = hex.EncodeToString(h.Sum(nil))
	}

	numWorkers := opts.Threads
	if numWorkers <= 0 {
		numWorkers = min(totalParts, 16)
	}
	if numWorkers > 16 {
		numWorkers = 16
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	dc := int32(c.GetDC())

	senders, err := c.createSenders(dc, numWorkers)
	if err != nil {
		return nil, err
	}

	c.Log.WithFields(map[string]any{
		"file": fileName, "size": SizetoHuman(size),
		"parts": totalParts, "workers": numWorkers,
	}).Info("starting parallel upload")

	ctx, cancel := context.WithCancel(opts.Ctx)
	defer cancel()

	var doneBytes atomic.Int64
	if opts.ProgressCallback != nil {
		pt := newProgressTracker(fileName, size, opts.ProgressCallback, opts.ProgressInterval)
		pt.start(&doneBytes)
		defer pt.stop()
	} else if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(fileName)
		opts.ProgressManager.SetTotalSize(size)
		pt := newProgressTracker(fileName, size, opts.ProgressManager.getCallback(), opts.ProgressInterval)
		pt.start(&doneBytes)
		defer pt.stop()
	}

	type buffer struct{ b []byte }
	pool := sync.Pool{
		New: func() any { return &buffer{b: make([]byte, partSize)} },
	}

	queue := make(chan int, totalParts)
	for i := 0; i < totalParts; i++ {
		queue <- i
	}

	var finishedParts atomic.Int32
	var fatal atomic.Value
	var wg sync.WaitGroup

	wg.Add(len(senders))
	for _, sender := range senders {
		go func(s *ExSender) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case p := <-queue:
					if ctx.Err() != nil || fatal.Load() != nil {
						return
					}

					bufObj := pool.Get().(*buffer)
					if int32(len(bufObj.b)) != partSize {
						bufObj.b = make([]byte, partSize)
					}

					offset := int64(p) * int64(partSize)
					thisSize := int(partSize)
					if offset+int64(partSize) > size {
						thisSize = int(size - offset)
					}

					_, readErr := reader.ReadAt(bufObj.b[:thisSize], offset)
					if readErr != nil && readErr != io.EOF {
						fatal.Store(&BoxedError{fmt.Errorf("read at %d: %w", offset, readErr)})
						pool.Put(bufObj)
						cancel()
						return
					}

					reqData := bufObj.b[:thisSize]
					var upErr error

					rCtx, rCancel := context.WithTimeout(ctx, 60*time.Second)
					if isBigFile {
						_, upErr = s.MakeRequestCtx(rCtx, &UploadSaveBigFilePartParams{
							FileID:         fileID,
							FilePart:       int32(p),
							FileTotalParts: int32(totalParts),
							Bytes:          reqData,
						})
					} else {
						_, upErr = s.MakeRequestCtx(rCtx, &UploadSaveFilePartParams{
							FileID:   fileID,
							FilePart: int32(p),
							Bytes:    reqData,
						})
					}
					rCancel()

					pool.Put(bufObj)

					if upErr != nil {
						if MatchError(upErr, "FLOOD_WAIT_") || MatchError(upErr, "FLOOD_PREMIUM_WAIT_") {
							wait := GetFloodWait(upErr)
							c.Log.Debug("upload flood wait: %ds (worker)", wait)
							time.Sleep(time.Duration(wait) * time.Second)

							select {
							case queue <- p:
							case <-ctx.Done():
							}
							continue
						}

						fatal.Store(&BoxedError{fmt.Errorf("upload part %d: %w", p, upErr)})
						cancel()
						return
					}

					doneBytes.Add(int64(thisSize))
					if finishedParts.Add(1) == int32(totalParts) {
						cancel()
						return
					}
				}
			}
		}(sender)
	}

	wg.Wait()

	if err := fatal.Load(); err != nil {
		return nil, err.(*BoxedError).error
	}

	if finishedParts.Load() != int32(totalParts) {
		return nil, fmt.Errorf("upload incomplete: %d/%d parts", finishedParts.Load(), totalParts)
	}

	c.Log.WithField("file", fileName).Info("upload complete")

	if isBigFile {
		return &InputFileBig{
			ID:    fileID,
			Parts: int32(totalParts),
			Name:  prettifyFileName(fileName),
		}, nil
	}
	return &InputFileObj{
		ID:          fileID,
		Parts:       int32(totalParts),
		Name:        prettifyFileName(fileName),
		Md5Checksum: fileMD5,
	}, nil
}

func (c *Client) uploadSequential(file io.Reader, size int64, fileName string, opts *UploadOptions) (InputFile, error) {
	partSize := int32(512 * 1024)
	if opts.ChunkSize > 0 {
		partSize = int32(opts.ChunkSize)
	}
	fileID := GenerateRandomLong()
	isBigFile := size > 10*1024*1024 || size <= 0

	// MD5 for small files
	var h hash.Hash
	var tee io.Reader = file
	if !isBigFile {
		h = md5.New()
		tee = io.TeeReader(file, h)
	}

	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	var doneBytes atomic.Int64
	if opts.ProgressCallback != nil {
		pt := newProgressTracker(fileName, size, opts.ProgressCallback, opts.ProgressInterval)
		pt.start(&doneBytes)
		defer pt.stop()
	} else if opts.ProgressManager != nil {
		opts.ProgressManager.SetFileName(fileName)
		opts.ProgressManager.SetTotalSize(size)
		pt := newProgressTracker(fileName, size, opts.ProgressManager.getCallback(), opts.ProgressInterval)
		pt.start(&doneBytes)
		defer pt.stop()
	}

	buf := make([]byte, partSize)
	part := 0

	for {
		n, err := io.ReadFull(tee, buf)
		if n == 0 {
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
		}

		chunk := buf[:n]

		// Retry loop
		for attempts := 0; attempts < 10; attempts++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			var reqErr error
			if isBigFile {
				_, reqErr = c.MakeRequestCtx(ctx, &UploadSaveBigFilePartParams{
					FileID:         fileID,
					FilePart:       int32(part),
					FileTotalParts: -1,
					Bytes:          chunk,
				})
			} else {
				_, reqErr = c.MakeRequestCtx(ctx, &UploadSaveFilePartParams{
					FileID:   fileID,
					FilePart: int32(part),
					Bytes:    chunk,
				})
			}

			if reqErr == nil {
				break
			}

			if MatchError(reqErr, "FLOOD_WAIT_") {
				wait := GetFloodWait(reqErr)
				time.Sleep(time.Duration(wait) * time.Second)
				continue
			}
			time.Sleep(time.Duration(attempts) * time.Second)
		}

		doneBytes.Add(int64(n))
		part++

		if err == io.EOF || n < int(partSize) {
			break
		}
	}

	if isBigFile {
		return &InputFileBig{
			ID:    fileID,
			Parts: int32(part),
			Name:  prettifyFileName(fileName),
		}, nil
	}
	return &InputFileObj{
		ID:          fileID,
		Parts:       int32(part),
		Name:        prettifyFileName(fileName),
		Md5Checksum: hex.EncodeToString(h.Sum(nil)),
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
		windowSize = 512 * 1024 // 512KB is the maximum limit for UploadGetFile
		align      = 4096       // Offset and limit must be a multiple of 4KB
	)

	threads := opts.Threads
	if threads <= 0 {
		if size < 20<<20 {
			threads = 2
		} else if size < 100<<20 {
			threads = 8
		} else {
			threads = 8 // Increase parallelism for larger files
		}
	}
	if threads > 16 {
		threads = 16
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
	var finishedCount atomic.Int32
	var fatal atomic.Value
	var wg sync.WaitGroup
	wg.Add(len(senders))

	delay := opts.Delay
	if delay <= 0 {
		delay = 50 // Default 50ms delay as requested
	}

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
				if remaining <= 0 {
					return
				}

				limit := int32(windowSize)
				if remaining < int64(windowSize) {
					// Align the last chunk to 4KB but don't exceed remaining + align
					limit = int32((remaining + int64(align) - 1) / int64(align) * align)
				}

				req := &UploadGetFileParams{
					Location: location,
					Offset:   base,
					Limit:    limit,
					Precise:  true,
				}

				var res any
				var err error

				for i := 0; i < 5; i++ {
					if ctx.Err() != nil {
						return
					}
					rctx, cancelReq := context.WithTimeout(ctx, 15*time.Second)
					res, err = s.MakeRequestCtx(rctx, req)
					cancelReq()

					if err == nil {
						break
					}

					if MatchError(err, "FLOOD_WAIT_") || MatchError(err, "FLOOD_PREMIUM_WAIT_") {
						break
					}

					if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
						continue
					}
					break
				}

				if err != nil {
					if MatchError(err, "FLOOD_WAIT_") || MatchError(err, "FLOOD_PREMIUM_WAIT_") {
						wait := GetFloodWait(err)
						time.Sleep(time.Duration(wait) * time.Second)
						nextWindow.Add(-1)
						continue
					}
					fatal.Store(&BoxedError{err})
					cancel()
					return
				}

				data := res.(*UploadFileObj).Bytes
				written := int64(len(data))
				done.Add(written)

				if outFile != nil {
					if _, err := outFile.WriteAt(data, base); err != nil {
						fatal.Store(&BoxedError{err})
						cancel()
						return
					}
				} else {
					copy(memBuf[base:], data)
				}

				count := finishedCount.Add(1)
				if count%10 == 0 || count == int32(totalWindows) {
					speed := 0.0
					if tracker != nil {
						pt := tracker
						pt.mu.Lock()
						elapsed := time.Since(pt.startTime).Seconds()
						if elapsed > 0 {
							speed = float64(done.Load()) / elapsed
						}
						pt.mu.Unlock()
					} else {
						elapsed := time.Since(startTime).Seconds()
						if elapsed > 0 {
							speed = float64(done.Load()) / elapsed
						}
					}
					c.Log.Debug("downloading %s: %d/%d chunks (speed: %.2f MB/s)", dest, count, totalWindows, speed/1024/1024)
				}

				if delay > 0 {
					time.Sleep(time.Duration(delay) * time.Millisecond)
				}
			}
		}(sender)
	}

	wg.Wait()

	if err := fatal.Load(); err != nil {
		return "", err.(*BoxedError).error
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

	senderList, err := c.createSenders(int32(dc), 1)
	if err != nil {
		return nil, "", err
	}
	sender := senderList[0]

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
