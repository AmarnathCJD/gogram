// Copyright (c) 2025 @AmarnathCJD

package tl

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"
)

var bufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 8*1024)) // 8kb
	},
}

func Marshal(v any) ([]byte, error) {
	buf := bufferPool.Get().(*bytes.Buffer)
	defer func() {
		if buf.Cap() <= 1024*1024 {
			bufferPool.Put(buf)
		}
	}()
	buf.Reset()

	encoder := NewEncoder(buf)
	encoder.encodeValue(reflect.ValueOf(v))
	if err := encoder.CheckErr(); err != nil {
		return nil, err
	}

	return bytes.Clone(buf.Bytes()), nil
}

func (c *Encoder) encodeValue(value reflect.Value) {
	if c.err != nil {
		return
	}
	if !value.IsValid() || ((value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) && value.IsNil()) {
		c.err = fmt.Errorf("cannot marshal a nil value")
		return
	}
	if !value.CanInterface() {
		c.err = fmt.Errorf("cannot marshal unexported field")
		return
	}
	if c.depth >= 128 {
		c.err = fmt.Errorf("TL nesting exceeds 128 levels")
		return
	}
	c.depth++
	defer func() { c.depth-- }()
	if m, ok := value.Interface().(Marshaler); ok {
		if c.err != nil {
			return
		}
		c.err = m.MarshalTL(c)
		return
	}

	switch value.Type().Kind() {
	case reflect.Uint32:
		c.PutUint(uint32(value.Uint()))

	case reflect.Int32:
		c.PutUint(uint32(value.Int()))

	case reflect.Int64:
		c.PutLong(value.Int())

	case reflect.Float64:
		c.PutDouble(value.Float())

	case reflect.Bool:
		c.PutBool(value.Bool())

	case reflect.String:
		c.PutString(value.String())

	case reflect.Struct:
		if !value.CanAddr() {
			copy := reflect.New(value.Type())
			copy.Elem().Set(value)
			c.encodeValue(copy)
			return
		}
		c.encodeStruct(value.Addr())

	case reflect.Ptr, reflect.Interface:
		if value.IsNil() {
			c.err = fmt.Errorf("value can't be nil")
			break
		}

		c.encodeValue(value.Elem())

	case reflect.Slice:
		if b, ok := value.Interface().([]byte); ok {
			c.PutMessage(b)
			break
		}

		c.encodeVectorValue(value)

	case reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint64:
		c.err = fmt.Errorf("int kind: %v (must be converted to int32, int64 or uint32 explicitly)", value.Kind())

	case reflect.Float32, reflect.Complex64, reflect.Complex128:
		c.err = fmt.Errorf("float kind: %s (must be converted to float64 explicitly)", value.Kind())

	default:
		c.err = fmt.Errorf("unsupported type: %v", value.Type())
	}
}

// v must be pointer to struct
func (c *Encoder) encodeStruct(v reflect.Value) {
	if c.err != nil {
		return
	}

	o, ok := v.Interface().(Object)
	if !ok {
		c.err = fmt.Errorf("%s doesn't implement tl.Object interface", v.Type().String())
		return
	}

	v = reflect.Indirect(v)
	vtyp := v.Type()
	cachedTags := GetCachedTags(vtyp)
	indices, count, err := flagLayout(o, v.NumField())
	if err != nil {
		c.err = err
		return
	}
	var flags [2]uint32
	for i, info := range cachedTags {
		if info == nil || info.ignore {
			continue
		}
		if info.version < 1 || info.version > count {
			c.err = fmt.Errorf("field %s.%s has no corresponding flags word", vtyp.Name(), vtyp.Field(i).Name)
			return
		}
		if info.encodedInBitflag && v.Field(i).Kind() != reflect.Bool {
			c.err = fmt.Errorf("field %s.%s: bitflag field must be bool", vtyp.Name(), vtyp.Field(i).Name)
			return
		}
		if !v.Field(i).IsZero() || info.explicit {
			flags[info.version-1] |= 1 << info.index
		}
	}
	c.PutCRC(o.CRC())
	for i := 0; i <= v.NumField(); i++ {
		for version := range count {
			if indices[version] == i {
				c.PutUint(flags[version])
			}
		}
		if c.err != nil || i == v.NumField() {
			break
		}
		field := v.Field(i)
		if !field.CanInterface() {
			if tag := cachedTags[i]; tag == nil || !tag.ignore {
				c.err = fmt.Errorf("cannot marshal unexported field %s", vtyp.Field(i).Name)
				return
			}
		}
		if info := cachedTags[i]; info != nil {
			if info.ignore || info.encodedInBitflag {
				continue
			}
			if flags[info.version-1]&(1<<info.index) == 0 {
				continue
			}
			if field.IsZero() && !info.explicit {
				c.err = c.encodeZeroForFlagPartner(field, vtyp.Field(i).Name)
			} else {
				c.encodeValue(field)
			}
		} else {
			c.encodeValue(field)
		}
		if c.err != nil {
			return
		}
	}

}

func (c *Encoder) encodeZeroForFlagPartner(fieldVal reflect.Value, fieldName string) error {
	switch fieldVal.Kind() {
	case reflect.String:
		c.PutString("")
	case reflect.Int32:
		c.PutUint(0)
	case reflect.Uint32:
		c.PutUint(0)
	case reflect.Int64:
		c.PutLong(0)
	case reflect.Float64:
		c.PutDouble(0)
	case reflect.Bool:
		c.PutBool(false)
	case reflect.Slice:
		if _, ok := fieldVal.Interface().([]byte); ok {
			c.PutMessage(nil)
		} else {
			c.PutCRC(CrcVector)
			c.PutUint(0)
		}
	default:
		return fmt.Errorf("field %q is empty but shares a flag bit with a set field; cannot emit a zero value for type %s", fieldName, fieldVal.Type())
	}
	return nil
}

func (c *Encoder) encodeVectorValue(slice reflect.Value) {
	c.PutCRC(CrcVector)
	c.PutUint(uint32(slice.Len()))

	for i := 0; i < slice.Len(); i++ {
		c.encodeValue(slice.Index(i))
		if c.err != nil {
			c.err = fmt.Errorf("[%v]: %w", i, c.err)
			return
		}
	}
}

// FlagIndex2Getter identifies the second flags word's insertion point among
// Go struct fields. Like FlagIndex, flags words themselves are not fields.
type FlagIndex2Getter interface{ FlagIndex2() int }

func flagLayout(o Object, fields int) (indices [2]int, count int, err error) {
	if g, ok := o.(FlagIndexGetter); ok {
		indices[0], count = g.FlagIndex(), 1
	}
	if g, ok := o.(FlagIndex2Getter); ok {
		indices[1], count = g.FlagIndex2(), 2
	}
	for i := 0; i < count; i++ {
		if indices[i] < 0 || indices[i] > fields || (i > 0 && indices[i] < indices[i-1]) {
			return indices, count, fmt.Errorf("invalid flags insertion point %d for %T", indices[i], o)
		}
	}
	return indices, count, nil
}
