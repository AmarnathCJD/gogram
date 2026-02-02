// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"time"

	"github.com/pkg/errors"
)

type InlineSendOptions struct {
	Gallery      bool   `json:"gallery,omitempty"`
	NextOffset   string `json:"next_offset,omitempty"`
	CacheTime    int32  `json:"cache_time,omitempty"`
	Private      bool   `json:"private,omitempty"`
	SwitchPm     string `json:"switch_pm,omitempty"`
	SwitchPmText string `json:"switch_pm_text,omitempty"`
}

func (c *Client) AnswerInlineQuery(QueryID int64, Results []InputBotInlineResult, Options ...*InlineSendOptions) (bool, error) {
	options := getVariadic(Options, &InlineSendOptions{})
	options.CacheTime = getValue(options.CacheTime, 60)
	request := &MessagesSetInlineBotResultsParams{
		Gallery:    options.Gallery,
		Private:    options.Private,
		QueryID:    QueryID,
		Results:    Results,
		CacheTime:  options.CacheTime,
		NextOffset: options.NextOffset,
	}
	if options.SwitchPm != "" {
		request.SwitchPm = &InlineBotSwitchPm{
			Text:       options.SwitchPm,
			StartParam: getValue(options.SwitchPmText, "start"),
		}
	}
	resp, err := c.MessagesSetInlineBotResults(request)
	if err != nil {
		return false, err
	}
	return resp, nil
}

type CallbackOptions struct {
	Alert     bool   `json:"alert,omitempty"`
	CacheTime int32  `json:"cache_time,omitempty"`
	URL       string `json:"url,omitempty"`
}

func (c *Client) AnswerCallbackQuery(QueryID int64, Text string, Opts ...*CallbackOptions) (bool, error) {
	options := getVariadic(Opts, &CallbackOptions{})
	request := &MessagesSetBotCallbackAnswerParams{
		QueryID: QueryID,
		Message: Text,
		Alert:   options.Alert,
	}
	if options.URL != "" {
		request.URL = options.URL
	}
	if options.CacheTime != 0 {
		request.CacheTime = options.CacheTime
	}
	resp, err := c.MessagesSetBotCallbackAnswer(request)
	if err != nil {
		return false, err
	}
	return resp, nil
}

// BOT COMMANDS

func (c *Client) SetBotCommands(commands []*BotCommand, scope *BotCommandScope, languageCode ...string) (bool, error) {
	resp, err := c.BotsSetBotCommands(*scope, getVariadic(languageCode, "en"), commands)
	if err != nil {
		return false, err
	}
	return resp, nil
}

func (c *Client) SetBotDefaultPrivileges(privileges *ChatAdminRights, ForChannels ...bool) (resp bool, err error) {
	forCh := getVariadic(ForChannels, true)
	if forCh {
		resp, err = c.BotsSetBotBroadcastDefaultAdminRights(privileges)
		return
	}
	resp, err = c.BotsSetBotGroupDefaultAdminRights(privileges)
	return
}

func (c *Client) SetChatMenuButton(userID int64, button *BotMenuButton) (bool, error) {
	peer, err := c.ResolvePeer(userID)
	if err != nil {
		return false, err
	}
	peerUser, ok := peer.(*InputPeerUser)
	if !ok {
		return false, errors.New("invalid user")
	}
	resp, err := c.BotsSetBotMenuButton(&InputUserObj{AccessHash: peerUser.AccessHash, UserID: peerUser.UserID}, *button)
	if err != nil {
		return false, err
	}
	return resp, nil
}

// In testing stage, TODO
// returns list of users and chats in chans
func (c *Client) Broadcast() (chan User, chan Chat, error) {
	s, err := c.UpdatesGetState()
	if err != nil {
		return nil, nil, err
	}

	endPts := s.Pts

	var users = make(map[int64]User)
	var chats = make(map[int64]Chat)

	var userChan = make(chan User, 100)
	var chatChan = make(chan Chat, 100)

	req := &UpdatesGetDifferenceParams{
		Pts:           1,
		Date:          1,
		PtsLimit:      5000,
		PtsTotalLimit: 2147483647, // max int32
		Qts:           1,
	}

	go func() {
		defer close(userChan)
		defer close(chatChan)

		for req.Pts < endPts {
			updates, err := c.UpdatesGetDifference(req)
			if err != nil {
				if handleIfFlood(err, c) {
					continue
				}
				c.Logger.Error(err)
				return
			}

			switch u := updates.(type) {
			case *UpdatesDifferenceObj:
				for _, user := range u.Users {
					switch uz := user.(type) {
					case *UserObj:
						if _, ok := users[uz.ID]; !ok {
							userChan <- uz
						}
						users[uz.ID] = uz
					}
				}
				for _, chat := range u.Chats {
					switch cz := chat.(type) {
					case *ChatObj:
						if _, ok := chats[cz.ID]; !ok {
							chatChan <- cz
						}
						chats[cz.ID] = cz
					case *Channel:
						if _, ok := chats[cz.ID]; !ok {
							chatChan <- cz
						}
						chats[cz.ID] = cz
					}
				}

				req.Pts = u.State.Pts
				req.Qts = u.State.Qts
				req.Date = u.State.Date
			case *UpdatesDifferenceSlice:
				for _, user := range u.Users {
					switch uz := user.(type) {
					case *UserObj:
						if _, ok := users[uz.ID]; !ok {
							userChan <- uz
						}
						users[uz.ID] = uz
					}
				}
				for _, chat := range u.Chats {
					switch cz := chat.(type) {
					case *ChatObj:
						if _, ok := chats[cz.ID]; !ok {
							chatChan <- cz
						}
						chats[cz.ID] = cz
					case *Channel:
						if _, ok := chats[cz.ID]; !ok {
							chatChan <- cz
						}
						chats[cz.ID] = cz
					}
				}

				req.Pts = u.IntermediateState.Pts
				req.Qts = u.IntermediateState.Qts
				req.Date = u.IntermediateState.Date
			case *UpdatesDifferenceEmpty:
				break
			case *UpdatesDifferenceTooLong:
				endPts = u.Pts
			}

			time.Sleep(150 * time.Millisecond)
		}
	}()

	return userChan, chatChan, nil
}
