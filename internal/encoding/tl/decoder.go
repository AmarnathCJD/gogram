// Copyright (c) 2025 @AmarnathCJD

package tl

import (
	"fmt"
	"reflect"
)

func DecodeUnknownObject(data []byte, expectNextTypes ...reflect.Type) (Object, error) {
	d := NewDecoderBytes(data)
	if len(expectNextTypes) > 0 {
		d.ExpectTypesInInterface(expectNextTypes...)
	}

	obj := d.decodeRegisteredObject()

	if d.err != nil {
		return obj, fmt.Errorf("decoding predicted object: %w", d.err)
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
			d.err = fmt.Errorf("read crc: %w", d.err)
			return
		}

		if crcCode != o.CRC() {
			d.err = fmt.Errorf("invalid crc code: %#v, want: %#v", crcCode, o.CRC())
			return
		}
	}

	value := reflect.ValueOf(o)

	if value.Kind() != reflect.Pointer {
		d.err = fmt.Errorf("not pointer to struct: %s", value.Type().String())
		return
	}

	value = reflect.Indirect(value)
	if value.Kind() != reflect.Struct {
		d.err = fmt.Errorf("not receiving on struct: %s -> %s", value.Type().String(), value.Kind().String())
		return
	}

	vtyp := value.Type()
	cachedTags := GetCachedTags(vtyp)
	indices, count, err := flagLayout(o, value.NumField())
	if err != nil {
		d.err = err
		return
	}
	var flags [2]uint32
	for i := 0; i <= value.NumField(); i++ {
		for version := 0; version < count; version++ {
			if indices[version] == i {
				flags[version] = d.PopUint()
			}
		}
		if d.err != nil || i == value.NumField() {
			break
		}
		field := value.Field(i)
		info := cachedTags[i]
		if !field.CanSet() && (info == nil || !info.ignore) {
			d.err = fmt.Errorf("field %s.%s cannot be decoded", vtyp.Name(), vtyp.Field(i).Name)
			return
		}
		if info != nil {
			if info.ignore {
				continue
			}
			if info.version < 1 || info.version > count {
				d.err = fmt.Errorf("field %s.%s has no corresponding flags word", vtyp.Name(), vtyp.Field(i).Name)
				return
			}
			if flags[info.version-1]&(1<<info.index) == 0 {
				field.SetZero()
				continue
			}
			if info.encodedInBitflag {
				if field.Kind() != reflect.Bool {
					d.err = fmt.Errorf("bitflag field must be bool")
					return
				}
				field.SetBool(true)
				continue
			}
		}
		if !field.CanSet() {
			d.err = fmt.Errorf("field %s.%s cannot be decoded", vtyp.Name(), vtyp.Field(i).Name)
			return
		}
		if field.Kind() == reflect.Pointer {
			field.Set(reflect.New(field.Type().Elem()))
		}
		d.decodeValue(field)
		if d.err != nil {
			d.err = fmt.Errorf("decode object: %s.%s: %w", vtyp.Name(), vtyp.Field(i).Name, d.err)
			return
		}
	}

}

func (d *Decoder) decodeValue(value reflect.Value) {
	if d.err != nil {
		return
	}

	if !value.IsValid() || !value.CanInterface() {
		d.err = fmt.Errorf("invalid or unexported destination")
		return
	}
	if d.depth >= 128 {
		d.err = fmt.Errorf("TL nesting exceeds 128 levels")
		return
	}
	d.depth++
	defer func() { d.depth-- }()
	if m, ok := value.Interface().(Unmarshaler); ok {
		err := m.UnmarshalTL(d)
		if err != nil {
			d.err = err
		}
		return
	}

	val := d.decodeValueGeneral(value)
	if val != nil {
		d.assignValue(value, val)
		return
	}

	switch value.Kind() {

	case reflect.Slice:
		if _, ok := value.Interface().([]byte); ok {
			val = d.PopMessage()
		} else {
			val = d.PopVector(value.Type().Elem())
		}

	case reflect.Pointer:
		if o, ok := value.Interface().(Object); ok {
			d.decodeObject(o, false)
		} else {
			d.decodeValue(value.Elem())
		}

		return

	case reflect.Interface:
		val = d.decodeRegisteredObject()

		if d.err != nil {
			d.err = fmt.Errorf("decode interface: %w", d.err)
			return
		}

		if v, ok := val.(*WrappedSlice); ok {
			if reflect.TypeOf(v.data).ConvertibleTo(value.Type()) {
				val = v.data
			}
		}
	default:
		d.err = fmt.Errorf("unknown kind of value: %s", value.Type().String())
		return
	}

	if d.err != nil {
		return
	}

	d.assignValue(value, val)
}

func (d *Decoder) assignValue(dst reflect.Value, value any) {
	src := reflect.ValueOf(value)
	if !src.IsValid() || !dst.CanSet() || !src.Type().ConvertibleTo(dst.Type()) {
		d.err = fmt.Errorf("decoded %T cannot be assigned to %v", value, dst.Type())
		return
	}
	dst.Set(src.Convert(dst.Type()))
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
		d.err = fmt.Errorf("%s is not supported", value.Kind().String())
		return nil

	case reflect.Struct:
		d.err = fmt.Errorf("%v must implement tl.Object for decoding (also it must be pointer)", value.Type())

	case reflect.Map:
		d.err = fmt.Errorf("map is not ordered object (must order like structs): %s", value.Type())

	case reflect.Array:
		d.err = fmt.Errorf("array must be slice: %s", value.Type())

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
	if d.err != nil {
		return nil
	}
	if d.depth >= 128 {
		d.err = fmt.Errorf("TL nesting exceeds 128 levels")
		return nil
	}
	d.depth++
	defer func() { d.depth-- }()
	crc := d.PopCRC()
	if d.err != nil {
		d.err = fmt.Errorf("reading crc: %w", d.err)
		return nil
	}

	var _typ reflect.Type
	switch crc {
	case CrcVector:
		if len(d.expectedTypes) == 0 {
			lenVector := d.PopUint() // pop vector length
			if lenVector == 0 {
				return &PseudoNil{}
			}
			vecCrc := d.PopCRC() // inspect the first element without consuming it

			if vecCrc == CrcTrue || vecCrc == CrcFalse {
				d.expectedTypes = append(d.expectedTypes, reflect.TypeOf([]bool{}))
			} else {
				d.expectedTypes = append(d.expectedTypes, reflect.TypeOf([]Object{}))
				crc = vecCrc
			}

			d.unread(8)
		}

		_typ = d.expectedTypes[0]
		d.expectedTypes = d.expectedTypes[1:]
		if _typ.Kind() != reflect.Slice {
			d.err = fmt.Errorf("vector hint must be a slice, got %v", _typ)
			return nil
		}

		res := d.popVector(_typ.Elem(), true)

		if d.err != nil {
			return nil
		}

		switch res := res.(type) {
		case []bool:
			return &WrappedSlice{data: res}

		case []Object:
			if len(res) == 0 {
				return &PseudoNil{}
			}

			if _typ, ok := lookupObjectType(crc); ok {
				_v := reflect.MakeSlice(reflect.SliceOf(_typ), 0, 0)
				for _, o := range res {
					if !reflect.TypeOf(o).ConvertibleTo(_typ) {
						return &WrappedSlice{data: res}
					}
					_v = reflect.Append(_v, reflect.ValueOf(o).Convert(_typ))
				}

				return &WrappedSlice{data: _v.Interface()}
			}
			return &WrappedSlice{data: res}

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

	_typ, ok := lookupObjectType(crc)

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

	if !isEnumCRC(crc) {
		d.decodeObject(o, true)
		if d.err != nil {
			d.err = fmt.Errorf("decode registered object %T: %w", o, d.err)
			return o
		}
	}

	return o
}
