// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"errors"

	"github.com/amarnathcjd/gogram/internal/utils"
)

// TaskPool limits the number of concurrent goroutines
type TaskPool struct {
	tasks  chan func()
	wg     sync.WaitGroup
	active atomic.Int32
	closed atomic.Bool
}

func NewTaskPool(size int) *TaskPool {
	p := &TaskPool{
		tasks: make(chan func(), size*100),
	}
	p.wg.Add(size)
	for range size {
		go p.worker()
	}
	return p
}

func (p *TaskPool) worker() {
	defer p.wg.Done()
	for task := range p.tasks {
		p.active.Add(1)
		task()
		p.active.Add(-1)
	}
}

func (p *TaskPool) Submit(task func()) {
	if p.closed.Load() {
		return
	}
	select {
	case p.tasks <- task:
	default:
		p.tasks <- task
	}
}

func (p *TaskPool) Close() {
	if p.closed.Swap(true) {
		return
	}
	close(p.tasks)
	p.wg.Wait()
}

type mimeTypeManager struct {
	mimeTypes       map[string]string // ext -> mime
	streamableMimes map[string]bool   // streamable video mime types
	streamableExts  map[string]bool   // streamable video extensions
	audioExts       map[string]bool   // audio file extensions
	imageExts       map[string]bool   // image file extensions
}

func (m *mimeTypeManager) addMime(ext, mime string) {
	m.mimeTypes[ext] = mime
}

func (m *mimeTypeManager) match(filePath string) string {
	if IsURL(filePath) {
		if u, err := url.Parse(filePath); err == nil {
			ext := strings.ToLower(filepath.Ext(u.Path))
			if mime, ok := m.mimeTypes[ext]; ok {
				return mime
			}
		}

		req, err := http.NewRequest("HEAD", filePath, nil)
		if err != nil {
			return ""
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; rv:60.0) Gecko/20100101 Firefox/60.0")
		HTTPClient := &http.Client{Timeout: 4 * time.Second}

		resp, err := HTTPClient.Do(req)
		if err != nil {
			return ""
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 && resp.Header.Get("Content-Type") != "" {
			return resp.Header.Get("Content-Type")
		}

		req, err = http.NewRequest("GET", filePath, nil)
		if err != nil {
			return ""
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; rv:60.0) Gecko/20100101 Firefox/60.0")
		req.Header.Set("Range", "bytes=0-0")

		resp, err = HTTPClient.Do(req)
		if err != nil {
			return ""
		}
		defer resp.Body.Close()

		if (resp.StatusCode == 200 || resp.StatusCode == 206) && resp.Header.Get("Content-Type") != "" {
			return resp.Header.Get("Content-Type")
		}
		return ""
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if mime, ok := m.mimeTypes[ext]; ok {
		return mime
	}

	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer file.Close()

	buffer := make([]byte, 512)
	if _, err = file.Read(buffer); err != nil {
		return ""
	}
	return http.DetectContentType(buffer)
}

func (m *mimeTypeManager) Ext(mime string) string {
	for ext, mimeType := range m.mimeTypes {
		if mimeType == mime {
			return ext
		}
	}
	return ""
}

func (m *mimeTypeManager) IsPhoto(mime string) bool {
	return strings.HasPrefix(mime, "image/") && !strings.Contains(mime, "image/webp")
}

func (m *mimeTypeManager) MIME(filePath string) (string, bool) {
	mime := m.match(filePath)
	return mime, m.IsPhoto(mime)
}

func (m *mimeTypeManager) IsStreamable(mimeType string) bool {
	return m.streamableMimes[mimeType]
}

func (m *mimeTypeManager) IsStreamableFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return m.streamableExts[ext]
}

func (m *mimeTypeManager) IsAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return m.audioExts[ext]
}

func (m *mimeTypeManager) IsImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return m.imageExts[ext]
}

func (m *mimeTypeManager) IsVideoFile(path string) bool {
	return m.IsStreamableFile(path)
}

func (m *mimeTypeManager) GetInlineType(mimeType string, voiceNote bool) string {
	if voiceNote {
		return "voice"
	}

	switch mimeType {
	case "audio/mp3", "audio/mpeg", "audio/ogg", "audio/x-flac", "audio/x-alac", "audio/x-wav", "audio/x-m4a", "audio/aac", "audio/opus":
		return "audio"
	case "image/gif":
		return "gif"
	case "image/jpeg", "image/png":
		return "photo"
	case "image/webp", "application/x-tgsticker":
		return "sticker"
	case "video/mp4", "video/x-matroska", "video/webm":
		return "video"
	default:
		return "file"
	}
}

var MimeTypes = &mimeTypeManager{
	mimeTypes:       make(map[string]string),
	streamableMimes: make(map[string]bool),
	streamableExts:  make(map[string]bool),
	audioExts:       make(map[string]bool),
	imageExts:       make(map[string]bool),
}

func init() {
	for ext, mime := range map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".webp": "image/webp", ".gif": "image/gif", ".bmp": "image/bmp",
		".tga": "image/x-tga", ".tiff": "image/tiff", ".tif": "image/tiff",
		".ico": "image/x-icon", ".svg": "image/svg+xml", ".heic": "image/heic",
		".heif": "image/heif", ".avif": "image/avif", ".psd": "image/vnd.adobe.photoshop", ".raw": "image/x-raw",
	} {
		MimeTypes.addMime(ext, mime)
		MimeTypes.imageExts[ext] = true
	}

	for ext, mime := range map[string]string{
		".mp4": "video/mp4", ".m4v": "video/x-m4v", ".mov": "video/quicktime",
		".avi": "video/x-msvideo", ".wmv": "video/x-ms-wmv", ".flv": "video/x-flv",
		".mkv": "video/x-matroska", ".webm": "video/webm", ".3gp": "video/3gpp",
		".3g2": "video/3gpp2", ".mpeg": "video/mpeg", ".mpg": "video/mpeg",
		".ogv": "video/ogg", ".ts": "video/mp2t", ".mts": "video/mp2t",
		".m2ts": "video/mp2t", ".vob": "video/mpeg", ".asf": "video/x-ms-asf", ".divx": "video/x-divx",
	} {
		MimeTypes.addMime(ext, mime)
		MimeTypes.streamableExts[ext] = true
	}

	for _, mime := range []string{
		"video/mp4", "video/webm", "video/mpeg", "video/ogg", "video/x-msvideo", "video/x-ms-wmv",
		"video/x-flv", "video/x-m4v", "video/quicktime", "video/3gpp", "video/3gpp2", "video/matroska",
		"video/x-matroska", "video/mp2t", "video/x-ms-asf", "video/divx", "video/x-divx", "video/vnd.dlna.mpeg-tts", "video/avi",
	} {
		MimeTypes.streamableMimes[mime] = true
	}

	for ext, mime := range map[string]string{
		".mp3": "audio/mpeg", ".m4a": "audio/m4a", ".aac": "audio/aac",
		".ogg": "audio/ogg", ".oga": "audio/ogg", ".flac": "audio/x-flac",
		".opus": "audio/opus", ".wav": "audio/wav", ".alac": "audio/x-alac",
		".wma": "audio/x-ms-wma", ".aiff": "audio/aiff", ".aif": "audio/aiff",
		".ape": "audio/x-ape", ".wv": "audio/x-wavpack", ".mka": "audio/x-matroska",
		".mid": "audio/midi", ".midi": "audio/midi", ".amr": "audio/amr",
		".ac3": "audio/ac3", ".dts": "audio/vnd.dts", ".ra": "audio/vnd.rn-realaudio", ".spx": "audio/ogg",
	} {
		MimeTypes.addMime(ext, mime)
		MimeTypes.audioExts[ext] = true
	}

	for ext, mime := range map[string]string{
		".tgs": "application/x-tgsticker", ".pdf": "application/pdf", ".zip": "application/zip",
		".rar": "application/x-rar-compressed", ".7z": "application/x-7z-compressed", ".tar": "application/x-tar",
		".gz": "application/gzip", ".json": "application/json", ".xml": "application/xml",
		".txt": "text/plain", ".html": "text/html", ".css": "text/css", ".js": "application/javascript",
	} {
		MimeTypes.addMime(ext, mime)
	}
}

func parseInt64(s string) (int64, error) {
	s = strings.TrimSpace(s)
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

type Proxy interface {
	GetHost() string
	GetPort() int
	Type() string
	GetUsername() string
	GetPassword() string
	GetSecret() string
	toInternal() *utils.Proxy
}

type BaseProxy struct {
	Host string
	Port int
}

func (p *BaseProxy) GetHost() string { return p.Host }
func (p *BaseProxy) GetPort() int    { return p.Port }

type Socks5Proxy struct {
	BaseProxy
	Username string
	Password string
}

func (s *Socks5Proxy) Type() string        { return "socks5" }
func (s *Socks5Proxy) GetUsername() string { return s.Username }
func (s *Socks5Proxy) GetPassword() string { return s.Password }
func (s *Socks5Proxy) GetSecret() string   { return "" }

func (s *Socks5Proxy) toInternal() *utils.Proxy {
	return &utils.Proxy{
		Type:     "socks5",
		Host:     s.Host,
		Port:     s.Port,
		Username: s.Username,
		Password: s.Password,
	}
}

type Socks4Proxy struct {
	BaseProxy
	UserID string
}

func (s *Socks4Proxy) Type() string        { return "socks4" }
func (s *Socks4Proxy) GetUsername() string { return s.UserID }
func (s *Socks4Proxy) GetPassword() string { return "" }
func (s *Socks4Proxy) GetSecret() string   { return "" }

func (s *Socks4Proxy) toInternal() *utils.Proxy {
	return &utils.Proxy{
		Type:     "socks4",
		Host:     s.Host,
		Port:     s.Port,
		Username: s.UserID,
	}
}

type HttpProxy struct {
	BaseProxy
	Username string
	Password string
}

func (h *HttpProxy) Type() string        { return "http" }
func (h *HttpProxy) GetUsername() string { return h.Username }
func (h *HttpProxy) GetPassword() string { return h.Password }
func (h *HttpProxy) GetSecret() string   { return "" }

func (h *HttpProxy) toInternal() *utils.Proxy {
	return &utils.Proxy{
		Type:     "http",
		Host:     h.Host,
		Port:     h.Port,
		Username: h.Username,
		Password: h.Password,
	}
}

type MTProxy struct {
	BaseProxy
	Secret string
}

func (m *MTProxy) Type() string        { return "mtproxy" }
func (m *MTProxy) GetUsername() string { return "" }
func (m *MTProxy) GetPassword() string { return "" }
func (m *MTProxy) GetSecret() string   { return m.Secret }

func (m *MTProxy) toInternal() *utils.Proxy {
	return &utils.Proxy{
		Type:   "mtproxy",
		Host:   m.Host,
		Port:   m.Port,
		Secret: m.Secret,
	}
}

// ProxyFromURL creates a Proxy from a URL string
//   - socks4://[userid@]host:port
//   - socks5://[user:pass@]host:port
//   - http://[user:pass@]host:port
//   - https://[user:pass@]host:port
//   - mtproxy://secret@host:port
//   - tg://proxy?server=host&port=port&secret=secret
//   - secret@host:port (for mtproxy without scheme)
func ProxyFromURL(proxyURL string) (Proxy, error) {
	if proxyURL == "" {
		return nil, errors.New("empty proxy URL")
	}

	u, err := url.Parse(proxyURL)
	if err != nil {
		matches := proxyURLRegex.FindStringSubmatch(proxyURL)
		if len(matches) == 4 {
			port, _ := strconv.Atoi(matches[3])
			return &MTProxy{
				BaseProxy: BaseProxy{
					Host: matches[2],
					Port: port,
				},
				Secret: matches[1],
			}, nil
		}
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)

	if (scheme == "http" || scheme == "https") && strings.Contains(u.Host, "t.me") && strings.HasPrefix(u.Path, "/proxy") {
		scheme = "tg"
	}

	var port int
	portStr := u.Port()
	if portStr != "" {
		port, _ = strconv.Atoi(portStr)
	} else {
		switch scheme {
		case "socks4", "socks4a", "socks5", "socks5h":
			port = 1080
		case "http":
			port = 8080
		case "https", "mtproxy", "tg":
			port = 443
		}
	}

	switch scheme {
	case "socks4", "socks4a":
		proxy := &Socks4Proxy{
			BaseProxy: BaseProxy{
				Host: u.Hostname(),
				Port: port,
			},
		}
		if u.User != nil {
			proxy.UserID = u.User.Username()
		}
		return proxy, nil

	case "socks5", "socks5h":
		proxy := &Socks5Proxy{
			BaseProxy: BaseProxy{
				Host: u.Hostname(),
				Port: port,
			},
		}
		if u.User != nil {
			proxy.Username = u.User.Username()
			proxy.Password, _ = u.User.Password()
		}
		return proxy, nil

	case "http", "https":
		proxy := &HttpProxy{
			BaseProxy: BaseProxy{
				Host: u.Hostname(),
				Port: port,
			},
		}
		if u.User != nil {
			proxy.Username = u.User.Username()
			proxy.Password, _ = u.User.Password()
		}
		return proxy, nil

	case "mtproxy":
		proxy := &MTProxy{
			BaseProxy: BaseProxy{
				Host: u.Hostname(),
				Port: port,
			},
		}
		if u.User != nil {
			proxy.Secret = u.User.Username()
		}
		return proxy, nil

	case "tg":
		// Format: tg://proxy?server=host&port=port&secret=secret
		q := u.Query()
		host := q.Get("server")
		if host == "" {
			host = u.Hostname()
		}
		if portQuery := q.Get("port"); portQuery != "" {
			port, _ = strconv.Atoi(portQuery)
		}
		return &MTProxy{
			BaseProxy: BaseProxy{
				Host: host,
				Port: port,
			},
			Secret: q.Get("secret"),
		}, nil

	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", scheme)
	}
}

func GetFloodWait(err error) int {
	if err == nil {
		return 0
	}

	if regexFloodWait.MatchString(err.Error()) {
		wait, _ := strconv.Atoi(regexFloodWait.FindStringSubmatch(err.Error())[1])
		return wait
	} else if regexFloodWaitBasic.MatchString(err.Error()) {
		wait, _ := strconv.Atoi(regexFloodWaitBasic.FindStringSubmatch(err.Error())[1])
		return wait
	} else if regexFloodWaitPremium.MatchString(err.Error()) {
		wait, _ := strconv.Atoi(regexFloodWaitPremium.FindStringSubmatch(err.Error())[1])
		return wait
	}

	return 0
}

func MatchError(err error, str string) bool {
	if err != nil {
		return strings.Contains(err.Error(), str)
	}
	return false
}

type FileLocationOptions struct {
	ThumbOnly bool      // Get thumbnail location only
	Video     bool      // Get video version (for animated content)
	ThumbSize PhotoSize // Specific thumbnail size to retrieve
}

// GetFileLocation returns file location, datacenter, file size and file name
func GetFileLocation(file any, opts ...FileLocationOptions) (InputFileLocation, int32, int64, string, error) {
	var opt = getVariadic(opts, FileLocationOptions{})

	var (
		location   any
		dataCenter int32 = 4
		fileSize   int64
	)
mediaMessageSwitch:
	switch f := file.(type) {
	case InputFileLocation:
		return f, dataCenter, fileSize, "", nil
	case UserPhoto:
		location, _ = f.InputLocation()
		dataCenter = f.DcID()
		fileSize = f.FileSize()
	case Photo, Document:
		location = f
	case *MessageMediaDocument:
		location = f.Document
	case *MessageMediaPhoto:
		location = f.Photo
	case *NewMessage:
		if !f.IsMedia() {
			return nil, 0, 0, "", errors.New("message is not media")
		}
		file = f.Media()
		goto mediaMessageSwitch
	case *MessageService:
		if f.Action != nil {
			switch f := f.Action.(type) {
			case *MessageActionChatEditPhoto:
				location = f.Photo
			}
		}
	case *MessageMediaWebPage:
		if f.Webpage != nil {
			switch w := f.Webpage.(type) {
			case *WebPageObj:
				if w.Photo != nil {
					location = w.Photo
				} else if w.Document != nil {
					location = w.Document
				}
			}
		}
	default:
		return nil, 0, 0, "", errors.New("unsupported file type")
	}
	switch l := location.(type) {
	case *DocumentObj:
		if opt.ThumbOnly {
			var selectedThumb = l.Thumbs[0]
			if opt.ThumbSize != nil {
				selectedThumb = opt.ThumbSize
			}

			size, sizeType := getPhotoSize(selectedThumb)
			return &InputDocumentFileLocation{
				ID:            l.ID,
				AccessHash:    l.AccessHash,
				FileReference: l.FileReference,
				ThumbSize:     sizeType,
			}, l.DcID, size, GetFileName(l), nil
		}

		return &InputDocumentFileLocation{
			ID:            l.ID,
			AccessHash:    l.AccessHash,
			FileReference: l.FileReference,
			ThumbSize:     "",
		}, l.DcID, l.Size, GetFileName(l), nil
	case *PhotoObj:
		if len(l.VideoSizes) > 0 && opt.Video {
			var selectedThumb = l.VideoSizes[len(l.VideoSizes)-1]
			size, sizeType := getVideoSize(selectedThumb)
			return &InputPhotoFileLocation{
				ID:            l.ID,
				AccessHash:    l.AccessHash,
				FileReference: l.FileReference,
				ThumbSize:     sizeType,
			}, l.DcID, size, GetFileName(l, true), nil
		}

		var selectedThumb = l.Sizes[len(l.Sizes)-1]
		if len(opts) > 0 && opts[0].ThumbOnly {
			if opts[0].ThumbSize != nil {
				selectedThumb = opts[0].ThumbSize
			} else {
				selectedThumb = l.Sizes[0]
			}
		}

		size, sizeType := getPhotoSize(selectedThumb)
		return &InputPhotoFileLocation{
			ID:            l.ID,
			AccessHash:    l.AccessHash,
			FileReference: l.FileReference,
			ThumbSize:     sizeType,
		}, l.DcID, size, GetFileName(l), nil
	case *InputPhotoFileLocation:
		return l, dataCenter, fileSize, "", nil
	default:
		return nil, 0, 0, "", errors.New("unsupported file type")
	}
}

func getPhotoSize(sizes PhotoSize) (int64, string) {
	switch s := sizes.(type) {
	case *PhotoSizeObj:
		return int64(s.Size), s.Type
	case *PhotoStrippedSize:
		if len(s.Bytes) < 3 || s.Bytes[0] != 1 {
			return int64(len(s.Bytes)), s.Type
		}
		return int64(len(s.Bytes)) + 622, s.Type
	case *PhotoCachedSize:
		return int64(len(s.Bytes)), s.Type
	case *PhotoSizeEmpty:
		return 0, s.Type
	case *PhotoSizeProgressive:
		return int64(getMax(s.Sizes)), s.Type
	default:
		return 0, "w"
	}
}

func getVideoSize(sizes VideoSize) (int64, string) {
	switch s := sizes.(type) {
	case *VideoSizeObj:
		return int64(s.Size), s.Type
	}

	return 0, ""
}

func getMax(a []int32) int32 {
	if len(a) == 0 {
		return 0
	}
	var maximum = a[0]
	for _, v := range a {
		if v > maximum {
			maximum = v
		}
	}
	return maximum
}

func pathIsDir(path string) bool {
	return strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\")
}

func sanitizePath(path string, filename string) string {
	if pathIsDir(path) {
		return filepath.Join(path, filename)
	}
	return path
}

// GetFileName returns the file name of Media, if not specified then it generates a random name
func GetFileName(f any, video ...bool) string {
	var isVid = getVariadic(video, false)

	generateName := func(prefix, ext string) string {
		var b strings.Builder
		b.Grow(len(prefix) + 25 + len(ext))
		b.WriteString(prefix)
		b.WriteByte('_')
		b.WriteString(time.Now().Format("2006-01-02_15-04-05"))
		b.WriteByte('_')
		b.WriteString(strconv.Itoa(rand.IntN(1000)))
		b.WriteString(ext)
		return b.String()
	}

	getDocName := func(doc *DocumentObj) string {
		for _, attr := range doc.Attributes {
			if filename, ok := attr.(*DocumentAttributeFilename); ok && filename.FileName != "" {
				return filename.FileName
			}
		}

		for _, attr := range doc.Attributes {
			switch attr := attr.(type) {
			case *DocumentAttributeAudio:
				if attr.Title != "" {
					return attr.Title + ".mp3"
				}
			case *DocumentAttributeVideo:
				return generateName("video", ".mp4")
			case *DocumentAttributeAnimated:
				return generateName("animation", ".gif")
			case *DocumentAttributeSticker:
				return generateName("sticker", ".webp")
			}
		}

		if doc.MimeType != "" {
			return generateName("file", MimeTypes.Ext(doc.MimeType))
		}
		return generateName("file", "")
	}

	switch f := f.(type) {
	case *MessageMediaDocument:
		switch doc := f.Document.(type) {
		case *DocumentObj:
			return getDocName(doc)
		default:
			return generateName("file", "")
		}
	case *MessageMediaPhoto:
		if isVid {
			return generateName("video", ".mp4")
		}
		return generateName("photo", ".jpg")
	case *MessageMediaContact:
		return fmt.Sprintf("contact_%s_%d.vcf", f.FirstName, rand.IntN(1000))
	case *DocumentObj:
		return getDocName(f)
	case *PhotoObj:
		if isVid {
			return generateName("video", ".mp4")
		}
		return generateName("photo", ".jpg")
	case *InputPeerPhotoFileLocation:
		return generateName("profile_photo", ".jpg")
	case *InputPhotoFileLocation:
		return generateName("photo_file", ".jpg")
	default:
		return generateName("file", "")
	}
}

// GetFileSize returns the file size of Media
func GetFileSize(f any) int64 {
	switch f := f.(type) {
	case *MessageMediaDocument:
		if f.Document != nil {
			switch doc := f.Document.(type) {
			case *DocumentObj:
				return doc.Size
			default:
				return 0
			}
		}
		return 0
	case *MessageMediaPhoto:
		if photo, p := f.Photo.(*PhotoObj); p {
			if len(photo.Sizes) == 0 {
				return 0
			}
			s, _ := getPhotoSize(photo.Sizes[len(photo.Sizes)-1])
			return s
		} else {
			return 0
		}
	default:
		return 0
	}
}

// GetFileExt returns the file extension of Media
func GetFileExt(f any) string {
	switch f := f.(type) {
	case *MessageMediaDocument:
		if f.Document != nil {
			switch doc := f.Document.(type) {
			case *DocumentObj:
				if e := MimeTypes.Ext(doc.MimeType); e != "" {
					return e
				}
				for _, attr := range doc.Attributes {
					switch attr.(type) {
					case *DocumentAttributeAudio:
						return ".mp3"
					case *DocumentAttributeVideo:
						return ".mp4"
					case *DocumentAttributeAnimated:
						return ".gif"
					case *DocumentAttributeSticker:
						return ".webp"
					}
				}
			}
		}
	case *MessageMediaPhoto:
		return ".jpg"
	case *MessageMediaContact:
		return ".vcf"
	case *DocumentObj:
		for _, attr := range f.Attributes {
			switch attr := attr.(type) {
			case *DocumentAttributeFilename:
				return filepath.Ext(attr.FileName)
			case *DocumentAttributeAudio:
				return ".mp3"
			case *DocumentAttributeVideo:
				return ".mp4"
			case *DocumentAttributeAnimated:
				return ".gif"
			case *DocumentAttributeSticker:
				return ".webp"
			}
		}
		return ".file"
	case *PhotoObj:
		return ".jpg"
	case *InputPeerPhotoFileLocation:
		return ".jpg"
	}
	return ""
}

func GenerateRandomLong() int64 {
	return rand.Int64()
}

func getPeerUser(userID int64) *PeerUser {
	return &PeerUser{
		UserID: userID,
	}
}

func lp(loggerName string, sessionName string) string {
	if sessionName == "" {
		return fmt.Sprintf("[%s]", loggerName)
	}
	return fmt.Sprintf("[%s-%s]", loggerName, sessionName)
}

func IsURL(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func IsPhone(phone string) bool {
	return regexPhone.MatchString(phone)
}

func getValue[T comparable](val, def T) T {
	var zero T
	if val == zero {
		return def
	}
	return val
}

func getValueSlice[T any](val, def []T) []T {
	if val == nil {
		return def
	}
	return val
}

func getValueAny(val, def any) any {
	if val == nil {
		return def
	}
	return val
}

type Number interface {
	int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32 | uint64 | float32 | float64
}

func convertSlice[T2 Number, T1 Number](t1 []T1) []T2 {
	t2 := make([]T2, len(t1))
	for i := range t1 {
		t2[i] = T2(t1[i])
	}
	return t2
}

func parseInt32(a any) int32 {
	switch a := a.(type) {
	case int32:
		return a
	case int:
		return int32(a)
	case int64:
		return int32(a)
	default:
		return 0
	}
}

func SizetoHuman(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(size)/1024)
	} else if size < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(size)/1024/1024)
	} else {
		return fmt.Sprintf("%.2f GB", float64(size)/1024/1024/1024)
	}
}

// PackBotFileID packs a file object to a file-id, including file size (if available)
func PackBotFileID(file any) string {
	var (
		fID, accessHash int64
		fileType, dcID  int32
		fileSize        int64
	)

switchFileType:
	switch f := file.(type) {
	case *DocumentObj:
		fID = f.ID
		accessHash = f.AccessHash
		fileType = 5
		dcID = f.DcID
		fileSize = f.Size
		for _, attr := range f.Attributes {
			switch attr := attr.(type) {
			case *DocumentAttributeAudio:
				if attr.Voice {
					fileType = 3
					break switchFileType
				}
				fileType = 9
				break switchFileType
			case *DocumentAttributeVideo:
				if attr.RoundMessage {
					fileType = 13
					break switchFileType
				}
				fileType = 4
				break switchFileType
			case *DocumentAttributeSticker:
				fileType = 8
				break switchFileType
			case *DocumentAttributeAnimated:
				fileType = 10
				break switchFileType
			}
		}
	case *PhotoObj:
		fID = f.ID
		accessHash = f.AccessHash
		fileType = 2
		dcID = f.DcID
		fileSize = 0
	case *MessageMediaDocument:
		file = f.Document
		goto switchFileType
	case *MessageMediaPhoto:
		file = f.Photo
		goto switchFileType
	default:
		return ""
	}

	if fID == 0 || accessHash == 0 || fileType == 0 || dcID == 0 {
		return ""
	}

	if fileSize > 0 {
		buf := make([]byte, 4+8+8+8)
		binary.LittleEndian.PutUint32(buf[0:], uint32(fileType)|uint32(dcID)<<24)
		binary.LittleEndian.PutUint64(buf[4:], uint64(fID))
		binary.LittleEndian.PutUint64(buf[12:], uint64(accessHash))
		binary.LittleEndian.PutUint64(buf[20:], uint64(fileSize))
		return base64.RawURLEncoding.EncodeToString(buf)
	}

	buf := make([]byte, 4+8+8)
	binary.LittleEndian.PutUint32(buf[0:], uint32(fileType)|uint32(dcID)<<24)
	binary.LittleEndian.PutUint64(buf[4:], uint64(fID))
	binary.LittleEndian.PutUint64(buf[12:], uint64(accessHash))
	return base64.RawURLEncoding.EncodeToString(buf)
}

// UnpackBotFileID unpacks a file id to its components, including file size if present
func UnpackBotFileID(fileID string) (int64, int64, int32, int32, int64) {
	data, err := base64.RawURLEncoding.DecodeString(fileID)
	if err != nil {
		return 0, 0, 0, 0, 0
	}

	if len(data) == 28 {
		tmp := binary.LittleEndian.Uint32(data[0:])
		fileType := int32(tmp & 0x00FFFFFF)
		dcID := int32((tmp >> 24) & 0xFF)
		fID := int64(binary.LittleEndian.Uint64(data[4:]))
		accessHash := int64(binary.LittleEndian.Uint64(data[12:]))
		fileSize := int64(binary.LittleEndian.Uint64(data[20:]))
		return fID, accessHash, fileType, dcID, fileSize
	}

	if len(data) == 20 {
		tmp := binary.LittleEndian.Uint32(data[0:])
		fileType := int32(tmp & 0x00FFFFFF)
		dcID := int32((tmp >> 24) & 0xFF)
		fID := int64(binary.LittleEndian.Uint64(data[4:]))
		accessHash := int64(binary.LittleEndian.Uint64(data[12:]))
		return fID, accessHash, fileType, dcID, 0
	}

	if parts := strings.SplitN(string(data), "_", 5); len(parts) >= 4 {
		fileType, _ := strconv.Atoi(parts[0])
		dcID, _ := strconv.Atoi(parts[1])
		fID, _ := strconv.ParseInt(parts[2], 10, 64)
		accessHash, _ := strconv.ParseInt(parts[3], 10, 64)
		fileSize := int64(0)
		if len(parts) == 5 {
			fileSize, _ = strconv.ParseInt(parts[4], 10, 64)
		}
		return fID, accessHash, int32(fileType), int32(dcID), fileSize
	}

	return 0, 0, 0, 0, 0
}

// ResolveBotFileID resolves a file id to a MessageMedia object
func ResolveBotFileID(fileId string) (MessageMedia, error) {
	fID, accessHash, fileType, dcID, fileSize := UnpackBotFileID(fileId)
	if fID == 0 || accessHash == 0 || fileType == 0 || dcID == 0 {
		return nil, errors.New("failed to resolve file id: unrecognized format")
	}
	switch fileType {
	case 2:
		return &MessageMediaPhoto{
			Photo: &PhotoObj{
				ID:         fID,
				AccessHash: accessHash,
				DcID:       dcID,
			},
		}, nil
	case 3, 4, 5, 8, 9, 10, 13:
		var attributes = []DocumentAttribute{}
		switch fileType {
		case 3:
			attributes = append(attributes, &DocumentAttributeAudio{
				Voice: true,
			})
		case 4:
			attributes = append(attributes, &DocumentAttributeVideo{
				RoundMessage: false,
			})
		case 8:
			attributes = append(attributes, &DocumentAttributeSticker{})
		case 9:
			attributes = append(attributes, &DocumentAttributeAudio{})
		case 10:
			attributes = append(attributes, &DocumentAttributeAnimated{})
		case 13:
			attributes = append(attributes, &DocumentAttributeVideo{
				RoundMessage: true,
			})
		}
		return &MessageMediaDocument{
			Document: &DocumentObj{
				ID:         fID,
				AccessHash: accessHash,
				DcID:       dcID,
				Size:       fileSize,
				Attributes: attributes,
			},
		}, nil
	}
	return nil, errors.New("failed to resolve file id: unknown file type")
}

func IsFfmpegInstalled() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func MarshalWithTypeName(v any, noindent ...bool) string {
	rv := reflect.ValueOf(v)
	rt := reflect.TypeOf(v)

	if rt.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "{}"
		}
		rt = rt.Elem()
		rv = rv.Elem()
	}

	if rt.Kind() != reflect.Struct {
		return "{}"
	}

	typeName := rt.Name()

	result := map[string]any{
		typeName: buildRecursive(rv, false),
	}

	var b []byte
	var err error

	if getVariadic(noindent, false) {
		b, err = json.Marshal(result)
	} else {
		b, err = json.MarshalIndent(result, "", "  ")
	}
	if err != nil {
		return "{}"
	}

	return string(b)
}

func buildRecursive(v reflect.Value, _ bool) any {
	switch v.Kind() {

	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return buildRecursive(v.Elem(), false)

	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return buildRecursive(v.Elem(), false)

	case reflect.Struct:
		out := make(map[string]any)
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}

			name := f.Name

			out[name] = buildRecursive(v.Field(i), false)
		}
		return out

	case reflect.Slice, reflect.Array:
		arr := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			arr[i] = buildRecursive(v.Index(i), false)
		}
		return arr

	case reflect.Map:
		out := make(map[string]any)
		for _, key := range v.MapKeys() {
			strKey := fmt.Sprintf("%v", key.Interface())
			out[strKey] = buildRecursive(v.MapIndex(key), false)
		}
		return out

	default:
		return v.Interface()
	}
}
