// Copyright (c) 2025 @AmarnathCJD

package tl

const (
	WordLen   = 4           // Size of a word in TL (32 bits)
	LongLen   = WordLen * 2 // int64 occupies 8 bytes
	DoubleLen = WordLen * 2 // float64 occupies 8 bytes
	Int128Len = WordLen * 4 // int128 occupies 16 bytes
	Int256Len = WordLen * 8 // int256 occupies 32 bytes

	// Magic Numbers
	MaxArrayElements = 0xfe // Maximum number of elements that can be encoded in an array

	MagicNumber = 0xfe // 254

	// https://core.telegram.org/schema/mtproto
	CrcVector uint32 = 0x1cb5c415
	CrcFalse  uint32 = 0xbc799737
	CrcTrue   uint32 = 0x997275b5
	CrcNull   uint32 = 0x56730bcc

	bitsInByte = 8 // Number of bits in a byte (for consistency)
)
