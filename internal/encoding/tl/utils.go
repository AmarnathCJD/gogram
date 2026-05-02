// Copyright (c) 2025 @AmarnathCJD

package tl

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"slices"
)

func padding4(size int) int {
	t := (-uint64(size)) & 3
	return int(t)
}

func RandomBytes(size int) []byte {
	b := make([]byte, size)
	_, _ = rand.Read(b)
	return b
}

var BitLengths = []int{
	1 << 3,  // 8
	1 << 4,  // 16
	1 << 5,  // 32
	1 << 6,  // 64
	1 << 7,  // 128
	1 << 8,  // 256
	1 << 9,  // 512
	1 << 10, // 1024
	1 << 11, // 2048
}

func BigIntBytes(v *big.Int, bitsize int) ([]byte, error) {
	vbytes := v.Bytes()
	vbytesLen := len(vbytes)

	known := slices.Contains(BitLengths, bitsize)
	if !known {
		return nil, fmt.Errorf("BigIntBytes: bitsize not a power of two in 8..2048: %d", bitsize)
	}

	offset := bitsize/8 - vbytesLen
	if offset < 0 {
		return nil, fmt.Errorf("BigIntBytes: value too large for bitsize: have %d bytes, max %d", vbytesLen, bitsize/8)
	}

	return append(make([]byte, offset), vbytes...), nil
}
