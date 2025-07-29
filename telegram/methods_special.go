// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"github.com/pkg/errors"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

//invokeAfterMsg#cb9f372d {X:Type} msg_id:long query:!X = X;
//invokeAfterMsgs#3dc4b4f0 {X:Type} msg_ids:Vector<long> query:!X = X;

type InitConnectionParams struct {
	ApiID          int32             // Application identifier (see. App configuration)
	DeviceModel    string            // Device model
	SystemVersion  string            // Operation system version
	AppVersion     string            // Application version
	SystemLangCode string            // Code for the language used on the device's OS, ISO 639-1 standard
	LangPack       string            // Language pack to use
	LangCode       string            // Code for the language used on the client, ISO 639-1 standard
	Proxy          *InputClientProxy `tl:"flag:0"` // Info about an MTProto proxy
	Params         JsonValue         `tl:"flag:1"` // Additional initConnection parameters. For now, only the tz_offset field is supported, for specifying timezone offset in seconds.
	Query          tl.Object         // The query itself
}

func (*InitConnectionParams) CRC() uint32 {
	return 0xc1cd5ea9
}

func (*InitConnectionParams) FlagIndex() int {
	return 0
}

func (c *Client) InitConnection(params *InitConnectionParams) (tl.Object, error) {
	data, err := c.MakeRequest(params)
	if err != nil {
		return nil, errors.Wrap(err, "sending InitConnection")
	}

	return data.(tl.Object), nil
}

type InvokeWithLayerParams struct {
	Layer int32
	Query tl.Object
}

func (*InvokeWithLayerParams) CRC() uint32 {
	return 0xda9b0d0d
}

func (c *Client) InvokeWithLayer(layer int, query tl.Object) (tl.Object, error) {
	data, err := c.MakeRequest(&InvokeWithLayerParams{
		Layer: int32(layer),
		Query: query,
	})
	if err != nil {
		return nil, errors.Wrap(err, "sending InvokeWithLayer")
	}

	return data.(tl.Object), nil
}

//invokeWithoutUpdates#bf9459b7 {X:Type} query:!X = X;
//invokeWithMessagesRange#365275f2 {X:Type} range:MessageRange query:!X = X;

type InvokeWithTakeoutParams struct {
	TakeoutID int64
	Query     tl.Object
}

func (*InvokeWithTakeoutParams) CRC() uint32 {
	return 0xda9b0d0d
}

func (c *Client) InvokeWithTakeout(takeoutID int, query tl.Object) (tl.Object, error) {
	data, err := c.MakeRequest(&InvokeWithTakeoutParams{
		TakeoutID: int64(takeoutID),
		Query:     query,
	})
	if err != nil {
		return nil, errors.Wrap(err, "sending InvokeWithLayer")
	}

	return data.(tl.Object), nil
}
