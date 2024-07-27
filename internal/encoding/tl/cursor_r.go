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
	buf []byte
	pos int

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

	return &Decoder{buf: data}, nil
}

// ExpectTypesInInterface defines, how decoder must parse implicit objects.
// how does expectedTypes works:
// So, imagine: you want parse []int32, but also you can get []int64, or SomeCustomType, or even [][]bool.
// How to deal it?
// expectedTypes store your predictions (like "if you got unknown type, parse it as int32, not int64")
// also, if you have predictions deeper than first unknown type, you can say decoder to use predicted vals
//
// So, next time, when you'll have strucre object with interface{} which expect contains []float64 or sort
// of — use this feature via d.ExpectTypesInInterface()
func (d *Decoder) ExpectTypesInInterface(types ...reflect.Type) {
	d.expectedTypes = types
}

func (d *Decoder) read2(n int) []byte {
	if d.err != nil {
		return nil
	}

	m := len(d.buf) - d.pos
	if n > m {
		d.err = fmt.Errorf("buffer weren't fully read: want %v bytes, got %v", n, m)
		return nil
	}
	data := d.buf[d.pos : d.pos+n]
	d.pos += n
	return data
}

func (d *Decoder) read(buf []byte) {
	tmp := d.read2(len(buf))
	if d.err == nil {
		copy(buf, tmp)
	}
}

func (d *Decoder) unread(count int) {
	d.pos -= count
	if d.pos < 0 {
		d.pos = 0
	}
}

func (d *Decoder) PopLong() int64 {
	val := d.read2(LongLen)
	if d.err != nil {
		return 0
	}

	return int64(binary.LittleEndian.Uint64(val))
}

func (d *Decoder) PopDouble() float64 {
	val := d.read2(DoubleLen)
	if d.err != nil {
		return 0
	}

	return math.Float64frombits(binary.LittleEndian.Uint64(val))
}

func (d *Decoder) PopUint() uint32 {
	val := d.read2(WordLen)
	if d.err != nil {
		return 0
	}

	return binary.LittleEndian.Uint32(val)
}

func (d *Decoder) PopRawBytes(size int) []byte {
	return bytes.Clone(d.read2(size))
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
	m := len(d.buf) - d.pos
	return bytes.Clone(d.read2(m)), d.err
}

func (d *Decoder) DumpWithoutRead() ([]byte, error) {
	data, err := d.GetRestOfMessage()
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
	return bytes.Clone(d.popBytes())
}

func (d *Decoder) popBytes() []byte {
	tmp := d.PopUint()

	size := int(tmp & 0xff)
	pad := 0

	switch {
	case size == 0:
		return nil
	case size == 254:
		size = int(tmp >> 8)
		pad = padding4(size)
	default:
		pad = padding4(size + 1)
		d.unread(3)
	}

	buf := d.read2(size)
	_ = d.read2(pad) // padding

	return buf
}
