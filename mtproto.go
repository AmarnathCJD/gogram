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
	Addr          string
	appID         int32
	proxy         *url.URL
	transport     transport.Transport
	stopRoutines  context.CancelFunc
	routineswg    sync.WaitGroup
	memorySession bool
	tcpActive     bool
	timeOffset    int64
	mode          mode.Variant

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

	serviceChannel       chan tl.Object
	serviceModeActivated bool

	authKey404 []int64

	Logger *utils.Logger

	serverRequestHandlers []func(i any) bool
	floodHandler          func(err error) bool
}

type Config struct {
	AuthKeyFile    string
	StringSession  string
	SessionStorage session.SessionLoader
	MemorySession  bool
	AppID          int32
	FloodHandler   func(err error) bool

	ServerHost string
	PublicKey  *rsa.PublicKey
	DataCenter int
	LogLevel   string
	Proxy      *url.URL
	Mode       string
}

func NewMTProto(c Config) (*MTProto, error) {
	if c.SessionStorage == nil {
		if c.MemorySession {
			c.SessionStorage = session.NewInMemory()
		} else {
			c.SessionStorage = session.NewFromFile(c.AuthKeyFile)
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
		Logger:                utils.NewLogger("gogram [mtproto]").SetLevel(c.LogLevel),
		memorySession:         c.MemorySession,
		appID:                 c.AppID,
		proxy:                 c.Proxy,
		floodHandler:          func(err error) bool { return false },
		mode:                  parseTransportMode(c.Mode),
	}

	mtproto.Logger.Debug("initializing mtproto...")
	mtproto.offsetTime()

	if loaded != nil || c.StringSession != "" {
		mtproto.encrypted = true
	}
	if err := mtproto.loadAuth(c.StringSession, loaded); err != nil {
		return nil, errors.Wrap(err, "loading auth")
	}

	if c.FloodHandler != nil {
		mtproto.floodHandler = c.FloodHandler
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
	m.Logger.Debug("importing Auth from session...")
	if !m.memorySession {
		if err := m.SaveSession(); err != nil {
			return errors.Wrap(err, "saving session")
		}
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
	m.Logger.Debug("imported auth key, auth key hash, addr, dc, appID")
	if !m.memorySession {
		if err := m.SaveSession(); err != nil {
			return false, errors.Wrap(err, "saving session")
		}
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
	m.authKey, m.authKeyHash, m.Addr, m.appID = sessionString.AuthKey(), sessionString.AuthKeyHash(), sessionString.IpAddr(), sessionString.AppID()
	m.Logger.Debug("importing - auth from stringSession...")
	if !m.memorySession {
		if err := m.SaveSession(); err != nil {
			return false, fmt.Errorf("saving session: %w", err)
		}
	}
	return true, nil
}

func (m *MTProto) GetDC() int {
	return utils.SearchAddr(m.Addr)
}

func (m *MTProto) AppID() int32 {
	return m.appID
}

func (m *MTProto) SetAppID(appID int32) {
	m.appID = appID
}

func (m *MTProto) SwitchDc(dc int) (*MTProto, error) {
	if m.noRedirect {
		return m, nil
	}
	newAddr := utils.GetAddr(dc)
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
		MemorySession: m.memorySession,
		LogLevel:      m.Logger.Lev(),
		Proxy:         m.proxy,
		AppID:         m.appID,
	}
	sender, err := NewMTProto(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "creating new MTProto")
	}
	sender.serverRequestHandlers = m.serverRequestHandlers
	m.stopRoutines()
	m.Logger.Info(fmt.Sprintf("user migrated to DC %s - %s", strconv.Itoa(dc), newAddr))
	m.Logger.Debug("reconnecting to new DC with new auth key")
	errConn := sender.CreateConnection(true)
	if errConn != nil {
		return nil, errors.Wrap(errConn, "creating connection")
	}
	return sender, nil
}

func (m *MTProto) ExportNewSender(dcID int, mem bool) (*MTProto, error) {
	newAddr := utils.GetAddr(dcID)
	execWorkDir, err := os.Executable()
	if err != nil {
		return nil, errors.Wrap(err, "getting executable directory")
	}
	wd := filepath.Dir(execWorkDir)
	cfg := Config{DataCenter: dcID, PublicKey: m.publicKey, ServerHost: newAddr, AuthKeyFile: filepath.Join(wd, "exported_sender"), MemorySession: mem, LogLevel: "disabled", Proxy: m.proxy, AppID: m.appID}
	if dcID == m.GetDC() {
		cfg.SessionStorage = m.sessionStorage
	}

	sender, _ := NewMTProto(cfg)
	sender.noRedirect = true
	err = sender.CreateConnection(true)
	if err != nil {
		return nil, errors.Wrap(err, "creating connection")
	}

	return sender, nil
}

func (m *MTProto) CreateConnection(withLog bool) error {
	ctx, cancelfunc := context.WithCancel(context.Background())
	m.stopRoutines = cancelfunc
	if withLog {
		m.Logger.Info(fmt.Sprintf("connecting to [%s] - <Tcp> ...", m.Addr))
	} else {
		m.Logger.Debug("connecting to [" + m.Addr + "] - <Tcp> ...")
	}
	err := m.connect(ctx)
	if err != nil {
		m.Logger.Error(errors.Wrap(err, "creating connection"))
		return err
	}
	m.tcpActive = true
	if withLog {
		if m.proxy != nil && m.proxy.Host != "" {
			m.Logger.Info(fmt.Sprintf("connection to (~%s)[%s] - <Tcp> established", m.proxy.Host, m.Addr))
		} else {
			m.Logger.Info(fmt.Sprintf("connection to [%s] - <Tcp> established", m.Addr))
		}
	} else {
		if m.proxy != nil && m.proxy.Host != "" {
			m.Logger.Debug("connection to (~" + m.proxy.Host + ")[" + m.Addr + "] - <Tcp> established")
		} else {
			m.Logger.Debug("connection to [" + m.Addr + "] - <Tcp> established")
		}
	}

	m.startReadingResponses(ctx)
	go m.longPing()
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
			Host:    m.Addr,
			Timeout: defaultTimeout,
			Socks:   m.proxy,
		},
		m.mode,
	)
	if err != nil {
		return fmt.Errorf("creating transport: %w", err)
	}

	go closeOnCancel(ctx, m.transport)
	return nil
}

func (m *MTProto) makeRequest(data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	if !m.TcpActive() {
		_ = m.CreateConnection(false)
	}
	resp, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") || strings.Contains(err.Error(), "transport is closed") {
			m.Logger.Info("connection closed due to broken tcp, reconnecting to [" + m.Addr + "]" + " - <Tcp> ...")
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
		return nil, RpcErrorToNative(r)

	case *errorSessionConfigsChanged:
		m.Logger.Debug("session configs changed, resending request")
		return m.makeRequest(data, expectedTypes...)
	}

	return tl.UnwrapNativeTypes(response), nil
}

func (m *MTProto) InvokeRequestWithoutUpdate(data tl.Object, expectedTypes ...reflect.Type) error {
	_, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		return errors.Wrap(err, "sending packet")
	}
	return err
}

func (m *MTProto) TcpActive() bool {
	return m.tcpActive
}

func (m *MTProto) Disconnect() error {
	m.stopRoutines()
	m.tcpActive = false

	return nil
}

func (m *MTProto) Terminate() error {
	m.stopRoutines()
	m.responseChannels.Close()
	m.Logger.Info("terminating connection to [" + m.Addr + "] - <Tcp> ...")
	m.tcpActive = false
	return nil
}

func (m *MTProto) Reconnect(WithLogs bool) error {
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
	m.InvokeRequestWithoutUpdate(&utils.PingParams{
		PingID: 123456789,
	})

	return errors.Wrap(err, "recreating connection")
}

// keep pinging to keep the connection alive
func (m *MTProto) longPing() {
	for {
		time.Sleep(30 * time.Second)
		pingId := time.Now().Unix()
		m.InvokeRequestWithoutUpdate(&utils.PingParams{
			PingID: pingId,
		})
	}
}

func (m *MTProto) Ping() time.Duration {
	start := time.Now()
	m.InvokeRequestWithoutUpdate(&utils.PingParams{
		PingID: 123456789,
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
				if !m.tcpActive {
					m.Logger.Warn("connection is not established with, stopping Updates Queue")
					return
				}
				err := m.readMsg()

				if err != nil {
					if strings.Contains(err.Error(), "unexpected error: unexpected EOF") {
						m.Logger.Debug("unexpected EOF, reconnecting to [" + m.Addr + "] - <Tcp> ...") // TODO: beautify this
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
					m.Logger.Debug("EOF error, reconnecting to [" + m.Addr + "] - <Tcp> ...")
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

					m.Logger.Debug(errors.Wrap(err, "reading message"))

					if err := m.Reconnect(false); err != nil {
						m.Logger.Error(errors.Wrap(err, "reconnecting"))
					}
				}
			}
		}
	}()
}

func (m *MTProto) handle404Error() {
	if len(m.authKey404) == 0 {
		m.authKey404 = []int64{1, time.Now().Unix()}
	} else {
		if time.Now().Unix()-m.authKey404[1] < 30 { // repeated failures
			m.authKey404[0]++
		} else {
			m.authKey404[0] = 1
		}
		m.authKey404[1] = time.Now().Unix()
	}

	if m.authKey404[0] == 4 {
		m.Logger.Error(errors.New("(last retry: 4) reconnecting due to [AUTH_KEY_INVALID] (code -404)"))
		err := m.Reconnect(false)
		if err != nil {
			m.Logger.Error(errors.Wrap(err, "reconnecting"))
		}
	} else if m.authKey404[0] > 4 {
		panic("[AUTH_KEY_INVALID] the auth key is invalid and needs to be reauthenticated (code -404)")
	} else {
		m.Logger.Error(errors.New("(retry: " + strconv.FormatInt(m.authKey404[0], 10) + ") [AUTH_KEY_INVALID] the auth key is invalid and needs to be reauthenticated (code -404)"))
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
		if !m.memorySession {
			err := m.SaveSession()
			if err != nil {
				return errors.Wrap(err, "saving session")
			}
		}

		var respChannelsBackup *utils.SyncIntObjectChan
		m.mutex.Lock()
		defer m.mutex.Unlock()
		respChannelsBackup = m.responseChannels

		m.responseChannels = utils.NewSyncIntObjectChan()
		//m.Reconnect(false)

		sendToChannelWithTimeout := func(ch chan tl.Object, data tl.Object) {
			timeout := time.After(1 * time.Millisecond)
			select {
			case ch <- data:
			case <-timeout:
			}
		}

		for _, k := range respChannelsBackup.Keys() {
			v, _ := respChannelsBackup.Get(k)
			respChannelsBackup.Delete(k)
			sendToChannelWithTimeout(v, &errorSessionConfigsChanged{})
		}

	case *objects.NewSessionCreated:
		m.serverSalt = message.ServerSalt
		if !m.memorySession {
			err := m.SaveSession()
			if err != nil {
				m.Logger.Error(errors.Wrap(err, "saving session"))
			}
		}

	case *objects.MsgsNewDetailedInfo:
		m.pendingAcks.Add(message.AnswerMsgID)
		return nil

	case *objects.MsgsDetailedInfo:
		m.pendingAcks.Add(message.AnswerMsgID)
		return nil

	case *objects.Pong, *objects.MsgsAck:
		m.Logger.Debug("rpc - ping: " + fmt.Sprintf("%T", message))

	case *objects.BadMsgNotification:
		badMsg := BadMsgErrorFromNative(message)
		if badMsg.Code == 16 || badMsg.Code == 17 {
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
		m.Logger.Error(errors.Wrap(err, "off-setting time"))
		return
	}

	if timeResponse.Unixtime <= currentLocalTime || math.Abs(float64(timeResponse.Unixtime-currentLocalTime)) < 60 {
		return // -no need to offset time
	}

	m.timeOffset = timeResponse.Unixtime - currentLocalTime
	m.Logger.Info("system time is out of sync, offsetting time by " + strconv.FormatInt(m.timeOffset, 10) + " seconds")
}

func closeOnCancel(ctx context.Context, c io.Closer) {
	<-ctx.Done()
	go func() {
		defer func() { recover() }()
		c.Close()
	}()
}
