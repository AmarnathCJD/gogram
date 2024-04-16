package examples

//#include "ntgcalls.h"
//extern void handleStream(uint32_t uid, int64_t chatID, ntg_stream_type_enum streamType, void*);
//extern void handleUpgrade(uint32_t uid, int64_t chatID, ntg_media_state_struct state, void*);
//extern void handleConnectionChange(uint32_t uid, int64_t chatID, ntg_connection_state_enum state, void*);
//extern void handleSignal(uint32_t uid, int64_t chatID, uint8_t*, int, void*);
//extern void unlockMutex(void*);
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

var handlerEnd = make(map[uint32][]StreamEndCallback)
var handlerUpgrade = make(map[uint32][]UpgradeCallback)
var handlerConnectionChange = make(map[uint32][]ConnectionChangeCallback)
var handlerSignal = make(map[uint32][]SignalCallback)

type Client struct {
	uid       uint32
	exists    bool
	streamEnd []StreamEndCallback
}

type AuthParams struct {
	GAOrB          []byte
	KeyFingerprint int64
}

type Protocol struct {
	MinLayer     int32
	MaxLayer     int32
	UdpP2P       bool
	UdpReflector bool
	Versions     []string
}

type RTCServer struct {
	ID                 int64
	Ipv4, Ipv6         string
	Username, Password string
	Port               int32
	Turn, Stun, Tcp    bool
	PeerTag            []byte
}

type MediaState struct {
	Muted        bool
	VideoPaused  bool
	VideoStopped bool
}

type MediaDescription struct {
	Audio *AudioDescription
	Video *VideoDescription
}

func (ctx *MediaDescription) ParseToC() C.ntg_media_description_struct {
	var x C.ntg_media_description_struct
	if ctx.Audio != nil {
		audio := ctx.Audio.ParseToC()
		x.audio = &audio
	}
	if ctx.Video != nil {
		video := ctx.Video.ParseToC()
		x.video = &video
	}
	return x
}

type AudioDescription struct {
	InputMode                   InputMode
	Input                       string
	SampleRate                  uint32
	BitsPerSample, ChannelCount uint8
}

func (ctx *AudioDescription) ParseToC() C.ntg_audio_description_struct {
	var x C.ntg_audio_description_struct
	x.inputMode = ctx.InputMode.ParseToC()
	x.input = C.CString(ctx.Input)
	x.sampleRate = C.uint32_t(ctx.SampleRate)
	x.bitsPerSample = C.uint8_t(ctx.BitsPerSample)
	x.channelCount = C.uint8_t(ctx.ChannelCount)
	return x
}

type VideoDescription struct {
	InputMode     InputMode
	Input         string
	Width, Height uint16
	Fps           uint8
}

func (ctx *VideoDescription) ParseToC() C.ntg_video_description_struct {
	var x C.ntg_video_description_struct
	x.inputMode = ctx.InputMode.ParseToC()
	x.input = C.CString(ctx.Input)
	x.width = C.uint16_t(ctx.Width)
	x.height = C.uint16_t(ctx.Height)
	x.fps = C.uint8_t(ctx.Fps)
	return x
}

func NTgCalls() *Client {
	instance := &Client{
		uid:    uint32(C.ntg_init()),
		exists: true,
	}
	C.ntg_on_stream_end(C.uint32_t(instance.uid), (C.ntg_stream_callback)(unsafe.Pointer(C.handleStream)), nil)
	C.ntg_on_upgrade(C.uint32_t(instance.uid), (C.ntg_upgrade_callback)(unsafe.Pointer(C.handleUpgrade)), nil)
	C.ntg_on_signaling_data(C.uint32_t(instance.uid), (C.ntg_signaling_callback)(unsafe.Pointer(C.handleSignal)), nil)
	C.ntg_on_connection_change(C.uint32_t(instance.uid), (C.ntg_connection_callback)(unsafe.Pointer(C.handleConnectionChange)), nil)
	return instance
}

//export handleStream
func handleStream(uid C.uint32_t, chatID C.int64_t, streamType C.ntg_stream_type_enum, _ unsafe.Pointer) {
	goChatID := int64(chatID)
	goUID := uint32(uid)
	var goStreamType StreamType
	if streamType == C.NTG_STREAM_AUDIO {
		goStreamType = AudioStream
	} else {
		goStreamType = VideoStream
	}
	if handlerEnd[goUID] != nil {
		for _, x0 := range handlerEnd[goUID] {
			go x0(goChatID, goStreamType)
		}
	}
}

//export handleUpgrade
func handleUpgrade(uid C.uint32_t, chatID C.int64_t, state C.ntg_media_state_struct, _ unsafe.Pointer) {
	goChatID := int64(chatID)
	goUID := uint32(uid)
	goState := MediaState{
		Muted:        bool(state.muted),
		VideoPaused:  bool(state.videoPaused),
		VideoStopped: bool(state.videoStopped),
	}
	if handlerUpgrade[goUID] != nil {
		for _, x0 := range handlerUpgrade[goUID] {
			go x0(goChatID, goState)
		}
	}
}

//export handleSignal
func handleSignal(uid C.uint32_t, chatID C.int64_t, data *C.uint8_t, size C.int, _ unsafe.Pointer) {
	goChatID := int64(chatID)
	goUID := uint32(uid)
	if handlerSignal[goUID] != nil {
		for _, x0 := range handlerSignal[goUID] {
			go x0(goChatID, C.GoBytes(unsafe.Pointer(data), size))
		}
	}
}

//export handleConnectionChange
func handleConnectionChange(uid C.uint32_t, chatID C.int64_t, state C.ntg_connection_state_enum, _ unsafe.Pointer) {
	goChatID := int64(chatID)
	goUID := uint32(uid)
	var goState ConnectionState
	switch state {
	case C.NTG_STATE_CONNECTING:
		goState = Connecting
	case C.NTG_STATE_CONNECTED:
		goState = Connected
	case C.NTG_STATE_FAILED:
		goState = Failed
	case C.NTG_STATE_TIMEOUT:
		goState = Timeout
	case C.NTG_STATE_CLOSED:
		goState = Closed
	}
	if handlerConnectionChange[goUID] != nil {
		for _, x0 := range handlerConnectionChange[goUID] {
			go x0(goChatID, goState)
		}
	}
}

func (ctx *Client) OnStreamEnd(callback StreamEndCallback) {
	handlerEnd[ctx.uid] = append(handlerEnd[ctx.uid], callback)
}

func (ctx *Client) OnUpgrade(callback UpgradeCallback) {
	handlerUpgrade[ctx.uid] = append(handlerUpgrade[ctx.uid], callback)
}

func (ctx *Client) OnConnectionChange(callback ConnectionChangeCallback) {
	handlerConnectionChange[ctx.uid] = append(handlerConnectionChange[ctx.uid], callback)
}

func (ctx *Client) OnSignal(callback SignalCallback) {
	handlerSignal[ctx.uid] = append(handlerSignal[ctx.uid], callback)
}

func parseBool(res C.int) (bool, error) {
	return res == 0, parseErrorCode(res)
}

func parseBytes(data []byte) (*C.uint8_t, C.int) {
	if len(data) > 0 {
		rawBytes := C.CBytes(data)
		return (*C.uint8_t)(rawBytes), C.int(len(data))
	}
	return nil, 0
}

func parseStringVector(data unsafe.Pointer, size C.int) []string {
	result := make([]string, size)
	for i := 0; i < int(size); i++ {
		result[i] = C.GoString(*(**C.char)(unsafe.Pointer(uintptr(data) + uintptr(i)*unsafe.Sizeof(uintptr(0)))))
	}
	return result
}

func parseStringVectorC(data []string) (**C.char, C.int) {
	result := make([]*C.char, len(data))
	for i, v := range data {
		result[i] = C.CString(v)
	}
	return &result[0], C.int(len(data))
}

func parseErrorCode(errorCode C.int) error {
	pErrorCode := int16(errorCode)
	switch pErrorCode {
	case -100:
		return fmt.Errorf("connection already made")
	case -101:
		return fmt.Errorf("connection not found")
	case -102:
		return fmt.Errorf("cryptation error")
	case -103:
		return fmt.Errorf("missing fingerprint")
	case -200:
		return fmt.Errorf("file not found")
	case -201:
		return fmt.Errorf("encoder not found")
	case -202:
		return fmt.Errorf("ffmpeg not found")
	case -203:
		return fmt.Errorf("error while executing shell command")
	case -300:
		return fmt.Errorf("rtmp needed")
	case -301:
		return fmt.Errorf("invalid transport")
	case -302:
		return fmt.Errorf("connection failed")
	}
	if pErrorCode >= 0 {
		return nil
	} else {
		return fmt.Errorf("unknown error")
	}
}

func (ctx *Client) CreateCall(chatId int64, desc MediaDescription) (string, error) {
	var buffer [1024]C.char
	size := C.int(len(buffer))
	f := CreateFuture()
	C.ntg_create(C.uint32_t(ctx.uid), C.int64_t(chatId), desc.ParseToC(), &buffer[0], size, f.ParseToC())
	f.wait()
	return C.GoString(&buffer[0]), parseErrorCode(*f.errCode)
}

func (ctx *Client) CreateP2PCall(chatId int64, g int32, p []byte, r []byte, gAHash []byte, desc MediaDescription) ([]byte, error) {
	f := CreateFuture()
	var buffer [32]C.uint8_t
	size := C.int(len(buffer))
	pC, pSize := parseBytes(p)
	rC, rSize := parseBytes(r)
	gAHashC, gAHashSize := parseBytes(gAHash)
	C.ntg_create_p2p(C.uint32_t(ctx.uid), C.int64_t(chatId), C.int32_t(g), pC, pSize, rC, rSize, gAHashC, gAHashSize, desc.ParseToC(), &buffer[0], size, f.ParseToC())
	f.wait()
	return C.GoBytes(unsafe.Pointer(&buffer[0]), size), parseErrorCode(*f.errCode)
}

func (ctx *Client) ExchangeKeys(chatId int64, gAB []byte, fingerprint int64) (AuthParams, error) {
	f := CreateFuture()
	var buffer C.ntg_auth_params_struct
	gABC, gABSize := parseBytes(gAB)
	C.ntg_exchange_keys(C.uint32_t(ctx.uid), C.int64_t(chatId), gABC, gABSize, C.int64_t(fingerprint), &buffer, f.ParseToC())
	f.wait()
	return AuthParams{
		GAOrB:          C.GoBytes(unsafe.Pointer(buffer.g_a_or_b), buffer.sizeGAB),
		KeyFingerprint: int64(buffer.key_fingerprint),
	}, parseErrorCode(*f.errCode)
}

func (ctx *Client) ConnectP2P(chatId int64, rtcServers []RTCServer, versions []string, P2PAllowed bool) error {
	f := CreateFuture()
	servers := make([]C.ntg_rtc_server_struct, len(rtcServers))
	for i, server := range rtcServers {
		servers[i] = C.ntg_rtc_server_struct{
			ipv4:        C.CString(server.Ipv4),
			ipv6:        C.CString(server.Ipv6),
			username:    C.CString(server.Username),
			password:    C.CString(server.Password),
			port:        C.uint16_t(server.Port),
			turn:        C.bool(server.Turn),
			stun:        C.bool(server.Stun),
			tcp:         C.bool(server.Tcp),
			peerTag:     nil,
			peerTagSize: 0,
		}
		if len(server.PeerTag) > 0 {
			peerTagC, peerTagSize := parseBytes(server.PeerTag)
			servers[i].peerTag = peerTagC
			servers[i].peerTagSize = peerTagSize
		}
	}
	versionsC, sizeVersions := parseStringVectorC(versions)
	C.ntg_connect_p2p(C.uint32_t(ctx.uid), C.int64_t(chatId), (*C.ntg_rtc_server_struct)(unsafe.Pointer(&servers[0])), C.int(len(servers)), versionsC, C.int(sizeVersions), C.bool(P2PAllowed), f.ParseToC())
	f.wait()
	return parseErrorCode(*f.errCode)
}

func (ctx *Client) SendSignalingData(chatId int64, data []byte) error {
	f := CreateFuture()
	dataC, dataSize := parseBytes(data)
	C.ntg_send_signaling_data(C.uint32_t(ctx.uid), C.int64_t(chatId), dataC, dataSize, f.ParseToC())
	f.wait()
	return parseErrorCode(*f.errCode)
}

func (ctx *Client) GetProtocol() Protocol {
	var buffer C.ntg_protocol_struct
	C.ntg_get_protocol(C.uint32_t(ctx.uid), &buffer)
	return Protocol{
		MinLayer:     int32(buffer.minLayer),
		MaxLayer:     int32(buffer.maxLayer),
		UdpP2P:       bool(buffer.udpP2P),
		UdpReflector: bool(buffer.udpReflector),
		Versions:     parseStringVector(unsafe.Pointer(buffer.libraryVersions), buffer.libraryVersionsSize),
	}
}

func (ctx *Client) Connect(chatId int64, params string) error {
	f := CreateFuture()
	C.ntg_connect(C.uint32_t(ctx.uid), C.int64_t(chatId), C.CString(params), f.ParseToC())
	f.wait()
	return parseErrorCode(*f.errCode)
}

func (ctx *Client) ChangeStream(chatId int64, desc MediaDescription) error {
	f := CreateFuture()
	C.ntg_change_stream(C.uint32_t(ctx.uid), C.int64_t(chatId), desc.ParseToC(), f.ParseToC())
	f.wait()
	return parseErrorCode(*f.errCode)
}

func (ctx *Client) Pause(chatId int64) (bool, error) {
	f := CreateFuture()
	C.ntg_pause(C.uint32_t(ctx.uid), C.int64_t(chatId), f.ParseToC())
	f.wait()
	return parseBool(*f.errCode)
}

func (ctx *Client) Resume(chatId int64) (bool, error) {
	f := CreateFuture()
	C.ntg_resume(C.uint32_t(ctx.uid), C.int64_t(chatId), f.ParseToC())
	f.wait()
	return parseBool(*f.errCode)
}

func (ctx *Client) Mute(chatId int64) (bool, error) {
	f := CreateFuture()
	C.ntg_mute(C.uint32_t(ctx.uid), C.int64_t(chatId), f.ParseToC())
	f.wait()
	return parseBool(*f.errCode)
}

func (ctx *Client) UnMute(chatId int64) (bool, error) {
	f := CreateFuture()
	C.ntg_unmute(C.uint32_t(ctx.uid), C.int64_t(chatId), f.ParseToC())
	f.wait()
	return parseBool(*f.errCode)
}

func (ctx *Client) Stop(chatId int64) error {
	f := CreateFuture()
	C.ntg_stop(C.uint32_t(ctx.uid), C.int64_t(chatId), f.ParseToC())
	f.wait()
	return parseErrorCode(*f.errCode)
}

func (ctx *Client) Time(chatId int64) (uint64, error) {
	f := CreateFuture()
	var buffer C.int64_t
	C.ntg_time(C.uint32_t(ctx.uid), C.int64_t(chatId), &buffer, f.ParseToC())
	f.wait()
	return uint64(buffer), parseErrorCode(*f.errCode)
}

func (ctx *Client) CpuUsage() (float64, error) {
	f := CreateFuture()
	var buffer C.double
	C.ntg_cpu_usage(C.uint32_t(ctx.uid), &buffer, f.ParseToC())
	f.wait()
	return float64(buffer), parseErrorCode(*f.errCode)
}

func (ctx *Client) Calls() map[int64]StreamStatus {
	mapReturn := make(map[int64]StreamStatus)

	f := CreateFuture()
	var callSize C.uint64_t
	_ = C.ntg_calls_count(C.uint32_t(ctx.uid), &callSize, f.ParseToC())
	f.wait()
	f = CreateFuture()
	buffer := make([]C.ntg_group_call_struct, callSize)
	C.ntg_calls(C.uint32_t(ctx.uid), &buffer[0], callSize, f.ParseToC())
	f.wait()
	for _, call := range buffer {
		var goStreamType StreamStatus
		switch call.status {
		case C.NTG_PLAYING:
			goStreamType = PlayingStream
		case C.NTG_PAUSED:
			goStreamType = PausedStream
		case C.NTG_IDLING:
			goStreamType = IdlingStream
		}
		mapReturn[int64(call.chatId)] = goStreamType
	}
	return mapReturn
}

func Version() string {
	var buffer [8]C.char
	size := C.int(len(buffer))
	C.ntg_get_version(&buffer[0], size)
	return C.GoString(&buffer[0])
}

func (ctx *Client) Free() {
	C.ntg_destroy(C.uint32_t(ctx.uid))
	delete(handlerEnd, ctx.uid)
	delete(handlerUpgrade, ctx.uid)
	delete(handlerConnectionChange, ctx.uid)
	delete(handlerSignal, ctx.uid)
	ctx.exists = false
}

type Future struct {
	mutex   *sync.Mutex
	errCode *C.int
}

func CreateFuture() *Future {
	res := &Future{
		mutex:   &sync.Mutex{},
		errCode: new(C.int),
	}
	res.mutex.Lock()
	return res
}

func (ctx *Future) ParseToC() C.ntg_async_struct {
	var x C.ntg_async_struct
	x.userData = unsafe.Pointer(ctx.mutex)
	x.promise = (C.ntg_async_callback)(unsafe.Pointer(C.unlockMutex))
	x.errorCode = (*C.int)(unsafe.Pointer(ctx.errCode))
	return x
}

func (ctx *Future) wait() {
	ctx.mutex.Lock()
}

//export unlockMutex
func unlockMutex(p unsafe.Pointer) {
	m := (*sync.Mutex)(p)
	m.Unlock()
}

type StreamType int
type ConnectionState int
type StreamStatus int
type InputMode int

type StreamEndCallback func(chatId int64, streamType StreamType)
type UpgradeCallback func(chatId int64, state MediaState)
type ConnectionChangeCallback func(chatId int64, state ConnectionState)
type SignalCallback func(chatId int64, signal []byte)

const (
	AudioStream StreamType = iota
	VideoStream
)

const (
	InputModeFile InputMode = 1 << iota
	InputModeShell
	InputModeFFmpeg
	InputModeNoLatency
)

const (
	PlayingStream StreamStatus = iota
	PausedStream
	IdlingStream
)

const (
	Connecting ConnectionState = iota
	Connected
	Failed
	Timeout
	Closed
)

func (ctx InputMode) ParseToC() C.ntg_input_mode_enum {
	switch ctx {
	case InputModeFile:
		return C.NTG_FILE
	case InputModeShell:
		return C.NTG_SHELL
	case InputModeFFmpeg:
		return C.NTG_FFMPEG
	default:
		return C.NTG_FILE
	}
}
