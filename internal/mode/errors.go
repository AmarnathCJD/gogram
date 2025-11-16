// Copyright (c) 2024 RoseLoverX

package mode

import (
	"fmt"

	"errors"
)

var (
	ErrInterfaceIsNil        = errors.New("interface is nil")
	ErrModeNotSupported      = errors.New("mode is not supported")
	ErrAmbiguousModeAnnounce = errors.New("ambiguous mode announce, expected other byte sequence")
)

type ErrNotMultiple struct {
	Len int
}

func (e ErrNotMultiple) Error() string {
	msg := "size of message not multiple of 4"
	if e.Len != 0 {
		return fmt.Sprintf(msg+" (got %v)", e.Len)
	}
	return msg
}
