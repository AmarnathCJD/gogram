// Copyright (c) 2022 RoseLoverX

package tl_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

func TestEquality(t *testing.T) {
	tests := []struct {
		name string
		obj  any
		fill any
	}{
		{
			name: "MessagesChatsObj",
			obj: &MultipleChats{
				Chats: []any{
					&Chat{
						Creator:           false,
						Kicked:            false,
						Left:              false,
						Deactivated:       true,
						ID:                123,
						Title:             "abcdef",
						Photo:             "pikcha.png",
						ParticipantsCount: 123,
						Date:              1,
						Version:           1,
						AdminRights: &Rights{
							DeleteMessages: true,
							BanUsers:       true,
						},
						BannedRights: &Rights{
							DeleteMessages: false,
							BanUsers:       false,
						},
					},
				},
			},
			fill: &MultipleChats{},
		},
	}

	for _, tt := range tests {
		encoded, err := tl.Marshal(tt.obj)
		if !assert.NoError(t, err) {
			return
		}

		err = tl.Decode(encoded, tt.fill)
		if !assert.NoError(t, err) {
			return
		}

		assert.Equal(t, tt.obj, tt.fill)
	}
}
