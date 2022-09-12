package telegram

import (
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type (
	Mime struct {
		Extension string
		Mime      string
	}
)

var (
	MimeTypes = []Mime{
		{".3gp", "video/3gpp"}, {".7z", "application/x-7z-compressed"}, {".aac", "audio/x-aac"},
		{".abw", "application/x-abiword"}, {".arc", "application/x-freearc"}, {".avi", "video/x-msvideo"},
		{".azw", "application/vnd.amazon.ebook"}, {".bin", "application/octet-stream"}, {".bmp", "image/bmp"},
		{".bz", "application/x-bzip"}, {".bz2", "application/x-bzip2"}, {".csh", "application/x-csh"},
		{".css", "text/css"}, {".csv", "text/csv"}, {".doc", "application/msword"},
		{".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}, {".eot", "application/vnd.ms-fontobject"}, {".epub", "application/epub+zip"},
		{".gz", "application/gzip"}, {".gif", "image/gif"}, {".htm", "text/html"},
		{".html", "text/html"}, {".ico", "image/vnd.microsoft.icon"}, {".ics", "text/calendar"},
		{".jar", "application/java-archive"}, {".jpeg", "image/jpeg"}, {".jpg", "image/jpeg"},
		{".js", "text/javascript"}, {".json", "application/json"}, {".jsonld", "application/ld+json"},
		{".mid", "audio/midi audio/x-midi"}, {".midi", "audio/midi audio/x-midi"}, {".mjs", "text/javascript"},
		{".mp3", "audio/mpeg"}, {".mpeg", "video/mpeg"}, {".mpkg", "application/vnd.apple.installer+xml"},
		{".odp", "application/vnd.oasis.opendocument.presentation"}, {".ods", "application/vnd.oasis.opendocument.spreadsheet"}, {".odt", "application/vnd.oasis.opendocument.text"},
		{".oga", "audio/ogg"}, {".ogv", "video/ogg"}, {".ogx", "application/ogg"},
		{".opus", "audio/opus"}, {".otf", "font/otf"}, {".png", "image/png"},
		{".pdf", "application/pdf"}, {".php", "application/x-httpd-php"}, {".ppt", "application/vnd.ms-powerpoint"},
		{".pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{".rar", "application/vnd.rar"}, {".rtf", "application/rtf"}, {".sh", "application/x-sh"},
		{".svg", "image/svg+xml"}, {".swf", "application/x-shockwave-flash"}, {".tar", "application/x-tar"},
		{".tif", "image/tiff"}, {".tiff", "image/tiff"}, {".ts", "video/mp2t"},
		{".ttf", "font/ttf"}, {".txt", "text/plain"}, {".vsd", "application/vnd.visio"},
		{".wav", "audio/wav"}, {".weba", "audio/webm"}, {".webm", "video/webm"},
		{".webp", "image/webp"}, {".woff", "font/woff"}, {".woff2", "font/woff2"},
		{".xhtml", "application/xhtml+xml"}, {".xls", "application/vnd.ms-excel"}, {".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{".xml", "application/xml"}, {".xul", "application/vnd.mozilla.xul+xml"}, {".zip", "application/zip"},
		{".3gp", "video/3gpp"}, {".3g2", "video/3gpp2"}, {".7z", "application/x-7z-compressed"},
	}
)

func resolveMimeType(filePath string) (string, bool) {
	file, err := os.Open(filePath)
	if err != nil {
		if matchMimeType := matchMimeType(filePath); matchMimeType != "" {
			return matchMimeType, mimeIsPhoto(matchMimeType)
		}
	}
	defer file.Close()
	buffer := make([]byte, 512)
	_, err = file.Read(buffer)
	if err != nil {
		return "", false
	}
	mime := http.DetectContentType(buffer)
	return mime, mimeIsPhoto(mime)
}

func matchMimeType(filePath string) string {
	for _, mt := range MimeTypes {
		if strings.HasSuffix(filePath, mt.Extension) {
			return mt.Mime
		}
	}
	return ""
}

func mimeIsPhoto(mime string) bool {
	return strings.HasPrefix(mime, "image/") && !strings.Contains(mime, "image/webp")
}

func GetInputFileLocation(file interface{}) (InputFileLocation, int32, int64) {
	var location interface{}

MediaMessageSwitch:
	switch f := file.(type) {
	case *MessageMediaDocument:
		location = f.Document
	case *MessageMediaPhoto:
		location = f.Photo
	case *NewMessage:
		file = f.Media()
		goto MediaMessageSwitch
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
	case *InputPeerPhotoFileLocation:
		return f, 0, 0
	default:
		return nil, 0, 0
	}
	switch l := location.(type) {
	case *DocumentObj:
		return &InputDocumentFileLocation{
			ID:            l.ID,
			AccessHash:    l.AccessHash,
			FileReference: l.FileReference,
			ThumbSize:     "",
		}, l.DcID, l.Size
	case *PhotoObj:
		size, sizeType := photoSizeByteCount(l.Sizes[len(l.Sizes)-1])
		return &InputPhotoFileLocation{
			ID:            l.ID,
			AccessHash:    l.AccessHash,
			FileReference: l.FileReference,
			ThumbSize:     sizeType,
		}, l.DcID, size

	default:
		return nil, 0, 0
	}
}

func photoSizeByteCount(sizes PhotoSize) (int64, string) {
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

func isPathDirectoryLike(path string) bool {
	return strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\")
}

func getFileName(f interface{}) string {
	switch f := f.(type) {
	case *MessageMediaDocument:
		for _, attr := range f.Document.(*DocumentObj).Attributes {
			switch attr := attr.(type) {
			case *DocumentAttributeFilename:
				return attr.FileName
			}
		}
	case *MessageMediaPhoto:
		return "photo.jpg"
	}
	return "download"
}

func GenerateRandomLong() int64 {
	return int64(rand.Int31())<<32 | int64(rand.Int31())
}

func getFileStat(filePath string) (int64, string) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, ""
	}
	defer file.Close()
	fileInfo, _ := file.Stat()
	return fileInfo.Size(), fileInfo.Name()
}

func getAppropriatedPartSize(fileSize int64) int {
	if fileSize < 104857600 {
		return 128 * 1024
	} else if fileSize < 786432000 {
		return 256 * 1024
	} else if fileSize < 2097152000 {
		return 256 * 1024
	}
	return 0
}

func getPeerUser(userID int64) *PeerUser {
	return &PeerUser{
		UserID: userID,
	}
}

// Fix this
func GetOption(Type interface{}, options []interface{}) interface{} {
	if len(options) == 0 {
		return Type
	}
	return options[0]
}

func IsUrl(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func IsPhone(phone string) bool {
	phoneRe := regexp.MustCompile(`^\+?[0-9]{10,13}$`)
	return phoneRe.MatchString(phone)
}

func getValue(val interface{}, def interface{}) interface{} {
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
