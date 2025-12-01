// Copyright (c) 2025 @AmarnathCJD

package tl

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

type Encoder struct {
	w io.Writer
	// this error is last unsuccessful write into w. if this err != nil,
	// write() method will not write anything more.
	err error
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func (e *Encoder) write(b []byte) {
	if e.err != nil {
		return
	}

	n, err := e.w.Write(b)
	if err != nil {
		e.err = err
		return
	}

	if n != len(b) {
		e.err = &ErrorPartialWrite{Has: n, Want: len(b)}
	}
}

// CheckErr must be called after encoding has been finished. If this function returns a non-nil value,
// the encoding has failed, and the resulting data should not be used.
func (e *Encoder) CheckErr() error {
	return e.err
}

// PutBool is a very specific type. Since there are separate constructors for true and false,
// they can be considered as two CRC constants.
func (e *Encoder) PutBool(v bool) {
	crc := CrcFalse
	if v {
		crc = CrcTrue
	}

	e.PutUint(uint32(crc))
}

func (e *Encoder) putUint8(v uint8) {
	tmp := [1]byte{v}
	e.write(tmp[:])
}

func (e *Encoder) PutUint(v uint32) {
	buf := make([]byte, WordLen)
	binary.LittleEndian.PutUint32(buf, v)
	e.write(buf)
}

// PutCRC is an alias for Encoder.PutUint. It is used for better code readability and self-documentation.
func (e *Encoder) PutCRC(v uint32) {
	e.PutUint(v) // The CRC calculation order is unclear, but it is not necessarily big-endian.
}

func (e *Encoder) PutInt(v int32) {
	e.PutUint(uint32(v))
}

func (e *Encoder) PutLong(v int64) {
	buf := make([]byte, LongLen)
	binary.LittleEndian.PutUint64(buf, uint64(v))
	e.write(buf)
}

func (e *Encoder) PutDouble(v float64) {
	buf := make([]byte, DoubleLen)
	binary.LittleEndian.PutUint64(buf, math.Float64bits(v))
	e.write(buf)
}

func (e *Encoder) PutMessage(b []byte) {
	size := len(b)
	pad := 0

	maxLen := 1 << 24 // 3 bytes 24 bits, the first one is 0xfe, the remaining 3 times the length
	if size > maxLen {
		e.err = fmt.Errorf("message entity too large: expect less than %v, got %v", maxLen, size)
		return
	}

	switch {
	case size == 0: // optimization for empty messages
		e.PutUint(0)
		return
	case size < MagicNumber:
		pad = padding4(size + 1)
		e.putUint8(uint8(size))
	default:
		pad = padding4(size)
		e.PutUint(uint32(size)<<8 | MagicNumber)
	}

	e.write(b)

	if pad > 0 {
		var zero [4]byte
		e.write(zero[:pad])
	}
}

func (e *Encoder) PutString(msg string) {
	e.PutMessage([]byte(msg))
}

func (e *Encoder) PutRawBytes(b []byte) {
	e.write(b)
}

func (e *Encoder) PutVector(v any) {
	e.encodeVector(sliceToInterfaceSlice(v)...)
}
