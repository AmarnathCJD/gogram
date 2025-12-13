// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

func (c *Client) StartGroupCallMedia(peer any, rtmp ...bool) (PhoneCall, error) {
	peerDialog, err := c.ResolvePeer(peer)
	if err != nil {
		return nil, err
	}

	var useRtmp bool
	if len(rtmp) > 0 {
		useRtmp = rtmp[0]
	}
	// Start the group call
	updates, e := c.PhoneCreateGroupCall(&PhoneCreateGroupCallParams{
		Peer:       peerDialog,
		RandomID:   int32(GenRandInt()),
		RtmpStream: useRtmp,
	})
	if e != nil {
		return nil, e
	}

	switch updates.(type) {
	case *UpdatesObj:
		for _, update := range updates.(*UpdatesObj).Updates {
			switch update := update.(type) {
			case *UpdatePhoneCall:
				return update.PhoneCall, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to start group call media")
}

func (c *Client) GetGroupCall(chatId any) (*InputGroupCall, error) {
	resolvedPeer, err := c.ResolvePeer(chatId)
	if err != nil {
		return nil, err
	}

	if inPeer, ok := resolvedPeer.(*InputPeerChannel); !ok {
		return nil, fmt.Errorf("resolved peer is not a channel")
	} else {
		fullchannel, err := c.ChannelsGetFullChannel(
			&InputChannelObj{
				ChannelID:  inPeer.ChannelID,
				AccessHash: inPeer.AccessHash,
			},
		)

		if err != nil {
			return nil, err
		}

		switch channel := fullchannel.FullChat.(type) {
		case *ChannelFull:
			if channel.Call == nil {
				return nil, fmt.Errorf("no active group call in the channel")
			}
			return &channel.Call, nil
		case *ChatFullObj:
			if channel.Call == nil {
				return nil, fmt.Errorf("no active group call in the chat")
			}
			return &channel.Call, nil
		default:
			return nil, fmt.Errorf("failed to get full channel info")
		}
	}
}

type GroupCallStream struct {
	Channels        []*GroupCallStreamChannel
	call            *InputGroupCall
	currentTs       int64
	selectedChannel int32
	scale           int32
	client          *Client
	dcId            int32
}

func (s *GroupCallStream) SetTS(ts int64) {
	s.currentTs = ts
}

func (s *GroupCallStream) GetTS() int64 {
	return s.currentTs
}

func (s *GroupCallStream) GetScale() int32 {
	return s.scale
}

func (s *GroupCallStream) SetScale(scale int32) {
	s.scale = scale
}

func (s *GroupCallStream) GetChannel() *GroupCallStreamChannel {
	if s.selectedChannel < 0 || s.selectedChannel >= int32(len(s.Channels)) {
		return nil
	}
	return s.Channels[s.selectedChannel]
}

func (s *GroupCallStream) SetChannel(channel int32) {
	if channel < 0 || channel >= int32(len(s.Channels)) {
		return
	}
	s.selectedChannel = channel
	s.scale = s.Channels[channel].Scale
	s.currentTs = s.Channels[channel].LastTimestampMs
}

func (s *GroupCallStream) NextChunk() ([]byte, error) {
	if s.selectedChannel < 0 || s.selectedChannel >= int32(len(s.Channels)) {
		return nil, fmt.Errorf("selected channel index out of range")
	}

	channel := s.GetChannel()
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}

	input := &InputGroupCallStream{
		Call:         *s.call,
		TimeMs:       s.currentTs,
		Scale:        s.scale,
		VideoChannel: channel.Channel,
		VideoQuality: 2,
	}

	fi, err := s.client.UploadGetFile(&UploadGetFileParams{
		Location: input,
		Offset:   0,
		Limit:    512 * 1024,
	})

	if err != nil {
		return nil, err
	}

	if fi == nil {
		return nil, fmt.Errorf("file info is nil")
	}

	switch fi := fi.(type) {
	case *UploadFileObj:
		s.currentTs += 1000 >> s.scale
		return fi.Bytes, nil
	case *UploadFileCdnRedirect:
		return nil, fmt.Errorf("CDN redirects not supported yet")
	}

	return nil, fmt.Errorf("unknown file info type")
}

func (c *Client) GetGroupCallStream(chatId any) (*GroupCallStream, error) {
	call, err := c.GetGroupCall(chatId)
	if err != nil {
		return nil, err
	}

	stream, err := c.PhoneGetGroupCallStreamChannels(*call)
	if err != nil {
		return nil, err
	}

	return &GroupCallStream{
		Channels: stream.Channels,
		scale:    stream.Channels[0].Scale,
		call:     call,
		client:   c,
		dcId:     int32(c.GetDC()),
	}, nil
}

type StreamState int

const (
	StreamStateIdle StreamState = iota
	StreamStatePlaying
	StreamStatePaused
	StreamStateStopped
)

func (s StreamState) String() string {
	switch s {
	case StreamStateIdle:
		return "idle"
	case StreamStatePlaying:
		return "playing"
	case StreamStatePaused:
		return "paused"
	case StreamStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

var (
	ErrFFmpegNotFound  = errors.New("ffmpeg not found in PATH")
	ErrStreamPlaying   = errors.New("stream already playing")
	ErrStreamNotPaused = errors.New("stream not paused")
	ErrNoRTMPURL       = errors.New("RTMP URL not set, call FetchRTMPURL() first")
	ErrNoInputSource   = errors.New("no input source available")
	ErrFileNotFound    = errors.New("input file not found")
)

type RTMPStream struct {
	chatID     int64
	rtmpURL    string
	rtmpKey    string
	client     *Client
	state      StreamState
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stderr     bytes.Buffer
	cancelFunc context.CancelFunc
	mu         sync.Mutex
	inputFile  string
	inputData  []byte
	loopCount  int
	bitrate    string
	audioBit   string
	frameRate  int
	startTime  time.Time
	pausedAt   time.Duration
	seekPos    time.Duration
	lastError  error
	onError    func(error)
}

type RTMPConfig struct {
	Bitrate   string // Video bitrate (e.g., "2000k")
	AudioBit  string // Audio bitrate (e.g., "96k")
	FrameRate int    // Video frame rate (default: 30)
	LoopCount int    // Number of times to loop (-1 for infinite)
}

func DefaultRTMPConfig() *RTMPConfig {
	return &RTMPConfig{
		Bitrate:   "2000k",
		AudioBit:  "96k",
		FrameRate: 30,
		LoopCount: -1,
	}
}

func (c *Client) NewRTMPStream(chatID int64, config ...*RTMPConfig) (*RTMPStream, error) {
	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, ErrFFmpegNotFound
	}

	if len(config) == 0 {
		config = append(config, DefaultRTMPConfig())
	}

	rtmpConfig := config[0]

	return &RTMPStream{
		chatID:    chatID,
		client:    c,
		state:     StreamStateIdle,
		loopCount: rtmpConfig.LoopCount,
		bitrate:   rtmpConfig.Bitrate,
		audioBit:  rtmpConfig.AudioBit,
		frameRate: rtmpConfig.FrameRate,
	}, nil
}

// FetchRTMPURL fetches the RTMP URL and stream key from Telegram.
// NOTE: This can only be called by user accounts, not bot accounts.
// Bot accounts will receive an error. For bots, use SetURL() and SetKey() manually.
func (s *RTMPStream) FetchRTMPURL() error {
	peer, err := s.client.ResolvePeer(s.chatID)
	if err != nil {
		return fmt.Errorf("failed to resolve peer: %w", err)
	}
	rtmpInfo, err := s.client.PhoneGetGroupCallStreamRtmpURL(false, peer, false)
	if err != nil {
		return fmt.Errorf("failed to fetch RTMP URL: %w", err)
	}
	s.mu.Lock()
	s.rtmpURL = rtmpInfo.URL
	s.rtmpKey = rtmpInfo.Key
	s.mu.Unlock()
	return nil
}

func (s *RTMPStream) SetURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rtmpURL = url
}

func (s *RTMPStream) SetKey(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rtmpKey = key
}

func (s *RTMPStream) SetFullURL(fullURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if fullURL == "" {
		return fmt.Errorf("RTMP URL cannot be empty")
	}

	if !bytes.Contains([]byte(fullURL), []byte("rtmp://")) && !bytes.Contains([]byte(fullURL), []byte("rtmps://")) {
		return fmt.Errorf("invalid RTMP URL: must start with rtmp:// or rtmps://")
	}

	var url, key string

	// Try /s/ separator first (Telegram format)
	if parts := bytes.SplitN([]byte(fullURL), []byte("/s/"), 2); len(parts) == 2 {
		url = string(parts[0]) + "/s/"
		key = string(parts[1])
	} else if parts := bytes.SplitN([]byte(fullURL), []byte("/"), 4); len(parts) >= 4 {
		url = string(parts[0]) + "//" + string(parts[1]) + "/" + string(parts[2]) + "/"
		key = string(parts[3])
	} else {
		return fmt.Errorf("invalid RTMP URL format: expected rtmp://host/app/streamkey or similar")
	}

	if url == "" || key == "" {
		return fmt.Errorf("failed to parse URL: both URL and key must be non-empty")
	}

	s.rtmpURL = url
	s.rtmpKey = key
	return nil
}

func (s *RTMPStream) GetURL() string {
	return s.rtmpURL
}

func (s *RTMPStream) GetKey() string {
	return s.rtmpKey
}

func (s *RTMPStream) GetFullURL() string {
	return s.rtmpURL + s.rtmpKey
}

// RefreshRTMPURL fetches a new RTMP URL and stream key (revokes the old one).
// NOTE: This can only be called by user accounts, not bot accounts.
func (s *RTMPStream) RefreshRTMPURL() error {
	peer, err := s.client.ResolvePeer(s.chatID)
	if err != nil {
		return fmt.Errorf("failed to resolve peer: %w", err)
	}

	rtmpInfo, err := s.client.PhoneGetGroupCallStreamRtmpURL(false, peer, true)
	if err != nil {
		return fmt.Errorf("failed to refresh RTMP URL: %w", err)
	}
	s.mu.Lock()
	s.rtmpURL = rtmpInfo.URL
	s.rtmpKey = rtmpInfo.Key
	s.mu.Unlock()
	return nil
}

func (s *RTMPStream) State() StreamState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Play starts streaming from a file path (string) or raw bytes ([]byte)
func (s *RTMPStream) Play(source any) error {
	// Validate RTMP URL is set
	if s.rtmpURL == "" || s.rtmpKey == "" {
		return ErrNoRTMPURL
	}

	s.mu.Lock()
	if s.state == StreamStatePlaying {
		s.mu.Unlock()
		return ErrStreamPlaying
	}

	switch src := source.(type) {
	case string:
		// Check if file exists
		if _, err := os.Stat(src); err != nil {
			s.mu.Unlock()
			if os.IsNotExist(err) {
				return fmt.Errorf("%w: %s", ErrFileNotFound, src)
			}
			return fmt.Errorf("failed to access file: %w", err)
		}
		s.inputFile = src
		s.inputData = nil
		s.mu.Unlock()
		return s.startFFmpeg(src, false)
	case []byte:
		if len(src) == 0 {
			s.mu.Unlock()
			return errors.New("empty byte input")
		}
		s.inputData = src
		s.inputFile = ""
		s.mu.Unlock()
		return s.startFFmpeg("pipe:0", true)
	default:
		s.mu.Unlock()
		return fmt.Errorf("unsupported source type: expected string or []byte, got %T", source)
	}
}

func (s *RTMPStream) startFFmpeg(input string, pipeInput bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFunc = cancel

	args := s.buildFFmpegArgs(input)
	s.cmd = exec.CommandContext(ctx, "ffmpeg", args...)

	// Reset and capture stderr for error messages
	s.stderr.Reset()
	s.cmd.Stderr = &s.stderr
	s.cmd.Stdout = nil

	if pipeInput {
		stdin, err := s.cmd.StdinPipe()
		if err != nil {
			cancel()
			return fmt.Errorf("failed to get stdin pipe: %w", err)
		}
		s.stdin = stdin
	}

	if err := s.cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	s.mu.Lock()
	s.state = StreamStatePlaying
	s.startTime = time.Now()
	s.lastError = nil
	s.mu.Unlock()

	if pipeInput && s.inputData != nil {
		go func() {
			defer s.stdin.Close()
			s.stdin.Write(s.inputData)
		}()
	}

	go func() {
		err := s.cmd.Wait()
		s.mu.Lock()
		// Don't reset state if paused or stopped
		if s.state == StreamStatePlaying {
			s.state = StreamStateIdle
			// Check for ffmpeg errors (non-zero exit and not cancelled)
			if err != nil && ctx.Err() == nil {
				errMsg := s.stderr.String()
				if errMsg != "" {
					s.lastError = fmt.Errorf("ffmpeg error: %s", errMsg)
				} else {
					s.lastError = fmt.Errorf("ffmpeg exited with error: %w", err)
				}
				if s.onError != nil {
					go s.onError(s.lastError)
				}
			}
		}
		s.mu.Unlock()
	}()

	return nil
}

func (s *RTMPStream) buildFFmpegArgs(input string) []string {
	args := []string{}

	if s.seekPos > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", s.seekPos.Seconds()))
	}

	args = append(args, "-re")

	if s.loopCount != 0 {
		args = append(args, "-stream_loop", fmt.Sprintf("%d", s.loopCount))
	}

	args = append(args,
		"-i", input,
		"-c:v", "libx264",
		"-preset", "superfast",
		"-b:v", s.bitrate,
		"-maxrate", s.bitrate,
		"-bufsize", s.doubleBitrate(),
		"-pix_fmt", "yuv420p",
		"-g", fmt.Sprintf("%d", s.frameRate),
		"-threads", "0",
		"-c:a", "aac",
		"-b:a", s.audioBit,
		"-ac", "2",
		"-ar", "44100",
		"-f", "flv",
		"-rtmp_buffer", "100",
		"-rtmp_live", "live",
		s.GetFullURL(),
	)

	return args
}

func (s *RTMPStream) doubleBitrate() string {
	var val int
	fmt.Sscanf(s.bitrate, "%dk", &val)
	return fmt.Sprintf("%dk", val*2)
}

func (s *RTMPStream) Pause() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != StreamStatePlaying {
		return fmt.Errorf("cannot pause: stream is %s", s.state)
	}

	s.pausedAt = time.Since(s.startTime) + s.seekPos

	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	s.state = StreamStatePaused
	return nil
}

func (s *RTMPStream) Resume() error {
	s.mu.Lock()
	if s.state != StreamStatePaused {
		s.mu.Unlock()
		return ErrStreamNotPaused
	}

	s.seekPos = s.pausedAt
	s.mu.Unlock()

	if s.inputFile != "" {
		return s.startFFmpeg(s.inputFile, false)
	} else if s.inputData != nil {
		return s.startFFmpeg("pipe:0", true)
	}
	return ErrNoInputSource
}

// Seek to a specific position (only works with file input)
func (s *RTMPStream) Seek(position time.Duration) error {
	s.mu.Lock()
	if s.inputFile == "" {
		s.mu.Unlock()
		return errors.New("seek only supported for file input")
	}

	if position < 0 {
		s.mu.Unlock()
		return errors.New("seek position cannot be negative")
	}

	wasPlaying := s.state == StreamStatePlaying
	s.seekPos = position
	s.mu.Unlock()

	if wasPlaying {
		s.Stop()
		return s.startFFmpeg(s.inputFile, false)
	}
	return nil
}

// CurrentPosition returns the current playback position
func (s *RTMPStream) CurrentPosition() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.state {
	case StreamStatePlaying:
		return time.Since(s.startTime) + s.seekPos
	case StreamStatePaused:
		return s.pausedAt
	default:
		return 0
	}
}

// LastError returns the last error that occurred during streaming
func (s *RTMPStream) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError
}

// OnError sets a callback function that will be called when an error occurs
func (s *RTMPStream) OnError(fn func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onError = fn
}

func (s *RTMPStream) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == StreamStateIdle || s.state == StreamStateStopped {
		return nil
	}

	s.state = StreamStateStopped

	if s.cancelFunc != nil {
		s.cancelFunc()
	}

	if s.stdin != nil {
		s.stdin.Close()
	}

	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}

	return nil
}

func (s *RTMPStream) SetBitrate(bitrate string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bitrate = bitrate
}

func (s *RTMPStream) SetAudioBitrate(bitrate string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audioBit = bitrate
}

func (s *RTMPStream) SetFrameRate(fps int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frameRate = fps
}

func (s *RTMPStream) SetLoopCount(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loopCount = count
}

// StartPipe starts the RTMP stream expecting data to be fed via FeedChunk().
// Use this when you want to stream data progressively in chunks.
func (s *RTMPStream) StartPipe() error {
	if s.rtmpURL == "" || s.rtmpKey == "" {
		return ErrNoRTMPURL
	}

	s.mu.Lock()
	if s.state == StreamStatePlaying {
		s.mu.Unlock()
		return ErrStreamPlaying
	}
	s.inputFile = ""
	s.inputData = nil
	s.mu.Unlock()

	return s.startFFmpegPipe()
}

func (s *RTMPStream) startFFmpegPipe() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFunc = cancel

	args := s.buildFFmpegPipeArgs()
	s.cmd = exec.CommandContext(ctx, "ffmpeg", args...)

	s.stderr.Reset()
	s.cmd.Stderr = &s.stderr
	s.cmd.Stdout = nil

	stdin, err := s.cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	s.stdin = stdin

	if err := s.cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	s.mu.Lock()
	s.state = StreamStatePlaying
	s.startTime = time.Now()
	s.lastError = nil
	s.mu.Unlock()

	go func() {
		err := s.cmd.Wait()
		s.mu.Lock()
		if s.state == StreamStatePlaying {
			s.state = StreamStateIdle
			if err != nil && ctx.Err() == nil {
				errMsg := s.stderr.String()
				if errMsg != "" {
					s.lastError = fmt.Errorf("ffmpeg error: %s", errMsg)
				} else {
					s.lastError = fmt.Errorf("ffmpeg exited with error: %w", err)
				}
				if s.onError != nil {
					go s.onError(s.lastError)
				}
			}
		}
		s.mu.Unlock()
	}()

	return nil
}

func (s *RTMPStream) buildFFmpegPipeArgs() []string {
	return []string{
		"-re",
		"-i", "pipe:0",
		"-c:v", "libx264",
		"-preset", "superfast",
		"-b:v", s.bitrate,
		"-maxrate", s.bitrate,
		"-bufsize", s.doubleBitrate(),
		"-pix_fmt", "yuv420p",
		"-g", fmt.Sprintf("%d", s.frameRate),
		"-threads", "0",
		"-c:a", "aac",
		"-b:a", s.audioBit,
		"-ac", "2",
		"-ar", "44100",
		"-f", "flv",
		"-rtmp_buffer", "100",
		"-rtmp_live", "live",
		s.GetFullURL(),
	}
}

// FeedChunk writes a chunk of data to the RTMP stream.
// Must call StartPipe() first to initialize the stream.
// Returns error if stream is not playing or write fails.
func (s *RTMPStream) FeedChunk(data []byte) error {
	s.mu.Lock()
	if s.state != StreamStatePlaying {
		s.mu.Unlock()
		return fmt.Errorf("cannot feed chunk: stream is %s", s.state)
	}
	if s.stdin == nil {
		s.mu.Unlock()
		return errors.New("stdin pipe not initialized, call StartPipe() first")
	}
	s.mu.Unlock()

	_, err := s.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write chunk: %w", err)
	}
	return nil
}

// FeedReader reads from an io.Reader and feeds data to the RTMP stream.
// This is useful for streaming from HTTP responses, files, etc.
// Must call StartPipe() first.
func (s *RTMPStream) FeedReader(r io.Reader) error {
	s.mu.Lock()
	if s.state != StreamStatePlaying {
		s.mu.Unlock()
		return fmt.Errorf("cannot feed reader: stream is %s", s.state)
	}
	if s.stdin == nil {
		s.mu.Unlock()
		return errors.New("stdin pipe not initialized, call StartPipe() first")
	}
	s.mu.Unlock()

	_, err := io.Copy(s.stdin, r)
	if err != nil {
		return fmt.Errorf("failed to copy from reader: %w", err)
	}
	return nil
}

// ClosePipe closes the stdin pipe, signaling EOF to ffmpeg.
// Call this when you're done feeding data.
func (s *RTMPStream) ClosePipe() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stdin != nil {
		return s.stdin.Close()
	}
	return nil
}
