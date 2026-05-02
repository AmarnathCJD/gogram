// Copyright (c) 2025 @AmarnathCJD

package tl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"reflect"
	"sync"
)

const largeBufferThreshold = 128 * 1024 // 128KB
const maxVectorElements = 1 << 24

// LargeBytePool reuses large byte buffers (>= 128KB) to reduce heap
// allocations during file downloads. Callers that receive a large []byte
// from TL decoding should call ReleaseLargeBuffer when done with it.
var LargeBytePool = sync.Pool{
	New: func() any {
		b := make([]byte, 1024*1024) // 1MB default
		return &b
	},
}

// ReleaseLargeBuffer returns a large byte buffer to the pool for reuse.
// It is safe to call with any slice (small slices are ignored).
func ReleaseLargeBuffer(buf []byte) {
	if cap(buf) >= largeBufferThreshold {
		buf = buf[:cap(buf)]
		LargeBytePool.Put(&buf)
	}
}

// A Decoder reads and decodes TL values from an input stream.
type Decoder struct {
	buf *bytes.Reader
	err error

	// see Decoder.ExpectTypesInInterface description
	expectedTypes []reflect.Type
}

// NewDecoder returns a new decoder that reads from r.
//
// Note: The decoder cannot work with partial data. The entire input must be read before decoding can begin.
func NewDecoder(r io.Reader) (*Decoder, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading data before decoding: %w", err)
	}

	return &Decoder{buf: bytes.NewReader(data)}, nil
}

// NewDecoderBytes returns a new decoder that reads from b.
func NewDecoderBytes(b []byte) *Decoder {
	return &Decoder{buf: bytes.NewReader(b)}
}

// ExpectTypesInInterface defines how the decoder should parse implicit objects.
//
// How `expectedTypes` works:
//
// Imagine you want to parse a `[]int32`, but you might also receive `[]int64`, `SomeCustomType`,
// or even `[][]bool`. How do you handle this?
//
// `expectedTypes` stores your predictions. For example, if you encounter an unknown type, it
// will parse it as `int32` instead of `int64`.
//
// If you have predictions deeper than the first unknown type, you can tell the decoder to use those
// predicted values.
//
// So, next time you have a structure with an `any` field that expects to contain `[]float64`
// or similar, use this feature via `d.ExpectTypesInInterface()`.
func (d *Decoder) ExpectTypesInInterface(types ...reflect.Type) {
	d.expectedTypes = types
}

func (d *Decoder) read(buf []byte) {
	if d.err != nil {
		return
	}

	n, err := d.buf.Read(buf)
	if err != nil {
		d.unread(n)
		d.err = err
		return
	}

	if n != len(buf) {
		d.unread(n)
		d.err = fmt.Errorf("buffer weren't fully read: want %v bytes, got %v", len(buf), n)
		return
	}
}

func (d *Decoder) unread(count int) {
	for range count {
		if d.buf.UnreadByte() != nil {
			return
		}
	}
}

func (d *Decoder) PopLong() int64 {
	var val [LongLen]byte
	d.read(val[:])
	if d.err != nil {
		return 0
	}

	return int64(binary.LittleEndian.Uint64(val[:]))
}

func (d *Decoder) PopDouble() float64 {
	var val [DoubleLen]byte
	d.read(val[:])
	if d.err != nil {
		return 0
	}

	return math.Float64frombits(binary.LittleEndian.Uint64(val[:]))
}

func (d *Decoder) PopUint() uint32 {
	var val [WordLen]byte
	d.read(val[:])
	if d.err != nil {
		return 0
	}

	return binary.LittleEndian.Uint32(val[:])
}

func (d *Decoder) PopRawBytes(size int) []byte {
	if size < 0 {
		return nil
	}

	val := make([]byte, size)
	d.read(val)
	if d.err != nil {
		return nil
	}

	return val
}

func (d *Decoder) PopBool() bool {
	crc := d.PopUint()
	if d.err != nil {
		return false
	}

	switch crc {
	case CrcTrue:
		return true
	case CrcFalse:
		return false
	default:
		d.err = fmt.Errorf("not a bool value, actually: %#v", crc)
		return false
	}
}

func (d *Decoder) PopNull() {
	crc := d.PopUint()
	if d.err != nil {
		return
	}

	if crc != CrcNull {
		d.err = fmt.Errorf("not a null value, actually: %#v", crc)
		return
	}
}

func (d *Decoder) PopCRC() uint32 {
	return d.PopUint() // The CRC calculation appears to be in big-endian order, but it's not entirely clear.
}

func (d *Decoder) PopInt() int32 {
	return int32(d.PopUint())
}

func (d *Decoder) GetRestOfMessage() ([]byte, error) {
	return io.ReadAll(d.buf)
}

func (d *Decoder) DumpWithoutRead() ([]byte, error) {
	data, err := io.ReadAll(d.buf)
	if err != nil {
		return nil, err
	}

	d.unread(len(data))
	return data, nil
}

func (d *Decoder) PopVector(as reflect.Type) any {
	return d.popVector(as, false)
}

func (d *Decoder) popVector(as reflect.Type, ignoreCRC bool) any {
	if d.err != nil {
		return nil
	}
	if !ignoreCRC {
		crc := d.PopCRC()
		if d.err != nil {
			d.err = fmt.Errorf("read crc: %w", d.err)
			return nil
		}

		if crc != CrcVector {
			d.err = fmt.Errorf("not a vector: 0x%08x, want: 0x%08x", crc, CrcVector)
			return nil
		}
	}

	size := d.PopUint()
	if d.err != nil {
		d.err = fmt.Errorf("read vector size: %w", d.err)
		return nil
	}

	if size > maxVectorElements {
		d.err = fmt.Errorf("vector size %d exceeds max %d", size, maxVectorElements)
		return nil
	}

	if int(size) > d.buf.Len() {
		d.err = fmt.Errorf("vector size %d exceeds remaining input %d", size, d.buf.Len())
		return nil
	}

	x := reflect.MakeSlice(reflect.SliceOf(as), int(size), int(size))
	for i := 0; i < int(size); i++ {
		var val reflect.Value
		if as.Kind() == reflect.Pointer {
			val = reflect.New(as.Elem())
		} else {
			val = reflect.New(as).Elem()
		}

		d.decodeValue(val)
		if d.err != nil {
			return nil
		}

		x.Index(i).Set(val)
	}

	return x.Interface()
}

func (d *Decoder) PopMessage() []byte {
	var first [1]byte
	d.read(first[:])
	if d.err != nil {
		return nil
	}

	firstByte := first[0]

	var realSize int
	var lenNumberSize int

	if firstByte != MagicNumber {
		realSize = int(firstByte)
		lenNumberSize = 1
	} else {
		var sizeBuf [WordLen - 1]byte
		d.read(sizeBuf[:])
		if d.err != nil {
			d.err = fmt.Errorf("reading last %v bytes of message size: %w", WordLen-1, d.err)
			return nil
		}

		realSize = int(uint32(sizeBuf[0]) | uint32(sizeBuf[1])<<8 | uint32(sizeBuf[2])<<16)
		lenNumberSize = WordLen
	}
	
	if realSize > d.buf.Len() {
		d.err = fmt.Errorf("message size %d exceeds remaining input %d", realSize, d.buf.Len())
		return nil
	}

	var buf []byte
	var pooled *[]byte
	if realSize >= largeBufferThreshold {
		pooled = LargeBytePool.Get().(*[]byte)
		if cap(*pooled) < realSize {
			*pooled = make([]byte, realSize)
		}
		buf = (*pooled)[:realSize]
	} else {
		buf = make([]byte, realSize)
	}
	d.read(buf)
	if d.err != nil {
		d.err = fmt.Errorf("reading message data with len of %v: %w", realSize, d.err)
		if pooled != nil {
			LargeBytePool.Put(pooled)
		}
		return nil
	}

	readLen := lenNumberSize + realSize
	if readLen%WordLen != 0 {
		pad := WordLen - readLen%WordLen
		var voidBytes [WordLen - 1]byte
		d.read(voidBytes[:pad])
		if d.err != nil {
			d.err = fmt.Errorf("reading %v last void bytes: %w", pad, d.err)
			return nil
		}

		for _, b := range voidBytes[:pad] {
			if b != 0 {
				d.err = fmt.Errorf("some of void bytes doesn't equal zero: %#v", voidBytes[:pad])
				return nil
			}
		}
	}

	return buf
}
