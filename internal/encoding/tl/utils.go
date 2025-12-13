// Copyright (c) 2025 @AmarnathCJD

package tl

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"reflect"
)

func padding4(size int) int {
	t := (-uint64(size)) & 3
	return int(t)
}

func haveFlag(v any) bool {
	typ := reflect.TypeOf(v)
	cachedTags := GetCachedTags(typ)
	for _, info := range cachedTags {
		if info != nil && !info.ignore {
			return true
		}
	}
	return false
}

func sliceToInterfaceSlice(in any) []any {
	if in == nil {
		return nil
	}

	ival := reflect.ValueOf(in)
	if ival.Type().Kind() != reflect.Slice {
		panic("not a slice: " + ival.Type().String())
	}

	res := make([]any, ival.Len())
	for i := 0; i < ival.Len(); i++ {
		res[i] = ival.Index(i).Interface()
	}

	return res
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

func BigIntBytes(v *big.Int, bitsize int) []byte {
	vbytes := v.Bytes()
	vbytesLen := len(vbytes)
	for i, b := range BitLengths {
		if b == bitsize {
			break
		}

		if i == len(BitLengths)-1 {
			panic(fmt.Errorf("bitsize not squaring by 2: bitsize %v", bitsize))
		}
	}

	offset := bitsize/8 - vbytesLen
	if offset < 0 {
		panic(fmt.Errorf("bitsize too small: have %v, want at least %v", bitsize, vbytes))
	}

	return append(make([]byte, offset), vbytes...)
}
