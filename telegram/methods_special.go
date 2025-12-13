// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"fmt"

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
		return nil, fmt.Errorf("sending InitConnection: %w", err)
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
		return nil, fmt.Errorf("sending InvokeWithLayer: %w", err)
	}

	return data.(tl.Object), nil
}

type InvokeWithoutUpdatesParams struct {
	Query tl.Object
}

func (*InvokeWithoutUpdatesParams) CRC() uint32 {
	return 0xbf9459b7
}

func (c *Client) InvokeWithoutUpdates(query tl.Object) (tl.Object, error) {
	data, err := c.MakeRequest(&InvokeWithoutUpdatesParams{
		Query: query,
	})
	if err != nil {
		return nil, fmt.Errorf("sending InvokeWithoutUpdates: %w", err)
	}
	return data.(tl.Object), nil
}

type InvokeWithMessagesRangeParams struct {
	Range MessageRange
	Query tl.Object
}

func (*InvokeWithMessagesRangeParams) CRC() uint32 {
	return 0x365275f2
}

func (c *Client) InvokeWithMessagesRange(r MessageRange, query tl.Object) (tl.Object, error) {
	data, err := c.MakeRequest(&InvokeWithMessagesRangeParams{
		Range: r,
		Query: query,
	})
	if err != nil {
		return nil, fmt.Errorf("sending InvokeWithMessagesRange: %w", err)
	}
	return data.(tl.Object), nil
}

type InvokeWithTakeoutParams struct {
	TakeoutID int64
	Query     tl.Object
}

func (*InvokeWithTakeoutParams) CRC() uint32 {
	return 0xaca9fd2e
}

func (c *Client) InvokeWithTakeout(takeoutID int, query tl.Object) (tl.Object, error) {
	data, err := c.MakeRequest(&InvokeWithTakeoutParams{
		TakeoutID: int64(takeoutID),
		Query:     query,
	})
	if err != nil {
		return nil, fmt.Errorf("sending InvokeWithLayer: %w", err)
	}

	return data.(tl.Object), nil
}
