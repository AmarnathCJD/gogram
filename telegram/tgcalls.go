// Copyright (c) 2024 RoseLoverX

package telegram

import "fmt"

func (c *Client) StartGroupCallMedia(peer any) (PhoneCall, error) {
	peerDialog, err := c.ResolvePeer(peer)
	if err != nil {
		return nil, err
	}
	// Start the group call
	updates, e := c.PhoneCreateGroupCall(&PhoneCreateGroupCallParams{
		Peer:     peerDialog,
		RandomID: int32(GenRandInt()),
	})
	if e != nil {
		return nil, e
	}

	switch updates.(type) {
	case *UpdatesObj:
		for _, update := range updates.(*UpdatesObj).Updates {
			switch update := update.(type) {
			case *UpdatePhoneCall:
				return update.PhoneCall, nil
			}
		}
	}

	return nil, fmt.Errorf("StartGroupCallMedia: failed to start group call")
}

func (c *Client) GetGroupCall(chatId any) (*InputGroupCall, error) {
	resolvedPeer, err := c.ResolvePeer(chatId)
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
