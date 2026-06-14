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
	defer bufferPool.Put(buf)
	buf.Reset()

	encoder := NewEncoder(buf)
	encoder.encodeValue(reflect.ValueOf(v))
	if err := encoder.CheckErr(); err != nil {
		return nil, err
	}

	return bytes.Clone(buf.Bytes()), nil
}

func (c *Encoder) encodeValue(value reflect.Value) {
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

	var hasFlagsField bool
	var flag, flag2 uint32
	var flagIndex int
	g, ok := v.Interface().(FlagIndexGetter)
	if ok {
		hasFlagsField = true
		flagIndex = g.FlagIndex()
	}

	v = reflect.Indirect(v)

	c.PutCRC(o.CRC())
	vtyp := v.Type()
	cachedTags := GetCachedTags(vtyp)
	numFields := v.NumField()

	hasFlag2 := false
	firstFlag2FieldIndex := -1
	for i := 0; i < numFields; i++ {
		info := cachedTags[i]

		if info == nil || info.ignore {
			continue
		}

		if info.encodedInBitflag && vtyp.Field(i).Type.Kind() != reflect.Bool {
			c.err = fmt.Errorf("field '%s': only bool values can be encoded in bitflag", vtyp.Field(i).Name)
			return
		}

		fieldVal := v.Field(i)
		set := !fieldVal.IsZero() || info.explicit
		if info.version == 2 {
			if firstFlag2FieldIndex < 0 {
				firstFlag2FieldIndex = i
			}
			hasFlag2 = true
			if set {
				flag2 |= 1 << info.index
			}
		} else {
			if set {
				flag |= 1 << info.index
			}
		}
	}

	flag2Emitted := !hasFlag2
	for i := range numFields {
		if hasFlagsField && flagIndex == i {
			c.PutUint(flag)
			if c.err != nil {
				return
			}
		}

		info := cachedTags[i]
		if hasFlag2 && !flag2Emitted && info != nil && info.version == 2 && i == firstFlag2FieldIndex {
			c.PutUint(flag2)
			flag2Emitted = true
			if c.err != nil {
				return
			}
		}

		if info != nil {
			if info.ignore {
				continue
			}

			fieldVal := v.Field(i)
			if fieldVal.IsZero() && !info.explicit {
				continue
			}
			if info.encodedInBitflag {
				continue
			}

			c.encodeValue(fieldVal)
		} else {
			c.encodeValue(v.Field(i))
		}

		if c.err != nil {
			return
		}
	}

	if hasFlag2 && !flag2Emitted {
		c.PutUint(flag2)
	}
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
