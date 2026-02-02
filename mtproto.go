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
	"net/url"
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
	"github.com/pkg/errors"
)

const (
	defaultTimeout = 60 * time.Second // after 60 sec without any read/write, lib will try to reconnect
	acksThreshold  = 10
)

type MTProto struct {
	Addr      string
	appID     int32
	proxy     *url.URL
	transport transport.Transport

	ctxCancel     context.CancelFunc
	routineswg    sync.WaitGroup
	memorySession bool
	tcpState      *TcpState
	timeOffset    int64
	mode          mode.Variant
	DcList        *utils.DCOptions

	authKey []byte

	authKeyHash []byte

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
	exported              bool
	cdn                   bool
	terminated            atomic.Bool
}

type Config struct {
	AuthKeyFile    string
	AuthAESKey     string
	StringSession  string
	SessionStorage session.SessionLoader
	MemorySession  bool
	AppID          int32

	FloodHandler func(err error) bool
	ErrorHandler func(err error)

	ServerHost string
	PublicKey  *rsa.PublicKey
	DataCenter int
	Logger     *utils.Logger
	Proxy      *url.URL
	Mode       string
	Ipv6       bool
	CustomHost bool
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
		if !(strings.Contains(err.Error(), session.ErrFileNotExists) || strings.Contains(err.Error(), session.ErrPathNotFound)) {
			// if the error is not because of file not found or path not found, return the error
			// else, continue with the execution
			// check if have write permission in the directory
			if _, err := os.OpenFile(filepath.Dir(c.AuthKeyFile), os.O_WRONLY, 0222); err != nil {
				return nil, errors.Wrap(err, "check if you have write permission in the directory")
			}
			return nil, errors.Wrap(err, "loading session")
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
		floodHandler:          func(err error) bool { return false },
		errorHandler:          func(err error) {},
		mode:                  parseTransportMode(c.Mode),
		IpV6:                  c.Ipv6,
		tcpState:              NewTcpState(),
		DcList:                utils.NewDCOptions(),
	}

	mtproto.Logger.Debug("initializing mtproto...")

	if loaded != nil || c.StringSession != "" {
		mtproto.encrypted = true
	}
	if err := mtproto.loadAuth(c.StringSession, loaded); err != nil {
		return nil, errors.Wrap(err, "loading auth")
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
	default:
		return mode.Abridged
	}
}

func (m *MTProto) LoadSession(sess *session.Session) error {
	m.authKey, m.authKeyHash, m.Addr, m.appID = sess.Key, sess.Hash, sess.Hostname, sess.AppID
	m.Logger.Debug("importing auth from session...")
	if err := m.SaveSession(m.memorySession); err != nil {
		return errors.Wrap(err, "saving session")
	}
	return nil
}

func (m *MTProto) loadAuth(stringSession string, sess *session.Session) error {
	if stringSession != "" {
		_, err := m.ImportAuth(stringSession)
		if err != nil {
			return errors.Wrap(err, "importing string session")
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
	m.Logger.Debug("imported authKey, authKeyHash, addr, appId")
	if err := m.SaveSession(m.memorySession); err != nil {
		return false, errors.Wrap(err, "saving session")
	}
	if err := m.Reconnect(false); err != nil {
		return false, errors.Wrap(err, "reconnecting")
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
	m.Logger.Debug("importing - auth from stringSession...")
	if err := m.SaveSession(m.memorySession); err != nil {
		return false, fmt.Errorf("saving session: %w", err)
	}
	return true, nil
}

func (m *MTProto) GetDC() int {
	return m.DcList.SearchAddr(m.Addr)
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

func (m *MTProto) SwitchDc(dc int) (*MTProto, error) {
	if m.noRedirect {
		return m, nil
	}
	newAddr := m.DcList.GetHostIp(dc, false, m.IpV6)
	if newAddr == "" {
		return nil, errors.New("dc_id not found")
	}

	m.Logger.Debug("migrating to new DC... dc-" + strconv.Itoa(dc))
	m.sessionStorage.Delete()
	m.Logger.Debug("deleted old auth key file")

	cfg := Config{
		DataCenter:    dc,
		PublicKey:     m.publicKey,
		ServerHost:    newAddr,
		AuthKeyFile:   m.sessionStorage.Path(),
		AuthAESKey:    m.sessionStorage.Key(),
		MemorySession: m.memorySession,
		Logger:        m.Logger,
		Proxy:         m.proxy,
		AppID:         m.appID,
		Ipv6:          m.IpV6,
	}

	sender, err := NewMTProto(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "creating new MTProto")
	}
	sender.serverRequestHandlers = m.serverRequestHandlers
	m.stopRoutines()
	m.Logger.Info(fmt.Sprintf("user migrated to new dc (%s) - %s", strconv.Itoa(dc), newAddr))
	m.Logger.Debug("reconnecting to new dc... dc-" + strconv.Itoa(dc))
	errConn := sender.CreateConnection(true)
	if errConn != nil {
		return nil, errors.Wrap(errConn, "creating connection")
	}
	return sender, nil
}

func (m *MTProto) ExportNewSender(dcID int, mem bool, cdn ...bool) (*MTProto, error) {
	newAddr := m.DcList.GetHostIp(dcID, false, m.IpV6)
	logger := utils.NewLogger("gogram [mtproto-exp]").SetLevel(utils.InfoLevel)

	if len(cdn) > 0 && cdn[0] {
		newAddr, _ = m.DcList.GetCdnAddr(dcID)
		logger.SetPrefix("gogram [mtproto-cdn]")
	}

	cfg := Config{
		DataCenter:    dcID,
		PublicKey:     m.publicKey,
		ServerHost:    newAddr,
		AuthKeyFile:   "__exp_" + strconv.Itoa(dcID) + ".dat",
		MemorySession: mem,
		Logger:        logger,
		Proxy:         m.proxy,
		AppID:         m.appID,
		Ipv6:          m.IpV6,
	}

	if dcID == m.GetDC() {
		cfg.SessionStorage = m.sessionStorage
		cfg.StringSession = session.NewStringSession(
			m.authKey, m.authKeyHash, dcID, newAddr, m.appID,
		).Encode()
	}

	sender, err := NewMTProto(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "creating new MTProto")
	}

	sender.noRedirect = true
	sender.exported = true
	if len(cdn) > 0 && cdn[0] {
		sender.cdn = true
	}

	if err := sender.CreateConnection(false); err != nil {
		return nil, errors.Wrap(err, "creating connection: exporting")
	}

	return sender, nil
}

func (m *MTProto) CreateConnection(withLog bool) error {
	if m.terminated.Load() {
		return errors.New("mtproto is terminated, cannot create connection")
	}
	m.stopRoutines()

	ctx, cancelfunc := context.WithCancel(context.Background())
	m.ctxCancel = cancelfunc
	if withLog {
		m.Logger.Info(fmt.Sprintf("connecting to [%s] - <%s> ...", utils.FmtIp(m.Addr), utils.Vtcp(m.IpV6)))
	} else {
		m.Logger.Debug(fmt.Sprintf("connecting to [%s] - <%s> ...", utils.FmtIp(m.Addr), utils.Vtcp(m.IpV6)))
	}
	err := m.connect(ctx)
	if err != nil {
		m.Logger.Error(errors.Wrap(err, "creating connection"))
		return err
	}
	m.tcpState.SetActive(true)
	if withLog {
		if m.proxy != nil && m.proxy.Host != "" {
			m.Logger.Info(fmt.Sprintf("connection to (~%s)[%s] - <%s> established", utils.FmtIp(m.proxy.Host), m.Addr, utils.Vtcp(m.IpV6)))
		} else {
			m.Logger.Info(fmt.Sprintf("connection to [%s] - <%s> established", utils.FmtIp(m.Addr), utils.Vtcp(m.IpV6)))
		}
	} else {
		if m.proxy != nil && m.proxy.Host != "" {
			m.Logger.Debug(fmt.Sprintf("connection to (~%s)[%s] - <%s> established", utils.FmtIp(m.proxy.Host), m.Addr, utils.Vtcp(m.IpV6)))
		} else {
			m.Logger.Debug(fmt.Sprintf("connection to [%s] - <%s> established", utils.FmtIp(m.Addr), utils.Vtcp(m.IpV6)))
		}
	}

	m.startReadingResponses(ctx)

	if !m.exported && !m.cdn {
		go m.longPing(ctx)
	}

	if !m.encrypted {
		m.Logger.Debug("authKey not found, creating new one")
		err = m.makeAuthKey()
		if err != nil {
			return err
		}
		m.Logger.Debug("authKey created and saved")
	}

	return nil
}

func (m *MTProto) connect(ctx context.Context) error {
	var err error
	m.transport, err = transport.NewTransport(
		m,
		transport.TCPConnConfig{
			Ctx:     ctx,
			Host:    utils.FmtIp(m.Addr),
			IpV6:    m.IpV6,
			Timeout: defaultTimeout,
			Socks:   m.proxy,
		},
		m.mode,
	)
	if err != nil {
		return fmt.Errorf("creating transport: %w", err)
	}

	return nil
}

func (m *MTProto) makeRequest(data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	m.tcpState.WaitForActive()

	resp, _, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") || strings.Contains(err.Error(), "transport is closed") || strings.Contains(err.Error(), "connection was forcibly closed") || strings.Contains(err.Error(), "connection reset by peer") || strings.Contains(err.Error(), "broken pipe") {
			m.Logger.Info(fmt.Sprintf("connection closed due to broken tcp, reconnecting to [%s] - <%s> ...", m.Addr, utils.Vtcp(m.IpV6)))
			err = m.Reconnect(false)
			if err != nil {
				return nil, errors.Wrap(err, "reconnecting")
			}
			return m.makeRequest(data, expectedTypes...)
		}
		return nil, errors.Wrap(err, "sending packet")
	}
	response := <-resp
	switch r := response.(type) {
	case *objects.RpcError:
		if err := RpcErrorToNative(r).(*ErrResponseCode); strings.Contains(err.Message, "FLOOD_WAIT_") || strings.Contains(err.Message, "FLOOD_PREMIUM_WAIT_") {
			if done := m.floodHandler(err); !done {
				return nil, RpcErrorToNative(r)
			} else {
				return m.makeRequest(data, expectedTypes...)
			}
		}
		m.errorHandler(RpcErrorToNative(r))

		return nil, RpcErrorToNative(r)

	case *errorSessionConfigsChanged:
		m.Logger.Debug("session configs changed, resending request")
		return m.makeRequest(data, expectedTypes...)
	}

	return tl.UnwrapNativeTypes(response), nil
}

func (m *MTProto) makeRequestCtx(ctx context.Context, data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	m.tcpState.WaitForActive()

	resp, msgId, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") || strings.Contains(err.Error(), "transport is closed") || strings.Contains(err.Error(), "connection was forcibly closed") || strings.Contains(err.Error(), "connection reset by peer") || strings.Contains(err.Error(), "broken pipe") {
			m.Logger.Debug("connection closed due to broken tcp, reconnecting to [" + m.Addr + "]" + " - <Tcp> ...")
			err = m.Reconnect(false)
			if err != nil {
				return nil, errors.Wrap(err, "reconnecting")
			}
			return m.makeRequestCtx(ctx, data, expectedTypes...)
		}
		return nil, errors.Wrap(err, "sending packet")
	}

	select {
	case <-ctx.Done():
		go m.writeRPCResponse(int(msgId), &objects.Null{})
		return nil, ctx.Err()
	case response := <-resp:
		switch r := response.(type) {
		case *objects.RpcError:
			if err := RpcErrorToNative(r).(*ErrResponseCode); strings.Contains(err.Message, "FLOOD_WAIT_") || strings.Contains(err.Message, "FLOOD_PREMIUM_WAIT_") {
				if done := m.floodHandler(err); !done {
					return nil, RpcErrorToNative(r)
				} else {
					return m.makeRequestCtx(ctx, data, expectedTypes...)
				}
			}
			return nil, RpcErrorToNative(r)

		case *errorSessionConfigsChanged:
			m.Logger.Debug("session configs changed, resending request")
			return m.makeRequestCtx(ctx, data, expectedTypes...)
		}

		return tl.UnwrapNativeTypes(response), nil
	}
}

func (m *MTProto) InvokeRequestWithoutUpdate(data tl.Object, expectedTypes ...reflect.Type) error {
	_, _, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		return errors.Wrap(err, "sending packet")
	}
	return err
}

func (m *MTProto) IsTcpActive() bool {
	return m.tcpState.Active.Load()
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

func (m *MTProto) Reconnect(WithLogs bool) error {
	if m.terminated.Load() {
		return nil
	}
	err := m.Disconnect()
	if err != nil {
		return errors.Wrap(err, "disconnecting")
	}
	if WithLogs {
		m.Logger.Info(fmt.Sprintf("reconnecting to [%s] - <Tcp> ...", m.Addr))
	}
	err = m.CreateConnection(WithLogs)
	if err == nil && WithLogs {
		m.Logger.Info(fmt.Sprintf("reconnected to [%s] - <Tcp>", m.Addr))
	}
	m.Ping()

	return errors.Wrap(err, "recreating connection")
}

// keep pinging to keep the connection alive
func (m *MTProto) longPing(ctx context.Context) {
	m.routineswg.Add(1)
	defer m.routineswg.Done()

	for {
		time.Sleep(30 * time.Second)
		select {
		case <-ctx.Done():
			return
		default:
			m.tcpState.WaitForActive()
			m.Ping()
		}
	}
}

func (m *MTProto) Ping() time.Duration {
	start := time.Now()
	m.Logger.Debug("rpc - pinging server ...")
	m.InvokeRequestWithoutUpdate(&utils.PingParams{
		PingID: time.Now().Unix(),
	})
	return time.Since(start)
}

func (m *MTProto) startReadingResponses(ctx context.Context) {
	m.routineswg.Add(1)

	go func() {
		defer m.routineswg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				m.TcpState().WaitForActive()
				err := m.readMsg()

				if err != nil {
					if strings.Contains(err.Error(), "unexpected error: unexpected EOF") {
						m.Logger.Debug("tcp connection closed, reconnecting to [" + m.Addr + "] - <Tcp> ...")
						err = m.Reconnect(false)
						if err != nil {
							m.Logger.Error(errors.Wrap(err, "reconnecting"))
						}
					} else if strings.Contains(err.Error(), "required to reconnect!") { // network is not stable
						m.Logger.Debug("packet read error, reconnecting to [" + m.Addr + "] - <Tcp> ...")
						err = m.Reconnect(false)
						if err != nil {
							m.Logger.Error(errors.Wrap(err, "reconnecting"))
						}
					}
				}

				switch err {
				case nil:
				case context.Canceled:
					return
				case io.EOF:
					m.Logger.Debug("eof error, reconnecting to [" + m.Addr + "] - <Tcp> ...")
					err = m.Reconnect(false)
					if err != nil {
						m.Logger.Error(errors.Wrap(err, "reconnecting"))
					}
					return
				default:
					switch e := err.(type) {
					case *ErrResponseCode:
						if e.Code == 4294966892 {
							m.handle404Error()
						} else {
							m.Logger.Debug(errors.New("[RESPONSE_ERROR_CODE] - " + e.Error()))
						}
					case *transport.ErrCode:
						m.Logger.Error(errors.New("[TRANSPORT_ERROR_CODE] - " + e.Error()))
					}

					if !m.terminated.Load() {
						m.Logger.Debug(errors.Wrap(err, "reading message >>"))
						// is reconnect required here?
						m.Reconnect(false)
					}
				}
			}
		}
	}()
}

func (m *MTProto) handle404Error() {
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
		m.Logger.Debug(fmt.Sprintf("-404 error occurred %d times, attempting to reconnect", m.authKey404[0]))
		err := m.Reconnect(false)
		if err != nil {
			m.Logger.Error(errors.Wrap(err, "reconnecting"))
		}
	} else if m.authKey404[0] >= 16 {
		panic("[AUTH_KEY_INVALID] (code -404) - too many failures")
	}
}

func (m *MTProto) readMsg() error {
	if m.transport == nil {
		return errors.New("must setup connection before reading messages")
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
			return errors.Wrap(err, "reading message")
		}
	}

	if m.serviceModeActivated {
		var obj tl.Object
		obj, err = tl.DecodeUnknownObject(response.GetMsg())
		if err != nil {
			return errors.Wrap(err, "parsing object")
		}
		m.serviceChannel <- obj
		return nil
	}

	err = m.processResponse(response)
	if err != nil {
		m.Logger.Debug(errors.Wrap(err, "decoding unknown object"))
		return errors.Wrap(err, "incoming update")
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
		return fmt.Errorf("unmarshalling response: %w", err)
	}

messageTypeSwitching:
	switch message := data.(type) {
	case *objects.MessageContainer:
		for _, v := range *message {
			err := m.processResponse(v)
			if err != nil {
				return errors.Wrap(err, "processing item in container")
			}
		}

	case *objects.BadServerSalt:
		m.serverSalt = message.NewSalt
		if err := m.SaveSession(m.memorySession); err != nil {
			return errors.Wrap(err, "saving session")
		}

		var respChannelsBackup *utils.SyncIntObjectChan
		m.mutex.Lock()
		defer m.mutex.Unlock()
		respChannelsBackup = m.responseChannels

		m.responseChannels = utils.NewSyncIntObjectChan()
		for _, k := range respChannelsBackup.Keys() {
			if v, ok := respChannelsBackup.Get(k); ok {
				respChannelsBackup.Delete(k)
				select {
				case v <- &errorSessionConfigsChanged{}:
				case <-time.After(1 * time.Millisecond):
				}
			}
		}

	case *objects.NewSessionCreated:
		m.serverSalt = message.ServerSalt
		if err := m.SaveSession(m.memorySession); err != nil {
			m.Logger.Error(errors.Wrap(err, "saving session"))
		}

	case *objects.MsgsNewDetailedInfo:
		m.pendingAcks.Add(message.AnswerMsgID)
		return nil

	case *objects.MsgsDetailedInfo:
		m.pendingAcks.Add(message.AnswerMsgID)
		return nil

	case *objects.Pong:
		m.Logger.Debug("rpc - ping: " + fmt.Sprintf("%T", message))

	case *objects.MsgsAck:
		// do nothing

	case *objects.BadMsgNotification:
		badMsg := BadMsgErrorFromNative(message)
		if badMsg.Code == 16 || badMsg.Code == 17 {
			m.Logger.Warn("Your system date and time are possibly incorrect, please adjust them")
			m.offsetTime()
		}
		m.Logger.Debug("bad-msg-notification: " + badMsg.Error())
		return badMsg

	case *objects.RpcResult:
		obj := message.Obj
		if v, ok := obj.(*objects.GzipPacked); ok {
			obj = v.Obj
		}
		m.Logger.Debug("rpc - response: " + fmt.Sprintf("%T", obj))
		err := m.writeRPCResponse(int(message.ReqMsgID), obj)
		if err != nil {
			if strings.Contains(err.Error(), "no response channel found") {
				m.Logger.Debug(errors.Wrap(err, "writing rpc response"))
			} else {
				return errors.Wrap(err, "writing rpc response")
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
			m.Logger.Debug("unhandled update: " + fmt.Sprintf("%T", message))
		}
	}

	if m.pendingAcks.Len() >= acksThreshold {
		m.Logger.Debug("Sending acks", m.pendingAcks.Len())

		_, err := m.MakeRequest(&objects.MsgsAck{MsgIDs: m.pendingAcks.Keys()})
		if err != nil {
			return errors.Wrap(err, "sending acks")
		}

		m.pendingAcks.Clear()
	}

	return nil
}

func MessageRequireToAck(msg tl.Object) bool {
	switch msg.(type) {
	case *objects.MsgsAck:
		return false
	default:
		return true
	}
}

type TcpState struct {
	Active atomic.Bool
	Cond   *sync.Cond
}

func (m *MTProto) TcpState() *TcpState {
	return m.tcpState
}

func (m *TcpState) SetActive(active bool) {
	m.Cond.L.Lock()
	m.Active.Store(active)
	m.Cond.L.Unlock()
	m.Cond.Broadcast()
}

func (m *TcpState) WaitForActive() {
	m.Cond.L.Lock()
	defer m.Cond.L.Unlock()

	for !m.Active.Load() {
		m.Cond.Wait()
	}
}

func NewTcpState() *TcpState {
	return &TcpState{
		Active: atomic.Bool{},
		Cond:   sync.NewCond(&sync.Mutex{}),
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
