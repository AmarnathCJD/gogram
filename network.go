// Copyright (c) 2022 RoseLoverX

package mtproto

import (
	"fmt"
	"reflect"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/mtproto/messages"
	"github.com/amarnathcjd/gogram/internal/mtproto/objects"
	"github.com/amarnathcjd/gogram/internal/utils"
)

var NetworkLogger = NewLogger("Network - ")

func (m *MTProto) sendPacket(request tl.Object, expectedTypes ...reflect.Type) (chan tl.Object, error) {
	msg, err := tl.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding message: %w", err)
	}

	var (
		data  messages.Common
		msgID = utils.GenerateMessageId()
	)

	// adding types for parser if required
	if len(expectedTypes) > 0 {
		m.expectedTypes.Add(int(msgID), expectedTypes)
	}

	// dealing with response channel
	resp := m.getRespChannel()
	if isNullableResponse(request) {
		go func() { resp <- &objects.Null{} }() // goroutine cuz we don't read from it RIGHT NOW
	} else {
		m.responseChannels.Add(int(msgID), resp)
	}

	if m.encrypted {
		data = &messages.Encrypted{
			Msg:         msg,
			MsgID:       msgID,
			AuthKeyHash: m.authKeyHash,
		}
	} else {
		data = &messages.Unencrypted{ //nolint: errcheck нешифрованое не отправляет ошибки
			Msg:   msg,
			MsgID: msgID,
		}
	}

	// must write synchroniously, cuz seqno must be upper each request
	m.seqNoMutex.Lock()
	defer m.seqNoMutex.Unlock()

	err = m.transport.WriteMsg(data, MessageRequireToAck(request))
	if err != nil {
		NetworkLogger.Printf("error writing message: %s", err.Error())
		return nil, fmt.Errorf("writing message: %w", err)
	}

	if m.encrypted {
		m.seqNo += 2
	}

	return resp, nil
}

func (m *MTProto) writeRPCResponse(msgID int, data tl.Object) error {
	v, ok := m.responseChannels.Get(msgID)
	if !ok {
		NetworkLogger.Printf("no response channel for message %d", msgID)
		return fmt.Errorf("no response channel for message %d", msgID)
	}

	v <- data

	m.responseChannels.Delete(msgID)
	m.expectedTypes.Delete(msgID)
	return nil
}

func (m *MTProto) getRespChannel() chan tl.Object {
	if m.serviceModeActivated {
		return m.serviceChannel
	}
	return make(chan tl.Object)
}

func isNullableResponse(t tl.Object) bool {
	switch t.(type) {
	case *objects.Pong, *objects.MsgsAck:
		return true
	default:
		return false
	}
}
