package utils

import (
	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

var (
	ApiVersion = 145
)

type PingParams struct {
	PingID int64
}

func (*PingParams) CRC() uint32 {
	return 0x7abe77ec
}

type InvokeWithLayerParams struct {
	Layer int32
	Query tl.Object
}

func (*InvokeWithLayerParams) CRC() uint32 {
	return 0xda9b0d0d
}

type InitConnectionParams struct {
	ApiID          int32     // Application identifier (see. App configuration)
	DeviceModel    string    // Device model
	SystemVersion  string    // Operation system version
	AppVersion     string    // Application version
	SystemLangCode string    // Code for the language used on the device's OS, ISO 639-1 standard
	LangPack       string    // Language pack to use
	LangCode       string    // Code for the language used on the client, ISO 639-1 standard
	Query          tl.Object // The query itself
}

func (*InitConnectionParams) CRC() uint32 {
	return 0xc1cd5ea9 //nolint:gomnd not magic
}

type HelpGetConfigParams struct{}

func (*HelpGetConfigParams) CRC() uint32 {
	return 0xc4f9186b
}

func (*InitConnectionParams) FlagIndex() int {
	return 0
}

type AuthExportAuthorizationParams struct {
	DcID int32
}

func (*AuthExportAuthorizationParams) CRC() uint32 {
	return 0xe5bfffcd
}

func GetDCID(ip, port string) int {
	return 0
}
