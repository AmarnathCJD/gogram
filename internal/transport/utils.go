// Copyright (c) 2024 RoseLoverX

package transport

import (
	"fmt"
	"strings"
)

type ErrNotMultiple struct {
	Len int
}

func (e *ErrNotMultiple) Error() string {
	msg := "size of message not multiple of 4"
	if e.Len != 0 {
		return fmt.Sprintf(msg+" (got %v)", e.Len)
	}
	return msg
}

func formatIPv6WithPort(ipv6WithPort string) (string, error) {
	lastColon := strings.LastIndex(ipv6WithPort, ":")
	if lastColon == -1 {
		return "", fmt.Errorf("invalid IPv6 address with port")
	}

	address := ipv6WithPort[:lastColon]
	port := ipv6WithPort[lastColon+1:]

	return fmt.Sprintf("[%s]:%s", address, port), nil
}
