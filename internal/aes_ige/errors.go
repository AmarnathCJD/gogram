// Copyright (c) 2025 @AmarnathCJD

package ige

import "errors"

var (
	ErrDataTooSmall     = errors.New("AES256IGE: data too small")
	ErrDataNotDivisible = errors.New("AES256IGE: data not divisible by block size")
	ErrKeySize          = errors.New("AES256IGE: invalid key size: must be 256 bits")
	ErrMsgKeySize       = errors.New("AES256IGE: invalid msg_key size: must be 16 bytes")
)
