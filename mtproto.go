// Copyright (c) 2022 RoseLoverX

package mtproto

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
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

var wd, _ = os.Getwd()

type MTProto struct {
	Addr          string
	AppID         int32
	transport     transport.Transport
	stopRoutines  context.CancelFunc
	routineswg    sync.WaitGroup
	memorySession bool

	authKey []byte

	authKeyHash []byte

	serverSalt int64
	encrypted  bool
	sessionId  int64

	mutex            sync.Mutex
	responseChannels *utils.SyncIntObjectChan
	expectedTypes    *utils.SyncIntReflectTypes

	seqNoMutex sync.Mutex
	seqNo      int32
	dclist     map[int]string

	tokensStorage session.SessionLoader

	PublicKey *rsa.PublicKey

	serviceChannel       chan tl.Object
	serviceModeActivated bool

	Logger *log.Logger

	serverRequestHandlers []customHandlerFunc
}

type customHandlerFunc = func(i any) bool

type Config struct {
	AuthKeyFile    string
	StringSession  string
	SessionStorage session.SessionLoader
	MemorySession  bool

	ServerHost string
	PublicKey  *rsa.PublicKey
	DataCenter int
	AppID      int32
}

func NewMTProto(c Config) (*MTProto, error) {
	if c.SessionStorage == nil {
		if c.AuthKeyFile == "" {
			return nil, fmt.Errorf("auth key file is not specified")
		}

		c.SessionStorage = session.NewFromFile(c.AuthKeyFile)
	}

	s, err := c.SessionStorage.Load()
	if err != nil {
		if !(strings.Contains(err.Error(), session.ErrFileNotExists) || strings.Contains(err.Error(), session.ErrPathNotFound)) {
			return nil, fmt.Errorf("loading session: %w", err)
		}
	}
	var encrypted bool
	if s != nil {
		encrypted = true
	} else if c.StringSession != "" {
		encrypted = true
	}

	m := &MTProto{
		tokensStorage:         c.SessionStorage,
		Addr:                  c.ServerHost,
		encrypted:             encrypted,
		sessionId:             utils.GenerateSessionID(),
		serviceChannel:        make(chan tl.Object),
		PublicKey:             c.PublicKey,
		responseChannels:      utils.NewSyncIntObjectChan(),
		expectedTypes:         utils.NewSyncIntReflectTypes(),
		serverRequestHandlers: make([]customHandlerFunc, 0),
		Logger:                log.New(os.Stderr, "MTProto - ", log.LstdFlags),
		AppID:                 c.AppID,
		memorySession:         c.MemorySession,
	}
	if c.StringSession != "" {
		m.ImportAuth(c.StringSession)
	} else {
		if s != nil {
			m.LoadSession(s)
		}
	}
	return m, nil
}

func (m *MTProto) ExportAuth() ([]byte, []byte, string, int) {
	return m.authKey, m.authKeyHash, m.Addr, m.GetDC()
}

func (m *MTProto) ImportAuth(Session string) (bool, error) {
	StringSession := &session.StringSession{
		Encoded: Session,
	}
	AuthKey, AuthKeyHash, _, IpAddr, err := StringSession.Decode()
	if err != nil {
		return false, fmt.Errorf("decoding string session: %w", err)
	}
	m.authKey = AuthKey
	m.authKeyHash = AuthKeyHash
	m.Addr = fmt.Sprint(IpAddr)
	if !m.memorySession {
		if err := m.SaveSession(); err != nil {
			return false, fmt.Errorf("saving session: %w", err)
		}
	}
	return true, nil
}

func (m *MTProto) GetDC() int {
	addr := m.Addr
	for k, v := range utils.DcList {
		if v == addr {
			return k
		}
	}
	return 4
}

func (m *MTProto) ExportNewSender(dcID int, mem bool) (*MTProto, error) {
	newAddr := utils.DcList[dcID]
	cfg := Config{
		AppID:         m.AppID,
		DataCenter:    dcID,
		PublicKey:     m.PublicKey,
		ServerHost:    newAddr,
		AuthKeyFile:   filepath.Join(wd, "sesion.session"),
		MemorySession: mem,
	}
	if dcID == m.GetDC() {
		cfg.SessionStorage = m.tokensStorage
	}
	sender, _ := NewMTProto(cfg)
	m.Logger.Println("Exporting new sender for DC", dcID)
	err := sender.CreateConnection(true)
	if err != nil {
		return nil, fmt.Errorf("creating connection: %w", err)
	}

	return sender, nil
}

func (m *MTProto) SetDCList(in map[int]string) {
	if m.dclist == nil {
		m.dclist = make(map[int]string)
	}
	for k, v := range in {
		m.dclist[k] = v
	}
}

func (m *MTProto) CreateConnection(withLog bool) error {
	ctx, cancelfunc := context.WithCancel(context.Background())
	m.stopRoutines = cancelfunc
	if withLog {
		m.Logger.Printf("Connecting to %s/TcpFull...", m.Addr)
	}
	err := m.connect(ctx)
	if err != nil {
		return err
	}
	if withLog {
		m.Logger.Printf("Connection to %s/TcpFull complete!", m.Addr)
	}
	m.startReadingResponses(ctx)

	if !m.encrypted {
		err = m.makeAuthKey()
		if err != nil {
			return err
		}
	}

	m.startPinging(ctx)

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
		},
		mode.Intermediate,
	)
	if err != nil {
		return fmt.Errorf("creating transport: %w", err)
	}

	CloseOnCancel(ctx, m.transport)
	return nil
}

func (m *MTProto) makeRequest(data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	resp, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		return nil, fmt.Errorf("sending packet: %w", err)
	}

	response := <-resp

	switch r := response.(type) {
	case *objects.RpcError:
		realErr := RpcErrorToNative(r)

		err = m.tryToProcessErr(realErr.(*ErrResponseCode))
		if err != nil {
			return nil, err
		}

		return m.makeRequest(data, expectedTypes...)

	case *errorSessionConfigsChanged:
		return m.makeRequest(data, expectedTypes...)
	}

	return tl.UnwrapNativeTypes(response), nil
}

func (m *MTProto) InvokeRequestWithoutUpdate(data tl.Object, expectedTypes ...reflect.Type) error {
	_, err := m.sendPacket(data, expectedTypes...)
	if err != nil {
		return fmt.Errorf("sending packet: %w", err)
	}
	return err
}

func (m *MTProto) Disconnect() error {
	m.stopRoutines()
	// m.responseChannels.Close()
	return nil
}

func (m *MTProto) Terminate() error {
	m.stopRoutines()
	m.responseChannels.Close()
	m.Logger.Printf("Disconnecting Borrowed Sender from %s/TcpFull...", m.Addr)
	return nil
}

func (m *MTProto) Reconnect(WithLogs bool) error {
	err := m.Disconnect()
	if err != nil {
		return errors.Wrap(err, "disconnecting")
	}
	if WithLogs {
		m.Logger.Printf("Reconnecting to %s/TcpFull...", m.Addr)
	}

	err = m.CreateConnection(WithLogs)
	if err == nil && WithLogs {
		m.Logger.Printf("Connected to %s/TcpFull complete!", m.Addr)
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

func (m *MTProto) startPinging(ctx context.Context) {
	m.routineswg.Add(1)
	go func() {
		ticker := time.NewTicker(time.Minute * 1)
		defer ticker.Stop()
		defer m.routineswg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, err := m.ping(0xCADACADA)
				if err != nil {
					m.Logger.Printf("ping unsuccessfull: %v", err)
				}
			}
		}
	}()
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
				err := m.readMsg()
				switch err {
				case nil:
				case context.Canceled:
					return
				case io.EOF:
					err = m.Reconnect(false)
					if err != nil {
						m.Logger.Println("reconnecting error:", err)
					}
					return

				default:
					if strings.Contains(err.Error(), "required to reconnect!") {
						err = m.Reconnect(false)
						if err != nil {

							m.Logger.Println("reconnecting error:", err)
						}
						return
					} else {
						m.Logger.Println("reading error:", err)
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
		return errors.Wrap(err, "processing response")
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

		// m.Ping()
		m.mutex.Unlock()

	case *objects.NewSessionCreated:
		m.serverSalt = message.ServerSalt
		if !m.memorySession {
			err := m.SaveSession()
			if err != nil {
				m.Logger.Println(errors.Wrap(err, "saving session"))
			}
		}

	case *objects.Pong, *objects.MsgsAck:
		// skip

	case *objects.BadMsgNotification:
		return BadMsgErrorFromNative(message)

	case *objects.RpcResult:
		obj := message.Obj
		if v, ok := obj.(*objects.GzipPacked); ok {
			obj = v.Obj
		}

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
			m.Logger.Println(errors.New("got nonsystem message from server: " + reflect.TypeOf(message).String()))
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

func (m *MTProto) tryToProcessErr(e *ErrResponseCode) error {
	if e.Code == 303 && strings.Contains(e.Message, "PHONE_MIGRATE_") {
		newDc := e.AdditionalInfo.(int)
		m.Logger.Printf("Phone migrated to %v", newDc)
		return m.SwitchDC(newDc)
	} else if e.Code == 303 && strings.Contains(e.Message, "USER_MIGRATE_") {
		// newDc := e.AdditionalInfo.(int)
		// m.Logger.Printf("User migrated to %v", newDc)
		// return m.SwitchDC(newDc)
		return e
	} else {
		return e
	}
}

func (m *MTProto) SwitchDC(dc int) error {
	newIP, found := m.dclist[dc]
	if !found {
		return fmt.Errorf("DC with id %v not found", dc)
	}

	m.Addr = newIP
	m.Logger.Printf("Reconnecting to new data center %v", dc)
	m.encrypted = false
	err := m.Reconnect(true)
	if err != nil {
		fmt.Println("Reconnect error:", err)
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

func CloseOnCancel(ctx context.Context, c io.Closer) {
	go func() {
		<-ctx.Done()
		c.Close()
	}()
}
