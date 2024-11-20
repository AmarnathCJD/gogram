// Copyright (c) 2024 RoseLoverX

package mode

import (
	"fmt"
)

var (
	ErrInterfaceIsNil        = fmt.Errorf("interface is nil")
	ErrModeNotSupported      = fmt.Errorf("mode is not supported")
	ErrAmbiguousModeAnnounce = fmt.Errorf("ambiguous mode announce, expected other byte sequence")
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
