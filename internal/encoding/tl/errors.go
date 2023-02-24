// Copyright (c) 2022,RoseLoverX
package tl

import "fmt"

type ErrRegisteredObjectNotFound struct {
	Crc  uint32
	Data []byte
}

func (e *ErrRegisteredObjectNotFound) Error() string {
	return fmt.Sprintf("object with provided crc not registered: 0x%08x", e.Crc)
}

type ErrMustParseSlicesExplicitly struct{}

func (*ErrMustParseSlicesExplicitly) Error() string {
	return "got vector CRC code when parsing unknown object: vectors can't be parsed as predicted objects"
}

type ErrorPartialWrite struct {
	Has  int
	Want int
}

func (e *ErrorPartialWrite) Error() string {
	return fmt.Sprintf("write failed: writed only %v bytes, expected %v", e.Has, e.Want)
}
