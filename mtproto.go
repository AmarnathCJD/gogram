// Copyright (c) 2024 RoseLoverX

package gogram

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
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
	defaultMaxReconnectAttempts = 500
	defaultBaseReconnectDelay   = 2 * time.Second
	defaultPingInterval         = 30 * time.Second
	defaultPendingAcksThreshold = 10
)

type MTProto struct {
	Addr      string
	appID     int32
	proxy     *utils.Proxy
	transport transport.Transport
	localAddr string

	ctxCancel     context.CancelFunc
	routineswg    sync.WaitGroup
	memorySession bool
	tcpState      *TcpState
	timeOffset    int64
	reqTimeout    time.Duration
	mode          mode.Variant
	DcList        *utils.DCOptions

	authKey []byte

	authKeyHash []byte

	tempAuthKey       []byte
	tempAuthKeyHash   []byte
	tempAuthExpiresAt int64

	noRedirect bool

	serverSalt int64
	encrypted  bool
	sessionId  int64

	mutex            sync.Mutex
	responseChannels *utils.SyncIntObjectChan
	expectedTypes    *utils.SyncIntReflectTypes
	pendingAcks      *utils.SyncSet[int64]

	genMsgID     func(int64) int64
	currentSeqNo atomic.Int32

	sessionStorage session.SessionLoader

	publicKey *rsa.PublicKey
	cdnKeys   map[int32]*rsa.PublicKey

	serviceChannel       chan tl.Object
	serviceModeActivated bool

	authKey404 [2]int64
	IpV6       bool

	Logger *utils.Logger

	serverRequestHandlers []func(i any) bool
	floodHandler          func(err error) bool
	errorHandler          func(err error)
	connectionHandler     func(err error) error
	exported              bool
	cdn                   bool
	terminated            atomic.Bool
	reconnectInProgress   atomic.Bool
	timeout               time.Duration

	reconnectAttempts    int
	reconnectMutex       sync.Mutex
	maxReconnectDelay    time.Duration
	maxReconnectAttempts int
	baseReconnectDelay   time.Duration

	lastSuccessfulConnect time.Time
	rapidReconnectCount   int
	rapidReconnectMutex   sync.Mutex

	useWebSocket    bool
	useWebSocketTLS bool
	enablePFS       bool

	onMigration func()
}

type Config struct {
	AuthKeyFile    string
	AuthAESKey     string
	StringSession  string
	SessionStorage session.SessionLoader
	MemorySession  bool
	AppID          int32
	EnablePFS      bool

	FloodHandler      func(err error) bool
	ErrorHandler      func(err error)
	ConnectionHandler func(err error) error

	ServerHost      string
	PublicKey       *rsa.PublicKey
	DataCenter      int
	Logger          *utils.Logger
	Proxy           *utils.Proxy
	Mode            string
	Ipv6            bool
	CustomHost      bool
	LocalAddr       string
	Timeout         int
	ReqTimeout      int
	UseWebSocket    bool
	UseWebSocketTLS bool

	MaxReconnectAttempts int
	BaseReconnectDelay   time.Duration
	MaxReconnectDelay    time.Duration

	OnMigration func()
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
			// check if have write permission in the directory
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
		Addr:                  c.ServerHost,
		encrypted:             false,
		sessionId:             utils.GenerateSessionID(),
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
		errorHandler:          func(err error) {},
		reqTimeout:            utils.MinSafeDuration(c.ReqTimeout),
		mode:                  parseTransportMode(c.Mode),
		IpV6:                  c.Ipv6,
		tcpState:              NewTcpState(),
		DcList:                utils.NewDCOptions(),
		timeout:               utils.MinSafeDuration(c.Timeout),
		reconnectAttempts:     0,
		maxReconnectDelay:     utils.OrDefault(c.MaxReconnectDelay, 15*time.Minute),
		maxReconnectAttempts:  utils.OrDefault(c.MaxReconnectAttempts, defaultMaxReconnectAttempts),
		baseReconnectDelay:    utils.OrDefault(c.BaseReconnectDelay, defaultBaseReconnectDelay),
		useWebSocket:          c.UseWebSocket,
		useWebSocketTLS:       c.UseWebSocketTLS,
		enablePFS:             c.EnablePFS,
		onMigration:           c.OnMigration,
	}

	mtproto.Logger.Debug("initializing mtproto...")

	if loaded != nil || c.StringSession != "" {
		mtproto.encrypted = true
	}
	if err := mtproto.loadAuth(c.StringSession, loaded); err != nil {
		return nil, fmt.Errorf("loading auth: %w", err)
	}

	if c.CustomHost {
		mtproto.Addr = c.ServerHost
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
	m.authKey, m.authKeyHash, m.Addr, m.appID = sess.Key, sess.Hash, sess.Hostname, sess.AppID
	m.Logger.Debug("loading - auth from session (IP: %s)...", utils.FmtIp(m.Addr))
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
		Salt:     m.serverSalt,
		Hostname: m.Addr,
		AppID:    m.AppID(),
	}, m.GetDC()
}

func (m *MTProto) ImportRawAuth(authKey, authKeyHash []byte, addr string, appID int32) (bool, error) {
	m.authKey, m.authKeyHash, m.Addr, m.appID = authKey, authKeyHash, addr, appID
	m.Logger.Debug("importing - raw auth...")
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
	m.authKey, m.authKeyHash, m.Addr = sessionString.AuthKey(), sessionString.AuthKeyHash(), sessionString.IpAddr()
	if m.appID == 0 {
		m.appID = sessionString.AppID()
	}
	m.Logger.Debug("importing - auth from string session (IP: %s)...", utils.FmtIp(m.Addr))
	if err := m.SaveSession(m.memorySession); err != nil {
		return false, fmt.Errorf("saving session: %w", err)
	}
	return true, nil
}

func (m *MTProto) GetDC() int {
	return m.DcList.SearchAddr(m.Addr)
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
	m.cdnKeys = keys
}

func (m *MTProto) HasCdnKey(dc int32) (*rsa.PublicKey, bool) {
	key, ok := m.cdnKeys[dc]
	return key, ok
}

func (m *MTProto) SwitchDc(dc int) error {
	if m.noRedirect {
		return nil
	}
	newAddr := m.DcList.GetHostIp(dc, false, m.IpV6)
	if newAddr == "" {
		return fmt.Errorf("dc %d not found in dc list", dc)
	}

	m.Logger.Debug("migrating to new data center... dc %d", dc)

	m.stopRoutines()
	// m.routineswg.Wait()

	m.sessionStorage.Delete()
	m.Logger.Debug("deleted old auth key file")

	m.authKey = nil
	m.authKeyHash = nil
	m.serverSalt = 0
	m.encrypted = false
	m.sessionId = utils.GenerateSessionID()

	m.tempAuthKey = nil
	m.tempAuthKeyHash = nil
	m.tempAuthExpiresAt = 0

	m.responseChannels = utils.NewSyncIntObjectChan()
	m.expectedTypes = utils.NewSyncIntReflectTypes()
	m.pendingAcks = utils.NewSyncSet[int64]()
	m.currentSeqNo.Store(0)

	m.authKey404 = [2]int64{0, 0}
	m.reconnectAttempts = 0
	m.rapidReconnectCount = 0
	m.lastSuccessfulConnect = time.Time{}

	m.Addr = newAddr

	m.Logger.Info("user migrated to new dc (%s) - %s", strconv.Itoa(dc), newAddr)
	m.Logger.Debug("reconnecting to new dc... dc %d", dc)

	errConn := m.CreateConnection(true)
	if errConn != nil {
		return fmt.Errorf("creating connection: %w", errConn)
	}

	return nil
}

func (m *MTProto) ExportNewSender(dcID int, mem bool, cdn ...bool) (*MTProto, error) {
	newAddr := m.DcList.GetHostIp(dcID, false, m.IpV6)
	senderID := utils.RandomSenderID()
	logger := utils.NewLogger("gogram [sender-" + senderID + "]").SetLevel(utils.InfoLevel)

	if len(cdn) > 0 && cdn[0] {
		newAddr, _ = m.DcList.GetCdnAddr(dcID)
		logger.SetPrefix("gogram [cdn-" + senderID + "]")
	}

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
		Timeout:         int(m.timeout.Seconds()),
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
		return nil, fmt.Errorf("creating connection: %w", err)
	}

	return sender, nil
}

func (m *MTProto) connectWithRetry(ctx context.Context) error {
	m.reconnectMutex.Lock()
	defer m.reconnectMutex.Unlock()

	err := m.connect(ctx)
	if err == nil {
		m.reconnectAttempts = 0
		return nil
	}

	if m.connectionHandler != nil {
		m.Logger.Debug("using custom connection handler for reconnection")
		return m.connectionHandler(err)
	}

	for attempt := range m.maxReconnectAttempts {
		err := m.connect(ctx)
		if err == nil {
			if attempt > 0 {
				m.Logger.Info("successfully reconnected after %d attempts", attempt+1)
			}
			m.reconnectAttempts = 0
			return nil
		}

		if m.terminated.Load() {
			return fmt.Errorf("mtproto terminated during reconnection")
		}

		delay := min(time.Duration(1<<uint(attempt))*m.baseReconnectDelay, m.maxReconnectDelay)

		m.Logger.Info("%v, retrying in %v (attempt %d/%d)...", err, delay, attempt+1, m.maxReconnectAttempts)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			continue
		}
	}

	return fmt.Errorf("max reconnection attempts (%d) reached", m.maxReconnectAttempts)
}

func (m *MTProto) CreateConnection(withLog bool) error {
	if m.terminated.Load() {
		return fmt.Errorf("mtproto is terminated, cannot create connection")
	}
	m.stopRoutines()

	ctx, cancelfunc := context.WithCancel(context.Background())
	m.ctxCancel = cancelfunc

	transportType := m.GetTransportType()
	if withLog {
		m.Logger.Info("connecting to [%s] - <%s> ...", utils.FmtIp(m.Addr), transportType)
	} else {
		m.Logger.Debug("connecting to [%s] - <%s> ...", utils.FmtIp(m.Addr), transportType)
	}

	err := m.connectWithRetry(ctx)
	if err != nil {
		m.Logger.WithError(err).Error("failed to create connection")
		return err
	}
	m.tcpState.SetActive(true)

	var localAddrLabel string
	if m.localAddr != "" {
		localAddrLabel = fmt.Sprintf("(-%s)", utils.FmtIp(m.localAddr))
	}

	var proxyLabel string
	if m.proxy != nil && m.proxy.Host != "" {
		proxyLabel = fmt.Sprintf("(~%s)", utils.FmtIp(m.proxy.Host))
	}

	transportType = m.GetTransportType()
	logMessage := fmt.Sprintf("connection to %s%s[%s] - <%s> established", localAddrLabel, proxyLabel, utils.FmtIp(m.Addr), transportType)

	if withLog {
		m.Logger.Info(logMessage)
	} else {
		m.Logger.Debug(logMessage)
	}

	m.startReadingResponses(ctx)

	if !m.exported && !m.cdn {
		go m.longPing(ctx)
	}

	if !m.encrypted {
		m.Logger.Debug("authkey not found, creating new one")
		err = m.makeAuthKey()
		if err != nil {
			return err
		}
		m.Logger.Debug("authkey created and saved")
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

	m.Logger.Debug("initializing %s transport for dc %d", transportType, dcId)

	var err error
	if m.useWebSocket {
		m.transport, err = transport.NewTransport(
			m,
			transport.WSConnConfig{
				Ctx:         ctx,
				Host:        utils.FmtIp(m.Addr),
				TLS:         m.useWebSocketTLS,
				DC:          dcId,
				TestMode:    false,
				Timeout:     m.timeout,
				Socks:       m.proxy,
				LocalAddr:   m.localAddr,
				ModeVariant: uint8(m.mode),
				Logger:      m.Logger,
			},
			m.mode,
		)
	} else {
		m.transport, err = transport.NewTransport(
			m,
			transport.TCPConnConfig{
				Ctx:         ctx,
				Host:        utils.FmtIp(m.Addr),
				IpV6:        m.IpV6,
				Timeout:     m.timeout,
				Socks:       m.proxy,
				LocalAddr:   m.localAddr,
				ModeVariant: uint8(m.mode),
				DC:          dcId,
				Logger:      m.Logger,
			},
			m.mode,
		)
	}

	if err != nil {
		m.Logger.Debug("failed to create %s transport: %v", transportType, err)
		return fmt.Errorf("creating transport: %w", err)
	}

	m.Logger.Debug("%s transport created successfully", transportType)
	m.rapidReconnectMutex.Lock()
	now := time.Now()
	if !m.lastSuccessfulConnect.IsZero() && now.Sub(m.lastSuccessfulConnect) < 5*time.Second {
		m.rapidReconnectCount++
		if m.rapidReconnectCount >= 10 {
			m.rapidReconnectMutex.Unlock()
			m.Logger.Error(fmt.Sprintf("detected rapid reconnection loop (%d consecutive reconnects in <5s intervals)", m.rapidReconnectCount))
			if m.proxy != nil && m.proxy.Type == "mtproxy" {
				return fmt.Errorf("mtproxy connection loop detected: connection succeeds but immediately closes - check proxy configuration, secret, or server availability")
			}
			return fmt.Errorf("rapid reconnection loop detected: connection succeeds but immediately closes - possible network or server issue")
		}
	} else {
		m.rapidReconnectCount = 0
	}
	m.lastSuccessfulConnect = now
	m.rapidReconnectMutex.Unlock()

	return nil
}

func (m *MTProto) startPFSManager(ctx context.Context) {
	const renewBeforeSeconds int64 = 60                   // renew 60s before expiry
	const defaultTempLifetimeSeconds int32 = 24 * 60 * 60 // 24h
	const retryDelayOnError = 30 * time.Second            // backoff on error
	const pollNoAuthKeyDelay = 5 * time.Second            // wait for authKey
	const minSleepSeconds int64 = 5                       // minimum wait between checks

	m.routineswg.Go(func() {
		m.Logger.Debug("PFS manager started")
		for {
			select {
			case <-ctx.Done():
				m.Logger.Debug("PFS manager stopped: context cancelled")
				return
			default:
			}

			// Require permanent auth key first.
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
				m.Logger.Debug("creating and binding new temporary auth key for pfs")
				if err := m.createTempAuthKey(defaultTempLifetimeSeconds); err != nil {
					m.Logger.WithError(err).Error("pfs: createTempAuthKey failed")
					select {
					case <-ctx.Done():
						return
					case <-time.After(retryDelayOnError):
					}
					continue
				}

				if err := m.bindTempAuthKey(); err != nil {
					m.Logger.WithError(err).Error("pfs: bindTempAuthKey failed")
					select {
					case <-ctx.Done():
						return
					case <-time.After(retryDelayOnError):
					}
					continue
				}

				// Refresh local expiry after successful bind.
				expiresAt = m.tempAuthExpiresAt
			}

			// Compute sleep until just before expiry.
			if expiresAt == 0 {
				// No expiry known (should not normally happen); poll later.
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

func (m *MTProto) makeRequest(data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.reqTimeout)
	defer cancel()
	return m.makeRequestCtx(ctx, data, expectedTypes...)
}

func (m *MTProto) makeRequestCtx(ctx context.Context, data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	if err := m.tcpState.WaitForActive(ctx); err != nil {
		return nil, fmt.Errorf("waiting for active tcp state: %w", err)
	}

	resp, msgId, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") || strings.Contains(err.Error(), "transport is closed") || strings.Contains(err.Error(), "connection was forcibly closed") || strings.Contains(err.Error(), "connection reset by peer") || strings.Contains(err.Error(), "broken pipe") {
			m.Logger.Debug("transport closed, reconnecting to [%s] - <%s> ...", m.Addr, m.GetTransportType())
			err = m.Reconnect(false)
			if err != nil {
				return nil, fmt.Errorf("reconnecting: %w", err)
			}
			return m.makeRequestCtx(ctx, data, expectedTypes...)
		}
		return nil, fmt.Errorf("sending packet: %w", err)
	}

	select {
	case <-ctx.Done():
		go m.writeRPCResponse(int(msgId), &objects.Null{})
		return nil, fmt.Errorf("context done while waiting for response: %w", ctx.Err())
	case response := <-resp:
		switch r := response.(type) {
		case *objects.RpcError:
			rpcError := RpcErrorToNative(r, utils.FmtMethod(data)).(*ErrResponseCode)

			// handle dc migration (303 errors)
			if rpcError.Code == 303 && (strings.HasPrefix(rpcError.Message, "USER_MIGRATE_") || strings.HasPrefix(rpcError.Message, "PHONE_MIGRATE_")) {
				dcIDStr := utils.RegexpDCMigrate.FindStringSubmatch(rpcError.Description)
				if len(dcIDStr) == 2 {
					dcID, err := strconv.Atoi(dcIDStr[1])
					if err != nil {
						return nil, fmt.Errorf("parsing dc id from rpc error: %w", err)
					}
					m.SwitchDc(dcID)

					if m.onMigration != nil {
						m.onMigration()
					}
					return nil, &errorDCMigrated{int32(dcID)}
				}
			}

			// handle flood wait errors (429 errors)
			if strings.Contains(rpcError.Message, "FLOOD_WAIT_") || strings.Contains(rpcError.Message, "FLOOD_PREMIUM_WAIT_") {
				if m.floodHandler(rpcError) {
					return m.makeRequestCtx(ctx, data, expectedTypes...)
				}
				return nil, rpcError
			}

			m.Logger.Trace("received rpc error: code=%d message=%s description=%s", rpcError.Code, rpcError.Message, rpcError.Description)
			return nil, rpcError

		case *errorSessionConfigsChanged:
			if !m.exported {
				m.Logger.Debug("session configs changed, resending request")
			} else {
				m.Logger.Trace("session configs changed, resending request")
			}
			return m.makeRequestCtx(ctx, data, expectedTypes...)
		}

		return tl.UnwrapNativeTypes(response), nil
	}
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
	if m.ctxCancel != nil {
		m.ctxCancel()
	}
}

func (m *MTProto) Disconnect() error {
	m.tcpState.SetActive(false)
	m.stopRoutines()

	return nil
}

func (m *MTProto) Terminate() error {
	m.terminated.Store(true)
	m.stopRoutines()
	m.responseChannels.Close()
	if m.transport != nil {
		m.transport.Close()
	}
	m.tcpState.SetActive(false)
	return nil
}

func (m *MTProto) SetTerminated(val bool) {
	m.terminated.Store(val)
}

func (m *MTProto) Reconnect(loggy bool) error {
	if !m.reconnectInProgress.CompareAndSwap(false, true) {
		m.Logger.Debug("reconnect already in progress, skipping")
		return nil
	}
	defer m.reconnectInProgress.Store(false)

	if m.terminated.Load() {
		return nil
	}

	err := m.Disconnect()
	if err != nil {
		return fmt.Errorf("disconnecting: %w", err)
	}
	if loggy {
		m.Logger.Info("reconnecting to [%s] - <%s> ...", m.Addr, m.GetTransportType())
	}
	err = m.CreateConnection(loggy)
	if err != nil {
		return fmt.Errorf("recreating connection: %w", err)
	}

	if loggy {
		m.Logger.Info("reconnected to [%s] - <%s>", m.Addr, m.GetTransportType())
	}
	if m.transport != nil {
		m.Ping()
	}

	return nil
}

// keep pinging to keep the connection alive
func (m *MTProto) longPing(ctx context.Context) {
	m.routineswg.Add(1)
	defer m.routineswg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(defaultPingInterval):
			if err := m.tcpState.WaitForActive(ctx); err != nil {
				return
			}
			m.Ping()
		}
	}
}

func (m *MTProto) Ping() time.Duration {
	if m.transport == nil || !m.IsTcpActive() {
		m.Logger.Debug("skipping ping: transport not available")
		return 0
	}
	start := time.Now()
	m.Logger.Debug("pinging server...")
	m.InvokeRequestWithoutUpdate(&utils.PingParams{
		PingID: time.Now().Unix(),
	})
	return time.Since(start)
}

func (m *MTProto) tryReconnect() error {
	if err := m.Reconnect(false); err != nil {
		m.Logger.Error("failed to reconnect to [%s] - <%s>: %v", utils.FmtIp(m.Addr), m.GetTransportType(), err)
		return err
	}
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
		err == io.EOF
}

func (m *MTProto) startReadingResponses(ctx context.Context) {
	m.routineswg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if err := m.tcpState.WaitForActive(ctx); err != nil {
					return
				}

				err := m.readMsg()
				if err == nil {
					continue
				}

				if err == context.Canceled {
					return
				}

				if isBrokenError(err) {
					m.Logger.Debug("connection error (%v), reconnecting to [%s] - <%s>...", err, utils.FmtIp(m.Addr), m.GetTransportType())
					m.tryReconnect()
					if err == io.EOF {
						return
					}
					continue
				}

				switch e := err.(type) {
				case *ErrResponseCode:
					if e.Code == 4294966892 {
						if authErr := m.handle404Error(); authErr != nil {
							m.Logger.Error("auth key error: %v", authErr)
							return
						}
					} else {
						m.Logger.Debug("transport response error code: %d - %s", e.Code, e.Error())
					}
				case *transport.ErrCode:
					m.Logger.Error("transport error code: %d - %s", int64(*e), e.Error())
				default:
					if !m.terminated.Load() {
						m.Logger.Debug("reading message: %v", err)
						m.tryReconnect()
					}
				}
			}
		}
	})
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
		m.Logger.Debug("-404 error occurred %d times, attempting to reconnect", m.authKey404[0])
		if err := m.tryReconnect(); err != nil {
			return err
		}
	} else if m.authKey404[0] >= 16 {
		m.errorHandler(ErrAuthKeyInvalid)
		return ErrAuthKeyInvalid
	}
	return nil
}

func (m *MTProto) readMsg() error {
	if m.transport == nil {
		return fmt.Errorf("must setup connection before reading messages")
	}

	response, err := m.transport.ReadMsg()
	if err != nil {
		if e, ok := err.(transport.ErrCode); ok {
			return &ErrResponseCode{Code: int64(e)}
		}
		switch err {
		case io.EOF, context.Canceled:
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
		m.serviceChannel <- obj
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
		m.serverSalt = message.NewSalt
		if err := m.SaveSession(m.memorySession); err != nil {
			m.Logger.Error("failed to save session after salt update: %v", err)
		}
		m.notifyPendingRequestsOfConfigChange()

	case *objects.NewSessionCreated:
		m.serverSalt = message.ServerSalt
		if err := m.SaveSession(m.memorySession); err != nil {
			m.Logger.Error("failed to save session: %v", err)
		}

	case *objects.MsgsNewDetailedInfo:
		m.pendingAcks.Add(message.AnswerMsgID)
		return nil

	case *objects.MsgsDetailedInfo:
		m.pendingAcks.Add(message.AnswerMsgID)
		return nil

	case *objects.Pong:
		if !m.exported && !m.cdn {
			m.Logger.Debug("rpc - pong: %d", message.PingID)
		} else {
			m.Logger.Trace("rpc - pong received")
		}

	case *objects.MsgsAck:
		// do nothing

	case *objects.BadMsgNotification:
		badMsg := BadMsgErrorFromNative(message)
		if badMsg.Code == 16 || badMsg.Code == 17 {
			m.Logger.Warn("Your system date and time are possibly incorrect, please adjust them")
			m.offsetTime()
		}
		m.Logger.Debug("bad-msg-notification: %s", badMsg.Error())
		return badMsg

	case *objects.RpcResult:
		obj := message.Obj
		if v, ok := obj.(*objects.GzipPacked); ok {
			obj = v.Obj
		}
		m.Logger.Debug("RPC: %T", obj)
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
			m.Logger.Debug("unhandled update: %T", message)
		}
	}

	if m.pendingAcks.Len() >= defaultPendingAcksThreshold {
		m.Logger.Debug("Sending acks %d", m.pendingAcks.Len())

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
	m.mutex.Lock()
	respChannelsBackup := m.responseChannels
	m.responseChannels = utils.NewSyncIntObjectChan()
	m.mutex.Unlock()

	for _, k := range respChannelsBackup.Keys() {
		if v, ok := respChannelsBackup.Get(k); ok {
			respChannelsBackup.Delete(k)
			select {
			case v <- &errorSessionConfigsChanged{}:
			case <-time.After(1 * time.Millisecond):
			}
		}
	}
}

func MessageRequireToAck(msg tl.Object) bool {
	switch msg.(type) {
	case *objects.MsgsAck:
		return false
	default:
		return true
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
	m.mu.RLock()
	ch := m.ch
	active := m.active
	m.mu.RUnlock()

	if active {
		return nil
	}

	select {
	case <-ch: // Unblocked when SetActive(true) closes the channel
		return nil
	case <-ctx.Done(): // Context canceled or timed out
		return ctx.Err()
	}
}

func NewTcpState() *TcpState {
	return &TcpState{
		ch: make(chan struct{}),
	}
}

func (m *MTProto) offsetTime() {
	currentLocalTime := time.Now().Unix()
	client := http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get("http://worldtimeapi.org/api/ip")
	if err != nil {
		return
	}

	defer resp.Body.Close()

	var timeResponse struct {
		Unixtime int64 `json:"unixtime"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&timeResponse); err != nil {
		return
	}

	if timeResponse.Unixtime <= currentLocalTime || math.Abs(float64(timeResponse.Unixtime-currentLocalTime)) < 60 {
		return // -no need to offset time
	}

	m.timeOffset = timeResponse.Unixtime - currentLocalTime
	m.genMsgID = utils.NewMsgIDGenerator()
	m.Logger.Info("system time is out of sync, offsetting time by " + strconv.FormatInt(m.timeOffset, 10) + " seconds")
}
