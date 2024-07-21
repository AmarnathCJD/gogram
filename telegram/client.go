// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"crypto/rsa"
	"log"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
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
	DisconnectExportedAfter = 5 * time.Minute
)

// TODO: fix session file issue

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

type exportedSender struct {
	client *Client
	dcID   int
}

type cachedExportedSenders struct {
	sync.RWMutex
	senders []exportedSender
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

type DeviceConfig struct {
	DeviceModel   string
	SystemVersion string
	AppVersion    string
}

type ClientConfig struct {
	AppID         int32
	AppHash       string
	DeviceConfig  DeviceConfig
	Session       string
	StringSession string
	LangCode      string
	ParseMode     string
	MemorySession bool
	DataCenter    int
	IpAddr        string
	PublicKeys    []*rsa.PublicKey
	NoUpdates     bool
	DisableCache  bool
	TestMode      bool
	LogLevel      string
	Proxy         *url.URL
}

type Session struct {
	Key      []byte `json:"key,omitempty"`
	Hash     []byte `json:"hash,omitempty"`
	Salt     int64  `json:"salt,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	AppID    int32  `json:"app_id,omitempty"`
}

func (s *Session) Encode() string {
	if s.Hash == nil || len(s.Hash) == 0 {
		s.Hash = utils.Sha1Byte(s.Key)[12:20]
	}
	return session.NewStringSession(s.Key, s.Hash, 0, s.Hostname, s.AppID).Encode()
}

func NewClient(config ClientConfig) (*Client, error) {
	client := &Client{wg: sync.WaitGroup{}, Log: utils.NewLogger("gogram [client]"), stopCh: make(chan struct{})}
	config = client.cleanClientConfig(config)
	client.setupClientData(config)

	client.Cache = NewCache(config.LogLevel, genCacheFileName(config.StringSession))
	if !config.DisableCache {
		client.Cache.writeFile = true
		client.Cache.ReadFile()
	}

	if err := client.setupMTProto(config); err != nil {
		return nil, err
	}
	if config.NoUpdates {
		client.Log.Debug("client is running in no updates mode, no updates will be handled")
	} else {
		client.setupDispatcher()
	}
	if err := client.clientWarnings(config); err != nil {
		return nil, err
	}
	return client, nil
}

func genCacheFileName(stringSession string) string {
	if stringSession != "" {
		// return middle 10 characters of the string session
		return "cache_" + stringSession[len(stringSession)/2-1:len(stringSession)/2+1]
	}
	return "cache"
}

func (c *Client) setupMTProto(config ClientConfig) error {
	toIpAddr := func() string {
		if config.IpAddr != "" {
			return config.IpAddr
		} else {
			return GetHostIp(config.DataCenter, config.TestMode)
		}
	}

	mtproto, err := mtproto.NewMTProto(mtproto.Config{
		AppID:         config.AppID,
		AuthKeyFile:   config.Session,
		ServerHost:    toIpAddr(),
		PublicKey:     config.PublicKeys[0],
		DataCenter:    config.DataCenter,
		LogLevel:      config.LogLevel,
		StringSession: config.StringSession,
		Proxy:         config.Proxy,
		MemorySession: config.MemorySession,
	})
	if err != nil {
		return errors.Wrap(err, "creating mtproto client")
	}
	c.MTProto = mtproto
	c.clientData.appID = mtproto.AppID() // in case the appId was not provided in the config but was in the session

	if config.StringSession != "" {
		if err := c.Connect(); err != nil {
			return errors.Wrap(err, "connecting to telegram servers")
		}
	}

	return nil
}

func (c *Client) clientWarnings(config ClientConfig) error {
	if config.NoUpdates {
		c.Log.Debug("client is running in no updates mode, no updates will be handled")
	}
	if !doesSessionFileExist(config.Session) && config.StringSession == "" && (c.AppID() == 0 || c.AppHash() == "") {
		if c.AppID() == 0 {
			log.Print("app id is empty, call ScrapeAppConfig()? (y/n)")
			if !utils.AskForConfirmation() {
				return errors.New("your app id is empty, please provide it")
			} else {
				c.ScrapeAppConfig()
			}
		} else {
			return errors.New("your app id or app hash is empty, please provide them")
		}
	}
	if config.AppHash == "" {
		c.Log.Debug("appHash is empty, some features may not work")
	}

	if !IsFfmpegInstalled() {
		c.Log.Debug("ffmpeg is not installed, some media metadata may not be available")
	}
	return nil
}

func (c *Client) setupDispatcher() {
	c.NewUpdateDispatcher()
	handleUpdaterWrapper := func(u any) bool {
		return HandleIncomingUpdates(u, c)
	}

	c.AddCustomServerRequestHandler(handleUpdaterWrapper)
}

func (c *Client) cleanClientConfig(config ClientConfig) ClientConfig {
	// if config.Session is a filename, join it with the working directory
	config.Session = joinAbsWorkingDir(config.Session)
	if config.TestMode {
		config.DataCenter = 2
	} else {
		config.DataCenter = getInt(config.DataCenter, DefaultDataCenter)
	}
	config.PublicKeys, _ = keys.GetRSAKeys()
	return config
}

// setupClientData sets up the client data from the config
func (c *Client) setupClientData(cnf ClientConfig) {
	c.clientData.appID = cnf.AppID
	c.clientData.appHash = cnf.AppHash
	c.clientData.deviceModel = getStr(cnf.DeviceConfig.DeviceModel, "gogram "+runtime.GOOS+" "+runtime.GOARCH)
	c.clientData.systemVersion = getStr(cnf.DeviceConfig.SystemVersion, runtime.GOOS+" "+runtime.GOARCH)
	c.clientData.appVersion = getStr(cnf.DeviceConfig.AppVersion, Version)
	c.clientData.langCode = getStr(cnf.LangCode, "en")
	c.clientData.logLevel = getStr(cnf.LogLevel, LogInfo)
	c.clientData.parseMode = getStr(cnf.ParseMode, "HTML")

	c.Log.SetLevel(c.clientData.logLevel)
}

// initialRequest sends the initial initConnection request
func (c *Client) InitialRequest() error {
	c.Log.Debug("sending initial invokeWithLayer request")
	serverConfig, err := c.InvokeWithLayer(ApiVersion, &InitConnectionParams{
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

	c.Log.Debug("received initial invokeWithLayer response")
	if config, ok := serverConfig.(*Config); ok {
		var dcs = make(map[int][]string)
		for _, dc := range config.DcOptions {
			if !dc.Ipv6 && !dc.MediaOnly && !dc.Cdn {
				if _, ok := dcs[int(dc.ID)]; !ok {
					dcs[int(dc.ID)] = []string{}
				}

				dcs[int(dc.ID)] = append(dcs[int(dc.ID)], dc.IpAddress+":"+strconv.Itoa(int(dc.Port)))
			}
		}

		utils.SetDCs(dcs)
	}

	return nil
}

// Establish connection to telegram servers
func (c *Client) Connect() error {
	if c.IsConnected() {
		return nil
	}

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
func (c *Client) switchDc(dcID int) error {
	c.Log.Debug("switching data center to (" + strconv.Itoa(dcID) + ")")
	newDcSender, err := c.MTProto.ReconnectToNewDC(dcID)
	if err != nil {
		return errors.Wrap(err, "reconnecting to new dc")
	}
	c.MTProto = newDcSender
	return c.InitialRequest()
}

func (c *Client) SetAppID(appID int32) {
	c.clientData.appID = appID
	c.MTProto.SetAppID(appID)
}

func (c *Client) SetAppHash(appHash string) {
	c.clientData.appHash = appHash
}

func (c *Client) AddNewExportedSenderToMap(dcID int, sender *Client) {
	c.exportedSenders.Lock()
	c.exportedSenders.senders = append(
		c.exportedSenders.senders,
		exportedSender{client: sender, dcID: dcID},
	)
	c.exportedSenders.Unlock()

	go func() {
		time.Sleep(DisconnectExportedAfter)
		c.exportedSenders.Lock()
		defer c.exportedSenders.Unlock()

		for i, s := range c.exportedSenders.senders {
			if s.client == sender {
				c.exportedSenders.senders = append(c.exportedSenders.senders[:i], c.exportedSenders.senders[i+1:]...)
				break
			}
		}
	}() // remove the sender from the map after the expiry time
}

func (c *Client) GetCachedExportedSenders(dcID int) []*Client {
	c.exportedSenders.RLock()
	defer c.exportedSenders.RUnlock()

	var senders []*Client
	for _, sender := range c.exportedSenders.senders {
		if sender.dcID == dcID {
			senders = append(senders, sender.client)
		}
	}

	return senders
}

// createExportedSender creates a new exported sender
func (c *Client) CreateExportedSender(dcID int) (*Client, error) {
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
		if err := exportedSender.shareAuth(c, exportedSender.MTProto.GetDC()); err != nil {
			return nil, errors.Wrap(err, "sharing auth")
		}
	}
	c.Log.Debug("exported sender for dc ", exported.GetDC(), " is ready")
	return exportedSender, nil
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

// cleanExportedSenders terminates all exported senders and removes them from cache
func (c *Client) cleanExportedSenders() {
	c.exportedSenders.Lock()
	defer c.exportedSenders.Unlock()

	for _, sender := range c.exportedSenders.senders {
		sender.client.Terminate()
	}
	c.exportedSenders.senders = nil
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
	authSession, dcId := c.MTProto.ExportAuth()
	c.Log.Debug("exporting string session...")
	return session.NewStringSession(authSession.Key, authSession.Hash, dcId, authSession.Hostname, authSession.AppID).Encode()
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
func (c *Client) ImportRawSession(authKey, authKeyHash []byte, IpAddr string, AppID int32) (bool, error) {
	return c.MTProto.ImportRawAuth(authKey, authKeyHash, IpAddr, AppID)
}

// ExportRawSession exports a session to raw TData
//
//	Returns:
//	  authKey: The auth key of the session
//	  authKeyHash: The auth key hash
//	  IpAddr: The IP address of the DC
//	  DcID: The DC ID to connect to
//	  AppID: The App ID to use
func (c *Client) ExportRawSession() *Session {
	mtSession, _ := c.MTProto.ExportAuth()
	return &Session{
		Key:      mtSession.Key,
		Hash:     mtSession.Hash,
		Salt:     mtSession.Salt,
		Hostname: mtSession.Hostname,
		AppID:    mtSession.AppID,
	}
}

// LoadSession loads a session from a file, database, etc.
//
//	Params:
//	  Session: The session to load
func (c *Client) LoadSession(sess *Session) error {
	return c.MTProto.LoadSession(&session.Session{
		Key:      sess.Key,
		Hash:     sess.Hash,
		Salt:     sess.Salt,
		Hostname: sess.Hostname,
		AppID:    sess.AppID,
	})
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
	go c.cleanExportedSenders()
	c.MTProto.SetTransfer(false) // to stop connection break check.
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

// return only the object, omitting the error
func (c *Client) W(obj any, err error) any {
	return obj
}

// return only the error, omitting the object
func (c *Client) E(obj any, err error) error {
	return err
}
