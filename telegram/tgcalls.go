// Copyright (c) 2025 @AmarnathCJD

package telegram

import (
	"errors"
	"fmt"
	"time"
)

func (c *Client) StartGroupCallMedia(peer any, rtmp ...bool) (PhoneCall, error) {
	peerDialog, err := c.ResolvePeer(peer)
	if err != nil {
		return nil, err
	}

	var useRtmp bool
	if len(rtmp) > 0 {
		useRtmp = rtmp[0]
	}
	// Start the group call
	updates, e := c.PhoneCreateGroupCall(&PhoneCreateGroupCallParams{
		Peer:       peerDialog,
		RandomID:   int32(GenRandInt()),
		RtmpStream: useRtmp,
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

	return nil, fmt.Errorf("failed to start group call media")
}

func (c *Client) GetGroupCall(chatId any) (*InputGroupCall, error) {
	resolvedPeer, err := c.ResolvePeer(chatId)
	if err != nil {
		return nil, err
	}

	if inPeer, ok := resolvedPeer.(*InputPeerChannel); !ok {
		return nil, fmt.Errorf("resolved peer is not a channel, but %T", resolvedPeer)
	} else {
		fullchannel, err := c.ChannelsGetFullChannel(
			&InputChannelObj{
				ChannelID:  inPeer.ChannelID,
				AccessHash: inPeer.AccessHash,
			},
		)

		if err != nil {
			return nil, err
		}

		switch channel := fullchannel.FullChat.(type) {
		case *ChannelFull:
			if channel.Call == nil {
				return nil, fmt.Errorf("no active group call in the channel")
			}
			return &channel.Call, nil
		case *ChatFullObj:
			if channel.Call == nil {
				return nil, fmt.Errorf("no active group call in the chat")
			}
			return &channel.Call, nil
		default:
			return nil, fmt.Errorf("failed to get full channel info")
		}
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
		return nil, fmt.Errorf("selected channel index out of range")
	}

	channel := s.GetChannel()
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
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
		return nil, err
	}

	if fi == nil {
		return nil, fmt.Errorf("file info is nil")
	}

	switch fi := fi.(type) {
	case *UploadFileObj:
		s.currentTs += 1000 >> s.scale
		return fi.Bytes, nil
	case *UploadFileCdnRedirect:
		return nil, fmt.Errorf("CDN redirects not supported yet")
	}

	return nil, fmt.Errorf("unknown file info type")
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


type StartGroupCallOptions struct {
	Title        string
	ScheduleDate time.Time
	RTMP         bool
}

func (c *Client) StartGroupCall(peer any, opts ...*StartGroupCallOptions) (InputGroupCall, error) {
	opt := getVariadic(opts, &StartGroupCallOptions{})
	resolved, err := c.ResolvePeer(peer)
	if err != nil {
		return nil, err
	}
	params := &PhoneCreateGroupCallParams{
		Peer:       resolved,
		RandomID:   int32(GenRandInt()),
		Title:      opt.Title,
		RtmpStream: opt.RTMP,
	}
	if !opt.ScheduleDate.IsZero() {
		params.ScheduleDate = int32(opt.ScheduleDate.Unix())
	}
	upd, err := c.PhoneCreateGroupCall(params)
	if err != nil {
		return nil, err
	}
	if call := extractGroupCall(upd); call != nil {
		return call, nil
	}
	return nil, errors.New("no GroupCall in response")
}

func extractGroupCall(upd Updates) InputGroupCall {
	scan := func(updates []Update) InputGroupCall {
		for _, u := range updates {
			if g, ok := u.(*UpdateGroupCall); ok {
				if call, ok := g.Call.(*GroupCallObj); ok {
					return &InputGroupCallObj{ID: call.ID, AccessHash: call.AccessHash}
				}
			}
		}
		return nil
	}
	switch v := upd.(type) {
	case *UpdatesObj:
		return scan(v.Updates)
	case *UpdateShort:
		return scan([]Update{v.Update})
	}
	return nil
}

func (c *Client) DiscardGroupCall(call InputGroupCall) error {
	if call == nil {
		return errors.New("call is nil")
	}
	_, err := c.PhoneDiscardGroupCall(call)
	return err
}

func (c *Client) EditGroupCallTitle(call InputGroupCall, title string) error {
	if call == nil {
		return errors.New("call is nil")
	}
	_, err := c.PhoneEditGroupCallTitle(call, title)
	return err
}

func (c *Client) StartScheduledGroupCall(call InputGroupCall) error {
	if call == nil {
		return errors.New("call is nil")
	}
	_, err := c.PhoneStartScheduledGroupCall(call)
	return err
}

func (c *Client) ExportGroupCallInvite(call InputGroupCall, canSelfUnmute bool) (string, error) {
	if call == nil {
		return "", errors.New("call is nil")
	}
	resp, err := c.PhoneExportGroupCallInvite(canSelfUnmute, call)
	if err != nil {
		return "", err
	}
	return resp.Link, nil
}

func (c *Client) InviteToGroupCall(call InputGroupCall, users ...any) error {
	if call == nil {
		return errors.New("call is nil")
	}
	if len(users) == 0 {
		return errors.New("at least one user required")
	}
	inputs := make([]InputUser, 0, len(users))
	for _, u := range users {
		peer, err := c.ResolvePeer(u)
		if err != nil {
			return fmt.Errorf("resolve user: %w", err)
		}
		inputs = append(inputs, toInputUser(peer))
	}
	_, err := c.PhoneInviteToGroupCall(call, inputs)
	return err
}

func toInputUser(p InputPeer) InputUser {
	switch v := p.(type) {
	case *InputPeerUser:
		return &InputUserObj{UserID: v.UserID, AccessHash: v.AccessHash}
	case *InputPeerSelf:
		return &InputUserSelf{}
	}
	return &InputUserEmpty{}
}

type GroupCallParticipantPatch struct {
	Muted              *bool
	RaiseHand          *bool
	VideoStopped       *bool
	VideoPaused        *bool
	PresentationPaused *bool
	Volume             *int32
}

func (c *Client) EditGroupCallParticipant(call InputGroupCall, participant any, change *GroupCallParticipantPatch) error {
	if call == nil {
		return errors.New("call is nil")
	}
	if change == nil {
		return errors.New("change required")
	}
	p, err := c.ResolvePeer(participant)
	if err != nil {
		return err
	}
	params := &PhoneEditGroupCallParticipantParams{
		Call:        call,
		Participant: p,
	}
	if change.Muted != nil {
		params.Muted = *change.Muted
	}
	if change.RaiseHand != nil {
		params.RaiseHand = *change.RaiseHand
	}
	if change.VideoStopped != nil {
		params.VideoStopped = *change.VideoStopped
	}
	if change.VideoPaused != nil {
		params.VideoPaused = *change.VideoPaused
	}
	if change.PresentationPaused != nil {
		params.PresentationPaused = *change.PresentationPaused
	}
	if change.Volume != nil {
		params.Volume = *change.Volume
	}
	_, err = c.PhoneEditGroupCallParticipant(params)
	return err
}

func (c *Client) MuteParticipant(call InputGroupCall, participant any) error {
	muted := true
	return c.EditGroupCallParticipant(call, participant, &GroupCallParticipantPatch{Muted: &muted})
}

func (c *Client) UnmuteParticipant(call InputGroupCall, participant any) error {
	muted := false
	return c.EditGroupCallParticipant(call, participant, &GroupCallParticipantPatch{Muted: &muted})
}

func (c *Client) RaiseHand(call InputGroupCall, participant any, raised bool) error {
	return c.EditGroupCallParticipant(call, participant, &GroupCallParticipantPatch{RaiseHand: &raised})
}

func (c *Client) GetGroupCallParticipants(call InputGroupCall, limit int32) ([]GroupCallParticipant, error) {
	if call == nil {
		return nil, errors.New("call is nil")
	}
	if limit <= 0 {
		limit = 100
	}
	resp, err := c.PhoneGetGroupParticipants(call, nil, nil, "", limit)
	if err != nil {
		return nil, err
	}
	out := make([]GroupCallParticipant, len(resp.Participants))
	for i, p := range resp.Participants {
		out[i] = *p
	}
	return out, nil
}

func (c *Client) ToggleGroupCallRecord(call InputGroupCall, start bool, title string, video, videoPortrait bool) error {
	if call == nil {
		return errors.New("call is nil")
	}
	_, err := c.PhoneToggleGroupCallRecord(&PhoneToggleGroupCallRecordParams{
		Call:          call,
		Start:         start,
		Video:         video,
		Title:         title,
		VideoPortrait: videoPortrait,
	})
	return err
}

func (c *Client) SetDefaultSendAs(call InputGroupCall, sendAs any) error {
	if call == nil {
		return errors.New("call is nil")
	}
	p, err := c.ResolvePeer(sendAs)
	if err != nil {
		return err
	}
	_, err = c.PhoneSaveDefaultSendAs(call, p)
	return err
}

func (c *Client) LeaveGroupCall(call InputGroupCall, source int32) error {
	if call == nil {
		return errors.New("call is nil")
	}
	_, err := c.PhoneLeaveGroupCall(call, source)
	return err
}
