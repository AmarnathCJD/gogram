// Copyright (c) 2025 @AmarnathCJD

package tl

import (
	"fmt"
	"reflect"
	"sync"
)

var (
	registryMu  sync.RWMutex
	objectByCrc = make(map[uint32]reflect.Type)
	enumCrcs    = make(map[uint32]struct{})
)

func lookupObjectType(crc uint32) (reflect.Type, bool) {
	registryMu.RLock()
	t, ok := objectByCrc[crc]
	registryMu.RUnlock()
	return t, ok
}

func isEnumCRC(crc uint32) bool {
	registryMu.RLock()
	_, ok := enumCrcs[crc]
	registryMu.RUnlock()
	return ok
}

func registerObject(o Object) {
	if o == nil {
		panic("object is nil")
	}
	objectByCrc[o.CRC()] = reflect.TypeOf(o)
}

func registerEnum(o Object) {
	registerObject(o)
	enumCrcs[o.CRC()] = struct{}{}
}

func RegisterObjects(obs ...Object) {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, o := range obs {
		if val, found := objectByCrc[o.CRC()]; found {
			panic(fmt.Errorf("object with that crc already registered as %v: 0x%08x", val.String(), o.CRC()))
		}

		registerObject(o)
	}
}

func RegisterObject(o Object, customCRC uint32) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if val, found := objectByCrc[customCRC]; found {
		panic(fmt.Errorf("object with that crc already registered as %v: 0x%08x", val.String(), customCRC))
	}
	objectByCrc[customCRC] = reflect.TypeOf(o)
}

func RegisterEnums(enums ...Object) {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, e := range enums {
		if _, found := enumCrcs[e.CRC()]; found {
			panic(fmt.Errorf("enum with that crc already registered"))
		}

		registerEnum(e)
	}
}
