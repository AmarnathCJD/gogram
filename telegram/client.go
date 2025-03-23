// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"context"
	"crypto/rsa"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"regexp"
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
	// The Initial DC to connect to, before auth
	DefaultDataCenter         = 4
	CleanExportedSendersDelay = 5 * time.Minute
)

type clientData struct {
	appID            int32
	appHash          string
	deviceModel      string
	systemVersion    string
	appVersion       string
	langCode         string
	parseMode        string
	logLevel         utils.LogLevel
	sleepThresholdMs int
	botAcc           bool
	me               *UserObj
}

// Client is the main struct of the library
type Client struct {
	*mtproto.MTProto
	Cache        *CACHE
	clientData   clientData
	dispatcher   *UpdateDispatcher
	wg           sync.WaitGroup
	stopCh       chan struct{}
	exSenders    *ExSenders
	exportedKeys map[int]*AuthExportedAuthorization
	Log          *utils.Logger
}

type DeviceConfig struct {
	DeviceModel   string // The device model to use
	SystemVersion string // The version of the system
	AppVersion    string // The version of the app
	LangCode      string // The language code
}

type ClientConfig struct {
	AppID            int32                // The App ID from my.telegram.org
	AppHash          string               // The App Hash from my.telegram.org
	DeviceConfig     DeviceConfig         // Device configuration
	Session          string               // The session file to use
	StringSession    string               // The string session to use
	SessionName      string               // The name of the session
	ParseMode        string               // The parse mode to use (HTML, Markdown)
	MemorySession    bool                 // Don't save the session to a file
	DataCenter       int                  // The data center to connect to (default: 4)
	IpAddr           string               // The IP address of the DC to connect to
	PublicKeys       []*rsa.PublicKey     // The public keys to verify the server with
	NoUpdates        bool                 // Don't handle updates
	DisableCache     bool                 // Disable caching peer and chat information
	TestMode         bool                 // Use the test data centers
	LogLevel         utils.LogLevel       // The library log level
	Logger           *utils.Logger        // The logger to use
	Proxy            *url.URL             // The proxy to use (SOCKS5, HTTP)
	ForceIPv6        bool                 // Force to use IPv6
	Cache            *CACHE               // The cache to use
	CacheSenders     bool                 // cache the exported file op sender (TODO: Stabilize this)
	TransportMode    string               // The transport mode to use (Abridged, Intermediate, Full)
	SleepThresholdMs int                  // The threshold in milliseconds to sleep before flood
	FloodHandler     func(err error) bool // The flood handler to use
	ErrorHandler     func(err error)      // The error handler to use
}

type Session struct {
	Key      []byte `json:"key,omitempty"`      // AUTH_KEY
	Hash     []byte `json:"hash,omitempty"`     // AUTH_KEY_HASH (SHA1 of AUTH_KEY)
	Salt     int64  `json:"salt,omitempty"`     // SERVER_SALT
	Hostname string `json:"hostname,omitempty"` // HOSTNAME (IP address of the DC)
	AppID    int32  `json:"app_id,omitempty"`   // APP_ID
}

func (s *Session) Encode() string {
	if len(s.Hash) == 0 {
		s.Hash = utils.Sha1Byte(s.Key)[12:20]
	}
	return session.NewStringSession(s.Key, s.Hash, 0, s.Hostname, s.AppID).Encode()
}

func NewClient(config ClientConfig) (*Client, error) {
	client := &Client{
		wg:     sync.WaitGroup{},
		stopCh: make(chan struct{}),
	}

	if config.Logger != nil {
		client.Log = config.Logger
		client.Log.Prefix = "gogram " + getLogPrefix("client", config.SessionName)
		config.LogLevel = config.Logger.Lev()
	} else {
		config.LogLevel = getValue(config.LogLevel, LogInfo)
		client.Log = utils.NewLogger("gogram " + getLogPrefix("client", config.SessionName))
		client.Log.SetLevel(config.LogLevel)
	}

	config = client.cleanClientConfig(config)
	client.setupClientData(config)

	if config.Cache == nil {
		client.Cache = NewCache(fmt.Sprintf("cache%s.db", config.SessionName), &CacheConfig{
			Disabled:   config.DisableCache,
			LogLevel:   config.LogLevel,
			LogName:    config.SessionName,
			LogNoColor: !client.Log.Color(),
		})
	} else {
		client.Cache = config.Cache
	}

	client.Cache.disabled = config.DisableCache

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

	client.exSenders = NewExSenders()

	return client, nil
}

func (c *Client) setupMTProto(config ClientConfig) error {
	var customHost bool
	toIpAddr := func() string {
		if config.IpAddr != "" {
			customHost = true
			return config.IpAddr
		} else {
			return utils.GetHostIp(config.DataCenter, config.TestMode, config.ForceIPv6)
		}
	}

	mtproto, err := mtproto.NewMTProto(mtproto.Config{
		AppID:       config.AppID,
		AuthKeyFile: config.Session,
		ServerHost:  toIpAddr(),
		PublicKey:   config.PublicKeys[0],
		DataCenter:  config.DataCenter,
		Logger: utils.NewLogger("gogram " + getLogPrefix("mtproto", config.SessionName)).
			SetLevel(config.LogLevel).
			NoColor(!c.Log.Color()),
		StringSession: config.StringSession,
		Proxy:         config.Proxy,
		MemorySession: config.MemorySession,
		Ipv6:          config.ForceIPv6,
		CustomHost:    customHost,
		FloodHandler:  config.FloodHandler,
		ErrorHandler:  config.ErrorHandler,
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
			log.Print("app id is empty, fetch from api.telegram.org? (y/n): ")
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
		config.DataCenter = getValue(config.DataCenter, DefaultDataCenter)
	}
	config.PublicKeys, _ = keys.GetRSAKeys()
	return config
}

// setupClientData sets up the client data from the config
func (c *Client) setupClientData(cnf ClientConfig) {
	c.clientData.appID = cnf.AppID
	c.clientData.appHash = cnf.AppHash
	c.clientData.deviceModel = getValue(cnf.DeviceConfig.DeviceModel, "gogram "+runtime.GOOS+" "+runtime.GOARCH)
	c.clientData.systemVersion = getValue(cnf.DeviceConfig.SystemVersion, runtime.GOOS+" "+runtime.GOARCH)
	c.clientData.appVersion = getValue(cnf.DeviceConfig.AppVersion, Version)
	c.clientData.langCode = getValue(cnf.DeviceConfig.LangCode, "en")
	c.clientData.logLevel = getValue(cnf.LogLevel, LogInfo)
	c.clientData.parseMode = getValue(cnf.ParseMode, "HTML")
	c.clientData.sleepThresholdMs = getValue(cnf.SleepThresholdMs, 0)

	if cnf.LogLevel == LogDebug {
		c.Log.SetLevel(LogDebug)
	} else {
		c.Log.SetLevel(c.clientData.logLevel)
	}
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
		var dcs = make(map[int][]utils.DC)
		var cdnDcs = make(map[int][]utils.DC)
		for _, dc := range config.DcOptions {
			if !dc.MediaOnly && !dc.Cdn {
				if _, ok := dcs[int(dc.ID)]; !ok {
					dcs[int(dc.ID)] = []utils.DC{}
				}

				dcs[int(dc.ID)] = append(dcs[int(dc.ID)], utils.DC{Addr: dc.IpAddress + ":" + strconv.Itoa(int(dc.Port)), V: dc.Ipv6})
			} else if dc.Cdn {
				if _, ok := cdnDcs[int(dc.ID)]; !ok {
					cdnDcs[int(dc.ID)] = []utils.DC{}
				}

				cdnDcs[int(dc.ID)] = append(cdnDcs[int(dc.ID)], utils.DC{Addr: dc.IpAddress + ":" + strconv.Itoa(int(dc.Port)), V: dc.Ipv6})
			}
		}

		utils.SetDCs(dcs, cdnDcs) // set the upto-date DC configuration for the library
	}

	return nil
}

// Establish connection to telegram servers
func (c *Client) Connect() error {
	defer c.GetMe()

	if c.IsConnected() {
		return nil
	}

	c.Log.Debug("connecting to telegram servers")

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
	return c.MTProto.IsTcpActive()
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

	c.stopCh = make(chan struct{}) // reset the stop channel
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
	return c.MTProto.Disconnect()
}

// switchDC permanently switches the data center
func (c *Client) SwitchDc(dcID int) error {
	c.Log.Debug("switching data center to (" + strconv.Itoa(dcID) + ")")
	newDcSender, err := c.MTProto.SwitchDc(dcID)
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

func (c *Client) Me() *UserObj {
	if c.clientData.me == nil {
		me, err := c.GetMe()
		if err != nil {
			return &UserObj{}
		}
		c.clientData.me = me
	}

	return c.clientData.me
}

type ExSenders struct {
	sync.Mutex
	senders     map[int][]*ExSender
	cleanupDone chan struct{}
}

type ExSender struct {
	*mtproto.MTProto
	lastUsed time.Time
}

func NewExSenders() *ExSenders {
	es := &ExSenders{
		senders: make(map[int][]*ExSender),
	}
	go es.cleanupLoop()
	return es
}

func (es *ExSenders) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	es.cleanupDone = make(chan struct{})
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			es.cleanupIdleSenders()
		case <-es.cleanupDone:
			return
		}
	}
}

func (es *ExSenders) cleanupIdleSenders() {
	es.Lock()
	defer es.Unlock()

	for _, senders := range es.senders {
		for i, sender := range senders {
			if time.Since(sender.lastUsed) > 30*time.Minute {
				sender.Terminate()
				senders[i] = nil
			}
		}
	}

	// Remove nil senders
	for dcID, senders := range es.senders {
		var newSenders []*ExSender
		for _, sender := range senders {
			if sender != nil {
				newSenders = append(newSenders, sender)
			}
		}
		es.senders[dcID] = newSenders
	}
}

func (es *ExSenders) Close() {
	close(es.cleanupDone)
}

// CreateExportedSender creates a new exported sender for the given DC
func (c *Client) CreateExportedSender(dcID int, cdn bool, authParams ...*AuthExportedAuthorization) (*mtproto.MTProto, error) {
	const retryLimit = 3 // Retry only once
	var lastError error

	var authParam = getVariadic(authParams, &AuthExportedAuthorization{})

	for retry := 0; retry <= retryLimit; retry++ {
		c.Log.Debug("creating exported sender for DC ", dcID)
		if cdn {
			if _, has := c.MTProto.HasCdnKey(int32(dcID)); !has {
				cdnKeysResp, err := c.HelpGetCdnConfig()
				if err != nil {
					return nil, errors.Wrap(err, "getting cdn config")
				}

				var cdnKeys = make(map[int32]*rsa.PublicKey)
				for _, key := range cdnKeysResp.PublicKeys {
					cdnKeys[key.DcID], _ = keys.ParsePublicKey(key.PublicKey)
				}
			}
		}

		exported, err := c.MTProto.ExportNewSender(dcID, true, cdn)
		if err != nil {
			lastError = errors.Wrap(err, "exporting new sender")
			c.Log.Error("error exporting new sender: ", lastError)
			continue
		}

		initialReq := &InitConnectionParams{
			ApiID:          c.clientData.appID,
			DeviceModel:    c.clientData.deviceModel,
			SystemVersion:  c.clientData.systemVersion,
			AppVersion:     c.clientData.appVersion,
			SystemLangCode: c.clientData.langCode,
			LangCode:       c.clientData.langCode,
			Query:          &HelpGetConfigParams{},
		}

		if c.MTProto.GetDC() != exported.GetDC() {
			var auth *AuthExportedAuthorization
			if authParam.ID != 0 {
				auth = &AuthExportedAuthorization{
					ID:    authParam.ID,
					Bytes: authParam.Bytes,
				}
			} else {
				c.Log.Info(fmt.Sprintf("exporting auth for data-center %d", exported.GetDC()))
				auth, err = c.AuthExportAuthorization(int32(exported.GetDC()))
				if err != nil {
					lastError = errors.Wrap(err, "exporting auth")
					c.Log.Error("error exporting auth: ", lastError)
					continue
				}

				c.exportedKeys[dcID] = auth
			}

			initialReq.Query = &AuthImportAuthorizationParams{
				ID:    auth.ID,
				Bytes: auth.Bytes,
			}
		}

		c.Log.Debug("sending initial request...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err = exported.MakeRequestCtx(ctx, &InvokeWithLayerParams{
			Layer: ApiVersion,
			Query: initialReq,
		})
		c.Log.Debug(fmt.Sprintf("initial request for exported sender %d sent", dcID))

		if err != nil {
			if c.MatchRPCError(err, "AUTH_BYTES_INVALID") {
				authParam.ID = 0
				c.Log.Debug("auth bytes invalid, re-exporting auth")
				continue
			}

			lastError = errors.Wrap(err, "making initial request")
			if retry < retryLimit {
				c.Log.Debug(fmt.Sprintf("error making initial request, retrying (%d/%d)", retry+1, retryLimit))
			} else {
				c.Log.Error(fmt.Sprintf("exported sender: initialRequest: %s", lastError.Error()))
			}

			time.Sleep(200 * time.Millisecond)
			continue
		}

		return exported, nil
	}

	return nil, lastError
}

// setLogLevel sets the log level for all loggers
func (c *Client) SetLogLevel(level utils.LogLevel) {
	c.Log.Debug("setting library log level to ", level)
	c.Log.SetLevel(level)
	if c.Cache != nil {
		c.Cache.logger.SetLevel(level)
	}
	c.MTProto.Logger.SetLevel(level)
	if c.dispatcher != nil {
		c.dispatcher.logger.SetLevel(level)
	}
}

// disables color for all loggers
func (c *Client) LogColor(mode bool) {
	c.Log.Debug("disabling color for all loggers")

	c.Log.NoColor(!mode)
	if c.Cache != nil {
		c.Cache.logger.NoColor(!mode)
	}
	c.MTProto.Logger.NoColor(!mode)
	if c.dispatcher != nil {
		c.dispatcher.logger.NoColor(!mode)
	}
}

// SetParseMode sets the parse mode for the client
func (c *Client) SetParseMode(mode string) {
	c.clientData.parseMode = mode
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
	c.Log.Debug("exporting auth to string session...")
	return session.NewStringSession(authSession.Key, authSession.Hash, dcId, authSession.Hostname, authSession.AppID).Encode()
}

// ImportSession imports a session from a string
//
//	Params:
//	  sessionString: The sessionString to authenticate with
func (c *Client) ImportSession(sessionString string) (bool, error) {
	c.Log.Debug("importing auth from string session...")
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
	//go c.cleanExportedSenders()
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
	go func() { defer c.wg.Done(); <-c.stopCh; c.exSenders.Close() }()
	c.wg.Wait()
}

// Stop stops the client and disconnects from telegram server
func (c *Client) Stop() error {
	// close(c.stopCh)
	select {
	case <-c.stopCh:
	default:
		close(c.stopCh)
	}

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

type RpcError struct {
	Code        int32
	Message     string
	Description string
}

func (r *RpcError) Error() string {
	return fmt.Sprintf("%s (%d)", r.Message, r.Code)
}

func (c *Client) ToRpcError(err error) *RpcError {
	regex := regexp.MustCompile(`\[(.*)\] (.*) \(code (\d+)\)`)
	matches := regex.FindStringSubmatch(err.Error())
	if len(matches) != 4 {
		return nil
	}

	code, _ := strconv.Atoi(matches[3])
	return &RpcError{
		Code:        int32(code),
		Message:     matches[1],
		Description: matches[2],
	}
}

func (c *Client) MatchRPCError(err error, message string) bool {
	rpcErr := c.ToRpcError(err)
	if rpcErr == nil {
		return false
	}

	return rpcErr.Message == message
}
