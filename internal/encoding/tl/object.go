// Copyright (c) 2022 RoseLoverX

package tl

type Object interface {
	CRC() uint32
}

type FlagIndexGetter interface {
	FlagIndex() int
}

type Marshaler interface {
	MarshalTL(*Encoder) error
}

type Unmarshaler interface {
	UnmarshalTL(*Decoder) error
}

//==========================================================================================================//
// Next types are specific structs for handling bool types, slice and null as object.                       //
// See https://github.com/amarnathcjd/gogram/issues/51                                                           //
//==========================================================================================================//

// PseudoTrue is a support struct which is required to get native
type PseudoTrue struct{}

func (*PseudoTrue) CRC() uint32 {
	return CrcTrue
}

// PseudoFalse is a support struct which is required to get native
type PseudoFalse struct{}

func (*PseudoFalse) CRC() uint32 {
	return CrcFalse
}

type PseudoNil struct{}

func (*PseudoNil) CRC() uint32 {
	return CrcNull
}

// you won't use it, right?
func (*PseudoNil) Unwrap() any {
	return nil
}

// WrappedSlice is pseudo type. YOU SHOULD NOT use it customly, instead, you must encode/decode value by
// encoder.PutVector or decoder.PopVector
type WrappedSlice struct {
	data any
}

func (*WrappedSlice) CRC() uint32 {
	return CrcVector
}

func (w *WrappedSlice) Unwrap() any {
	return w.data
}

func UnwrapNativeTypes(in Object) any {
	switch i := in.(type) {
	case *PseudoTrue:
		return true
	case *PseudoFalse:
		return false
	case *PseudoNil:
		return nil
	case *WrappedSlice:
		return i.Unwrap()
	default:
		return in
	}
}
