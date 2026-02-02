// Copyright (c) 2024 RoseLoverX

package tl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"reflect"

	"github.com/pkg/errors"
)

// A Decoder reads and decodes TL values from an input stream.
type Decoder struct {
	buf *bytes.Reader
	err error

	// see Decoder.ExpectTypesInInterface description
	expectedTypes []reflect.Type
}

// NewDecoder returns a new decoder that reads from r.
// Unfortunately, decoder can't work with part of data, so reader must be read all before decoding.
func NewDecoder(r io.Reader) (*Decoder, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, errors.Wrap(err, "reading data before decoding")
	}

	return &Decoder{buf: bytes.NewReader(data)}, nil
}

// ExpectTypesInInterface defines, how decoder must parse implicit objects.
// how does expectedTypes works:
// So, imagine we have a struct with a field of type interface{}.
// It can be possibly any time like []int32, []int64, etc.
// Here we can define, what types we expect in this interface.
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
	for i := 0; i < count; i++ {
		if d.buf.UnreadByte() != nil {
			return
		}
	}
}

func (d *Decoder) PopLong() int64 {
	val := make([]byte, LongLen)
	d.read(val)
	if d.err != nil {
		return 0
	}

	return int64(binary.LittleEndian.Uint64(val))
}

func (d *Decoder) PopDouble() float64 {
	val := make([]byte, DoubleLen)
	d.read(val)
	if d.err != nil {
		return 0
	}

	return math.Float64frombits(binary.LittleEndian.Uint64(val))
}

func (d *Decoder) PopUint() uint32 {
	val := make([]byte, WordLen)
	d.read(val)
	if d.err != nil {
		return 0
	}

	return binary.LittleEndian.Uint32(val)
}

func (d *Decoder) PopRawBytes(size int) []byte {
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
	return d.PopUint()
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
			d.err = errors.Wrap(d.err, "read crc")
			return nil
		}

		if crc != CrcVector {
			d.err = fmt.Errorf("not a vector: 0x%08x, want: 0x%08x", crc, CrcVector)
			return nil
		}
	}

	size := d.PopUint()
	if d.err != nil {
		d.err = errors.Wrap(d.err, "read vector size")
		return nil
	}

	x := reflect.MakeSlice(reflect.SliceOf(as), int(size), int(size))
	for i := 0; i < int(size); i++ {
		var val reflect.Value
		if as.Kind() == reflect.Ptr {
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
	val := []byte{0}

	d.read(val)
	if d.err != nil {
		return nil
	}

	firstByte := val[0]

	var realSize int
	var lenNumberSize int

	if firstByte != MagicNumber {
		realSize = int(firstByte)
		lenNumberSize = 1
	} else {

		val = make([]byte, WordLen-1)
		d.read(val)
		if d.err != nil {
			d.err = errors.Wrapf(d.err, "reading last %v bytes of message size", WordLen-1)
			return nil
		}

		val = append(val, 0x0)

		realSize = int(binary.LittleEndian.Uint32(val))
		lenNumberSize = WordLen
	}

	buf := make([]byte, realSize)
	d.read(buf)
	if d.err != nil {
		d.err = errors.Wrapf(d.err, "reading message data with len of %v", realSize)
		return nil
	}

	readLen := lenNumberSize + realSize
	if readLen%WordLen != 0 {
		voidBytes := make([]byte, WordLen-readLen%WordLen)
		d.read(voidBytes)
		if d.err != nil {
			d.err = errors.Wrapf(d.err, "reading %v last void bytes", WordLen-readLen%WordLen)
			return nil
		}

		for _, b := range voidBytes {
			if b != 0 {
				d.err = fmt.Errorf("some of void bytes doesn't equal zero: %#v", voidBytes)
				return nil
			}
		}
	}

	return buf
}
