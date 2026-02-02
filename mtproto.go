// Copyright (c) 2024 RoseLoverX

package gogram

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
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

const defaultTimeout = 65 * time.Second

type MTProto struct {
	Addr          string
	appID         int32
	socksProxy    *url.URL
	socksActive   bool
	transport     transport.Transport
	stopRoutines  context.CancelFunc
	routineswg    sync.WaitGroup
	memorySession bool
	tcpActive     bool

	authKey []byte

	authKeyHash []byte

	serverSalt int64
	encrypted  bool
	sessionId  int64

	mutex            sync.Mutex
	responseChannels *utils.SyncIntObjectChan
	expectedTypes    *utils.SyncIntReflectTypes

	seqNoMutex         sync.Mutex
	seqNo              int32
	lastMessageIDMutex sync.Mutex
	lastMessageID      int64

	sessionStorage session.SessionLoader

	PublicKey *rsa.PublicKey

	serviceChannel       chan tl.Object
	serviceModeActivated bool

	Logger *utils.Logger

	serverRequestHandlers []func(i any) bool
}

type Config struct {
	AuthKeyFile    string
	StringSession  string
	SessionStorage session.SessionLoader
	MemorySession  bool
	AppID          int32

	ServerHost string
	PublicKey  *rsa.PublicKey
	DataCenter int
	LogLevel   string
	SocksProxy *url.URL
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
			if _, err := os.OpenFile(filepath.Dir(c.AuthKeyFile), os.O_WRONLY, 0666); err != nil {
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
		PublicKey:             c.PublicKey,
		responseChannels:      utils.NewSyncIntObjectChan(),
		expectedTypes:         utils.NewSyncIntReflectTypes(),
		serverRequestHandlers: make([]func(i any) bool, 0),
		Logger:                utils.NewLogger("gogram - mtproto").SetLevel(c.LogLevel),
		memorySession:         c.MemorySession,
		appID:                 c.AppID,
		socksProxy:            c.SocksProxy,
	}
	if loaded != nil || c.StringSession != "" {
		mtproto.encrypted = true
	}
	if err := mtproto.loadAuth(c.StringSession, loaded); err != nil {
		return nil, errors.Wrap(err, "loading auth")
	}
	return mtproto, nil
}

func (m *MTProto) loadAuth(stringSession string, sess *session.Session) error {
	if stringSession != "" {
		_, err := m.ImportAuth(stringSession)
		if err != nil {
			return errors.Wrap(err, "importing string session")
		}
	} else {
		if sess != nil {
			m.LoadSession(sess)
		}
	}
	return nil
}

func (m *MTProto) ExportAuth() ([]byte, []byte, string, int, int32) {
	return m.authKey, m.authKeyHash, m.Addr, m.GetDC(), m.appID
}

func (m *MTProto) ImportRawAuth(authKey []byte, authKeyHash []byte, addr string, _ int, appID int32) (bool, error) {
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
	m.Logger.Debug("importing Auth from stringSession...")
	if !m.memorySession {
		if err := m.SaveSession(); err != nil {
			return false, fmt.Errorf("saving session: %w", err)
		}
	}
	return true, nil
}

func (m *MTProto) GetDC() int {
	for dc, addr := range utils.DcList {
		if addr == m.Addr {
			return dc
		}
	}
	return 4
}

func (m *MTProto) AppID() int32 {
	return m.appID
}

func (m *MTProto) ReconnectToNewDC(dc int) (*MTProto, error) {
	newAddr, isValid := utils.DcList[dc]
	if !isValid {
		return nil, errors.New("invalid DC ID provided")
	}
	m.sessionStorage.Delete()
	m.Logger.Debug("deleted old auth key file")
	cfg := Config{
		DataCenter:    dc,
		PublicKey:     m.PublicKey,
		ServerHost:    newAddr,
		AuthKeyFile:   m.sessionStorage.Path(),
		MemorySession: m.memorySession,
		LogLevel:      m.Logger.Lev(),
		SocksProxy:    m.socksProxy,
		AppID:         m.appID,
	}
	sender, err := NewMTProto(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "creating new MTProto")
	}
	sender.serverRequestHandlers = m.serverRequestHandlers
	m.stopRoutines()
	m.Logger.Info(fmt.Sprintf("user migrated to -> [DC %d]", dc))
	m.Logger.Debug("reconnecting to new DC with new auth key")
	errConn := sender.CreateConnection(true)
	if errConn != nil {
		return nil, errors.Wrap(errConn, "creating connection")
	}
	return sender, nil
}

func (m *MTProto) ExportNewSender(dcID int, mem bool) (*MTProto, error) {
	newAddr := utils.DcList[dcID]
	execWorkDir, err := os.Executable()
	if err != nil {
		return nil, errors.Wrap(err, "getting executable directory")
	}
	wd := filepath.Dir(execWorkDir)
	cfg := Config{DataCenter: dcID, PublicKey: m.PublicKey, ServerHost: newAddr, AuthKeyFile: filepath.Join(wd, "exported_sender"), MemorySession: mem, LogLevel: m.Logger.Lev(), SocksProxy: m.socksProxy, AppID: m.appID}
	if dcID == m.GetDC() {
		cfg.SessionStorage = m.sessionStorage
	}
	sender, _ := NewMTProto(cfg)
	m.Logger.Info("exporting new sender for [DC " + strconv.Itoa(dcID) + "]")
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
		m.Logger.Info("Connecting to [" + m.Addr + "] - <TCPFull> ...")
	}
	err := m.connect(ctx)
	if err != nil {
		return err
	}
	m.tcpActive = true
	if withLog {
		if m.socksProxy != nil && m.socksProxy.Host != "" {
			m.Logger.Info("Connection to (" + m.socksProxy.Host + ")[" + m.Addr + "] - <TCPFull> established")
		} else {
			m.Logger.Info("Connection to [" + m.Addr + "] - <TCPFull> established")
		}
	}
	m.startReadingResponses(ctx)
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
			Socks:   m.socksProxy,
		},
		mode.Intermediate,
	)
	if err != nil {
		return fmt.Errorf("creating transport: %w", err)
	}

	closeOnCancel(ctx, m.transport)
	return nil
}

func (m *MTProto) makeRequest(data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	if !m.TcpActive() {
		return nil, errors.New("Can't make request. Connection is not established")
	}
	resp, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") || strings.Contains(err.Error(), "transport is closed") {
			m.Logger.Info("connection closed due to broken pipe, reconnecting to [" + m.Addr + "]" + " - <TCPFull> ...")
			err = m.Reconnect(false)
			if err != nil {
				m.Logger.Error("reconnecting: " + err.Error())
				return nil, errors.New("reconnecting: " + err.Error())
			}
			return m.makeRequest(data, expectedTypes...)
		}
		return nil, errors.Wrap(err, "sending packet")
	}
	response := <-resp
	switch r := response.(type) {
	case *objects.RpcError:
		realErr := RpcErrorToNative(r).(*ErrResponseCode)
		if strings.Contains(realErr.Message, "FLOOD_WAIT_") {
			m.Logger.Info("Flood wait detected on '" + strings.ReplaceAll(reflect.TypeOf(data).Elem().Name(), "Params", "") + fmt.Sprintf("' request. sleeping for %s", (time.Duration(realErr.AdditionalInfo.(int))*time.Second).String()))
			time.Sleep(time.Duration(realErr.AdditionalInfo.(int)) * time.Second)
			return m.makeRequest(data, expectedTypes...)
		}
		return nil, realErr

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
	// m.responseChannels.Close()
	return nil
}

func (m *MTProto) Terminate() error {
	m.stopRoutines()
	m.responseChannels.Close()
	m.Logger.Info("terminating connection to [" + m.Addr + "] - <TCPFull> ...")
	m.tcpActive = false
	return nil
}

func (m *MTProto) Reconnect(WithLogs bool) error {
	err := m.Disconnect()
	if err != nil {
		return errors.Wrap(err, "disconnecting")
	}
	if WithLogs {
		m.Logger.Info("Reconnecting to [" + m.Addr + "] - <TCPFull> ...")
	}

	err = m.CreateConnection(WithLogs)
	if err == nil && WithLogs {
		m.Logger.Info("Reconnected to [" + m.Addr + "] - <TCPFull> ...")
	}
	m.InvokeRequestWithoutUpdate(&utils.PingParams{
		PingID: 123456789,
	})
	return errors.Wrap(err, "recreating connection")
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
					m.Logger.Warn("Connection is not established with, stopping Updates Queue")
					return
				}
				err := m.readMsg()
				switch err {
				case nil:
				case context.Canceled:
					return
				case io.EOF:
					err = m.Reconnect(false)
					if err != nil {
						m.Logger.Error(errors.Wrap(err, "reconnecting"))
					}
					return

				default:
					if e, ok := err.(transport.ErrCode); ok {
						if int(e) == 4294966892 {
							err = m.makeAuthKey()
							if err != nil {
								m.Logger.Error(errors.Wrap(err, "making auth key"))
							}
						} else {
							m.Logger.Error("Unhandled errorCode: " + fmt.Sprintf("%d", e))
						}
					}
					if strings.Contains(err.Error(), "required to reconnect!") {
						err = m.Reconnect(false)
						if err != nil {
							m.Logger.Error(errors.Wrap(err, "reconnecting error"))
						}
						return
					} else {
						err = m.Reconnect(false)
						if err != nil {
							m.Logger.Error(errors.Wrap(err, "reconnecting error"))
						}
					}
				}
			}
		}
	}()
}

func (m *MTProto) readMsg() error {
	if m.transport == nil {
		return errors.New("must setup connection before reading messages")
	}
	response, err := m.transport.ReadMsg()
	if err != nil {
		if e, ok := err.(transport.ErrCode); ok {
			return &ErrResponseCode{Code: int(e)}
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
		return errors.Wrap(err, "incoming update")
	}
	return nil
}

func (m *MTProto) processResponse(msg messages.Common) error {
	var data tl.Object
	var err error
	if et, ok := m.expectedTypes.Get(msg.GetMsgID()); ok && len(et) > 0 {
		data, err = tl.DecodeUnknownObject(msg.GetMsg(), et...)
	} else {
		data, err = tl.DecodeUnknownObject(msg.GetMsg())
	}
	if err != nil {
		m.Logger.Error(errors.Wrap(err, "decoding unknown object"))
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
		m.Reconnect(false)

		m.mutex.Lock()
		for _, k := range m.responseChannels.Keys() {
			v, _ := m.responseChannels.Get(k)
			v <- &errorSessionConfigsChanged{}
		}

		m.mutex.Unlock()

	case *objects.NewSessionCreated:
		m.serverSalt = message.ServerSalt
		if !m.memorySession {
			err := m.SaveSession()
			if err != nil {
				m.Logger.Error(errors.Wrap(err, "saving session"))
			}
		}

	case *objects.Pong, *objects.MsgsAck:

	case *objects.BadMsgNotification:
		badMsg := BadMsgErrorFromNative(message)
		m.Logger.Debug("BadMsgNotification: " + badMsg.Error())
		return badMsg
	case *objects.RpcResult:
		obj := message.Obj
		if v, ok := obj.(*objects.GzipPacked); ok {
			obj = v.Obj
		}
		m.Logger.Debug("RPC response: " + fmt.Sprintf("%T", obj))
		err := m.writeRPCResponse(int(message.ReqMsgID), obj)
		if err != nil {
			return errors.Wrap(err, "writing RPC response")
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
			m.Logger.Warn("unhandled message: " + fmt.Sprintf("%T", message))
		}
	}

	if (msg.GetSeqNo() & 1) != 0 {
		_, err := m.MakeRequest(&objects.MsgsAck{MsgIDs: []int64{int64(msg.GetMsgID())}})
		if err != nil {
			return errors.Wrap(err, "sending ack")
		}
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

func closeOnCancel(ctx context.Context, c io.Closer) {
	go func() {
		<-ctx.Done()
		go func() {
			defer func() { recover() }()
			c.Close()
		}()
	}()
}
