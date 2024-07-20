package utils_test

import (
	"testing"

	"github.com/amarnathcjd/gogram/internal/utils"
)

func TestMsgIdGenerator(t *testing.T) {
	generateMsgId := utils.NewMsgIDGenerator()

	// Generate a set of msg_ids to test
	msgIds := make([]int64, 10000)
	for i := 0; i < 10000; i++ {
		msgIds[i] = generateMsgId(0)
	}

	// Check that all msg_ids are divisible by 4
	for i, msgId := range msgIds {
		if msgId%4 != 0 {
			t.Errorf("msgId at index %d is not divisible by 4: %d", i, msgId)
		}
	}

	// Check that msg_ids are increasing
	for i := 1; i < len(msgIds); i++ {
		if msgIds[i] <= msgIds[i-1] {
			t.Errorf("msgId at index %d is not greater than the previous one: %d <= %d", i, msgIds[i], msgIds[i-1])
		}
	}

	// Check that there are no duplicate msg_ids
	msgIdSet := make(map[int64]struct{})
	for i, msgId := range msgIds {
		if _, exists := msgIdSet[msgId]; exists {
			t.Errorf("Duplicate msgId found at index %d: %d", i, msgId)
		}
		msgIdSet[msgId] = struct{}{}
	}
}
