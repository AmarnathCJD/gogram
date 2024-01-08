// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"crypto/rsa"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	mtproto "github.com/amarnathcjd/gogram"
	"github.com/pkg/errors"

	"github.com/amarnathcjd/gogram/internal/keys"
	"github.com/amarnathcjd/gogram/internal/session"
	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	// DefaultDC is the default data center id
	DefaultDataCenter       = 4
	DefaultDevice           = "Android Device"
	DefaultSystem           = runtime.GOOS + " " + runtime.GOARCH
	DisconnectExportedAfter = 60 * time.Second
)

type clientData struct {
	appID         int32
	appHash       string
	deviceModel   string
	systemVersion string
	appVersion    string
	langCode      string
	parseMode     string
	logLevel      string
	botAcc        bool
}

type cachedExportedSenders struct {
	sync.RWMutex
	senders map[int][]*Client
}

// Client is the main struct of the library
type Client struct {
	*mtproto.MTProto
	Cache           *CACHE
	exportedSenders cachedExportedSenders
	clientData      clientData
	dispatcher      *UpdateDispatcher
	wg              sync.WaitGroup
	stopCh          chan struct{}
	Log             *utils.Logger
}

type ClientConfig struct {
	AppID         int32
	AppHash       string
	DeviceModel   string
	SystemVersion string
	AppVersion    string
	Session       string
	StringSession string
	LangCode      string
	ParseMode     string
	MemorySession bool
	DataCenter    int
	PublicKeys    []*rsa.PublicKey
	NoUpdates     bool
	EnableCache   bool
	LogLevel      string
	SocksProxy    *url.URL
}

func NewClient(config ClientConfig) (*Client, error) {
	client := &Client{wg: sync.WaitGroup{}, Log: utils.NewLogger("gogram - client"), stopCh: make(chan struct{})}
	config = client.cleanClientConfig(config)
	client.setupClientData(config)

	if config.EnableCache {
		client.Cache.startCacheFileUpdater()
	}

	client.Cache = NewCache(config.LogLevel)
	if err := client.setupMTProto(config); err != nil {
		return nil, err
	}
	if !config.NoUpdates {
		client.setupDispatcher()
	}
	if err := client.clientWarnings(config); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) setupMTProto(config ClientConfig) error {
	mtproto, err := mtproto.NewMTProto(mtproto.Config{
		AppID:         config.AppID,
		AuthKeyFile:   config.Session,
		ServerHost:    GetHostIp(config.DataCenter),
		PublicKey:     config.PublicKeys[0],
		DataCenter:    config.DataCenter,
		LogLevel:      config.LogLevel,
		StringSession: config.StringSession,
		SocksProxy:    config.SocksProxy,
		MemorySession: config.MemorySession,
	})
	if err != nil {
		return errors.Wrap(err, "creating mtproto client")
	}
	c.MTProto = mtproto
	c.clientData.appID = mtproto.AppID() // in case the app id was not provided in the config but was in the session

	if config.StringSession != "" {
		if err := c.Connect(); err != nil {
			return errors.Wrap(err, "connecting to telegram servers")
		}
	}

	return nil
}

func (c *Client) clientWarnings(config ClientConfig) error {
	if config.NoUpdates {
		c.Log.Warn("client is running in no updates mode, no updates will be handled")
	}
	if !doesSessionFileExist(config.Session) && config.StringSession == "" && (c.AppID() == 0 || c.AppHash() == "") {
		return errors.New("your app id or app hash is empty, please provide them")
	}
	if config.AppHash == "" {
		c.Log.Warn("appHash is empty, some features may not work")
	}

	if !IsFfmpegInstalled() {
		c.Log.Debug("ffmpeg is not installed, some media features may not work")
	}
	return nil
}

func (c *Client) setupDispatcher() {
	c.dispatcher = &UpdateDispatcher{}
	handleUpdaterWrapper := func(u any) bool {
		return HandleIncomingUpdates(u, c)
	}

	c.AddCustomServerRequestHandler(handleUpdaterWrapper)
}

func (c *Client) cleanClientConfig(config ClientConfig) ClientConfig {
	if config.Session != "" {
		configSession, err := filepath.Abs(config.Session)
		if err != nil {
			c.Log.Error("error getting absolute path of session file: ", err)
		} else {
			config.Session = configSession
		}
	}
	config.Session = getStr(config.Session, filepath.Join(getAbsWorkingDir(), "session.session"))
	config.DataCenter = getInt(config.DataCenter, DefaultDataCenter)
	config.PublicKeys, _ = keys.GetRSAKeys()
	return config
}

// setupClientData sets up the client data from the config
func (c *Client) setupClientData(cnf ClientConfig) {
	c.clientData.appID = cnf.AppID
	c.clientData.appHash = cnf.AppHash
	c.clientData.deviceModel = getStr(cnf.DeviceModel, DefaultDevice)
	c.clientData.systemVersion = getStr(cnf.SystemVersion, DefaultSystem)
	c.clientData.appVersion = getStr(cnf.AppVersion, "1.0")
	c.clientData.langCode = getStr(cnf.LangCode, "en")
	c.clientData.logLevel = getStr(cnf.LogLevel, LogInfo)
	c.clientData.parseMode = getStr(cnf.ParseMode, "HTML")

	c.Log.SetLevel(c.clientData.logLevel)
}

// initialRequest sends the initial initConnection request
func (c *Client) InitialRequest() error {
	c.Log.Debug("sending initial invokeWithLayer request")
	_, err := c.InvokeWithLayer(ApiVersion, &InitConnectionParams{
		ApiID:          c.clientData.appID,
		DeviceModel:    c.clientData.deviceModel,
		SystemVersion:  c.clientData.systemVersion,
		AppVersion:     c.clientData.appVersion,
		SystemLangCode: c.clientData.langCode,
		LangCode:       c.clientData.langCode,
		Query:          &HelpGetConfigParams{},
	})
	if err != nil {
		return errors.Wrap(err, "sending invokeWithLayer")
	}
	return nil
}

// Establish connection to telegram servers
func (c *Client) Connect() error {
	err := c.MTProto.CreateConnection(true)
	if err != nil {
		return errors.Wrap(err, "connecting to telegram servers")
	}
	// Initial request (invokeWithLayer) must be sent after connection is established
	return c.InitialRequest()
}

// Wrapper for Connect()
func (c *Client) Conn() (*Client, error) {
	return c, c.Connect()
}

// Returns true if the client is connected to telegram servers
func (c *Client) IsConnected() bool {
	return c.MTProto.TcpActive()
}

func (c *Client) Start() error {
	if !c.IsConnected() {
		if err := c.Connect(); err != nil {
			return err
		}
	}
	if au, err := c.IsAuthorized(); err != nil && !au {
		if err := c.AuthPrompt(); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

// Returns true if the client is authorized as a user or a bot
func (c *Client) IsAuthorized() (bool, error) {
	c.Log.Debug("sending updates.getState request")
	_, err := c.UpdatesGetState()
	if err != nil {
		return false, err
	}
	return true, nil
}

// Disconnect from telegram servers
func (c *Client) Disconnect() error {
	go c.cleanExportedSenders()
	return c.MTProto.Disconnect()
}

// switchDC permanently switches the data center
func (c *Client) switchDC(dcID int) error {
	c.Log.Debug("switching data center to [" + strconv.Itoa(dcID) + "]")
	newDcSender, err := c.MTProto.ReconnectToNewDC(dcID)
	if err != nil {
		return errors.Wrap(err, "reconnecting to new dc")
	}
	c.MTProto = newDcSender
	return c.InitialRequest()
}

func (c *Client) GetCachedExportedSenders(dcID int) []*Client {
	c.exportedSenders.RLock()
	defer c.exportedSenders.RUnlock()
	v, ok := c.exportedSenders.senders[dcID]
	if !ok {
		return nil
	}
	return v
}

// createExportedSender creates a new exported sender
func (c *Client) createExportedSender(dcID int) (*Client, error) {
	c.Log.Debug("creating exported sender for DC ", dcID)
	exported, err := c.MTProto.ExportNewSender(dcID, true)
	if err != nil {
		return nil, errors.Wrap(err, "exporting new sender")
	}
	exportedSender := &Client{MTProto: exported, Cache: c.Cache, Log: utils.NewLogger("gogram - sender").SetLevel(c.Log.Lev()), wg: sync.WaitGroup{}, clientData: c.clientData, stopCh: make(chan struct{})}
	err = exportedSender.InitialRequest()
	if err != nil {
		return nil, errors.Wrap(err, "initial request")
	}
	if c.MTProto.GetDC() != exported.GetDC() {
		if err := exportedSender.shareAuthWithTimeout(c, exportedSender.MTProto.GetDC()); err != nil {
			return nil, errors.Wrap(err, "sharing auth")
		}
	}
	c.Log.Debug("exported sender for DC ", exported.GetDC(), " is ready")
	return exportedSender, nil
}

func (c *Client) shareAuthWithTimeout(main *Client, dcID int) error {
	// raise timeout error on timeout
	timeout := time.After(6 * time.Second)
	errMade := make(chan error)
	go func() {
		select {
		case <-timeout:
			errMade <- errors.New("sharing authorization timed out")
		case err := <-errMade:
			errMade <- err
		}
	}()
	go func() {
		errMade <- c.shareAuth(main, dcID)
	}()
	return <-errMade
}

// shareAuth shares authorization with another client
func (c *Client) shareAuth(main *Client, dcID int) error {
	mainAuth, err := main.AuthExportAuthorization(int32(dcID))
	if err != nil || mainAuth == nil {
		return errors.Wrap(err, "exporting authorization")
	}
	_, err = c.AuthImportAuthorization(mainAuth.ID, mainAuth.Bytes)
	if err != nil {
		return errors.Wrap(err, "importing authorization")
	}
	return nil
}

// BorrowExportedSender returns exported senders from cache or creates new ones
func (c *Client) BorrowExportedSenders(dcID int, count ...int) ([]*Client, error) {
	c.exportedSenders.Lock()
	defer c.exportedSenders.Unlock()
	if c.exportedSenders.senders == nil {
		c.exportedSenders.senders = make(map[int][]*Client)
	}
	countInt := 1
	if len(count) > 0 {
		countInt = count[0]
	}
	if countInt < 1 {
		return nil, errors.New("count must be greater than 0")
	}
	if countInt > 10 {
		return nil, errors.New("count must be less than 10")
	}
	returned := make([]*Client, 0, countInt)
	if c.exportedSenders.senders[dcID] == nil || len(c.exportedSenders.senders[dcID]) == 0 {
		c.exportedSenders.senders[dcID] = make([]*Client, 0, countInt)
		exportWaitGroup := sync.WaitGroup{}
		for i := 0; i < countInt; i++ {
			exportWaitGroup.Add(1)
			go func() {
				defer exportWaitGroup.Done()
				exportedSender, err := c.createExportedSender(dcID)
				if err != nil {
					const AuthInvalidError = "The provided authorization is invalid"
					if strings.Contains(err.Error(), AuthInvalidError) {
						exportedSender, err = c.createExportedSender(dcID)
						if err != nil {
							return
						}
					} else {
						c.Log.Error("error creating exported sender: ", err)
					}
				}
				returned = append(returned, exportedSender)
				c.exportedSenders.senders[dcID] = append(c.exportedSenders.senders[dcID], exportedSender)
			}()
		}
		exportWaitGroup.Wait()
	} else {
		total := len(c.exportedSenders.senders[dcID])
		if total < countInt {
			returned = append(returned, c.exportedSenders.senders[dcID]...)
			for i := 0; i < countInt-total; i++ {
				exportedSender, err := c.createExportedSender(dcID)
				if err != nil {
					return nil, errors.Wrap(err, "creating exported sender")
				}
				returned = append(returned, exportedSender)
				c.exportedSenders.senders[dcID] = append(c.exportedSenders.senders[dcID], exportedSender)
			}
		} else {
			for i := 0; i < countInt; i++ {
				returned = append(returned, c.exportedSenders.senders[dcID][i])
			}
		}
	}
	return returned, nil
}

// borrowSender returns a sender from cache or creates a new one
func (c *Client) borrowSender(dcID int) (*Client, error) {
	borrowed, err := c.BorrowExportedSenders(dcID, 1)
	if err != nil {
		return nil, errors.Wrap(err, "borrowing exported sender")
	}
	return borrowed[0], nil
}

// cleanExportedSenders terminates all exported senders and removes them from cache
func (c *Client) cleanExportedSenders() {
	if c.exportedSenders.senders == nil {
		return
	}
	c.exportedSenders.Lock()
	defer c.exportedSenders.Unlock()
	for dcID, senders := range c.exportedSenders.senders {
		if senders != nil {
			for i, sender := range senders {
				sender.Terminate()
				senders[i] = nil
			}
			c.exportedSenders.senders[dcID] = nil
		}
	}
}

// setLogLevel sets the log level for all loggers
func (c *Client) SetLogLevel(level string) {
	c.Log.Debug("setting library log level to ", level)
	c.Log.SetLevel(level)
}

// Ping telegram server TCP connection
func (c *Client) Ping() time.Duration {
	return c.MTProto.Ping()
}

// Gets the connected DC-ID
func (c *Client) GetDC() int {
	return c.MTProto.GetDC()
}

// ExportSession exports the current session to a string,
// This string can be used to import the session later
func (c *Client) ExportSession() string {
	authKey, authKeyHash, IpAddr, dcID, AppID := c.MTProto.ExportAuth()
	c.Log.Debug("Exporting string session...")
	return session.NewStringSession(authKey, authKeyHash, dcID, IpAddr, AppID).Encode()
}

// ImportSession imports a session from a string
//
//	Params:
//	  sessionString: The sessionString to authenticate with
func (c *Client) ImportSession(sessionString string) (bool, error) {
	c.Log.Debug("importing session: ", sessionString)
	return c.MTProto.ImportAuth(sessionString)
}

// ImportRawSession imports a session from raw TData
//
//	Params:
//	  authKey: The auth key of the session
//	  authKeyHash: The auth key hash
//	  IpAddr: The IP address of the DC
//	  DcID: The DC ID to connect to
//	  AppID: The App ID to use
func (c *Client) ImportRawSession(authKey, authKeyHash []byte, IpAddr string, DcID int, AppID int32) (bool, error) {
	return c.MTProto.ImportRawAuth(authKey, authKeyHash, IpAddr, DcID, AppID)
}

// ExportRawSession exports a session to raw TData
//
//	Returns:
//	  authKey: The auth key of the session
//	  authKeyHash: The auth key hash
//	  IpAddr: The IP address of the DC
//	  DcID: The DC ID to connect to
//	  AppID: The App ID to use
func (c *Client) ExportRawSession() ([]byte, []byte, string, int, int32) {
	return c.MTProto.ExportAuth()
}

// returns the AppID (api_id) of the client
func (c *Client) AppID() int32 {
	return c.clientData.appID
}

// returns the AppHash (api_hash) of the client
func (c *Client) AppHash() string {
	return c.clientData.appHash
}

// returns the ParseMode of the client (HTML or Markdown)
func (c *Client) ParseMode() string {
	return c.clientData.parseMode
}

// Terminate client and disconnect from telegram server
func (c *Client) Terminate() error {
	go c.cleanExportedSenders()
	return c.MTProto.Terminate()
}

// Idle blocks the current goroutine until the client is stopped/terminated
func (c *Client) Idle() {
	c.wg.Add(1)
	go func() {
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan, os.Interrupt, syscall.SIGTERM)
		<-sigchan
		c.Stop()
	}()
	go func() { defer c.wg.Done(); <-c.stopCh }()
	c.wg.Wait()
}

// Stop stops the client and disconnects from telegram server
func (c *Client) Stop() error {
	close(c.stopCh)
	return c.MTProto.Terminate()
}

// NewRecovery makes a new recovery object
func (c *Client) NewRecovery() func() {
	return func() {
		if r := recover(); r != nil {
			if c.Log.Lev() == LogDebug {
				c.Log.Panic(r, "\n\n", string(debug.Stack())) // print stacktrace for debug
			} else {
				c.Log.Panic(r)
			}
		}
	}
}

// WrapError sends an error to the error channel if it is not nil
func (c *Client) WrapError(err error) error {
	if err != nil {
		c.Log.Error(err)
	}
	return err
}
