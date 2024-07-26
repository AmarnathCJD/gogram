// Copyright (c) 2024 RoseLoverX

package tl

const (
	WordLen   = 4
	LongLen   = WordLen * 2 // int64
	DoubleLen = WordLen * 2 // float64
	Int128Len = WordLen * 4 // int128
	Int256Len = WordLen * 8 // int256

	MagicNumber = 0xfe // 254

	// https://core.telegram.org/schema/mtproto
	CrcVector uint32 = 0x1cb5c415
	CrcFalse  uint32 = 0xbc799737
	CrcTrue   uint32 = 0x997275b5
	CrcNull   uint32 = 0x56730bcc

	bitsInByte = 8 // cause we don't want store magic numbers
)
