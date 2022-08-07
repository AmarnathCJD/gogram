// Copyright (c) 2022 RoseLoverX

package mtproto

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"log"
	"os"
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
)

const defaultTimeout = 0 * time.Second

type MTProto struct {
	addr         string
	transport    transport.Transport
	stopRoutines context.CancelFunc
	routineswg   sync.WaitGroup

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

	publicKey *rsa.PublicKey

	serviceChannel       chan tl.Object
	serviceModeActivated bool

	Logger *log.Logger

	serverRequestHandlers []customHandlerFunc
}

type customHandlerFunc = func(i any) bool

type Config struct {
	AuthKeyFile    string
	SessionStorage session.SessionLoader

	ServerHost string
	PublicKey  *rsa.PublicKey
}

func NewMTProto(c Config) (*MTProto, error) {
	if c.SessionStorage == nil {
		if c.AuthKeyFile == "" {
			return nil, fmt.Errorf("auth key file is not specified")
		}

		c.SessionStorage = session.NewFromFile(c.AuthKeyFile)
	}

	s, err := c.SessionStorage.Load()
	switch {
	case err == nil, os.IsNotExist(err):
	default:
		return nil, fmt.Errorf("loading session: %w", err)
	}

	m := &MTProto{
		tokensStorage:         c.SessionStorage,
		addr:                  c.ServerHost,
		encrypted:             s != nil,
		sessionId:             utils.GenerateSessionID(),
		serviceChannel:        make(chan tl.Object),
		publicKey:             c.PublicKey,
		responseChannels:      utils.NewSyncIntObjectChan(),
		expectedTypes:         utils.NewSyncIntReflectTypes(),
		serverRequestHandlers: make([]customHandlerFunc, 0),
		dclist:                defaultDCList(),
		Logger:                NewLogger("MTProto - "),
	}

	if s != nil {
		m.LoadSession(s)
	}

	return m, nil
}

func (m *MTProto) SetDCList(in map[int]string) {
	if m.dclist == nil {
		m.dclist = make(map[int]string)
	}
	for k, v := range in {
		m.dclist[k] = v
	}
}

func (m *MTProto) CreateConnection() error {
	ctx, cancelfunc := context.WithCancel(context.Background())
	m.stopRoutines = cancelfunc
	m.Logger.Printf("Connecting to %s:443/TcpFull...", m.addr)
	err := m.connect(ctx)
	if err != nil {
		return err
	}
	m.Logger.Printf("Connection to %s:443/TcpFull complete!", m.addr)
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
			Host:    m.addr,
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

func (m *MTProto) Disconnect() error {
	m.stopRoutines()
	m.Logger.Print("Disconnected!")
	return nil
}

func (m *MTProto) Reconnect() error {
	m.Logger.Printf("Disconnecting from %s:443/TcpFull...", m.addr)
	err := m.Disconnect()
	if err != nil {
		return fmt.Errorf("disconnecting: %w", err)
	}
	m.Logger.Printf("Disconnection from %s:443/TcpFull complete!", m.addr)
	m.Logger.Printf("Connecting to %s:443/TcpFull...", m.addr)

	err = m.CreateConnection()
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	m.Logger.Printf("Connection to %s:443/TcpFull complete!", m.addr)
	return err
}

func (m *MTProto) startPinging(ctx context.Context) {
	m.routineswg.Add(1)
	go func() {
		ticker := time.NewTicker(time.Minute)
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
					m.Logger.Println("Reading responses canceled")
					return
				case io.EOF:
					err = m.Reconnect()
					if err != nil {
						m.Logger.Printf("can't reconnect: %v", err)
					}
				default:
					if strings.Contains(err.Error(), "required to reconnect") {
						err = m.Reconnect()
						if err != nil {
							m.Logger.Printf("can't reconnect: %v", err)
						}
					} else {
						m.Logger.Print(err)
					}
				}
			}
		}
	}()
}

func (m *MTProto) readMsg() error {
	if m.transport == nil {
		return fmt.Errorf("no connection to server")
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
			return fmt.Errorf("reading msg: %w", err)
		}
	}
	if m.serviceModeActivated {
		var obj tl.Object
		obj, err = tl.DecodeUnknownObject(response.GetMsg())
		if err != nil {
			return fmt.Errorf("decoding msg: %w", err)
		}
		m.serviceChannel <- obj
		return nil
	}
	err = m.processResponse(response)
	if err != nil {
		return fmt.Errorf("processing response: %w", err)
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
				return fmt.Errorf("processing response: %w", err)
			}
		}

	case *objects.BadServerSalt:
		m.serverSalt = message.NewSalt
		err := m.SaveSession()
		if err != nil {
			m.Logger.Print(err)
		}

		m.mutex.Lock()
		for _, k := range m.responseChannels.Keys() {
			v, _ := m.responseChannels.Get(k)
			v <- &errorSessionConfigsChanged{}
			m.responseChannels.Delete(msg.GetMsgID())
			m.expectedTypes.Delete(msg.GetMsgID())
		}
		m.mutex.Unlock()

	case *objects.NewSessionCreated:
		m.serverSalt = message.ServerSalt
		err := m.SaveSession()
		if err != nil {
			m.Logger.Printf("can't save session: %v", err)
		}

	case *objects.Pong, *objects.MsgsAck:
		// do nothing

	case *objects.BadMsgNotification:
		return BadMsgErrorFromNative(message)

	case *objects.RpcResult:
		obj := message.Obj
		if v, ok := obj.(*objects.GzipPacked); ok {
			obj = v.Obj
		}

		err := m.writeRPCResponse(int(message.ReqMsgID), obj)
		if err != nil {
			return fmt.Errorf("writing rpc response: %w", err)
		}

	case *objects.GzipPacked:
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
			m.Logger.Printf("unhandled message type: %s", reflect.TypeOf(message).String())
		}
	}

	if (msg.GetSeqNo() & 1) != 0 {
		_, err := m.MakeRequest(&objects.MsgsAck{MsgIDs: []int64{int64(msg.GetMsgID())}})
		if err != nil {
			return fmt.Errorf("making msgsack: %w", err)
		}
	}

	return nil
}

func (m *MTProto) tryToProcessErr(e *ErrResponseCode) error {
	if e.Code == 303 && strings.Contains(e.Message, "PHONE_MIGRATE_") {
		newDc := e.AdditionalInfo.(int)
		m.Logger.Printf("phone migration to DC %d", newDc)
		return m.SwitchDC(newDc)
	} else {
		return e
	}
}

func (m *MTProto) SwitchDC(dc int) error {
	newIP, found := m.dclist[dc]
	if !found {
		return fmt.Errorf("DC with id %v not found", dc)
	}

	m.addr = newIP
	m.Logger.Print("connecting to ", m.addr, " Tcp/Ip")
	err := m.Reconnect()
	if err == nil {
		m.Logger.Print("connected to dc", dc)
	}
	return err
}

type any = interface{}
type null = struct{}

func defaultDCList() map[int]string {
	return map[int]string{
		1: "149.154.175.58:443",
		2: "149.154.167.50:443",
		3: "149.154.175.100:443",
		4: "149.154.167.91:443",
		5: "91.108.56.151:443",
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

func CloseOnCancel(ctx context.Context, c io.Closer) {
	go func() {
		<-ctx.Done()
		c.Close()
	}()
}

func NewLogger(prefix string) *log.Logger {
	return log.New(log.Writer(), prefix, log.Ltime)
}
