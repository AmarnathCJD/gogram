// Copyright (c) 2024 RoseLoverX

package ige

import "errors"

var (
	ErrDataTooSmall     = errors.New("AES256IGE: data too small")
	ErrDataNotDivisible = errors.New("AES256IGE: data not divisible by block size")
)
