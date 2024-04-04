// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"encoding/base64"
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

	"github.com/pkg/errors"
)

type mimeTypeManager struct {
	mimeTypes map[string]string
}

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

var mimeTypes = &mimeTypeManager{}

func init() {
	mimeTypes.addMime(".png", "image/png")
	mimeTypes.addMime(".jpg", "image/jpeg")
	mimeTypes.addMime(".jpeg", "image/jpeg")

	mimeTypes.addMime(".webp", "image/webp")
	mimeTypes.addMime(".gif", "image/gif")
	mimeTypes.addMime(".bmp", "image/bmp")
	mimeTypes.addMime(".tga", "image/x-tga")
	mimeTypes.addMime(".tiff", "image/tiff")
	mimeTypes.addMime(".psd", "image/vnd.adobe.photoshop")

	mimeTypes.addMime(".mp4", "video/mp4")
	mimeTypes.addMime(".mov", "video/quicktime")
	mimeTypes.addMime(".avi", "video/avi")
	mimeTypes.addMime(".flv", "video/x-flv")
	mimeTypes.addMime(".m4v", "video/x-m4v")
	mimeTypes.addMime(".mkv", "video/x-matroska")
	mimeTypes.addMime(".webm", "video/webm")
	mimeTypes.addMime(".3gp", "video/3gpp")

	mimeTypes.addMime(".mp3", "audio/mpeg")
	mimeTypes.addMime(".m4a", "audio/m4a")
	mimeTypes.addMime(".aac", "audio/aac")
	mimeTypes.addMime(".ogg", "audio/ogg")
	mimeTypes.addMime(".flac", "audio/x-flac")
	mimeTypes.addMime(".opus", "audio/opus")
	mimeTypes.addMime(".wav", "audio/wav")
	mimeTypes.addMime(".alac", "audio/x-alac")

	mimeTypes.addMime(".tgs", "application/x-tgsticker")
}

func getErrorCode(err error) (int, int) {
	datacenter := 0
	code := 0
	if err != nil {
		if re := regexp.MustCompile(`DC (\d+)`); re.MatchString(err.Error()) {
			datacenter, _ = strconv.Atoi(re.FindStringSubmatch(err.Error())[1])
		}
		if re := regexp.MustCompile(`code (\d+)`); re.MatchString(err.Error()) {
			code, _ = strconv.Atoi(re.FindStringSubmatch(err.Error())[1])
		}
	}
	return datacenter, code
}

func matchError(err error, str string) bool {
	if err != nil {
		return strings.Contains(err.Error(), str)
	}
	return false
}

// GetFileLocation returns file location, datacenter, file size and file name
func GetFileLocation(file interface{}) (InputFileLocation, int32, int64, string, error) {
	var (
		location   interface{}
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
		return &InputDocumentFileLocation{
			ID:            l.ID,
			AccessHash:    l.AccessHash,
			FileReference: l.FileReference,
			ThumbSize:     "",
		}, l.DcID, l.Size, getFileName(l), nil
	case *PhotoObj:
		size, sizeType := getPhotoSize(l.Sizes[len(l.Sizes)-1])
		return &InputPhotoFileLocation{
			ID:            l.ID,
			AccessHash:    l.AccessHash,
			FileReference: l.FileReference,
			ThumbSize:     sizeType,
		}, l.DcID, size, getFileName(l), nil
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

func getMax(a []int32) int32 {
	if len(a) == 0 {
		return 0
	}
	var max = a[0]
	for _, v := range a {
		if v > max {
			max = v
		}
	}
	return max
}

func pathIsDir(path string) bool {
	return strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\")
}

// Func to get the file name of Media
//
//	Accepted types:
//	 *MessageMedia
//	 *Document
//	 *Photo
func getFileName(f interface{}) string {
	getDocName := func(doc *DocumentObj) string {
		for _, attr := range doc.Attributes {
			switch attr := attr.(type) {
			case *DocumentAttributeFilename:
				return attr.FileName
			case *DocumentAttributeAudio:
				return attr.Title + ".mp3"
			case *DocumentAttributeVideo:
				return fmt.Sprintf("video_%s_%d.mp4", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
			case *DocumentAttributeAnimated:
				return fmt.Sprintf("animation_%s_%d.gif", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
			case *DocumentAttributeSticker:
				return fmt.Sprintf("sticker_%s_%d.webp", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
			}
		}
		if doc.MimeType != "" {
			return fmt.Sprintf("file_%s_%d%s", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000), mimeTypes.Ext(doc.MimeType))
		}
		return fmt.Sprintf("file_%s_%d", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
	}

	switch f := f.(type) {
	case *MessageMediaDocument:
		return getDocName(f.Document.(*DocumentObj))
	case *MessageMediaPhoto:
		return fmt.Sprintf("photo_%s_%d.jpg", time.Now().Format("2006-01-02_15-04-05"), rand.Intn(1000))
	case *MessageMediaContact:
		return fmt.Sprintf("contact_%s_%d.vcf", f.FirstName, rand.Intn(1000))
	case *DocumentObj:
		return getDocName(f)
	case *PhotoObj:
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
func getFileSize(f interface{}) int64 {
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
func getFileExt(f interface{}) string {
	switch f := f.(type) {
	case *MessageMediaDocument:
		doc := f.Document.(*DocumentObj)
		if e := mimeTypes.Ext(doc.MimeType); e != "" {
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

func IsURL(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func IsPhone(phone string) bool {
	phoneRe := regexp.MustCompile(`^\+?[0-9]{10,13}$`)
	return phoneRe.MatchString(phone)
}

func getValue(val, def interface{}) interface{} {
	switch v := val.(type) {
	case string:
		if v == "" {
			return def
		}
	case int, int32, int64:
		if v == 0 {
			return def
		}
	default:
		if v == nil {
			return def
		}
	}
	return val
}

// Inverse operation of ResolveBotFileID
// https://core.telegram.org/bots/api#file
//
//	Accepted Types:
//		*MessageMedia
//		*Document
//		*Photo
func PackBotFileID(file interface{}) string {
	var fileID, accessHash, fileType, dcID string
switchFileType:
	switch f := file.(type) {
	case *DocumentObj:
		fileID = strconv.FormatInt(f.ID, 10)
		accessHash = strconv.FormatInt(f.AccessHash, 10)
		fileType = "5"
		dcID = fmt.Sprintf("%d", f.DcID)
		for _, attr := range f.Attributes {
			switch attr := attr.(type) {
			case *DocumentAttributeAudio:
				if attr.Voice {
					fileType = "3"
					break switchFileType
				}
				fileType = "9"
				break switchFileType
			case *DocumentAttributeVideo:
				if attr.RoundMessage {
					fileType = "13"
					break switchFileType
				}
				fileType = "4"
				break switchFileType
			case *DocumentAttributeSticker:
				fileType = "8"
				break switchFileType
			case *DocumentAttributeAnimated:
				fileType = "10"
				break switchFileType
			}
		}
	case *PhotoObj:
		fileID = strconv.FormatInt(f.ID, 10)
		accessHash = strconv.FormatInt(f.AccessHash, 10)
		fileType = "2"
		dcID = fmt.Sprintf("%d", f.DcID)
	case *MessageMediaDocument:
		file = f.Document
		goto switchFileType
	case *MessageMediaPhoto:
		file = f.Photo
		goto switchFileType
	}
	if fileID == "" || accessHash == "" || fileType == "" || dcID == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s_%s_%s_%s", fileType, dcID, fileID, accessHash)))
}

func UnpackBotFileID(fileID string) (int64, int64, int32, int32) {
	data, err := base64.RawURLEncoding.DecodeString(fileID)
	if err != nil {
		return 0, 0, 0, 0
	}
	parts := strings.Split(string(data), "_")
	if len(parts) != 4 {
		return 0, 0, 0, 0
	}
	fileType, _ := strconv.Atoi(parts[0])
	dcID, _ := strconv.Atoi(parts[1])
	fID, _ := strconv.ParseInt(parts[2], 10, 64)
	accessHash, _ := strconv.ParseInt(parts[3], 10, 64)
	return fID, accessHash, int32(fileType), int32(dcID)
}

// Inverse operation of PackBotFileID,
// https://core.telegram.org/bots/api#file
//
//	Accepted Types:
//		string
func ResolveBotFileID(fileID string) (MessageMedia, error) {
	fID, accessHash, fileType, dcID := UnpackBotFileID(fileID)
	if fID == 0 || accessHash == 0 || fileType == 0 || dcID == 0 {
		return nil, errors.New("invalid file id")
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
				Attributes: attributes,
			},
		}, nil
	}
	return nil, errors.New("invalid file type")
}

func doesSessionFileExist(filePath string) bool {
	_, ol := os.Stat(filePath)
	return !os.IsNotExist(ol)
}

func IsFfmpegInstalled() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}
