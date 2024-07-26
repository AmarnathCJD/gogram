// Copyright (c) 2024 RoseLoverX

package tl

import (
	"bytes"
	"fmt"
	"reflect"

	"github.com/pkg/errors"
)

func Decode(data []byte, res any) error {
	if res == nil {
		return errors.New("can't unmarshal to nil value")
	}
	if reflect.TypeOf(res).Kind() != reflect.Ptr {
		return fmt.Errorf("res value is not pointer as expected. got %v", reflect.TypeOf(res))
	}

	d, err := NewDecoder(bytes.NewReader(data))
	if err != nil {
		return err
	}

	d.decodeValue(reflect.ValueOf(res))
	if d.err != nil {
		return errors.Wrapf(d.err, "decode %T", res)
	}

	return nil
}

func DecodeUnknownObject(data []byte, expectNextTypes ...reflect.Type) (Object, error) {
	d, err := NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if len(expectNextTypes) > 0 {
		d.ExpectTypesInInterface(expectNextTypes...)
	}

	obj := d.decodeRegisteredObject()

	if d.err != nil {
		return obj, errors.Wrap(d.err, "decoding predicted object")
	}
	return obj, nil
}

func (d *Decoder) decodeObject(o Object, ignoreCRC bool) {
	if d.err != nil {
		return
	}

	if !ignoreCRC {
		crcCode := d.PopCRC()
		if d.err != nil {
			d.err = errors.Wrap(d.err, "read crc")
			return
		}

		if crcCode != o.CRC() {
			d.err = fmt.Errorf("invalid crc code: %#v, want: %#v", crcCode, o.CRC())
			return
		}
	}

	value := reflect.ValueOf(o)

	if value.Kind() != reflect.Ptr {
		panic("not a pointer")
	}

	value = reflect.Indirect(value)
	if value.Kind() != reflect.Struct {
		panic("not receiving on struct: " + value.Type().String() + " -> " + value.Kind().String())
	}

	vtyp := value.Type()

	var optionalBitSetA uint32
	var optionalBitSetB uint32

	var flagsetIndex = -1
	if haveFlag(value.Interface()) {
		indexGetter, ok := reflect.New(vtyp).Interface().(FlagIndexGetter)
		if !ok {
			panic("type " + value.Type().String() + " has type bit flag tags, but doesn't implement tl.FlagIndexGetter")
		}
		flagsetIndex = indexGetter.FlagIndex()
		if flagsetIndex < 0 {
			panic("flag index is below zero, must be index of parameters")
		}
	}

	var isBitsetAParsed bool // flag
	var isBitsetBParsed bool // flag2

	loopCycles := value.NumField()
	if flagsetIndex >= 0 {
		loopCycles++
	}

	var alreadyParsed []string

	if vtyp.Name() == "UserFull" { // special case for UserFull
		optionalBitSetA = d.PopUint()
		optionalBitSetB = d.PopUint()
		isBitsetAParsed = true
		isBitsetBParsed = true
	}

	for i := 0; i < loopCycles; i++ {
		if flagsetIndex == i && !isBitsetAParsed {
			optionalBitSetA = d.PopUint()
			if d.err != nil {
				d.err = errors.Wrap(d.err, "reading bitset("+vtyp.Name()+")")
				return
			}
			isBitsetAParsed = true
			i = 0
			continue
		}

		fieldIndex := i
		if isBitsetAParsed && i > 0 {
			fieldIndex--
		}
		field := value.Field(fieldIndex)

		if _, found := vtyp.Field(fieldIndex).Tag.Lookup(tagName); found {
			info, err := parseTag(vtyp.Field(fieldIndex).Tag)
			if err != nil {
				d.err = errors.Wrap(err, "parse tag")
				return
			}
			if info.version == 1 {
				if optionalBitSetA&(1<<info.index) == 0 {
					continue
				}
			} else if info.version == 2 {
				if (!isBitsetBParsed) && isBitsetAParsed {
					optionalBitSetB = d.PopUint()
					if d.err != nil {
						d.err = errors.Wrap(d.err, "read bitset")
						return
					}
					isBitsetBParsed = true
				}
				if optionalBitSetB&(1<<info.index) == 0 {
					continue
				}
			}

			if info.encodedInBitflag {
				field.Set(reflect.ValueOf(true).Convert(field.Type()))
				continue
			}
		}

		if field.Kind() == reflect.Ptr {
			val := reflect.New(field.Type().Elem())
			field.Set(val)
		}

		if !haveInSlice(vtyp.Field(fieldIndex).Name, alreadyParsed) {
			d.decodeValue(field)
			alreadyParsed = append(alreadyParsed, vtyp.Field(fieldIndex).Name)
		}

		if d.err != nil {
			d.err = errors.Wrap(d.err, "decode object: "+vtyp.Name()+"."+vtyp.Field(fieldIndex).Name)
			break
		}
	}
}

func (d *Decoder) decodeValue(value reflect.Value) {
	if d.err != nil {
		return
	}

	if m, ok := value.Interface().(Unmarshaler); ok {
		err := m.UnmarshalTL(d)
		if err != nil {
			d.err = err
		}
		return
	}

	val := d.decodeValueGeneral(value)
	if val != nil {
		value.Set(reflect.ValueOf(val).Convert(value.Type()))
		return
	}

	switch value.Kind() {

	case reflect.Slice:
		if _, ok := value.Interface().([]byte); ok {
			val = d.PopMessage()
		} else {
			val = d.PopVector(value.Type().Elem())
		}

	case reflect.Ptr:
		if o, ok := value.Interface().(Object); ok {
			d.decodeObject(o, false)
		} else {
			d.decodeValue(value.Elem())
		}

		return

	case reflect.Interface:
		val = d.decodeRegisteredObject()

		if d.err != nil {
			d.err = errors.Wrap(d.err, "decode interface")
			return
		}

		if v, ok := val.(*WrappedSlice); ok {
			if reflect.TypeOf(v.data).ConvertibleTo(value.Type()) {
				val = v.data
			}
		}
	default:
		panic("unknown kind of value: " + value.Type().String())
	}

	if d.err != nil {
		return
	}

	value.Set(reflect.ValueOf(val).Convert(value.Type()))
}

func (d *Decoder) decodeValueGeneral(value reflect.Value) any {
	var val any

	switch value.Kind() {
	case reflect.Float64:
		val = d.PopDouble()

	case reflect.Int64:
		val = d.PopLong()

	case reflect.Uint32:
		val = d.PopUint()

	case reflect.Int32:
		val = int32(d.PopUint())

	case reflect.Bool:
		val = d.PopBool()

	case reflect.String:
		val = string(d.PopMessage())

	case reflect.Chan, reflect.Func, reflect.Uintptr, reflect.UnsafePointer:
		panic(value.Kind().String() + " is not supported")

	case reflect.Struct:
		d.err = fmt.Errorf("%v must implement tl.Object for decoding (also it must be pointer)", value.Type())

	case reflect.Map:
		d.err = errors.New("map is not ordered object (must order like structs)")

	case reflect.Array:
		d.err = errors.New("array must be slice")

	case reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint64:
		d.err = fmt.Errorf("int kind: %v (must converted to int32, int64 or uint32 explicitly)", value.Kind())
		return nil

	case reflect.Float32, reflect.Complex64, reflect.Complex128:
		d.err = fmt.Errorf("float kind: %s (must be converted to float64 explicitly)", value.Kind())
		return nil

	default:
		return nil
	}

	return val
}

func (d *Decoder) decodeRegisteredObject() Object {
	crc := d.PopCRC()
	if d.err != nil {
		d.err = errors.Wrap(d.err, "reading crc")
	}

	var _typ reflect.Type
	switch crc {
	case CrcVector:
		if len(d.expectedTypes) == 0 {
			lenVector := d.PopUint() // pop vector length
			vecCrc := d.PopCRC()     // read the crc of the vector<...>

			if lenVector == 0 {
				if d.err != nil && d.err.Error() == "EOF" { // if vector is empty, return nil
					d.err = nil
				}
				return &PseudoNil{}
			}

			if vecCrc == CrcTrue || vecCrc == CrcFalse {
				d.expectedTypes = append(d.expectedTypes, reflect.TypeOf([]bool{}))
			} else {
				d.expectedTypes = append(d.expectedTypes, reflect.TypeOf([]Object{}))
				crc = vecCrc
			}

			// seek back to initial position
			d.unread(8) // d.buf.Seek(-8, io.SeekCurrent)
		}

		_typ = d.expectedTypes[0]
		d.expectedTypes = d.expectedTypes[1:]

		res := d.popVector(_typ.Elem(), true)

		if d.err != nil {
			//d.err = nil
			return nil // &PseudoNil{}
		}

		switch res := res.(type) {
		case []bool:
			if len(res) > 0 {
				switch res[0] {
				case true:
					return &PseudoTrue{}
				case false:
					return &PseudoFalse{}
				}
			}

		case []Object:
			if len(res) == 0 {
				return &PseudoNil{}
			}

			if _typ, ok := objectByCrc[crc]; ok {
				_v := reflect.MakeSlice(reflect.SliceOf(_typ), 0, 0)
				for _, o := range res {
					_v = reflect.Append(_v, reflect.ValueOf(o).Convert(_typ))
				}

				return &WrappedSlice{data: _v.Interface()}
			}

		default:
			return &WrappedSlice{data: res}
		}

	case CrcFalse:
		return &PseudoFalse{}

	case CrcTrue:
		return &PseudoTrue{}

	case CrcNull:
		return &PseudoNil{}
	}

	_typ, ok := objectByCrc[crc]

	if !ok {
		msg, err := d.DumpWithoutRead()
		if err != nil {
			return nil
		}

		d.err = &ErrRegisteredObjectNotFound{
			Crc:  crc,
			Data: msg,
		}

		return nil
	}

	o := reflect.New(_typ.Elem()).Interface().(Object)

	if m, ok := o.(Unmarshaler); ok {
		err := m.UnmarshalTL(d)
		if err != nil {
			d.err = err
			return nil
		}
		return o
	}

	if _, isEnum := enumCrcs[crc]; !isEnum {
		d.decodeObject(o, true)
		if d.err != nil {
			d.err = errors.Wrapf(d.err, "decode registered object %T", o)
			return o
		}
	}

	return o
}
