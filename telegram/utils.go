// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/amarnathcjd/gogram/internal/utils"
	"github.com/pkg/errors"
)

type mimeTypeManager struct {
	mimeTypes map[string]string
}

// type ExportedAuthParams struct {
// 	ID    int64
// 	Bytes []byte
// }

func (m *mimeTypeManager) addMime(ext, mime string) {
	m.mimeTypes[ext] = mime
}

func (m *mimeTypeManager) match(filePath string) string {
	if IsURL(filePath) {
		// do a get request with timeout and get the content type
		req, err := http.NewRequest("GET", filePath, nil)
		if err != nil {
			return ""
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; rv:60.0) Gecko/20100101 Firefox/60.0")
		HTTPClient := &http.Client{
			Timeout: 4 * time.Second,
		}

		resp, err := HTTPClient.Do(req)
		if err != nil {
			return ""
		}

		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			if resp.Header.Get("Content-Type") != "" {
				return resp.Header.Get("Content-Type")
			}
		}

		return ""
	}

	if mime, ok := m.mimeTypes[filepath.Ext(filePath)]; ok {
		return mime
	}

	// use http.DetectContentType if no match
	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}

	defer file.Close()
	buffer := make([]byte, 512)
	_, err = file.Read(buffer)

	if err != nil {
		return ""
	}

	return http.DetectContentType(buffer)
}

func (m *mimeTypeManager) Ext(mime string) string {
	for ext, m := range m.mimeTypes {
		if m == mime {
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

var MimeTypes = &mimeTypeManager{
	mimeTypes: make(map[string]string),
}

func init() {
	MimeTypes.addMime(".png", "image/png")
	MimeTypes.addMime(".jpg", "image/jpeg")
	MimeTypes.addMime(".jpeg", "image/jpeg")

	MimeTypes.addMime(".webp", "image/webp")
	MimeTypes.addMime(".gif", "image/gif")
	MimeTypes.addMime(".bmp", "image/bmp")
	MimeTypes.addMime(".tga", "image/x-tga")
	MimeTypes.addMime(".tiff", "image/tiff")
	MimeTypes.addMime(".psd", "image/vnd.adobe.photoshop")

	MimeTypes.addMime(".mp4", "video/mp4")
	MimeTypes.addMime(".mov", "video/quicktime")
	MimeTypes.addMime(".avi", "video/avi")
	MimeTypes.addMime(".flv", "video/x-flv")
	MimeTypes.addMime(".m4v", "video/x-m4v")
	MimeTypes.addMime(".mkv", "video/x-matroska")
	MimeTypes.addMime(".webm", "video/webm")
	MimeTypes.addMime(".3gp", "video/3gpp")

	MimeTypes.addMime(".mp3", "audio/mpeg")
	MimeTypes.addMime(".m4a", "audio/m4a")
	MimeTypes.addMime(".aac", "audio/aac")
	MimeTypes.addMime(".ogg", "audio/ogg")
	MimeTypes.addMime(".flac", "audio/x-flac")
	MimeTypes.addMime(".opus", "audio/opus")
	MimeTypes.addMime(".wav", "audio/wav")
	MimeTypes.addMime(".alac", "audio/x-alac")

	MimeTypes.addMime(".tgs", "application/x-tgsticker")
}

var (
	regexDataCenter = regexp.MustCompile(`DC (\d+)`)
	regexCode       = regexp.MustCompile(`code (\d+)`)
)

func GetErrorCode(err error) (int, int) {
	datacenter := 0
	code := 0
	if err == nil {
		return datacenter, code
	}

	if regexDataCenter.MatchString(err.Error()) {
		datacenter, _ = strconv.Atoi(regexDataCenter.FindStringSubmatch(err.Error())[1])
	}
	if regexCode.MatchString(err.Error()) {
		code, _ = strconv.Atoi(regexCode.FindStringSubmatch(err.Error())[1])
	}

	return datacenter, code
}

var (
	regexFloodWait        = regexp.MustCompile(`A wait of (\d+) seconds is required`)
	regexFloodWaitBasic   = regexp.MustCompile(`FLOOD_WAIT_(\d+)`)
	regexFloodWaitPremium = regexp.MustCompile(`FLOOD_PREMIUM_WAIT_(\d+)`)
)

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
	ThumbOnly bool
	ThumbSize PhotoSize
	Video     bool
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

// Func to get the file name of Media
//
//	Accepted types:
//	 *MessageMedia
//	 *Document
//	 *Photo
func GetFileName(f any, video ...bool) string {
	var isVid = getVariadic(video, false)

	getDocName := func(doc *DocumentObj) string {
		var name string
		for _, attr := range doc.Attributes {
			switch attr := attr.(type) {
			case *DocumentAttributeFilename:
				if attr.FileName != "" {
					return attr.FileName
				}
			case *DocumentAttributeAudio:
				if attr.Title != "" {
					name = attr.Title + ".mp3"
				}
			case *DocumentAttributeVideo:
				name = fmt.Sprintf("video_%s_%d.mp4", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
			case *DocumentAttributeAnimated:
				name = fmt.Sprintf("animation_%s_%d.gif", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
			case *DocumentAttributeSticker:
				return fmt.Sprintf("sticker_%s_%d.webp", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
			}
		}
		if name != "" {
			return name
		}

		if doc.MimeType != "" {
			return fmt.Sprintf("file_%s_%d%s", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000), MimeTypes.Ext(doc.MimeType))
		}
		return fmt.Sprintf("file_%s_%d", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
	}

	switch f := f.(type) {
	case *MessageMediaDocument:
		return getDocName(f.Document.(*DocumentObj))
	case *MessageMediaPhoto:
		if isVid {
			return fmt.Sprintf("video_%s_%d.mp4", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
		}
		return fmt.Sprintf("photo_%s_%d.jpg", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
	case *MessageMediaContact:
		return fmt.Sprintf("contact_%s_%d.vcf", f.FirstName, rand.Intn(1000))
	case *DocumentObj:
		return getDocName(f)
	case *PhotoObj:
		if isVid {
			return fmt.Sprintf("video_%s_%d.mp4", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
		}
		return fmt.Sprintf("photo_%s_%d.jpg", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
	case *InputPeerPhotoFileLocation:
		return fmt.Sprintf("profile_photo_%s_%d.jpg", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
	case *InputPhotoFileLocation:
		return fmt.Sprintf("photo_file_%s_%d.jpg", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
	default:
		return fmt.Sprintf("file_%s_%d", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
	}
}

// Func to get the file size of Media
//
//	Accepted types:
//	 *MessageMedia
func getFileSize(f any) int64 {
	switch f := f.(type) {
	case *MessageMediaDocument:
		return f.Document.(*DocumentObj).Size
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

// Func to get the file extension of Media
//
//	Accepted types:
//	 *MessageMedia
//	 *Document
//	 *Photo
func getFileExt(f any) string {
	switch f := f.(type) {
	case *MessageMediaDocument:
		doc := f.Document.(*DocumentObj)
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
	return int64(rand.Int31())<<32 | int64(rand.Int31())
}

func getPeerUser(userID int64) *PeerUser {
	return &PeerUser{
		UserID: userID,
	}
}

func getLogPrefix(loggerName string, sessionName string) string {
	if sessionName == "" {
		return fmt.Sprintf("[%s]", loggerName)
	}
	return fmt.Sprintf("[%s] {%s}", loggerName, sessionName)
}

func IsURL(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

var regexPhone = regexp.MustCompile(`^\+?[0-9]{10,13}$`)

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

func getInlineDocumentType(mimeType string, voiceNote bool) string {
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
	case "video/mp4", "video/x-matroksa", "video/webm":
		return "video"
	default:
		return "file"
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

// PackBotFileID packs a file object to a file-id
func PackBotFileID(file any) string {
	var (
		fID, accessHash int64
		fileType, dcID  int32
		fileSize        int64 = -1
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

	bufSize := 4 + 8 + 8
	hasSize := fileSize >= 0
	if hasSize {
		bufSize += 8
	}

	buf := make([]byte, bufSize)
	binary.LittleEndian.PutUint32(buf[0:], uint32(fileType)|uint32(dcID)<<24)
	binary.LittleEndian.PutUint64(buf[4:], uint64(fID))
	binary.LittleEndian.PutUint64(buf[4+8:], uint64(accessHash))

	// Only include file size if it's known (>= 0)
	if hasSize {
		binary.LittleEndian.PutUint64(buf[4+8+8:], uint64(fileSize))
	}

	return base64.RawURLEncoding.EncodeToString(buf)
}

// UnpackBotFileID unpacks a file id to its components
func UnpackBotFileID(fileID string) (int64, int64, int32, int32, int64) {
	data, err := base64.RawURLEncoding.DecodeString(fileID)
	if err != nil {
		return 0, 0, 0, 0, 0
	}

	if len(data) == 4+8+8+8 {
		tmp := binary.LittleEndian.Uint32(data[0:])
		fileType := int32(tmp & 0x00FFFFFF)
		dcID := int32((tmp >> 24) & 0xFF)
		fID := int64(binary.LittleEndian.Uint64(data[4:]))
		accessHash := int64(binary.LittleEndian.Uint64(data[4+8:]))
		fileSize := int64(binary.LittleEndian.Uint64(data[4+8+8:]))
		return fID, accessHash, fileType, dcID, fileSize
	}

	if len(data) == 4+8+8 {
		tmp := binary.LittleEndian.Uint32(data[0:])
		fileType := int32(tmp & 0x00FFFFFF)
		dcID := int32((tmp >> 24) & 0xFF)
		fID := int64(binary.LittleEndian.Uint64(data[4:]))
		accessHash := int64(binary.LittleEndian.Uint64(data[4+8:]))
		return fID, accessHash, fileType, dcID, -1
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
		var attributes []DocumentAttribute
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

func doesSessionFileExist(filePath string) bool {
	_, ol := os.Stat(filePath)
	return !os.IsNotExist(ol)
}

func IsFfmpegInstalled() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func NewLogger(level utils.LogLevel, prefix ...string) *utils.Logger {
	return utils.NewLogger(getVariadic(prefix, "gogram")).SetLevel(level)
}
