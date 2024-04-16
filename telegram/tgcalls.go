// Copyright (c) 2024 RoseLoverX

package telegram

import "fmt"

type GroupCallMedia struct {
	Peer        Peer
	Started     bool
	CurrentFile string
}

func (c *Client) StartGroupCallMedia(peer interface{}) (*GroupCallMedia, error) {
	peerDialog, err := c.GetSendablePeer(peer)
	if err != nil {
		return nil, err
	}
	// Start the group call
	_, e := c.PhoneCreateGroupCall(&PhoneCreateGroupCallParams{
		Peer:     peerDialog,
		RandomID: int32(GenRandInt()),
	})
	if e != nil {
		return nil, e
	}
	// TODO : Check if the group call is already started
	// TODO : Implement this
	return &GroupCallMedia{}, nil
}

// TODO: after implementing latest Layer.

func (c *Client) GetGroupCall(chatId interface{}) (*InputGroupCall, error) {
	resolvedPeer, err := c.GetSendablePeer(chatId)
	if err != nil {
		return nil, err
	}

	if inPeer, ok := resolvedPeer.(*InputPeerChannel); !ok {
		return nil, fmt.Errorf("GetGroupCall: chatId is not a channel")
	} else {
		fullChatRaw, err := c.ChannelsGetFullChannel(
			&InputChannelObj{
				ChannelID:  inPeer.ChannelID,
				AccessHash: inPeer.AccessHash,
			},
		)

		if err != nil {
			return nil, err
		}

		if fullChatRaw == nil {
			return nil, fmt.Errorf("GetGroupCall: fullChatRaw is nil")
		}

		fullChat, ok := fullChatRaw.FullChat.(*ChannelFull)

		if !ok {
			return nil, fmt.Errorf("GetGroupCall: fullChatRaw.FullChat is not a ChannelFull")
		}

		if fullChat.Call == nil {
			return nil, fmt.Errorf("GetGroupCall: No active group call")
		}

		return fullChat.Call, nil
	}
}
