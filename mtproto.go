// Copyright (c) 2025 @AmarnathCJD

package gogram

import (
	"context"
	"crypto/rsa"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/keys"
	"github.com/amarnathcjd/gogram/internal/mode"
	"github.com/amarnathcjd/gogram/internal/mtproto/messages"
	"github.com/amarnathcjd/gogram/internal/mtproto/objects"
	"github.com/amarnathcjd/gogram/internal/session"
	"github.com/amarnathcjd/gogram/internal/transport"
	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	defaultMaxReconnectAttempts = 2000
	defaultBaseReconnectDelay   = 2 * time.Second
	defaultPingInterval         = 30 * time.Second
	defaultPendingAcksThreshold = 10
	maxMsgsAckPerBatch          = 8192
)

type TransportType uint8

const (
	TransportTCP TransportType = iota
	TransportWebSocket
	TransportWebSocketTLS
	TransportHTTP
	TransportHTTPS
)

func (t TransportType) String() string {
	switch t {
	case TransportWebSocket:
		return "websocket"
	case TransportWebSocketTLS:
		return "websocket (tls)"
	case TransportHTTP:
		return "http"
	case TransportHTTPS:
		return "https"
	default:
		return "tcp"
	}
}

func (t TransportType) isHTTP() bool {
	return t == TransportHTTP || t == TransportHTTPS
}

type ReconnectConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Timeout     time.Duration
}

type ReconnectState struct {
	InProgress              atomic.Bool
	Attempts                atomic.Int32
	ConsecutiveTimeouts     atomic.Int32
	ConsecutiveTimeoutStart atomic.Int64
	LastSuccessfulConnect   atomic.Int64
}

type MTProto struct {
	Addr      atomic.Value
	appID     int32
	dcID      atomic.Int32
	testMode  bool
	mediaDC   bool
	proxy     *utils.Proxy
	transport transport.Transport
	localAddr string

	ctxCancel      context.CancelFunc
	ctxCancelMutex sync.Mutex
	lifecycleMu    contextMutex
	routineswg     sync.WaitGroup
	memorySession  bool
	tcpState       *TcpState
	timeOffset     atomic.Int64
	reqTimeout     time.Duration
	mode           mode.Variant
	DcList         *utils.DCOptions
	transportMu    sync.Mutex
	writeMu        contextMutex

	authKey []byte
	authMu  sync.RWMutex

	authKeyHash []byte

	tempAuthKey          []byte
	tempAuthKeyHash      []byte
	tempAuthExpiresAt    int64
	pendingTempAuthKey   []byte
	pendingTempKeyHash   []byte
	pendingTempExpiresAt int64
	tempServerSalt       int64
	pendingTempSalt      int64
	previousTempAuthKey  []byte
	previousTempKeyHash  []byte
	previousTempSalt     int64

	noRedirect bool

	serverSalt atomic.Int64
	encrypted  atomic.Bool
	sessionId  atomic.Int64

	responseChannels *utils.SyncInt64ObjectChan
	expectedTypes    *utils.SyncInt64ReflectTypes
	pendingAcks      *utils.SyncSet[int64]
	receivedIDs      receivedMessageWindow
	serviceMessages  chan tl.Object
	acksReady        chan struct{}

	genMsgID     func(int64) int64
	currentSeqNo atomic.Int32

	sessionStorage session.SessionLoader

	publicKey *rsa.PublicKey
	cdnKeysMu sync.RWMutex
	cdnKeys   map[int32]*rsa.PublicKey

	serviceChannel       chan tl.Object
	serviceModeActivated atomic.Bool

	authKey404Count atomic.Int64
	authKey404Time  atomic.Int64
	IpV6            bool
	obfuscated      bool

	Logger *utils.Logger

	serverRequestHandlers []func(i any) bool
	rpcResponseHandlers   []func(i any)
	handlersMu            sync.RWMutex
	floodHandler          func(err error) bool
	errorHandler          func(err error) bool
	connectionHandler     func(err error) error
	exported              bool
	cdn                   bool
	terminated            atomic.Bool
	disconnected          atomic.Bool
	senderCounters        sync.Map // map[int]int32 - tracks sender count per DC

	connConfig ReconnectConfig
	connState  ReconnectState

	txType         TransportType
	httpPath       string
	enablePFS      bool
	pfsKeyLifetime int32

	onMigration func()

	messageTracker     *utils.SyncInt64Int64
	messageTypesMap    sync.Map // msgID -> request type name
	maxRequestAttempts int      // Maximum total attempts for one RPC
}

type Config struct {
	AuthKeyFile    string                // Path to auth key file for persistent sessions
	AuthAESKey     string                // AES-256 key for encrypting session file
	StringSession  string                // Base64 encoded session string
	SessionStorage session.SessionLoader // Custom session storage implementation
	MemorySession  bool                  // Keep session in memory only
	AppID          int32                 // Telegram API ID
	EnablePFS      bool                  // Enable Perfect Forward Secrecy

	FloodHandler      func(err error) bool  // Called on FLOOD_WAIT; return true to retry
	ErrorHandler      func(err error) bool  // Called on errors; return true to retry
	ConnectionHandler func(err error) error // Custom reconnection handler

	ServerHost     string         // Telegram server address (IP:port)
	PublicKey      *rsa.PublicKey // RSA public key for server verification
	DataCenter     int            // Data center ID (1-5)
	TestMode       bool           // Use the Telegram test environment.
	MediaDC        bool           // Target is a media-only DC (not a CDN).
	Logger         *utils.Logger  // Logger instance
	Proxy          *utils.Proxy   // Proxy configuration
	Mode           string         // Transport mode (Abridged, Intermediate, Full)
	Ipv6           bool           // Prefer IPv6 connections
	CustomHost     bool           // Use custom ServerHost instead of DC lookup
	LocalAddr      string         // Local address to bind (IP:port)
	Timeout        int            // TCP connection timeout (seconds)
	ReqTimeout     int            // RPC request timeout (seconds)
	Transport      TransportType  // Transport variant (default TransportTCP)
	Obfuscated     bool           // Wrap the TCP transport with obfuscation (mtproto obfuscated2)
	HTTPPath       string         // HTTP request path (default "/api"; only used for HTTP/HTTPS)
	PFSKeyLifetime int32          // Lifetime (seconds) for temp auth keys when EnablePFS is set; 0 = 24h

	MaxReconnectAttempts int           // Max reconnection attempts (default: 2000)
	BaseReconnectDelay   time.Duration // Initial reconnect delay (default: 2s)
	MaxReconnectDelay    time.Duration // Maximum reconnect delay (default: 15m)

	OnMigration func() // Called after DC migration completes
}

func NewMTProto(c Config) (*MTProto, error) {
	if c.PublicKey == nil {
		index := 0
		if c.TestMode {
			index = 1
		}
		c.PublicKey = keys.GetRSAKeys()[index]
	}
	if c.Logger == nil {
		c.Logger = utils.NewLogger("gogram [mtproto]").SetLevel(utils.InfoLevel)
	}
	if c.SessionStorage == nil {
		if c.MemorySession {
			c.SessionStorage = session.NewInMemory()
		} else {
			c.SessionStorage = session.NewFromFile(c.AuthKeyFile, c.AuthAESKey)
		}
	}

	loaded, err := c.SessionStorage.Load()
	if err != nil {
		if !AnyError(err, session.ErrFileNotExists, session.ErrPathNotFound, session.ErrNotImplementedInJS) {
			// if the error is not because of file not found or path not found
			if !c.MemorySession {
				dir := filepath.Dir(c.AuthKeyFile)
				if info, err := os.Stat(dir); err == nil {
					if info.IsDir() {
						if testFile, err := os.CreateTemp(dir, ".write-test-*"); err != nil {
							c.Logger.Warn("no write permission in session directory %s: %v", dir, err)
						} else {
							testFile.Close()
							os.Remove(testFile.Name())
						}
					} else {
						c.Logger.Warn("session directory path is not a directory: %s", dir)
					}
				}
			}
			c.Logger.Warn("failed to load session: %v", err)
		}
	}

	mtproto := &MTProto{
		sessionStorage:        c.SessionStorage,
		serviceChannel:        make(chan tl.Object),
		publicKey:             c.PublicKey,
		responseChannels:      utils.NewSyncInt64ObjectChan(),
		expectedTypes:         utils.NewSyncInt64ReflectTypes(),
		pendingAcks:           utils.NewSyncSet[int64](),
		serviceMessages:       make(chan tl.Object, 32),
		acksReady:             make(chan struct{}, 1),
		genMsgID:              utils.NewMsgIDGenerator(),
		serverRequestHandlers: make([]func(i any) bool, 0),
		Logger:                c.Logger,
		memorySession:         c.MemorySession,
		appID:                 c.AppID,
		testMode:              c.TestMode,
		mediaDC:               c.MediaDC,
		proxy:                 c.Proxy,
		localAddr:             c.LocalAddr,
		floodHandler:          func(err error) bool { return false },
		errorHandler:          func(err error) bool { return false },
		reqTimeout:            utils.MinSafeDuration(c.ReqTimeout),
		mode:                  parseTransportMode(c.Mode),
		IpV6:                  c.Ipv6,
		obfuscated:            c.Obfuscated,
		tcpState:              NewTcpState(),
		DcList:                utils.NewDCOptions(),
		connConfig: ReconnectConfig{
			Timeout:     utils.MinSafeDuration(c.Timeout),
			MaxDelay:    utils.OrDefault(c.MaxReconnectDelay, 15*time.Minute),
			MaxAttempts: utils.OrDefault(c.MaxReconnectAttempts, defaultMaxReconnectAttempts),
			BaseDelay:   utils.OrDefault(c.BaseReconnectDelay, defaultBaseReconnectDelay),
		},
		txType:             c.Transport,
		httpPath:           c.HTTPPath,
		enablePFS:          c.EnablePFS,
		pfsKeyLifetime:     c.PFSKeyLifetime,
		onMigration:        c.OnMigration,
		messageTracker:     utils.NewSyncInt64Int64(),
		maxRequestAttempts: 10,
	}

	mtproto.SetAddr(c.ServerHost)
	mtproto.dcID.Store(int32(c.DataCenter))
	mtproto.encrypted.Store(false)
	mtproto.sessionId.Store(utils.GenerateSessionID())
	mtproto.connState.Attempts.Store(0)

	mtproto.Logger.Debug("mtproto sender initialized")

	if loaded != nil || c.StringSession != "" {
		mtproto.encrypted.Store(true)
	}
	if err := mtproto.loadAuth(c.StringSession, loaded); err != nil {
		return nil, fmt.Errorf("loading auth: %w", err)
	}

	if c.CustomHost {
		mtproto.SetAddr(c.ServerHost)
	}

	if c.FloodHandler != nil {
		mtproto.floodHandler = c.FloodHandler
	}

	if c.ErrorHandler != nil {
		mtproto.errorHandler = c.ErrorHandler
	}

	if c.ConnectionHandler != nil {
		mtproto.connectionHandler = c.ConnectionHandler
	}

	return mtproto, nil
}

func parseTransportMode(sMode string) mode.Variant {
	switch sMode {
	case "Full":
		return mode.Full
	case "Intermediate":
		return mode.Intermediate
	case "PaddedIntermediate":
		return mode.PaddedIntermediate
	default:
		return mode.Abridged
	}
}

func (m *MTProto) LoadSession(sess *session.Session) error {
	if err := sess.Validate(); err != nil {
		return err
	}
	m._loadSession(sess)
	m.Logger.Debug("loading session from %s", utils.FmtIP(sess.Hostname))
	if err := m.SaveSession(m.memorySession); err != nil {
		return fmt.Errorf("saving session: %w", err)
	}
	return nil
}

func (m *MTProto) loadAuth(stringSession string, sess *session.Session) error {
	if stringSession != "" {
		_, err := m.ImportAuth(stringSession)
		if err != nil {
			return fmt.Errorf("importing string session: %w", err)
		}
	} else if sess != nil {
		if err := sess.Validate(); err != nil {
			return err
		}
		m._loadSession(sess)
	}
	return nil
}

func (m *MTProto) ExportAuth() (*session.Session, int) {
	key, hash := m.permanentAuth()
	return &session.Session{
		Key:      key,
		Hash:     hash,
		Salt:     m.serverSalt.Load(),
		Hostname: m.GetAddr(),
		AppID:    m.AppID(),
	}, m.GetDC()
}

func (m *MTProto) ImportRawAuth(authKey, authKeyHash []byte, addr string, appID int32) (bool, error) {
	if err := (&session.Session{Key: authKey, Hash: authKeyHash, Hostname: addr, AppID: appID}).Validate(); err != nil {
		return false, err
	}
	m.SetAddr(addr)
	m.SetAuthKey(authKey)
	m.appID = appID
	m.Logger.Debug("importing raw auth credentials")
	if err := m.SaveSession(m.memorySession); err != nil {
		return false, fmt.Errorf("saving session: %w", err)
	}
	if err := m.Reconnect(context.Background(), false); err != nil {
		return false, fmt.Errorf("reconnecting: %w", err)
	}
	return true, nil
}

func (m *MTProto) ImportAuth(stringSession string) (bool, error) {
	sessionString := session.NewEmptyStringSession()
	if err := sessionString.Decode(stringSession); err != nil {
		return false, err
	}
	m.SetAuthKey(sessionString.AuthKey)
	m.dcID.Store(int32(sessionString.DcID))
	m.SetAddr(sessionString.IpAddr)

	if m.appID == 0 {
		m.appID = sessionString.AppID
	}
	m.Logger.Debug("importing session from string (%s)", utils.FmtIP(sessionString.IpAddr))
	if err := m.SaveSession(m.memorySession); err != nil {
		return false, fmt.Errorf("saving session: %w", err)
	}
	return true, nil
}

func (m *MTProto) GetDC() int {
	if dc := m.dcID.Load(); dc > 0 {
		return int(dc)
	}
	return m.DcList.SearchAddr(m.GetAddr())
}

func (m *MTProto) handshakeDC() int32 {
	dc := int32(m.GetDC())
	if m.testMode {
		dc += 10000
	}
	if m.mediaDC && !m.cdn {
		dc = -dc
	}
	return dc
}

func (m *MTProto) SetAddr(addr string) {
	m.Addr.Store(addr)
}

func (m *MTProto) GetAddr() string {
	return m.Addr.Load().(string)
}

func (m *MTProto) GetTransportType() string {
	switch m.txType {
	case TransportWebSocket:
		return "Ws"
	case TransportWebSocketTLS:
		return "Wss"
	case TransportHTTP:
		return "Http"
	case TransportHTTPS:
		return "Https"
	}

	if m.proxy != nil && !m.proxy.IsEmpty() {
		pType := strings.ToLower(m.proxy.Type)
		switch pType {
		case "socks4", "socks4a":
			return "Socks4"
		case "socks5", "socks5h":
			return "Socks5"
		case "http", "https":
			return "Http"
		case "mtproxy":
			return "Mtproxy"
		}
	}

	if m.IpV6 {
		return "Tcp6"
	}
	return "Tcp"
}

func (m *MTProto) AppID() int32 {
	return m.appID
}

func (m *MTProto) SetAppID(appID int32) {
	m.appID = appID
}

func (m *MTProto) SetCdnKeys(keys map[int32]*rsa.PublicKey) {
	m.cdnKeysMu.Lock()
	defer m.cdnKeysMu.Unlock()
	m.cdnKeys = make(map[int32]*rsa.PublicKey, len(keys))
	for dc, key := range keys {
		if key != nil && key.N != nil {
			m.cdnKeys[dc] = &rsa.PublicKey{N: new(big.Int).Set(key.N), E: key.E}
		}
	}
}

func (m *MTProto) HasCdnKey(dc int32) (*rsa.PublicKey, bool) {
	m.cdnKeysMu.RLock()
	defer m.cdnKeysMu.RUnlock()
	key, ok := m.cdnKeys[dc]
	if !ok {
		return nil, false
	}
	return &rsa.PublicKey{N: new(big.Int).Set(key.N), E: key.E}, true
}

func (m *MTProto) SwitchDc(ctx context.Context, dc int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.noRedirect {
		return nil
	}
	newAddr := m.DcList.GetHostIP(dc, m.testMode, m.IpV6)
	if newAddr == "" {
		return fmt.Errorf("dc %d not found in dc list", dc)
	}

	m.Logger.Debug("migrating to DC%d", dc)

	m.connState.InProgress.Store(true)
	defer m.connState.InProgress.Store(false)

	if err := m.Disconnect(); err != nil {
		return err
	}

	if err := m.sessionStorage.Delete(); err != nil {
		return err
	}
	m.Logger.Debug("cleared old session for migration")

	m.authMu.Lock()
	m.authKey = nil
	m.authKeyHash = nil
	m.serverSalt.Store(0)
	m.encrypted.Store(false)
	m.sessionId.Store(utils.GenerateSessionID())

	m.tempAuthKey = nil
	m.tempAuthKeyHash = nil
	m.tempAuthExpiresAt = 0
	m.pendingTempAuthKey = nil
	m.pendingTempKeyHash = nil
	m.previousTempAuthKey = nil
	m.previousTempKeyHash = nil
	m.authMu.Unlock()
	m.notifyPendingRequestsOfConfigChange()
	m.expectedTypes.SwapAndClear()
	for _, id := range m.pendingAcks.Keys() {
		m.pendingAcks.Delete(id)
	}
	m.receivedIDs.clear()
	m.currentSeqNo.Store(0)

	m.authKey404Count.Store(0)
	m.authKey404Time.Store(0)
	m.connState.Attempts.Store(0)
	m.connState.ConsecutiveTimeouts.Store(0)
	m.connState.LastSuccessfulConnect.Store(0)
	m.SetAddr(newAddr)
	m.dcID.Store(int32(dc))

	m.Logger.Info("migrated to DC%d (%s)", dc, newAddr)
	m.Logger.Debug("establishing connection to DC%d", dc)

	errConn := m.CreateConnection(ctx, true, true)
	if errConn != nil {
		return fmt.Errorf("creating connection: %w", errConn)
	}

	return nil
}

// ExportNewSender bounds connection and authorization setup by ctx.
func (m *MTProto) ExportNewSender(ctx context.Context, dcID int, mem bool, cdn ...bool) (*MTProto, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	isCdn := len(cdn) > 0 && cdn[0]
	isMedia := len(cdn) > 1 && cdn[1]
	newAddr := m.DcList.GetHostIP(dcID, m.testMode, m.IpV6)
	targetIsMedia := false

	var senderNum int32
	if val, ok := m.senderCounters.Load(dcID); ok {
		senderNum = val.(int32) + 1
	} else {
		senderNum = 1
	}
	m.senderCounters.Store(dcID, senderNum)

	var loggerPrefix string
	switch {
	case isCdn:
		newAddr, _ = m.DcList.GetCDNAddr(dcID)
		loggerPrefix = fmt.Sprintf("gogram [cdn>>dc%d#%d]", dcID, senderNum)
	case isMedia:
		if mediaAddr, ok := m.DcList.GetMediaAddr(dcID, m.IpV6); ok {
			newAddr = mediaAddr
			targetIsMedia = true
			m.Logger.Debug("sender #%d targeting media DC%d at %s", senderNum, dcID, mediaAddr)
		} else {
			m.Logger.Debug("sender #%d requested media DC%d but none advertised; using regular %s", senderNum, dcID, newAddr)
		}
		loggerPrefix = fmt.Sprintf("gogram [media>>dc%d#%d]", dcID, senderNum)
	default:
		loggerPrefix = fmt.Sprintf("gogram [sender>>dc%d#%d]", dcID, senderNum)
	}

	logger := utils.NewLogger(loggerPrefix).SetLevel(utils.InfoLevel)

	cfg := Config{
		DataCenter:     dcID,
		TestMode:       m.testMode,
		MediaDC:        targetIsMedia,
		CustomHost:     true,
		PublicKey:      m.publicKey,
		ServerHost:     newAddr,
		AuthKeyFile:    "__exp_" + strconv.Itoa(dcID) + ".dat",
		MemorySession:  mem,
		Logger:         logger,
		Proxy:          m.proxy,
		LocalAddr:      m.localAddr,
		AppID:          m.appID,
		Ipv6:           m.IpV6,
		Timeout:        int(m.connConfig.Timeout.Seconds()),
		ReqTimeout:     int(m.reqTimeout.Seconds()),
		Transport:      m.txType,
		Obfuscated:     m.obfuscated,
		HTTPPath:       m.httpPath,
		EnablePFS:      m.enablePFS,
		PFSKeyLifetime: m.pfsKeyLifetime,
	}

	if dcID == m.GetDC() && !isCdn {
		cfg.SessionStorage = m.sessionStorage
		key, hash := m.permanentAuth()
		cfg.StringSession = session.NewStringSession(
			key, hash, dcID, newAddr, m.appID,
		).Encode()
	}

	sender, err := NewMTProto(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating new MTProto: %w", err)
	}

	sender.noRedirect = true
	sender.mode = m.mode
	sender.exported = true
	if isCdn {
		sender.cdn = true
		m.cdnKeysMu.RLock()
		if len(m.cdnKeys) > 0 {
			inherited := make(map[int32]*rsa.PublicKey, len(m.cdnKeys))
			maps.Copy(inherited, m.cdnKeys)
			sender.cdnKeys = inherited
		}
		m.cdnKeysMu.RUnlock()
	}

	if err := sender.CreateConnection(ctx, false, true); err != nil {
		sender.Terminate()
		return nil, fmt.Errorf("creating connection: %w", err)
	}

	return sender, nil
}

func reconnectDelay(base, maximum time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = defaultBaseReconnectDelay
	}
	if maximum <= 0 {
		maximum = 15 * time.Minute
	}
	delay := min(base, maximum)
	for i := 0; i < attempt && delay < maximum; i++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	return delay
}

func (m *MTProto) connectWithRetry(ctx context.Context) error {
	err := m.connect(ctx)
	if err == nil {
		m.connState.Attempts.Store(0)
		return nil
	}

	if m.connectionHandler != nil {
		m.Logger.Debug("delegating reconnection to custom handler")
		return m.connectionHandler(err)
	}

	for attempt := range m.connConfig.MaxAttempts {
		err := m.connect(ctx)
		if err == nil {
			if attempt > 0 {
				m.Logger.Info("reconnected successfully after %d attempts", attempt+1)
			}
			m.connState.Attempts.Store(0)
			return nil
		}

		if utils.IsFatalConnectionError(err) {
			return err
		}

		if m.terminated.Load() {
			return fmt.Errorf("mtproto terminated during reconnection")
		}
		if m.disconnected.Load() {
			return fmt.Errorf("mtproto disconnected during reconnection")
		}

		delay := reconnectDelay(m.connConfig.BaseDelay, m.connConfig.MaxDelay, attempt)

		m.Logger.Debug("reconnection failed (%d/%d): %v; retrying in %s", attempt+1, m.connConfig.MaxAttempts, err, delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			continue
		}
	}

	return fmt.Errorf("max reconnection attempts (%d) reached", m.connConfig.MaxAttempts)
}

// CreateConnection limits connection setup, including authentication,
// to ctx. The established connection survives cancellation after this returns.
// allowDisconnected is true for an explicit Connect/Reconnect and false for
// background recovery, which must never undo a user's Disconnect.
func (m *MTProto) CreateConnection(parent context.Context, withLog, allowDisconnected bool) (resultErr error) {
	defer func() {
		if resultErr != nil && parent.Err() != nil {
			resultErr = parent.Err()
		}
	}()
	if err := m.lifecycleMu.Lock(parent); err != nil {
		return err
	}
	defer m.lifecycleMu.Unlock()
	if m.terminated.Load() {
		return fmt.Errorf("mtproto is terminated, cannot create connection")
	}
	if allowDisconnected {
		m.disconnected.Store(false)
	} else if m.disconnected.Load() {
		return fmt.Errorf("mtproto is disconnected")
	}
	m.stopRoutines()
	m.routineswg.Wait()

	m.transportMu.Lock()
	m.transport = nil
	m.transportMu.Unlock()

	ctx, cancelfunc := context.WithCancel(context.Background())
	stopParent := context.AfterFunc(parent, cancelfunc)
	defer stopParent()
	m.ctxCancelMutex.Lock()
	m.ctxCancel = cancelfunc
	m.ctxCancelMutex.Unlock()

	committed := false
	defer func() {
		if committed {
			return
		}
		cancelfunc()
		m.tcpState.SetActive(false)
		m.transportMu.Lock()
		if m.transport != nil {
			m.transport.Close()
			m.transport = nil
		}
		m.transportMu.Unlock()
		m.routineswg.Wait()
	}()

	transportType := m.GetTransportType()
	if withLog {
		m.Logger.Info("connecting to %s (%s)", utils.FmtIP(m.GetAddr()), transportType)
	} else {
		m.Logger.Debug("connecting to %s (%s)", utils.FmtIP(m.GetAddr()), transportType)
	}

	err := m.connectWithRetry(ctx)
	if err != nil {
		m.Logger.WithError(err).Error("failed to create connection")
		return err
	}
	m.tcpState.SetActive(true)

	var localAddrLabel string
	if m.localAddr != "" {
		localAddrLabel = fmt.Sprintf("(-%s)", utils.FmtIP(m.localAddr))
	}

	var proxyLabel string
	if m.proxy != nil && m.proxy.Host != "" {
		proxyLabel = fmt.Sprintf("(~%s)", utils.FmtIP(m.proxy.Host))
	}

	transportType = m.GetTransportType()
	logMessage := fmt.Sprintf("connected to %s%s%s (%s)", localAddrLabel, proxyLabel, utils.FmtIP(m.GetAddr()), transportType)

	if withLog {
		m.Logger.Info(logMessage)
	} else {
		m.Logger.Debug(logMessage)
	}

	m.startReadingResponses(ctx)
	m.routineswg.Add(1)
	go m.serviceWriter(ctx)

	if !m.exported && !m.cdn {
		m.routineswg.Add(1)
		go m.longPing(ctx)
		if m.isHTTPTransport() {
			m.routineswg.Add(1)
			go m.httpWaiter(ctx)
		}
	}

	if !m.encrypted.Load() {
		m.Logger.Debug("generating new auth key")
		err = m.makeAuthKey(ctx, 0)
		if err != nil {
			return err
		}
		m.Logger.Debug("auth key generated")
	}

	if m.enablePFS && !m.cdn {
		m.startPFSManager(ctx)
	}

	if err := parent.Err(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (m *MTProto) connect(ctx context.Context) error {
	dcId := m.GetDC()
	m.Logger.Debug("init transport [%s] for DC%d", m.txType, dcId)

	var err error
	cfg := transport.CommonConfig{
		Ctx:         ctx,
		Host:        utils.FmtIP(m.GetAddr()),
		Timeout:     m.connConfig.Timeout,
		Socks:       m.proxy,
		LocalAddr:   m.localAddr,
		ModeVariant: uint8(m.mode),
		DC:          dcId,
		Logger:      m.Logger,
	}

	var newTransport transport.Transport
	switch m.txType {
	case TransportHTTP, TransportHTTPS:
		newTransport, err = transport.NewTransport(m, transport.HTTPConnConfig{
			CommonConfig: cfg,
			TLS:          m.txType == TransportHTTPS,
			Path:         m.httpPath,
		}, m.mode)
	case TransportWebSocket, TransportWebSocketTLS:
		newTransport, err = transport.NewTransport(m, transport.WSConnConfig{
			CommonConfig: cfg,
			TLS:          m.txType == TransportWebSocketTLS,
			TestMode:     m.testMode,
		}, m.mode)
	default:
		newTransport, err = transport.NewTransport(m, transport.TCPConnConfig{
			CommonConfig: cfg,
			IpV6:         m.IpV6,
			Obfuscated:   m.obfuscated,
		}, m.mode)
	}

	if err != nil {
		m.Logger.Debug("failed to create [%s] transport: %v", m.txType, err)
		return fmt.Errorf("creating transport: %w", err)
	}

	m.transportMu.Lock()
	m.transport = newTransport
	m.transportMu.Unlock()

	m.Logger.Trace("transport ready")

	if err := m.checkRapidReconnect(); err != nil {
		return err
	}

	return nil
}

func (m *MTProto) startPFSManager(ctx context.Context) {
	const defaultTempLifetimeSeconds int32 = 24 * 60 * 60
	const retryDelayOnError = 30 * time.Second
	const pollNoAuthKeyDelay = 5 * time.Second
	const minSleepSeconds int64 = 5

	tempLifetime := defaultTempLifetimeSeconds
	if m.pfsKeyLifetime > 0 {
		tempLifetime = m.pfsKeyLifetime
	}
	renewBeforeSeconds := min(max(int64(tempLifetime)/4, 5), 60)

	m.routineswg.Go(func() {
		defer m.Logger.Debug("PFS manager stopped")

		m.Logger.Debug("PFS manager started")
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// require permanent auth key first.
			m.authMu.RLock()
			hasPermanent := len(m.authKey) > 0
			expiresAt := m.tempAuthExpiresAt
			hasTemp := len(m.tempAuthKey) > 0
			m.authMu.RUnlock()
			if !hasPermanent {
				select {
				case <-ctx.Done():
					return
				case <-time.After(pollNoAuthKeyDelay):
				}
				continue
			}

			now := time.Now().Unix()
			needNew := !hasTemp || expiresAt == 0 || now >= expiresAt-renewBeforeSeconds

			if needNew {
				m.Logger.Debug("generating new temporary auth key for PFS")
				if err := m.createTempAuthKey(ctx, tempLifetime); err != nil {
					m.Logger.WithError(err).Error("failed to create temporary auth key")
					select {
					case <-ctx.Done():
						return
					case <-time.After(retryDelayOnError):
					}
					continue
				}

				if err := m.bindTempAuthKey(ctx); err != nil {
					m.Logger.WithError(err).Error("failed to bind temporary auth key")
					select {
					case <-ctx.Done():
						return
					case <-time.After(retryDelayOnError):
					}
					continue
				}

				// refresh local expiry after successful bind.
				m.authMu.RLock()
				expiresAt = m.tempAuthExpiresAt
				m.authMu.RUnlock()
			}

			// compute sleep until just before expiry.
			if expiresAt == 0 {
				// no expiry known (should not normally happen); poll later.
				select {
				case <-ctx.Done():
					return
				case <-time.After(pollNoAuthKeyDelay):
				}
				continue
			}

			waitSec := max(expiresAt-now-renewBeforeSeconds, minSleepSeconds)
			waitDur := time.Duration(waitSec) * time.Second

			select {
			case <-ctx.Done():
				return
			case <-time.After(waitDur):
			}
		}
	})
}

// MakeRequest sends an RPC request and waits for the response.
func (m *MTProto) MakeRequest(ctx context.Context, data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && m.reqTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.reqTimeout)
		defer cancel()
	}

	var pendingID int64
	cleanup := func() {
		if pendingID != 0 {
			m.responseChannels.Delete(pendingID)
			m.expectedTypes.Delete(pendingID)
			m.messageTracker.Delete(pendingID)
			m.messageTypesMap.Delete(pendingID)
			pendingID = 0
		}
	}
	defer cleanup()

	for attempt := 0; attempt < m.maxRequestAttempts; attempt++ {
		cleanup()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if m.terminated.Load() || m.disconnected.Load() {
			return nil, errors.New("client is disconnected")
		}

		m.transportMu.Lock()
		hasTransport := m.transport != nil
		m.transportMu.Unlock()
		if !hasTransport && !m.IsTcpActive() {
			if err := m.CreateConnection(ctx, false, false); err != nil {
				return nil, fmt.Errorf("establishing connection: %w", err)
			}
		}
		if err := m.tcpState.WaitForActive(ctx); err != nil {
			if ctx.Err() == nil && m.errorHandler != nil && m.errorHandler(fmt.Errorf("tcp inactive: %w", err)) {
				continue
			}
			return nil, fmt.Errorf("tcp inactive: %w", err)
		}

		respChan, msgID, err := m.sendPacket(ctx, data, 0, expectedTypes...)
		pendingID = msgID
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if utils.IsTransportError(err) {
				if m.terminated.Load() || m.disconnected.Load() {
					return nil, fmt.Errorf("transport closed: %w", err)
				}
				m.Logger.WithError(err).Trace("transport error for msgID=%d, reconnecting (attempt=%d/%d)", msgID, attempt+1, m.maxRequestAttempts)
				if err := m.Reconnect(ctx, false); err != nil {
					return nil, fmt.Errorf("reconnecting after transport error: %w", err)
				}
				continue
			}
			if m.errorHandler != nil && m.errorHandler(err) {
				continue
			}
			return nil, err
		}
		if isNullableResponse(data) {
			return tl.UnwrapNativeTypes(<-respChan), nil
		}

		start := time.Now()
		if msgID != 0 {
			m.messageTracker.Add(msgID, start.Unix())
			m.messageTypesMap.Store(msgID, fmt.Sprintf("%T", data))
			m.Logger.Trace("request sent: %T (msgID=%d, attempt=%d)", data, msgID, attempt+1)
		}

		var response tl.Object
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				count := m.connState.ConsecutiveTimeouts.Add(1)
				if count == 1 {
					m.connState.ConsecutiveTimeoutStart.Store(time.Now().Unix())
				}
				if count >= 5 {
					duration := time.Now().Unix() - m.connState.ConsecutiveTimeoutStart.Load()
					m.connState.ConsecutiveTimeouts.Store(0)
					if duration < 60 {
						m.Logger.Debug("5 consecutive timeouts in %ds; reconnecting", duration)
						m.requestReconnect()
					}
				}
			} else {
				m.connState.ConsecutiveTimeouts.Store(0)
			}
			m.Logger.Debug("request timeout: %T (msgID=%d, attempt=%d, elapsed=%s): %v", data, msgID, attempt+1, time.Since(start), ctx.Err())
			return nil, fmt.Errorf("request timeout: %w", ctx.Err())
		case resp, ok := <-respChan:
			if !ok {
				return nil, errors.New("response channel closed")
			}
			response = resp
		}

		m.connState.ConsecutiveTimeouts.Store(0)
		m.Logger.Trace("response received: %T -> %T (msgID=%d, latency=%s, attempt=%d)", data, response, msgID, time.Since(start), attempt+1)
		cleanup()

		switch r := response.(type) {
		case *objects.RpcError:
			var rpcError *ErrResponseCode
			errors.As(RpcErrorToNative(r, utils.FmtMethod(data)), &rpcError)
			if rpcError.Code == 303 {
				if strings.HasPrefix(rpcError.Message, "USER_MIGRATE_") || strings.HasPrefix(rpcError.Message, "PHONE_MIGRATE_") {
					if match := utils.RegexpDCMigrate.FindStringSubmatch(rpcError.Description); len(match) == 2 {
						if dcID, err := strconv.Atoi(match[1]); err == nil {
							if err := m.SwitchDc(ctx, dcID); err == nil {
								if m.onMigration != nil {
									m.onMigration()
								}
								return nil, &errorDCMigrated{int32(dcID)}
							} else if ctx.Err() != nil {
								return nil, ctx.Err()
							}
						}
					}
				}
				return nil, rpcError
			}
			if strings.Contains(rpcError.Message, "FLOOD_WAIT_") || strings.Contains(rpcError.Message, "FLOOD_PREMIUM_WAIT_") {
				if m.floodHandler != nil && m.floodHandler(rpcError) {
					continue
				}
				return nil, rpcError
			}
			return nil, rpcError
		case *errorSessionConfigsChanged:
			m.Logger.Trace("session config changed, retrying request")
			continue
		default:
			return tl.UnwrapNativeTypes(response), nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("maximum request attempts exceeded (%d)", m.maxRequestAttempts)
}
func (m *MTProto) InvokeRequestWithoutUpdate(ctx context.Context, data tl.Object, expectedTypes ...reflect.Type) error {
	_, msgID, err := m.sendPacket(ctx, data, 0, expectedTypes...)
	if msgID != 0 {
		m.responseChannels.Delete(msgID)
		m.expectedTypes.Delete(msgID)
	}
	if err != nil {
		return fmt.Errorf("sending packet: %w", err)
	}
	return err
}

func (m *MTProto) IsTcpActive() bool {
	return m.tcpState.GetActive()
}

func (m *MTProto) stopRoutines() {
	m.ctxCancelMutex.Lock()
	if m.ctxCancel != nil {
		m.ctxCancel()
	}
	m.ctxCancelMutex.Unlock()

	m.transportMu.Lock()
	tr := m.transport
	m.transportMu.Unlock()

	if tr != nil {
		tr.Close()
	}

	m.notifyPendingRequestsOfConfigChange()
}

func (m *MTProto) Disconnect() error {
	m.disconnected.Store(true)
	m.tcpState.SetActive(false)
	m.stopRoutines()

	_ = m.lifecycleMu.Lock(context.Background())
	defer m.lifecycleMu.Unlock()
	m.disconnected.Store(true)
	m.stopRoutines()
	m.routineswg.Wait()
	m.transportMu.Lock()
	m.transport = nil
	m.transportMu.Unlock()
	m.Logger.Trace("all routines stopped gracefully")
	return nil
}

func (m *MTProto) Terminate() error {
	m.terminated.Store(true)
	m.disconnected.Store(true)
	m.tcpState.SetActive(false)
	m.stopRoutines()

	_ = m.lifecycleMu.Lock(context.Background())
	defer m.lifecycleMu.Unlock()
	m.terminated.Store(true)
	m.disconnected.Store(true)
	m.stopRoutines()
	m.routineswg.Wait()
	m.responseChannels.Close()
	m.transportMu.Lock()
	m.transport = nil
	m.transportMu.Unlock()
	return nil
}

func (m *MTProto) SetTerminated(val bool) { m.terminated.Store(val) }

// Reconnect applies ctx to waiting for and establishing a connection.
func (m *MTProto) Reconnect(ctx context.Context, loggy bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.terminated.Load() {
		return nil
	}
	if !m.connState.InProgress.CompareAndSwap(false, true) {
		m.Logger.Trace("reconnect already in progress")
		return nil
	}
	defer m.connState.InProgress.Store(false)

	addr := utils.FmtIP(m.GetAddr())
	tx := m.GetTransportType()
	start := time.Now()
	log := m.Logger.Debug
	if loggy {
		log = m.Logger.Info
	}
	log("reconnecting to %s (%s)", addr, tx)

	if err := m.CreateConnection(ctx, loggy, true); err != nil {
		m.Logger.WithError(err).Error("failed to recreate connection")
		return fmt.Errorf("recreating connection: %w", err)
	}

	log("reconnected to %s (%s) in %v", addr, tx, time.Since(start))
	m.Ping(ctx)
	return nil
}

func (m *MTProto) requestReconnect() {
	if m.terminated.Load() || m.disconnected.Load() {
		return
	}
	if !m.connState.InProgress.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer m.connState.InProgress.Store(false)
		ctx := context.Background()
		if err := m.CreateConnection(ctx, false, false); err != nil {
			m.Logger.WithError(err).Trace("auto-reconnect aborted")
			return
		}
		m.Ping(ctx)
	}()
}

// keep pinging to keep the connection alive
func (m *MTProto) longPing(ctx context.Context) {
	defer m.routineswg.Done()

	ticker := time.NewTicker(defaultPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.tcpState.WaitForActive(ctx); err != nil {
				return
			}
			m.Ping(ctx)
		}
	}
}

func (m *MTProto) isHTTPTransport() bool {
	m.transportMu.Lock()
	t := m.transport
	m.transportMu.Unlock()
	if t == nil {
		return m.txType.isHTTP()
	}
	h, ok := t.(transport.HTTPLike)
	return ok && h.IsHTTP()
}

func (m *MTProto) httpWaiter(ctx context.Context) {
	defer m.routineswg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !m.encrypted.Load() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		if err := m.tcpState.WaitForActive(ctx); err != nil {
			return
		}
		_, _, err := m.sendPacket(ctx, &objects.HttpWaitParams{
			MaxDelay:  0,
			WaitAfter: 0,
			MaxWait:   25000,
		}, 0)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(25 * time.Second):
		}
	}
}

func (m *MTProto) Ping(ctx context.Context) time.Duration {
	if !m.IsTcpActive() {
		m.Logger.Debug("ping skipped: transport unavailable")
		return 0
	}
	start := time.Now()
	m.Logger.Trace("sending ping")
	if err := m.InvokeRequestWithoutUpdate(ctx, &utils.PingParams{
		PingID: time.Now().Unix(),
	}); err != nil {
		m.Logger.Debug("ping failed: %v", err)
		return -1
	}
	return time.Since(start)
}

// checkRapidReconnect detects rapid reconnection loops that indicate connection instability
// Uses exponential backoff naturally via reconnectAttempts counter
func (m *MTProto) checkRapidReconnect() error {
	now := time.Now().Unix()
	last := m.connState.LastSuccessfulConnect.Load()
	attempts := m.connState.Attempts.Load()

	if last > 0 && (now-last) < 5 && attempts >= 10 {
		m.Logger.Warn("rapid reconnection loop detected: %d attempts within 5 seconds", attempts)
		if m.proxy != nil && m.proxy.Type == "mtproxy" {
			return fmt.Errorf("mtproxy connection loop detected: connection succeeds but immediately closes - check proxy configuration, secret, or server availability")
		}
		return fmt.Errorf("rapid reconnection loop detected: connection succeeds but immediately closes - possible network or server issue")
	}

	if last > 0 && (now-last) > 30 {
		m.connState.Attempts.Store(0)
	}

	m.connState.LastSuccessfulConnect.Store(now)
	return nil
}

// isBrokenError checks if an error should trigger a reconnection
func isBrokenError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "unexpected error: unexpected EOF") ||
		strings.Contains(errStr, "required to reconnect!") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "connection was aborted") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "i/o timeout") ||
		err == io.EOF
}

func (m *MTProto) startReadingResponses(ctx context.Context) {
	m.routineswg.Add(1)
	go func() {
		defer m.routineswg.Done()
		defer m.Logger.Trace("read responses goroutine exited")

		m.Logger.Trace("read responses goroutine started")

		for {
			select {
			case <-ctx.Done():
				m.Logger.Trace("read responses context canceled")
				return
			default:
			}

			if err := m.tcpState.WaitForActive(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					m.Logger.Trace("tcp wait canceled, exiting read loop")
					return
				}
				m.Logger.Trace("tcp wait error: %v", err)
				continue
			}

			err := m.readMsg(ctx)
			if err == nil {
				continue
			}

			if errors.Is(err, context.Canceled) {
				m.Logger.Trace("read message context canceled")
				return
			}

			if isBrokenError(err) {
				if m.connState.InProgress.Load() {
					m.Logger.Trace("connection error but reconnect in progress, sleeping: %v", err)
					time.Sleep(50 * time.Millisecond)
					continue
				}

				m.Logger.Trace("connection lost: %v; reconnecting", err)
				m.requestReconnect()
				return
			}

			m.Logger.Trace("error reading message: %v", err)
			var respErr *ErrResponseCode
			var transErr transport.ErrCode
			switch {
			case errors.As(err, &respErr):
				if respErr.Code == -404 {
					if authErr := m.handle404Error(); authErr != nil {
						m.Logger.Error("auth key error: %v", authErr)
						return
					}
				} else {
					m.Logger.Debug("transport response error code: %d - %s", respErr.Code, respErr.Error())
				}
			case errors.As(err, &transErr):
				m.Logger.Debug("transport error code: %d - %s", int64(transErr), transErr.Error())
			default:
				if !m.terminated.Load() {
					if strings.Contains(err.Error(), "object with provided crc") {
						m.Logger.Warn(FormatDecodeError(err))
					} else if !m.connState.InProgress.Load() {
						m.Logger.Trace("read error: %v; reconnecting", err)
						m.requestReconnect()
						return
					} else {
						m.Logger.Trace("error during active reconnect, waiting")
						time.Sleep(50 * time.Millisecond)
					}
				}
			}
		}
	}()
}

func (m *MTProto) handle404Error() error {
	count := m.authKey404Count.Load()
	lastTime := m.authKey404Time.Load()

	if count == 0 && lastTime == 0 {
		m.authKey404Count.Store(1)
		m.authKey404Time.Store(time.Now().Unix())
	} else {
		currentTime := time.Now().Unix()
		if currentTime-lastTime < 2 { // time frame to check if the error is repeating
			m.authKey404Count.Add(1)
			count = m.authKey404Count.Load()
		} else {
			m.authKey404Count.Store(1)
			m.authKey404Time.Store(currentTime)
			count = 1
		}
	}

	if count > 4 && count < 16 {
		m.Logger.Debug("auth key error occurred %d times, reconnecting", count)
		m.requestReconnect()
	} else if count >= 16 {
		m.errorHandler(ErrAuthKeyInvalid)
		return ErrAuthKeyInvalid
	}
	return nil
}

func (m *MTProto) readMsg(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("MTProto instance is nil")
	}

	m.transportMu.Lock()
	t := m.transport
	m.transportMu.Unlock()

	if t == nil {
		return fmt.Errorf("must setup connection before reading messages")
	}

	response, err := t.ReadMsg()
	if err != nil {
		var e transport.ErrCode
		if errors.As(err, &e) {
			return &ErrResponseCode{Code: int64(e)}
		}
		switch {
		case err == io.EOF, errors.Is(err, context.Canceled):
			return err
		default:
			return fmt.Errorf("reading message: %w", err)
		}
	}
	if encrypted, ok := response.(*messages.Encrypted); ok && encrypted.SessionID != m.GetSessionID() {
		return errors.New("incoming message belongs to a different session")
	}
	if _, plain := response.(*messages.Unencrypted); plain && m.encrypted.Load() {
		return errors.New("unencrypted message received after authorization")
	}

	if m.serviceModeActivated.Load() {
		var obj tl.Object
		obj, err = tl.DecodeUnknownObject(response.GetMsg())
		if err != nil {
			return fmt.Errorf("parsing object: %w", err)
		}
		select {
		case m.serviceChannel <- obj:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	err = m.processResponse(false, response)
	if err != nil {
		m.Logger.Debug("decoding unknown object: %v", err)
		return fmt.Errorf("incoming update: %w", err)
	}
	return nil
}

func (m *MTProto) processResponse(inContainer bool, msg messages.Common) error {
	if msg.GetMsgID()&1 == 0 || msg.GetSeqNo() < 0 {
		return errors.New("invalid server message ID or sequence number")
	}
	var data tl.Object
	var err error

	hintID := msg.GetMsgID()
	// rpc_result refers to the outgoing request ID; its envelope has a new,
	// unrelated server message ID.
	if body := msg.GetMsg(); len(body) >= 12 && binary.LittleEndian.Uint32(body[:4]) == objects.CrcRpcResult {
		hintID = int64(binary.LittleEndian.Uint64(body[4:12]))
	}
	if et, ok := m.expectedTypes.Get(hintID); ok && len(et) > 0 {
		data, err = tl.DecodeUnknownObject(msg.GetMsg(), et...)
	} else {
		data, err = tl.DecodeUnknownObject(msg.GetMsg())
	}
	if err != nil {
		return fmt.Errorf("unmarshaling response: %w", err)
	}
	if err := m.validateMessageTime(msg.GetMsgID(), data); err != nil {
		return err
	}
	if msg.GetSeqNo()&1 != 0 {
		if err := m.queueAck(msg.GetMsgID()); err != nil {
			return err
		}
	}
	if m.pendingAcks.Len() >= defaultPendingAcksThreshold {
		select {
		case m.acksReady <- struct{}{}:
		default:
		}
	}
	if !m.receivedIDs.remember(msg.GetMsgID()) {
		return nil
	}

messageTypeSwitching:
	if salts, ok := data.(*objects.FutureSalts); ok && m.responseChannels.Has(salts.ReqMsgID) {
		return m.writeRPCResponse(salts.ReqMsgID, salts)
	}
	switch message := data.(type) {
	case *objects.MessageContainer:
		if inContainer {
			return errors.New("nested message container")
		}
		for _, v := range *message {
			if v.MsgID >= msg.GetMsgID() || int(v.SeqNo) > msg.GetSeqNo() {
				return errors.New("container message ID or sequence number precedes its contents")
			}
		}
		for _, v := range *message {
			if outer, ok := msg.(*messages.Encrypted); ok {
				v.AuthKeyHash = outer.AuthKeyHash
			}
			err := m.processResponse(true, v)
			if err != nil {
				return fmt.Errorf("processing item in container: %w", err)
			}
		}

	case *objects.BadServerSalt:
		m.updateSalt(msg, message.NewSalt)
		if err := m.SaveSession(m.memorySession); err != nil {
			m.Logger.Debug("failed to save session: %v", err)
		}
		m.notifyPendingRequestsOfConfigChange()

	case *objects.NewSessionCreated:
		m.updateSalt(msg, message.ServerSalt)
		if err := m.SaveSession(m.memorySession); err != nil {
			m.Logger.Debug("failed to save session: %v", err)
		}

	case *objects.MsgsNewDetailedInfo:
		return m.queueAck(message.AnswerMsgID)

	case *objects.MsgsDetailedInfo:
		return m.queueAck(message.AnswerMsgID)

	case *objects.MsgsStateReq:
		info := make([]byte, len(message.MsgIDs))
		for i, id := range message.MsgIDs {
			if m.receivedIDs.contains(id) {
				info[i] = 0x04
			} else {
				info[i] = 0x01
			}
		}
		select {
		case m.serviceMessages <- &objects.MsgsStateInfo{ReqMsgID: msg.GetMsgID(), Info: info}:
		default:
			return errors.New("service response queue full")
		}
		return nil

	case *objects.MsgResendReq:
		for _, id := range message.MsgIDs {
			// The server asks for our outbound requests; acknowledging their IDs
			// as inbound messages is invalid. Wake only the affected callers.
			if m.responseChannels.Has(id) {
				_ = m.writeRPCResponse(id, &errorSessionConfigsChanged{})
			}
		}
		return nil

	case *objects.MsgsAllInfo:
		return nil

	case *objects.Pong:
		if m.responseChannels.Has(message.MsgID) {
			return m.writeRPCResponse(message.MsgID, message)
		}
		if !m.exported && !m.cdn {
			m.Logger.Debug("received pong (id=%d)", message.PingID)
		} else {
			m.Logger.Trace("received pong")
		}

	case *objects.MsgsAck:
		// do nothing

	case *objects.BadMsgNotification:
		badMsg := BadMsgErrorFromNative(message)
		if badMsg.Code == 16 || badMsg.Code == 17 {
			// calculate offset from server's message ID
			serverTime := msg.GetMsgID() >> 32
			localTime := time.Now().Unix()
			if offset := serverTime - localTime; offset != 0 {
				m.timeOffset.Store(offset)
				m.Logger.Warn("system clock offset detected: %d seconds, auto-correcting", offset)
			}
			m.notifyPendingRequestsOfConfigChange()
			return nil
		}

		if badMsg.Code == 32 || badMsg.Code == 33 {
			m.notifyPendingRequestsOfConfigChange()
			return nil
		}
		m.Logger.Debug("bad-msg-notification: code=%d msg=%s", badMsg.Code, badMsg.Error())
		return badMsg

	case *objects.RpcResult:
		obj := message.Obj
		if v, ok := obj.(*objects.GzipPacked); ok {
			obj = v.Obj
		}
		m.Logger.Trace(" RPC < %T (msgID=%d)", obj, message.ReqMsgID)
		m.handlersMu.RLock()
		handlers := append([]func(any){}, m.rpcResponseHandlers...)
		m.handlersMu.RUnlock()
		for _, f := range handlers {
			f(obj)
		}
		err := m.writeRPCResponse(message.ReqMsgID, obj)
		if err != nil {
			if strings.Contains(err.Error(), "no response channel found") {
				m.Logger.Debug("writing rpc response: %v", err)
			} else {
				return fmt.Errorf("writing rpc response: %w", err)
			}
		}

	case *objects.GzipPacked:
		// sometimes telegram server returns gzip for unknown reason. so, we are extracting data from gzip and
		// reprocess it again
		data = message.Obj
		goto messageTypeSwitching

	default:
		processed := false
		m.handlersMu.RLock()
		handlers := append([]func(any) bool{}, m.serverRequestHandlers...)
		m.handlersMu.RUnlock()
		for _, f := range handlers {
			processed = f(message)
			if processed {
				break
			}
		}
		if !processed {
			m.Logger.Trace("unhandled update: %T", message)
		}
	}

	return nil
}

// notifyPendingRequestsOfConfigChange notifies all pending requests that session config changed
// Used when server salt changes and requests need to be resent
func (m *MTProto) notifyPendingRequestsOfConfigChange() {
	old := m.responseChannels.SwapAndClear()
	for msgID, ch := range old {
		m.expectedTypes.Delete(msgID)
		select {
		case ch <- &errorSessionConfigsChanged{}:
		default:
		}
	}
}

// TcpState represents a simple concurrency-safe state machine
// that can be either active or inactive.
// When the state becomes active, all goroutines waiting on WaitForActive()
// are released (via channel close).
// When the state becomes inactive again, a new channel is created for future waits.
type TcpState struct {
	mu     sync.RWMutex
	active bool
	ch     chan struct{}
}

func (m *MTProto) TcpState() *TcpState {
	return m.tcpState
}

// GetActive safely returns the current active flag.
// It can be called concurrently with other methods.
func (m *TcpState) GetActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// SetActive updates the active flag.
// - If switching from false → true, the channel is closed to notify all waiters.
// - If switching from true → false, a new channel is created for future waits.
func (m *TcpState) SetActive(active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// No state change → nothing to do
	if m.active == active {
		return
	}

	m.active = active

	if active {
		// Closing the channel releases all current waiters
		close(m.ch)
	} else {
		// Create a new channel for the next waiting round
		m.ch = make(chan struct{})
	}
}

// WaitForActive blocks until the TcpState becomes active or
// until the provided context is canceled.
// Returns nil when the state is active, or ctx.Err() if canceled.
func (m *TcpState) WaitForActive(ctx context.Context) error {
	for {
		m.mu.RLock()
		active := m.active
		ch := m.ch
		m.mu.RUnlock()

		if active {
			return nil
		}

		select {
		case <-ch: // Unblocked when SetActive(true) closes the channel
			// Channel was closed, re-check state in case it changed
			m.mu.RLock()
			stillActive := m.active
			m.mu.RUnlock()
			if stillActive {
				return nil
			}
			// State changed back to inactive, loop again
			continue
		case <-ctx.Done(): // Context canceled or timed out
			return ctx.Err()
		}
	}
}

func NewTcpState() *TcpState {
	return &TcpState{
		ch: make(chan struct{}),
	}
}

// contextMutex lets canceled callers leave a serialized network operation
// without starting a goroutine to wait for a mutex.
type contextMutex struct {
	once sync.Once
	gate chan struct{}
}

func (m *contextMutex) Lock(ctx context.Context) error {
	m.once.Do(func() { m.gate = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *contextMutex) Unlock() { <-m.gate }

// Keep the highest 1024 received IDs, independently of the acknowledgment
// queue. Clearing sent acknowledgments must not permit replayed updates.
type receivedMessageWindow struct {
	mu   sync.Mutex
	ids  map[int64]struct{}
	heap []int64
}

func (w *receivedMessageWindow) remember(id int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ids == nil {
		w.ids = make(map[int64]struct{})
	}
	if _, ok := w.ids[id]; ok {
		return false
	}
	if len(w.heap) == 1024 {
		if id < w.heap[0] {
			return false
		}
		delete(w.ids, w.heap[0])
		w.heap[0] = id
		for i := 0; ; {
			child := 2*i + 1
			if child >= len(w.heap) {
				break
			}
			if child+1 < len(w.heap) && w.heap[child+1] < w.heap[child] {
				child++
			}
			if w.heap[i] <= w.heap[child] {
				break
			}
			w.heap[i], w.heap[child] = w.heap[child], w.heap[i]
			i = child
		}
	} else {
		w.heap = append(w.heap, id)
		for i := len(w.heap) - 1; i > 0; {
			parent := (i - 1) / 2
			if w.heap[parent] <= w.heap[i] {
				break
			}
			w.heap[parent], w.heap[i] = w.heap[i], w.heap[parent]
			i = parent
		}
	}
	w.ids[id] = struct{}{}
	return true
}

func (w *receivedMessageWindow) contains(id int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.ids[id]
	return ok
}
func (w *receivedMessageWindow) clear() { w.mu.Lock(); defer w.mu.Unlock(); w.ids = nil; w.heap = nil }

func (m *MTProto) isMatchedServiceResponse(obj tl.Object) bool {
	var id int64
	switch v := obj.(type) {
	case *objects.RpcResult:
		id = v.ReqMsgID
	case *objects.BadServerSalt:
		id = v.BadMsgID
	case *objects.BadMsgNotification:
		id = v.BadMsgID
	case *objects.GzipPacked:
		return m.isMatchedServiceResponse(v.Obj)
	case *objects.MessageContainer:
		for _, msg := range *v {
			b := msg.GetMsg()
			if len(b) < 12 {
				continue
			}
			switch binary.LittleEndian.Uint32(b[:4]) {
			case objects.CrcRpcResult, (&objects.BadServerSalt{}).CRC(), (&objects.BadMsgNotification{}).CRC():
				if m.responseChannels.Has(int64(binary.LittleEndian.Uint64(b[4:12]))) {
					return true
				}
			}
		}
	}
	return id != 0 && m.responseChannels.Has(id)
}

func (m *MTProto) validateMessageTime(id int64, obj tl.Object) error {
	seconds := id >> 32
	now := time.Now().Unix() + m.timeOffset.Load()
	if seconds < now-300 || seconds > now+30 {
		if !m.isMatchedServiceResponse(obj) {
			return fmt.Errorf("server message time outside allowed window")
		}
	}
	return nil
}

func (m *MTProto) serviceWriter(ctx context.Context) {
	defer m.routineswg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var request tl.Object
		select {
		case <-ctx.Done():
			return
		case request = <-m.serviceMessages:
		case <-ticker.C:
		case <-m.acksReady:
		}
		if request != nil {
			if _, _, err := m.sendPacket(ctx, request, 0); err != nil {
				m.Logger.Debug("service response: %v", err)
				m.requestReconnect()
				return
			}
		}
		ids := m.pendingAcks.Keys()
		for start := 0; start < len(ids); start += maxMsgsAckPerBatch {
			batch := ids[start:min(start+maxMsgsAckPerBatch, len(ids))]
			if _, _, err := m.sendPacket(ctx, &objects.MsgsAck{MsgIDs: batch}, 0); err != nil {
				m.Logger.Debug("sending acknowledgments: %v", err)
				m.requestReconnect()
				return
			}
			for _, id := range batch {
				m.pendingAcks.Delete(id)
			}
		}
	}
}

func (m *MTProto) queueAck(id int64) error {
	if m.pendingAcks.Has(id) {
		return nil
	}
	if m.pendingAcks.Len() >= 4096 {
		return errors.New("acknowledgment queue full")
	}
	m.pendingAcks.Add(id)
	if m.pendingAcks.Len() >= defaultPendingAcksThreshold {
		select {
		case m.acksReady <- struct{}{}:
		default:
		}
	}
	return nil
}
