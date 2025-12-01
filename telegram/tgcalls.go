// Copyright (c) 2025 @AmarnathCJD

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

		return &fullChat.Call, nil
	}
}

type GroupCallStream struct {
	Channels        []*GroupCallStreamChannel
	call            *InputGroupCall
	currentTs       int64
	selectedChannel int32
	scale           int32
	client          *Client
	dcId            int32
}

func (s *GroupCallStream) SetTS(ts int64) {
	s.currentTs = ts
}

func (s *GroupCallStream) GetTS() int64 {
	return s.currentTs
}

func (s *GroupCallStream) GetScale() int32 {
	return s.scale
}

func (s *GroupCallStream) SetScale(scale int32) {
	s.scale = scale
}

func (s *GroupCallStream) GetChannel() *GroupCallStreamChannel {
	if s.selectedChannel < 0 || s.selectedChannel >= int32(len(s.Channels)) {
		return nil
	}
	return s.Channels[s.selectedChannel]
}

func (s *GroupCallStream) SetChannel(channel int32) {
	if channel < 0 || channel >= int32(len(s.Channels)) {
		return
	}
	s.selectedChannel = channel
	s.scale = s.Channels[channel].Scale
	s.currentTs = s.Channels[channel].LastTimestampMs
}

func (s *GroupCallStream) NextChunk() ([]byte, error) {
	if s.selectedChannel < 0 || s.selectedChannel >= int32(len(s.Channels)) {
		return nil, fmt.Errorf("GetGroupCallStream: selected channel is out of range")
	}

	channel := s.GetChannel()
	if channel == nil {
		return nil, fmt.Errorf("GetGroupCallStream: channel is nil")
	}

	input := &InputGroupCallStream{
		Call:         *s.call,
		TimeMs:       s.currentTs,
		Scale:        s.scale,
		VideoChannel: channel.Channel,
		VideoQuality: 2,
	}

	fi, err := s.client.UploadGetFile(&UploadGetFileParams{
		Location: input,
		Offset:   0,
		Limit:    512 * 1024,
	})

	if err != nil {
		fmt.Printf("GetGroupCallStream: UploadGetFile error: %v\n", err)
		return nil, err
	}

	if fi == nil {
		return nil, fmt.Errorf("GetGroupCallStream: file info is nil")
	}

	switch fi := fi.(type) {
	case *UploadFileObj:
		s.currentTs += 1000 >> s.scale
		return fi.Bytes, nil
	case *UploadFileCdnRedirect:
		return nil, fmt.Errorf("GetGroupCallStream: CDN redirect error")
	}

	return nil, fmt.Errorf("GetGroupCallStream: unknown file info type")
}

func (c *Client) GetGroupCallStream(chatId any) (*GroupCallStream, error) {
	call, err := c.GetGroupCall(chatId)
	if err != nil {
		return nil, err
	}

	stream, err := c.PhoneGetGroupCallStreamChannels(*call)
	if err != nil {
		return nil, err
	}

	return &GroupCallStream{
		Channels: stream.Channels,
		scale:    stream.Channels[0].Scale,
		call:     call,
		client:   c,
		dcId:     int32(c.GetDC()),
	}, nil
}
