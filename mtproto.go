// Copyright (c) 2025 @AmarnathCJD

package gogram

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
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
)

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
	proxy     *utils.Proxy
	transport transport.Transport
	localAddr string

	ctxCancel      context.CancelFunc
	ctxCancelMutex sync.Mutex
	routineswg     sync.WaitGroup
	memorySession  bool
	tcpState       *TcpState
	timeOffset     atomic.Int64
	reqTimeout     time.Duration
	mode           mode.Variant
	DcList         *utils.DCOptions
	transportMu    sync.Mutex

	authKey []byte

	authKeyHash []byte

	tempAuthKey       []byte
	tempAuthKeyHash   []byte
	tempAuthExpiresAt int64

	noRedirect bool

	serverSalt atomic.Int64
	encrypted  atomic.Bool
	sessionId  atomic.Int64

	responseChannels *utils.SyncIntObjectChan
	expectedTypes    *utils.SyncIntReflectTypes
	pendingAcks      *utils.SyncSet[int64]

	genMsgID     func(int64) int64
	currentSeqNo atomic.Int32

	sessionStorage session.SessionLoader

	publicKey *rsa.PublicKey
	cdnKeysMu sync.RWMutex
	cdnKeys   map[int32]*rsa.PublicKey

	serviceChannel       chan tl.Object
	serviceModeActivated bool

	authKey404 [2]int64
	IpV6       bool

	Logger *utils.Logger

	serverRequestHandlers []func(i any) bool
	floodHandler          func(err error) bool
	errorHandler          func(err error) bool
	connectionHandler     func(err error) error
	exported              bool
	cdn                   bool
	terminated            atomic.Bool
	senderCounters        sync.Map // map[int]int32 - tracks sender count per DC

	connConfig ReconnectConfig
	connState  ReconnectState

	useWebSocket    bool
	useWebSocketTLS bool
	enablePFS       bool

	onMigration func()

	messageTracker  *utils.SyncIntInt64
	messageTypesMap sync.Map // msgID -> request type name
	maxRetryDepth   int      // Maximum retry depth to prevent stack overflow
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

	ServerHost      string         // Telegram server address (IP:port)
	PublicKey       *rsa.PublicKey // RSA public key for server verification
	DataCenter      int            // Data center ID (1-5)
	Logger          *utils.Logger  // Logger instance
	Proxy           *utils.Proxy   // Proxy configuration
	Mode            string         // Transport mode (Abridged, Intermediate, Full)
	Ipv6            bool           // Prefer IPv6 connections
	CustomHost      bool           // Use custom ServerHost instead of DC lookup
	LocalAddr       string         // Local address to bind (IP:port)
	Timeout         int            // TCP connection timeout (seconds)
	ReqTimeout      int            // RPC request timeout (seconds)
	UseWebSocket    bool           // Use WebSocket transport
	UseWebSocketTLS bool           // Use secure WebSocket (wss://)

	MaxReconnectAttempts int           // Max reconnection attempts (default: 2000)
	BaseReconnectDelay   time.Duration // Initial reconnect delay (default: 2s)
	MaxReconnectDelay    time.Duration // Maximum reconnect delay (default: 15m)

	OnMigration func() // Called after DC migration completes
}

func NewMTProto(c Config) (*MTProto, error) {
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
			// if the error is not because of file not found or path not found, return the error
			// else, continue with the execution
			// check if you have write permission in the directory
			if _, err := os.OpenFile(filepath.Dir(c.AuthKeyFile), os.O_WRONLY, 0222); err != nil {
				return nil, fmt.Errorf("check if you have write permission in the directory: %w", err)
			}
			return nil, fmt.Errorf("loading session: %w", err)
		}
	}
	if c.Logger == nil {
		c.Logger = utils.NewLogger("gogram [mtproto]").SetLevel(utils.InfoLevel)
	}

	mtproto := &MTProto{
		sessionStorage:        c.SessionStorage,
		serviceChannel:        make(chan tl.Object),
		publicKey:             c.PublicKey,
		responseChannels:      utils.NewSyncIntObjectChan(),
		expectedTypes:         utils.NewSyncIntReflectTypes(),
		pendingAcks:           utils.NewSyncSet[int64](),
		genMsgID:              utils.NewMsgIDGenerator(),
		serverRequestHandlers: make([]func(i any) bool, 0),
		Logger:                c.Logger,
		memorySession:         c.MemorySession,
		appID:                 c.AppID,
		proxy:                 c.Proxy,
		localAddr:             c.LocalAddr,
		floodHandler:          func(err error) bool { return false },
		errorHandler:          func(err error) bool { return false },
		reqTimeout:            utils.MinSafeDuration(c.ReqTimeout),
		mode:                  parseTransportMode(c.Mode),
		IpV6:                  c.Ipv6,
		tcpState:              NewTcpState(),
		DcList:                utils.NewDCOptions(),
		connConfig: ReconnectConfig{
			Timeout:     utils.MinSafeDuration(c.Timeout),
			MaxDelay:    utils.OrDefault(c.MaxReconnectDelay, 15*time.Minute),
			MaxAttempts: utils.OrDefault(c.MaxReconnectAttempts, defaultMaxReconnectAttempts),
			BaseDelay:   utils.OrDefault(c.BaseReconnectDelay, defaultBaseReconnectDelay),
		},
		useWebSocket:    c.UseWebSocket,
		useWebSocketTLS: c.UseWebSocketTLS,
		enablePFS:       c.EnablePFS,
		onMigration:     c.OnMigration,
		messageTracker:  utils.NewSyncIntInt64(),
		maxRetryDepth:   10,
	}

	mtproto.SetAddr(c.ServerHost)
	mtproto.encrypted.Store(false)
	mtproto.sessionId.Store(utils.GenerateSessionID())
	mtproto.connState.Attempts.Store(0)

	mtproto.Logger.Debug("initializing MTProto client")

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
	case "modeAbridged":
		return mode.Abridged
	case "modeFull":
		return mode.Full
	case "modeIntermediate":
		return mode.Intermediate
	case "modePaddedIntermediate":
		return mode.PaddedIntermediate
	default:
		return mode.Abridged
	}
}

func (m *MTProto) LoadSession(sess *session.Session) error {
	m.SetAddr(sess.Hostname)
	m.authKey = sess.Key
	m.authKeyHash = sess.Hash
	m.appID = sess.AppID
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
		m._loadSession(sess)
	}
	return nil
}

func (m *MTProto) ExportAuth() (*session.Session, int) {
	return &session.Session{
		Key:      m.authKey,
		Hash:     m.authKeyHash,
		Salt:     m.serverSalt.Load(),
		Hostname: m.GetAddr(),
		AppID:    m.AppID(),
	}, m.GetDC()
}

func (m *MTProto) ImportRawAuth(authKey, authKeyHash []byte, addr string, appID int32) (bool, error) {
	m.SetAddr(addr)
	m.authKey = authKey
	m.authKeyHash = authKeyHash
	m.appID = appID
	m.Logger.Debug("importing raw authentication credentials")
	if err := m.SaveSession(m.memorySession); err != nil {
		return false, fmt.Errorf("saving session: %w", err)
	}
	if err := m.Reconnect(false); err != nil {
		return false, fmt.Errorf("reconnecting: %w", err)
	}
	return true, nil
}

func (m *MTProto) ImportAuth(stringSession string) (bool, error) {
	sessionString := session.NewEmptyStringSession()
	if err := sessionString.Decode(stringSession); err != nil {
		return false, err
	}
	m.authKey = sessionString.AuthKey
	m.authKeyHash = sessionString.AuthKeyHash
	m.SetAddr(sessionString.IpAddr)

	if m.appID == 0 {
		m.appID = sessionString.AppID
	}
	m.Logger.Debug("importing session from string: %s", utils.FmtIP(sessionString.IpAddr))
	if err := m.SaveSession(m.memorySession); err != nil {
		return false, fmt.Errorf("saving session: %w", err)
	}
	return true, nil
}

func (m *MTProto) GetDC() int {
	return m.DcList.SearchAddr(m.GetAddr())
}

func (m *MTProto) SetAddr(addr string) {
	m.Addr.Store(addr)
}

func (m *MTProto) GetAddr() string {
	return m.Addr.Load().(string)
}

func (m *MTProto) GetTransportType() string {
	if m.useWebSocket {
		if m.useWebSocketTLS {
			return "Wss"
		}
		return "Ws"
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
	m.cdnKeys = keys
}

func (m *MTProto) HasCdnKey(dc int32) (*rsa.PublicKey, bool) {
	m.cdnKeysMu.RLock()
	defer m.cdnKeysMu.RUnlock()
	key, ok := m.cdnKeys[dc]
	return key, ok
}

func (m *MTProto) SwitchDc(dc int) error {
	if m.noRedirect {
		return nil
	}
	newAddr := m.DcList.GetHostIP(dc, false, m.IpV6)
	if newAddr == "" {
		return fmt.Errorf("dc %d not found in dc list", dc)
	}

	m.Logger.Debug("initiating migration to DC%d", dc)

	m.connState.InProgress.Store(true)
	defer m.connState.InProgress.Store(false)

	if err := m.Disconnect(); err != nil {
		return err
	}

	if err := m.sessionStorage.Delete(); err != nil {
		return err
	}
	m.Logger.Debug("cleared old session data")

	m.authKey = nil
	m.authKeyHash = nil
	m.serverSalt.Store(0)
	m.encrypted.Store(false)
	m.sessionId.Store(utils.GenerateSessionID())

	m.tempAuthKey = nil
	m.tempAuthKeyHash = nil
	m.tempAuthExpiresAt = 0

	m.responseChannels = utils.NewSyncIntObjectChan()
	m.expectedTypes = utils.NewSyncIntReflectTypes()
	m.pendingAcks = utils.NewSyncSet[int64]()
	m.currentSeqNo.Store(0)

	m.authKey404 = [2]int64{0, 0}
	m.authKey404 = [2]int64{0, 0}
	m.connState.Attempts.Store(0)
	m.connState.ConsecutiveTimeouts.Store(0)
	m.connState.LastSuccessfulConnect.Store(0)
	m.SetAddr(newAddr)

	m.Logger.Info("migrated to DC%d (%s)", dc, newAddr)
	m.Logger.Debug("establishing connection to DC%d", dc)

	errConn := m.CreateConnection(true)
	if errConn != nil {
		return fmt.Errorf("creating connection: %w", errConn)
	}

	return nil
}

func (m *MTProto) ExportNewSender(dcID int, mem bool, cdn ...bool) (*MTProto, error) {
	newAddr := m.DcList.GetHostIP(dcID, false, m.IpV6)

	var senderNum int32
	if val, ok := m.senderCounters.Load(dcID); ok {
		senderNum = val.(int32) + 1
	} else {
		senderNum = 1
	}
	m.senderCounters.Store(dcID, senderNum)

	var loggerPrefix string
	if len(cdn) > 0 && cdn[0] {
		newAddr, _ = m.DcList.GetCDNAddr(dcID)
		loggerPrefix = fmt.Sprintf("gogram [cdn>>dc%d#%d]", dcID, senderNum)
	} else {
		loggerPrefix = fmt.Sprintf("gogram [sender>>dc%d#%d]", dcID, senderNum)
	}

	logger := utils.NewLogger(loggerPrefix).SetLevel(utils.InfoLevel)

	cfg := Config{
		DataCenter:      dcID,
		PublicKey:       m.publicKey,
		ServerHost:      newAddr,
		AuthKeyFile:     "__exp_" + strconv.Itoa(dcID) + ".dat",
		MemorySession:   mem,
		Logger:          logger,
		Proxy:           m.proxy,
		LocalAddr:       m.localAddr,
		AppID:           m.appID,
		Ipv6:            m.IpV6,
		Timeout:         int(m.connConfig.Timeout.Seconds()),
		ReqTimeout:      int(m.reqTimeout.Seconds()),
		UseWebSocket:    m.useWebSocket,
		UseWebSocketTLS: m.useWebSocketTLS,
	}

	if dcID == m.GetDC() {
		cfg.SessionStorage = m.sessionStorage
		cfg.StringSession = session.NewStringSession(
			m.authKey, m.authKeyHash, dcID, newAddr, m.appID,
		).Encode()
	}

	sender, err := NewMTProto(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating new MTProto: %w", err)
	}

	sender.noRedirect = true
	sender.exported = true
	if len(cdn) > 0 && cdn[0] {
		sender.cdn = true
	}

	if err := sender.CreateConnection(false); err != nil {
		sender.Terminate()
		return nil, fmt.Errorf("creating connection: %w", err)
	}

	return sender, nil
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

		if m.terminated.Load() {
			return fmt.Errorf("mtproto terminated during reconnection")
		}

		delay := min(time.Duration(1<<uint(attempt))*m.connConfig.BaseDelay, m.connConfig.MaxDelay)

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

func (m *MTProto) CreateConnection(withLog bool) error {
	if m.terminated.Load() {
		return fmt.Errorf("mtproto is terminated, cannot create connection")
	}
	m.stopRoutines()

	m.transportMu.Lock()
	if m.transport != nil {
		m.transport.Close()
	}
	m.transport = nil
	m.transportMu.Unlock()

	ctx, cancelfunc := context.WithCancel(context.Background())
	m.ctxCancelMutex.Lock()
	m.ctxCancel = cancelfunc
	m.ctxCancelMutex.Unlock()

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

	if !m.exported && !m.cdn {
		go m.longPing(ctx)
	}

	if !m.encrypted.Load() {
		m.Logger.Debug("no auth key found, generating new key")
		err = m.makeAuthKey()
		if err != nil {
			return err
		}
		m.Logger.Debug("auth key generated successfully")
	}

	// Start Perfect Forward Secrecy manager if enabled on the main connection.
	if m.enablePFS && !m.exported && !m.cdn {
		m.startPFSManager(ctx)
	}

	return nil
}

func (m *MTProto) connect(ctx context.Context) error {
	dcId := m.GetDC()
	transportType := "[tcp]"
	if m.useWebSocket {
		transportType = "[websocket]"
		if m.useWebSocketTLS {
			transportType = "[websocket (tls)]"
		}
	}

	m.Logger.Debug("initializing %s transport for DC%d", transportType, dcId)

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
	if m.useWebSocket {
		newTransport, err = transport.NewTransport(m, transport.WSConnConfig{
			CommonConfig: cfg,
			TLS:          m.useWebSocketTLS,
			TestMode:     false,
		}, m.mode)
	} else {
		newTransport, err = transport.NewTransport(m, transport.TCPConnConfig{
			CommonConfig: cfg,
			IpV6:         m.IpV6,
		}, m.mode)
	}

	if err != nil {
		m.Logger.Debug("failed to create %s transport: %v", transportType, err)
		return fmt.Errorf("creating transport: %w", err)
	}

	m.transportMu.Lock()
	m.transport = newTransport
	m.transportMu.Unlock()

	m.Logger.Trace("%s transport initialized", transportType)

	if err := m.checkRapidReconnect(); err != nil {
		return err
	}

	return nil
}

func (m *MTProto) startPFSManager(ctx context.Context) {
	const renewBeforeSeconds int64 = 60                   // renew 60s before expiry
	const defaultTempLifetimeSeconds int32 = 24 * 60 * 60 // 24h
	const retryDelayOnError = 30 * time.Second            // backoff on error
	const pollNoAuthKeyDelay = 5 * time.Second            // wait for authKey
	const minSleepSeconds int64 = 5                       // minimum wait between checks

	m.routineswg.Add(1)
	go func() {
		defer m.routineswg.Done()
		defer m.Logger.Debug("PFS manager stopped")

		m.Logger.Debug("PFS manager started")
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// require permanent auth key first.
			if len(m.authKey) == 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(pollNoAuthKeyDelay):
				}
				continue
			}

			now := time.Now().Unix()
			expiresAt := m.tempAuthExpiresAt
			needNew := m.tempAuthKey == nil || expiresAt == 0 || now >= expiresAt-renewBeforeSeconds

			if needNew {
				m.Logger.Debug("generating new temporary auth key for PFS")
				if err := m.createTempAuthKey(defaultTempLifetimeSeconds); err != nil {
					m.Logger.WithError(err).Error("failed to create temporary auth key")
					select {
					case <-ctx.Done():
						return
					case <-time.After(retryDelayOnError):
					}
					continue
				}

				if err := m.bindTempAuthKey(); err != nil {
					m.Logger.WithError(err).Error("failed to bind temporary auth key")
					select {
					case <-ctx.Done():
						return
					case <-time.After(retryDelayOnError):
					}
					continue
				}

				// refresh local expiry after successful bind.
				expiresAt = m.tempAuthExpiresAt
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
	}()
}

func (m *MTProto) makeRequest(data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.reqTimeout)
	defer cancel()
	return m.makeRequestCtx(ctx, data, expectedTypes...)
}

func (m *MTProto) makeRequestCtx(ctx context.Context, data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	return m.makeRequestCtxWithDepth(ctx, data, 0, expectedTypes...)
}

func (m *MTProto) makeRequestCtxWithDepth(ctx context.Context, data tl.Object, retryDepth int, expectedTypes ...reflect.Type) (any, error) {
	if retryDepth >= m.maxRetryDepth {
		return nil, fmt.Errorf("maximum retry depth exceeded (%d) - aborting request", m.maxRetryDepth)
	}

	if err := m.tcpState.WaitForActive(ctx); err != nil {
		if m.shouldRetryError(fmt.Errorf("tcp inactive: %w", err)) && retryDepth < m.maxRetryDepth {
			m.Logger.Trace("tcp inactive, retrying (depth=%d/%d)", retryDepth+1, m.maxRetryDepth)
			retryCtx, cancel := context.WithTimeout(context.Background(), m.reqTimeout)
			defer cancel()
			return m.makeRequestCtxWithDepth(retryCtx, data, retryDepth+1, expectedTypes...)
		}
		return nil, fmt.Errorf("tcp inactive: %w", err)
	}

	respChan, msgID, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		if utils.IsTransportError(err) {
			m.Logger.WithError(err).Trace("transport error for msgID=%d, reconnecting (depth=%d/%d)", msgID, retryDepth, m.maxRetryDepth)
			if reconnErr := m.Reconnect(false); reconnErr != nil {
				m.Logger.WithError(reconnErr).Error("reconnect failed after transport error")
				return nil, fmt.Errorf("reconnecting after transport error: %w", reconnErr)
			}
			if retryDepth < m.maxRetryDepth {
				return m.makeRequestCtxWithDepth(ctx, data, retryDepth+1, expectedTypes...)
			}
			return nil, fmt.Errorf("max retries reached after transport error: %w", err)
		}

		if m.shouldRetryError(err) && retryDepth < m.maxRetryDepth {
			m.Logger.Trace("retrying request (depth=%d/%d): %v", retryDepth+1, m.maxRetryDepth, err)
			retryCtx, cancel := context.WithTimeout(context.Background(), m.reqTimeout)
			defer cancel()
			return m.makeRequestCtxWithDepth(retryCtx, data, retryDepth+1, expectedTypes...)
		}
		return nil, err
	}

	if msgID != 0 {
		m.messageTracker.Add(int(msgID), time.Now().Unix())
		m.messageTypesMap.Store(msgID, fmt.Sprintf("%T", data))
		m.Logger.Trace("request sent: %T (msgID=%d, d=%d)", data, msgID, retryDepth)
	}

	select {
	case <-ctx.Done():
		if msgID != 0 {
			_, channelExists := m.responseChannels.Get(int(msgID))
			_, expectedExists := m.expectedTypes.Get(int(msgID))
			sentTime, trackerExists := m.messageTracker.Get(int(msgID))

			m.responseChannels.Delete(int(msgID))
			m.expectedTypes.Delete(int(msgID))
			m.messageTracker.Delete(int(msgID))
			m.messageTypesMap.Delete(msgID)

			if channelExists && trackerExists {
				waitTime := time.Now().Unix() - sentTime
				m.Logger.Debug("request timeout: %T (msgID=%d, retryDepth=%d, waitTime=%ds, err=%v) [channel_exists=true, still_waiting=true]",
					data, msgID, retryDepth, waitTime, ctx.Err())
			} else {
				m.Logger.Debug("request timeout: %T (msgID=%d, retryDepth=%d, err=%v) [channel_exists=%v, expected_exists=%v, tracker_exists=%v - POSSIBLE PREMATURE CLEANUP]",
					data, msgID, retryDepth, ctx.Err(), channelExists, expectedExists, trackerExists)
			}
		}

		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			count := m.connState.ConsecutiveTimeouts.Add(1)
			if count == 1 {
				m.connState.ConsecutiveTimeoutStart.Store(time.Now().Unix())
			}

			if count >= 5 {
				duration := time.Now().Unix() - m.connState.ConsecutiveTimeoutStart.Load()
				// If 5 timeouts happen within 60 seconds, it's a rapid failure -> reconnect
				// Otherwise, it might be just slow network -> reset counter to avoid "random" reconnects later
				if duration < 60 {
					m.Logger.Debug("5 consecutive timeouts in %ds (request=%T, tcp_active=%v, retryDepth=%d); reconnecting", duration, data, m.tcpState.GetActive(), retryDepth)
					m.connState.ConsecutiveTimeouts.Store(0)
					m.tryReconnect()
				} else {
					m.Logger.Debug("5 consecutive timeouts in %ds (slow accumulation); resetting counter", duration)
					m.connState.ConsecutiveTimeouts.Store(0)
				}
			}
		} else {
			m.connState.ConsecutiveTimeouts.Store(0)
		}

		err := fmt.Errorf("request timeout: %w", ctx.Err())
		if m.shouldRetryError(err) && retryDepth < m.maxRetryDepth {
			m.Logger.Trace("timeout retry (depth=%d/%d)", retryDepth+1, m.maxRetryDepth)
			retryCtx, cancel := context.WithTimeout(context.Background(), m.reqTimeout)
			defer cancel()
			return m.makeRequestCtxWithDepth(retryCtx, data, retryDepth+1, expectedTypes...)
		}
		return nil, err

	case resp := <-respChan:
		m.connState.ConsecutiveTimeouts.Store(0)
		if msgID != 0 {
			m.messageTracker.Delete(int(msgID))
			if reqType, ok := m.messageTypesMap.LoadAndDelete(msgID); ok {
				sentTime, exists := m.messageTracker.Get(int(msgID))
				if exists {
					duration := time.Now().Unix() - sentTime
					m.Logger.Trace("response received: %s -> %T (msgID=%d, latency=%ds, d=%d)",
						reqType, resp, msgID, duration, retryDepth)
				} else {
					m.Logger.Trace("response received: %s -> %T (msgID=%d, d=%d)",
						reqType, resp, msgID, retryDepth)
				}
			}
		}
		return m.handleRPCResult(data, resp, expectedTypes...)
	}
}

func (m *MTProto) shouldRetryError(err error) bool {
	return m.errorHandler != nil && m.errorHandler(err)
}

func (m *MTProto) handleRPCResult(data tl.Object, response tl.Object, expectedTypes ...reflect.Type) (any, error) {
	switch r := response.(type) {
	case *objects.RpcError:
		var rpcError *ErrResponseCode
		errors.As(RpcErrorToNative(r, utils.FmtMethod(data)), &rpcError)

		// handle dc migration (code 303)
		if rpcError.Code == 303 {
			if strings.HasPrefix(rpcError.Message, "USER_MIGRATE_") || strings.HasPrefix(rpcError.Message, "PHONE_MIGRATE_") {
				if dcIDStr := utils.RegexpDCMigrate.FindStringSubmatch(rpcError.Description); len(dcIDStr) == 2 {
					if dcID, err := strconv.Atoi(dcIDStr[1]); err == nil {
						if err := m.SwitchDc(dcID); err == nil {
							if m.onMigration != nil {
								m.onMigration()
							}
							return nil, &errorDCMigrated{int32(dcID)}
						}
					}
				}
			}
			return nil, rpcError
		}

		// handle flood wait errors (code 420)
		if strings.Contains(rpcError.Message, "FLOOD_WAIT_") || strings.Contains(rpcError.Message, "FLOOD_PREMIUM_WAIT_") {
			if m.floodHandler(rpcError) {
				ctx, cancel := context.WithTimeout(context.Background(), m.reqTimeout)
				defer cancel()
				return m.makeRequestCtx(ctx, data, expectedTypes...)
			}
			return nil, rpcError
		}

		m.Logger.Trace("rpc error: code=%d message=%s", rpcError.Code, rpcError.Message)
		return nil, rpcError

	case *errorSessionConfigsChanged:
		if m.exported {
			m.Logger.Trace("session config changed, retrying request")
		} else {
			m.Logger.Debug("session config changed, retrying request")
		}
		ctx, cancel := context.WithTimeout(context.Background(), m.reqTimeout)
		defer cancel()
		// Start fresh with depth 0 for session config changes
		return m.makeRequestCtxWithDepth(ctx, data, 0, expectedTypes...)
	}

	return tl.UnwrapNativeTypes(response), nil
}

func (m *MTProto) InvokeRequestWithoutUpdate(data tl.Object, expectedTypes ...reflect.Type) error {
	_, _, err := m.sendPacket(data, expectedTypes...)
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
	m.tcpState.SetActive(false)
	m.stopRoutines()
	done := make(chan struct{})
	go func() {
		m.routineswg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.Logger.Trace("all routines stopped gracefully")
	case <-time.After(10 * time.Second):
		m.Logger.Debug("timeout waiting for routines to stop on disconnect")
	}

	return nil
}

func (m *MTProto) Terminate() error {
	m.terminated.Store(true)
	m.stopRoutines()
	m.responseChannels.Close()

	m.transportMu.Lock()
	if m.transport != nil {
		m.transport.Close()
		m.transport = nil
	}
	m.transportMu.Unlock()

	m.tcpState.SetActive(false)
	return nil
}

func (m *MTProto) SetTerminated(val bool) {
	m.terminated.Store(val)
}

func (m *MTProto) Reconnect(loggy bool) error {
	if !m.connState.InProgress.CompareAndSwap(false, true) {
		m.Logger.Trace("reconnection already in progress, skipping duplicate attempt")
		time.Sleep(50 * time.Millisecond)
		return nil
	}
	defer m.connState.InProgress.Store(false)

	if m.terminated.Load() {
		m.Logger.Trace("skipping reconnect: mtproto is terminated")
		return nil
	}

	startTime := time.Now()
	if loggy {
		m.Logger.Info("reconnecting to %s (%s)", utils.FmtIP(m.GetAddr()), m.GetTransportType())
	} else {
		m.Logger.Debug("reconnecting to %s (%s)", utils.FmtIP(m.GetAddr()), m.GetTransportType())
	}

	err := m.Disconnect()
	if err != nil {
		m.Logger.WithError(err).Warn("error during disconnect in reconnect")
	}

	err = m.CreateConnection(loggy)
	if err != nil {
		m.Logger.WithError(err).Error("failed to recreate connection")
		return fmt.Errorf("recreating connection: %w", err)
	}

	duration := time.Since(startTime)
	if loggy {
		m.Logger.Info("reconnected to %s (%s) in %v", utils.FmtIP(m.GetAddr()), m.GetTransportType(), duration)
	} else {
		m.Logger.Debug("reconnected to %s (%s) in %v", utils.FmtIP(m.GetAddr()), m.GetTransportType(), duration)
	}

	if m.transport != nil {
		m.Ping()
	}

	return nil
}

func (m *MTProto) Redial() error {
	if !m.connState.InProgress.CompareAndSwap(false, true) {
		m.Logger.Trace("redialing already in progress")
		return nil
	}
	defer m.connState.InProgress.Store(false)

	m.Logger.Debug("forcing transport redial")
	m.tcpState.SetActive(false)

	m.transportMu.Lock()
	if m.transport != nil {
		m.transport.Close()
	}
	m.transportMu.Unlock()

	if err := m.connectWithRetry(context.TODO()); err != nil {
		m.Logger.WithError(err).Error("redial failed")
		return err
	}

	m.tcpState.SetActive(true)
	m.connState.ConsecutiveTimeouts.Store(0)
	m.Logger.Debug("transport redial successful")
	return nil
}

// keep pinging to keep the connection alive
func (m *MTProto) longPing(ctx context.Context) {
	m.routineswg.Add(1)
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
			m.Ping()
		}
	}
}

func (m *MTProto) Ping() time.Duration {
	if m.transport == nil || !m.IsTcpActive() {
		m.Logger.Debug("ping skipped: transport unavailable")
		return 0
	}
	start := time.Now()
	m.Logger.Trace("sending ping")
	if err := m.InvokeRequestWithoutUpdate(&utils.PingParams{
		PingID: time.Now().Unix(),
	}); err != nil {
		m.Logger.Debug("ping failed: %v", err)
		return -1
	}
	return time.Since(start)
}

func (m *MTProto) tryReconnect() error {
	if err := m.Reconnect(false); err != nil {
		m.Logger.Debug("reconnect failed: %v", err)
		return err
	}
	return nil
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

				m.Logger.Trace("connection error: %v; reconnecting to %s (%s)", err, utils.FmtIP(m.GetAddr()), m.GetTransportType())
				if reconnErr := m.tryReconnect(); reconnErr != nil {
					m.Logger.Debug("failed to reconnect: %v", reconnErr)
				}

				if errors.Is(err, io.EOF) {
					m.Logger.Trace("connection closed by server")
					return
				}
				continue
			}

			m.Logger.Trace("error reading message: %v", err)
			var respErr *ErrResponseCode
			var transErr *transport.ErrCode
			switch {
			case errors.As(err, &respErr):
				if respErr.Code == 4294966892 {
					if authErr := m.handle404Error(); authErr != nil {
						m.Logger.Error("auth key error: %v", authErr)
						return
					}
				} else {
					m.Logger.Debug("transport response error code: %d - %s", respErr.Code, respErr.Error())
				}
			case errors.As(err, &transErr):
				m.Logger.Debug("transport error code: %d - %s", int64(*transErr), transErr.Error())
			default:
				if !m.terminated.Load() {
					if strings.Contains(err.Error(), "object with provided crc") {
						m.Logger.Warn(FormatDecodeError(err))
					} else if !m.connState.InProgress.Load() {
						m.Logger.Trace("reading message: %v", err)
						if err := m.tryReconnect(); err != nil {
							m.Logger.Debug("failed to reconnect: %v", err)
						}
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
	if m.authKey404[0] == 0 && m.authKey404[1] == 0 {
		m.authKey404 = [2]int64{1, time.Now().Unix()}
	} else {
		currentTime := time.Now().Unix()
		if currentTime-m.authKey404[1] < 2 { // time frame to check if the error is repeating
			m.authKey404[0]++
		} else {
			m.authKey404 = [2]int64{1, currentTime}
		}
	}

	if m.authKey404[0] > 4 && m.authKey404[0] < 16 {
		m.Logger.Debug("auth key error occurred %d times, reconnecting", m.authKey404[0])
		if err := m.tryReconnect(); err != nil {
			return err
		}
	} else if m.authKey404[0] >= 16 {
		m.errorHandler(ErrAuthKeyInvalid)
		return ErrAuthKeyInvalid
	}
	return nil
}

func (m *MTProto) readMsg(ctx context.Context) error {
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

	if m.serviceModeActivated {
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

	err = m.processResponse(response)
	if err != nil {
		m.Logger.Debug("decoding unknown object: %v", err)
		return fmt.Errorf("incoming update: %w", err)
	}
	return nil
}

func (m *MTProto) processResponse(msg messages.Common) error {
	var data tl.Object
	var err error

	if (msg.GetSeqNo() & 1) != 0 {
		msgID := int64(msg.GetMsgID())
		if m.pendingAcks.Has(msgID) {
			return nil
		} else {
			m.pendingAcks.Add(msgID)
		}
	}

	if et, ok := m.expectedTypes.Get(msg.GetMsgID()); ok && len(et) > 0 {
		data, err = tl.DecodeUnknownObject(msg.GetMsg(), et...)
	} else {
		data, err = tl.DecodeUnknownObject(msg.GetMsg())
	}
	if err != nil {
		return fmt.Errorf("unmarshaling response: %w", err)
	}

messageTypeSwitching:
	switch message := data.(type) {
	case *objects.MessageContainer:
		for _, v := range *message {
			err := m.processResponse(v)
			if err != nil {
				return fmt.Errorf("processing item in container: %w", err)
			}
		}

	case *objects.BadServerSalt:
		m.serverSalt.Store(message.NewSalt)
		if err := m.SaveSession(m.memorySession); err != nil {
			m.Logger.Debug("failed to save session: %v", err)
		}
		m.notifyPendingRequestsOfConfigChange()

	case *objects.NewSessionCreated:
		m.serverSalt.Store(message.ServerSalt)
		if err := m.SaveSession(m.memorySession); err != nil {
			m.Logger.Debug("failed to save session: %v", err)
		}

	case *objects.MsgsNewDetailedInfo:
		m.pendingAcks.Add(message.AnswerMsgID)
		return nil

	case *objects.MsgsDetailedInfo:
		m.pendingAcks.Add(message.AnswerMsgID)
		return nil

	case *objects.Pong:
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
			serverTime := int64(msg.GetMsgID()) >> 32
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
		m.Logger.Trace("received RPC response: %T (msgID=%d)", obj, message.ReqMsgID)
		err := m.writeRPCResponse(int(message.ReqMsgID), obj)
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
		for _, f := range m.serverRequestHandlers {
			processed = f(message)
			if processed {
				break
			}
		}
		if !processed {
			m.Logger.Trace("unhandled update: %T", message)
		}
	}

	if m.pendingAcks.Len() >= defaultPendingAcksThreshold {
		m.Logger.Trace("sending %d pending acknowledgments", m.pendingAcks.Len())

		_, err := m.MakeRequest(&objects.MsgsAck{MsgIDs: m.pendingAcks.Keys()})
		if err != nil {
			return fmt.Errorf("sending acks: %w", err)
		}

		m.pendingAcks.Clear()
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
		case <-time.After(1 * time.Millisecond):
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
