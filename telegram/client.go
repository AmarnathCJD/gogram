// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"context"
	"crypto/rsa"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strconv"
	"sync"
	"syscall"
	"time"

	"errors"

	mtproto "github.com/amarnathcjd/gogram"

	"github.com/amarnathcjd/gogram/internal/keys"
	"github.com/amarnathcjd/gogram/internal/session"
	"github.com/amarnathcjd/gogram/internal/utils"
)

const (
	// the initial data center to connect to, before authentication
	DefaultDataCenter = 4
)

type clientData struct {
	appID            int32
	appHash          string
	deviceModel      string
	systemVersion    string
	appVersion       string
	params           JsonValue
	langCode         string
	systemLangCode   string
	langPack         string
	parseMode        string
	logLevel         LogLevel
	sleepThresholdMs int
	albumWaitTime    int64
	botAcc           bool
	me               *UserObj
	commandPrefixes  string
	proxy            Proxy
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
	Log          Logger
}

type DeviceConfig struct {
	DeviceModel    string    // The device model to use
	SystemVersion  string    // The version of the system
	AppVersion     string    // The version of the app
	LangCode       string    // The language code
	SystemLangCode string    // The system language code
	LangPack       string    // The language pack
	Params         JsonValue // Additional parameters to send during initialization
}

type ClientConfig struct {
	AppID            int32                // The App ID from my.telegram.org
	AppHash          string               // The App Hash from my.telegram.org
	DeviceConfig     DeviceConfig         // Device configuration
	Session          string               // The session file to use
	StringSession    string               // The string session to use
	SessionName      string               // The name of the session
	SessionAESKey    string               // The AES key to use for encryption/decryption of the session file
	ParseMode        string               // The parse mode to use (HTML, Markdown)
	MemorySession    bool                 // Don't save the session to a file
	DataCenter       int                  // The data center to connect to (default: 4)
	IpAddr           string               // The IP address of the DC to connect to
	PublicKeys       []*rsa.PublicKey     // The public keys to verify the server with
	NoUpdates        bool                 // Don't handle updates
	DisableCache     bool                 // Disable caching peer and chat information
	TestMode         bool                 // Use the test data centers
	LogLevel         LogLevel             // The library log level
	Logger           Logger               // The logger to use
	Proxy            Proxy                // The proxy to use
	LocalAddr        string               // Local address binding for multi-interface support (IP:port)
	ForceIPv6        bool                 // Force to use IPv6
	NoPreconnect     bool                 // Don't preconnect to the DC until Connect() is called
	Cache            *CACHE               // The cache to use
	CacheSenders     bool                 // cache the exported file op sender
	TransportMode    string               // The transport mode to use (Abridged, Intermediate, Full)
	SleepThresholdMs int                  // The threshold in milliseconds to sleep before flood
	AlbumWaitTime    int64                // The time to wait for album messages (in milliseconds)
	CommandPrefixes  string               // Command prefixes to recognize (default: "/!"), can be multiple like ".?!-/"
	FloodHandler     func(err error) bool // The flood handler to use
	ErrorHandler     func(err error)      // The error handler to use
	Timeout          int                  // Tcp connection timeout in seconds (default: 60s)
	ReqTimeout       int                  // Rpc request timeout in seconds (default: 60s)
	UseWebSocket     bool                 // Use WebSocket transport instead of TCP
	UseWebSocketTLS  bool                 // Use wss:// (WebSocket over TLS) instead of ws://
	EnablePFS        bool                 // Enable Perfect Forward Secrecy using temporary auth keys
}

func NewClient(config ClientConfig) (*Client, error) {
	client := &Client{
		wg:     sync.WaitGroup{},
		stopCh: make(chan struct{}),
	}

	if config.Logger != nil {
		client.Log = config.Logger
		client.Log.SetPrefix("gogram " +
			lp("client", config.SessionName))
		config.LogLevel = config.Logger.Lev()
	} else {
		config.LogLevel = getValue(config.LogLevel, LogInfo)
		client.Log = NewDefaultLogger("gogram " +
			lp("client", config.SessionName))
		client.Log.SetLevel(config.LogLevel)
	}

	config = client.cleanClientConfig(config)
	client.setupClientData(config)

	if config.Cache == nil {
		client.Cache = NewCache(fmt.Sprintf("cache%s.db", config.SessionName), &CacheConfig{
			Disabled: config.DisableCache,
			Logger: getValue(config.Logger, client.Log.WithPrefix("gogram "+
				lp("cache", config.SessionName))),
			LogColor: client.Log.Color(),
			LogLevel: config.LogLevel,
		})
	} else {
		client.Cache = config.Cache
	}

	client.Cache.disabled = config.DisableCache

	if err := client.setupMTProto(config); err != nil {
		return nil, err
	}
	if config.NoUpdates {
		client.Log.Debug("no updates mode enabled, skipping dispatcher setup")
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
			return utils.DcList.GetHostIp(config.DataCenter, config.TestMode, config.ForceIPv6)
		}
	}

	mtpCfg := mtproto.Config{
		AppID:       config.AppID,
		AuthKeyFile: config.Session,
		AuthAESKey:  config.SessionAESKey,
		ServerHost:  toIpAddr(),
		PublicKey:   config.PublicKeys[0],
		DataCenter:  config.DataCenter,
		Logger: c.Log.CloneInternal().
			WithPrefix("gogram " +
				lp("mtproto", config.SessionName)),
		StringSession:   config.StringSession,
		LocalAddr:       config.LocalAddr,
		MemorySession:   config.MemorySession,
		Ipv6:            config.ForceIPv6,
		CustomHost:      customHost,
		FloodHandler:    config.FloodHandler,
		ErrorHandler:    config.ErrorHandler,
		Timeout:         config.Timeout,
		ReqTimeout:      config.ReqTimeout,
		UseWebSocket:    config.UseWebSocket,
		UseWebSocketTLS: config.UseWebSocketTLS,
		EnablePFS:       config.EnablePFS,
		OnMigration: func() {
			c.InitialRequest()
		},
	}

	if config.Proxy != nil {
		mtpCfg.Proxy = config.Proxy.toInternal()
	}

	mtproto, err := mtproto.NewMTProto(mtpCfg)
	if err != nil {
		return fmt.Errorf("creating mtproto client: %w", err)
	}
	c.MTProto = mtproto
	c.clientData.appID = mtproto.AppID() // in case the appId was not provided in the config but was in the session

	if config.StringSession != "" && !config.NoPreconnect {
		c.Log.Debug("preconnecting to telegram servers")
		if err := c.Connect(); err != nil {
			return fmt.Errorf("connecting to telegram servers: %w", err)
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
	langCode := getValue(cnf.DeviceConfig.LangCode, "en")
	c.clientData = clientData{
		appID:            cnf.AppID,
		appHash:          cnf.AppHash,
		deviceModel:      getValue(cnf.DeviceConfig.DeviceModel, "iPhone 17 Pro"),
		systemVersion:    getValue(cnf.DeviceConfig.SystemVersion, "iOS 26.0"),
		appVersion:       getValue(cnf.DeviceConfig.AppVersion, Version),
		langCode:         langCode,
		systemLangCode:   getValue(cnf.DeviceConfig.SystemLangCode, langCode),
		langPack:         getValue(cnf.DeviceConfig.LangPack, "ios"),
		logLevel:         getValue(cnf.LogLevel, LogInfo),
		parseMode:        getValue(cnf.ParseMode, "HTML"),
		sleepThresholdMs: getValue(cnf.SleepThresholdMs, 0),
		albumWaitTime:    getValue(cnf.AlbumWaitTime, 600),
		commandPrefixes:  getValue(cnf.CommandPrefixes, "/!"),
		proxy:            cnf.Proxy,
	}

	if cnf.DeviceConfig.Params != nil {
		c.clientData.params = cnf.DeviceConfig.Params
	}

	if cnf.LogLevel == LogDebug {
		c.Log.SetLevel(LogDebug)
	} else {
		c.Log.SetLevel(c.clientData.logLevel)
	}
}

// InitialRequest sends the initial initConnection request
func (c *Client) InitialRequest() error {
	c.Log.Debug("sending initial invokeWithLayer request")
	request := &InitConnectionParams{
		ApiID:          c.clientData.appID,
		DeviceModel:    c.clientData.deviceModel,
		SystemVersion:  c.clientData.systemVersion,
		AppVersion:     c.clientData.appVersion,
		SystemLangCode: c.clientData.systemLangCode,
		LangCode:       c.clientData.langCode,
		LangPack:       c.clientData.langPack,
		Query:          &HelpGetConfigParams{},
	}

	if c.clientData.proxy != nil && c.clientData.proxy.Type() == "mtproxy" {
		request.Proxy = &InputClientProxy{
			Address: c.clientData.proxy.GetHost(),
			Port:    int32(c.clientData.proxy.GetPort()),
		}
	}

	if c.clientData.params != nil {
		request.Params = c.clientData.params
	}

	serverConfig, err := c.InvokeWithLayer(ApiVersion, request)

	if err != nil {
		return fmt.Errorf("sending invokeWithLayer: %w", err)
	}

	c.Log.Debug("received server config from invokeWithLayer")
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

		c.DcList.SetDCs(dcs, cdnDcs) // set the up to-date DC configuration for the library
	}

	return nil
}

// Establish connection to telegram servers
func (c *Client) Connect() error {
	if c.IsConnected() {
		return nil
	}

	c.Log.Debug("connecting to telegram servers")

	err := c.MTProto.CreateConnection(true)
	if err != nil {
		return fmt.Errorf("connecting to telegram servers: %w", err)
	}

	// Initial request (invokeWithLayer) must be sent after connection is established
	err = c.InitialRequest()
	if err != nil {
		return fmt.Errorf("sending initial request: %w", err)
	}

	if is, err := c.IsAuthorized(); err == nil && is {
		_, _ = c.GetMe()
	}
	return err
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
	c.MTProto.SetTerminated(false) // reset the terminated state
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

func (c *Client) St() error {
	c.MTProto.SetTerminated(false) // reset the terminated state
	if !c.IsConnected() {
		if err := c.Connect(); err != nil {
			return err
		}
	}

	c.stopCh = make(chan struct{}) // reset the stop channel
	return nil
}

// Returns true if the client is authorized as a user or a bot
func (c *Client) IsAuthorized() (bool, error) {
	c.Log.Debug("fetching updates state to check authorization")
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
	c.Log.Debug("switching to data center: %d", dcID)
	if err := c.MTProto.SwitchDc(dcID); err != nil {
		return fmt.Errorf("reconnecting to new dc: %w", err)
	}
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
		if c.Cache != nil {
			if err := c.Cache.BindToUser(me.ID, c.clientData.appID); err != nil {
				c.Log.WithError(err).Warn("failed to bind cache to user")
			}
		}
	}

	return c.clientData.me
}

type ExSenders struct {
	sync.Mutex
	senders     map[int][]*ExSender
	cleanupDone chan struct{}
	closeOnce   sync.Once
}

type ExSender struct {
	*mtproto.MTProto
	lastUsed   time.Time
	lastUsedMu sync.Mutex
}

func NewExSender(mtProto *mtproto.MTProto) *ExSender {
	return &ExSender{
		MTProto:  mtProto,
		lastUsed: time.Now(),
	}
}

func (es *ExSender) GetLastUsedTime() time.Time {
	es.lastUsedMu.Lock()
	defer es.lastUsedMu.Unlock()
	return es.lastUsed
}

func NewExSenders() *ExSenders {
	es := &ExSenders{
		senders:     make(map[int][]*ExSender),
		cleanupDone: make(chan struct{}),
	}
	go es.cleanupLoop()
	return es
}

func (es *ExSenders) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Minute)
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
			if time.Since(sender.GetLastUsedTime()) > 30*time.Minute {
				sender.Terminate()
				senders[i] = nil
			}
		}
	}

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
	es.closeOnce.Do(func() {
		close(es.cleanupDone)
	})
}

// CreateExportedSender creates a new exported sender for the given DC
func (c *Client) CreateExportedSender(dcID int, cdn bool, authParams ...*AuthExportedAuthorization) (*mtproto.MTProto, error) {
	if dcID <= 0 {
		return nil, errors.New("invalid data center ID")
	}
	const retryLimit = 3
	var lastError error

	var authParam = getVariadic(authParams, &AuthExportedAuthorization{})

	for retry := 0; retry <= retryLimit; retry++ {
		c.Log.Debug("creating exported sender for dc %d", dcID)
		if cdn {
			if _, has := c.MTProto.HasCdnKey(int32(dcID)); !has {
				cdnKeysResp, err := c.HelpGetCdnConfig()
				if err != nil {
					return nil, fmt.Errorf("getting cdn config: %w", err)
				}

				var cdnKeys = make(map[int32]*rsa.PublicKey)
				for _, key := range cdnKeysResp.PublicKeys {
					cdnKeys[key.DcID], _ = keys.ParsePublicKey(key.PublicKey)
				}
			}
		}

		exported, err := c.MTProto.ExportNewSender(dcID, true, cdn)
		if err != nil {
			lastError = fmt.Errorf("exporting new sender: %w", err)
			c.Log.Error("error exporting new sender: %s", lastError.Error())
			continue
		}

		initialReq := &InitConnectionParams{
			ApiID:          c.clientData.appID,
			DeviceModel:    c.clientData.deviceModel,
			SystemVersion:  c.clientData.systemVersion,
			AppVersion:     c.clientData.appVersion,
			SystemLangCode: c.clientData.systemLangCode,
			LangCode:       c.clientData.langCode,
			LangPack:       c.clientData.langPack,
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
				c.Log.Info("exporting auth for dc %d", dcID)
				auth, err = c.AuthExportAuthorization(int32(exported.GetDC()))
				if err != nil {
					lastError = fmt.Errorf("exporting auth: %w", err)
					c.Log.Error("error exporting auth: %s", lastError.Error())
					continue
				}

				if c.exportedKeys == nil {
					c.exportedKeys = make(map[int]*AuthExportedAuthorization)
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
		_, err = exported.MakeRequestCtx(ctx, &InvokeWithLayerParams{
			Layer: ApiVersion,
			Query: initialReq,
		})
		cancel()
		c.Log.Debug(fmt.Sprintf("initial request for exported sender %d sent", dcID))

		if err != nil {
			if c.MatchRPCError(err, "AUTH_BYTES_INVALID") {
				authParam.ID = 0
				c.Log.Debug("AUTH_BYTES_INVALID received, retrying export of auth bytes")
				continue
			}

			lastError = fmt.Errorf("making initial request: %w", err)
			if retry < retryLimit {
				c.Log.Debug("error making initial request, retrying (%d/%d)", retry+1, retryLimit)
			} else {
				c.Log.Error("exported sender: initialRequest: %s", lastError.Error())
			}

			time.Sleep(200 * time.Millisecond)
			continue
		}

		return exported, nil
	}

	return nil, lastError
}

// setLogLevel sets the log level for all loggers
func (c *Client) SetLogLevel(level LogLevel) {
	c.Log.Debug("setting library log level to %d", level)
	c.Log.SetLevel(level)
	if c.Cache != nil {
		c.Cache.logger.SetLevel(level)
	}
	c.MTProto.Logger.SetLevel(utils.LogLevel(level))
	if c.dispatcher != nil {
		c.dispatcher.logger.SetLevel(level)
	}
}

// disables color for all loggers
func (c *Client) LogColor(mode bool) {
	c.Log.Debug("disabling color for all loggers")

	c.Log.SetColor(mode)
	if c.Cache != nil {
		c.Cache.logger.SetColor(mode)
	}
	c.MTProto.Logger.SetColor(mode)
	if c.dispatcher != nil {
		c.dispatcher.logger.SetColor(mode)
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

func (c *Client) GetCurrentIP() string {
	return c.MTProto.Addr
}

// ExportStringSession exports the current session to a string,
func (c *Client) ExportStringSession() string {
	return c.ExportSession()
}

// ImportStringSession imports a session from a string
func (c *Client) ImportStringSession(sessionString string) (bool, error) {
	return c.ImportSession(sessionString)
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

// ImportRawSession imports a session from raw fields
func (c *Client) ImportRawSession(sess *Session) error {
	return c.MTProto.LoadSession(&session.Session{
		Key:      sess.Key,
		Hash:     sess.Hash,
		Salt:     sess.Salt,
		Hostname: sess.Hostname,
		AppID:    sess.AppID,
	})
}

// ExportRawSession exports the current session to raw fields
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

// GetProxy returns the proxy configuration
func (c *Client) GetProxy() Proxy {
	return c.clientData.proxy
}

// SetProxy sets the proxy configuration
func (c *Client) SetProxy(proxy Proxy) {
	c.clientData.proxy = proxy
}

// CommandPrefixes returns the command prefixes configured for the client
func (c *Client) CommandPrefixes() string {
	return c.clientData.commandPrefixes
}

// SetCommandPrefixes sets the command prefixes for the client
func (c *Client) SetCommandPrefixes(prefixes string) {
	c.clientData.commandPrefixes = prefixes
}

// Terminate client and disconnect from telegram server
func (c *Client) Terminate() error {
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

// WrapError logs and returns the error if it's not nil.
// Returns the same error for chaining.
func (c *Client) WrapError(err error) error {
	if err != nil {
		c.Log.Error(err)
	}
	return err
}

// Must panics if err is not nil, otherwise returns obj.
// Useful for operations that should never fail in production.
func (c *Client) Must(obj any, err error) any {
	if err != nil {
		panic(err)
	}
	return obj
}

// Try returns the object and ignores the error.
// Use when you want to safely ignore errors.
func (c *Client) Try(obj any, err error) any {
	if err != nil {
		c.Log.Debug("try: ignoring error: %v", err)
	}
	return obj
}

// CheckErr returns only the error, discarding the object.
// Useful for checking if an operation succeeded without caring about the result.
func (c *Client) CheckErr(_ any, err error) error {
	return err
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

func (c *Client) TypeOf(obj any) string {
	return fmt.Sprintf("%T", obj)
}

func (c *Client) MatchRPCError(err error, message string) bool {
	rpcErr := c.ToRpcError(err)
	if rpcErr == nil {
		return false
	}

	return rpcErr.Message == message
}

// ClientConfigBuilder
type ClientConfigBuilder struct {
	config ClientConfig
}

func NewClientConfigBuilder(appID int32, appHash string) *ClientConfigBuilder {
	return &ClientConfigBuilder{
		config: ClientConfig{
			AppID:   appID,
			AppHash: appHash,
			DeviceConfig: DeviceConfig{
				DeviceModel:   "IPhone 17 Pro",
				SystemVersion: "iOS 26.0",
				AppVersion:    Version,
			},
			DataCenter:    4,          // Default DC
			ParseMode:     "HTML",     // Default parse mode
			LogLevel:      InfoLevel,  // Default log level
			TransportMode: "Abridged", // Default transport mode
		},
	}
}

func (b *ClientConfigBuilder) WithDeviceConfig(deviceModel, systemVersion, appVersion, langCode, sysLangCode, langPack string) *ClientConfigBuilder {
	b.config.DeviceConfig = DeviceConfig{
		DeviceModel:    deviceModel,
		SystemVersion:  systemVersion,
		AppVersion:     appVersion,
		LangCode:       langCode,
		SystemLangCode: sysLangCode,
		LangPack:       langPack,
	}
	return b
}

func (b *ClientConfigBuilder) WithSession(session string, sessionAESKey ...string) *ClientConfigBuilder {
	b.config.Session = session
	if len(sessionAESKey) > 0 {
		b.config.SessionAESKey = sessionAESKey[0]
	}
	return b
}

func (b *ClientConfigBuilder) WithStringSession(stringSession string) *ClientConfigBuilder {
	b.config.StringSession = stringSession
	return b
}

func (b *ClientConfigBuilder) WithParseMode(parseMode string) *ClientConfigBuilder {
	b.config.ParseMode = parseMode
	return b
}

func (b *ClientConfigBuilder) WithDataCenter(dc int) *ClientConfigBuilder {
	b.config.DataCenter = dc
	return b
}

func (b *ClientConfigBuilder) WithProxy(proxy Proxy) *ClientConfigBuilder {
	b.config.Proxy = proxy
	return b
}

func (b *ClientConfigBuilder) WithLocalAddr(localAddr string) *ClientConfigBuilder {
	b.config.LocalAddr = localAddr
	return b
}

func (b *ClientConfigBuilder) WithLogLevel(level LogLevel) *ClientConfigBuilder {
	b.config.LogLevel = level
	return b
}

func (b *ClientConfigBuilder) WithMemorySession() *ClientConfigBuilder {
	b.config.MemorySession = true
	return b
}

func (b *ClientConfigBuilder) WithNoUpdates() *ClientConfigBuilder {
	b.config.NoUpdates = true
	return b
}

func (b *ClientConfigBuilder) WithDisableCache() *ClientConfigBuilder {
	b.config.DisableCache = true
	return b
}

func (b *ClientConfigBuilder) WithTestMode() *ClientConfigBuilder {
	b.config.TestMode = true
	return b
}

func (b *ClientConfigBuilder) WithIpAddr(ipAddr string) *ClientConfigBuilder {
	b.config.IpAddr = ipAddr
	return b
}

func (b *ClientConfigBuilder) WithPublicKeys(publicKeys []*rsa.PublicKey) *ClientConfigBuilder {
	b.config.PublicKeys = publicKeys
	return b
}

func (b *ClientConfigBuilder) WithCache(cache *CACHE) *ClientConfigBuilder {
	b.config.Cache = cache
	return b
}

func (b *ClientConfigBuilder) WithCacheSenders() *ClientConfigBuilder {
	b.config.CacheSenders = true
	return b
}

func (b *ClientConfigBuilder) WithSleepThresholdMs(threshold int) *ClientConfigBuilder {
	b.config.SleepThresholdMs = threshold
	return b
}

func (b *ClientConfigBuilder) WithFloodHandler(handler func(err error) bool) *ClientConfigBuilder {
	b.config.FloodHandler = handler
	return b
}

func (b *ClientConfigBuilder) WithErrorHandler(handler func(err error)) *ClientConfigBuilder {
	b.config.ErrorHandler = handler
	return b
}

func (b *ClientConfigBuilder) WithSessionName(name string) *ClientConfigBuilder {
	b.config.SessionName = name
	return b
}

func (b *ClientConfigBuilder) WithTransportMode(mode string) *ClientConfigBuilder {
	b.config.TransportMode = mode
	return b
}

func (b *ClientConfigBuilder) WithLogger(logger Logger) *ClientConfigBuilder {
	b.config.Logger = logger
	return b
}

func (b *ClientConfigBuilder) WithTimeout(timeout int) *ClientConfigBuilder {
	b.config.Timeout = timeout
	return b
}

func (b *ClientConfigBuilder) WithReqTimeout(reqTimeout int) *ClientConfigBuilder {
	b.config.ReqTimeout = reqTimeout
	return b
}

func (b *ClientConfigBuilder) Build() ClientConfig {
	return b.config
}
