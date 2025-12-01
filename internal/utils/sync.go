// Copyright (c) 2025 @AmarnathCJD

package utils

import (
	"reflect"
	"sync"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

type SyncIntObjectChan struct {
	mutex sync.RWMutex
	m     map[int]chan tl.Object
}

func (s *SyncIntObjectChan) Reset() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.m = make(map[int]chan tl.Object)
}

func NewSyncIntObjectChan() *SyncIntObjectChan {
	return &SyncIntObjectChan{m: make(map[int]chan tl.Object)}
}

func (s *SyncIntObjectChan) Has(key int) bool {
	s.mutex.RLock()
	_, ok := s.m[key]
	s.mutex.RUnlock()
	return ok
}

func (s *SyncIntObjectChan) Get(key int) (chan tl.Object, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	v, ok := s.m[key]
	return v, ok
}

func (s *SyncIntObjectChan) Add(key int, value chan tl.Object) {
	s.mutex.Lock()
	s.m[key] = value
	s.mutex.Unlock()
}

func (s *SyncIntObjectChan) Keys() []int {
	keys := make([]int, 0, len(s.m))
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for k := range s.m {
		keys = append(keys, k)
	}
	return keys
}

func (s *SyncIntObjectChan) Delete(key int) bool {
	s.mutex.Lock()
	_, ok := s.m[key]
	delete(s.m, key)
	s.mutex.Unlock()
	return ok
}

type SyncIntReflectTypes struct {
	mutex sync.RWMutex
	m     map[int][]reflect.Type
}

func (s *SyncIntReflectTypes) Reset() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.m = make(map[int][]reflect.Type)
}

func NewSyncIntReflectTypes() *SyncIntReflectTypes {
	return &SyncIntReflectTypes{m: make(map[int][]reflect.Type)}
}

func (s *SyncIntReflectTypes) Has(key int) bool {
	s.mutex.RLock()
	_, ok := s.m[key]
	s.mutex.RUnlock()
	return ok
}

func (s *SyncIntReflectTypes) Get(key int) ([]reflect.Type, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	v, ok := s.m[key]
	return v, ok
}

func (s *SyncIntReflectTypes) Add(key int, value []reflect.Type) {
	s.mutex.Lock()
	s.m[key] = value
	s.mutex.Unlock()
}

func (s *SyncIntReflectTypes) Keys() []int {
	keys := make([]int, 0, len(s.m))
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for k := range s.m {
		keys = append(keys, k)
	}
	return keys
}

func (s *SyncIntReflectTypes) Delete(key int) bool {
	s.mutex.Lock()
	_, ok := s.m[key]
	delete(s.m, key)
	s.mutex.Unlock()
	return ok
}

func (s *SyncIntObjectChan) Close() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for k, v := range s.m {
		CloseChannelWithoutPanic(v)
		delete(s.m, k)
	}
}

func CloseChannelWithoutPanic(c chan tl.Object) {
	defer func() {
		if r := recover(); r != nil {
			close(c)
		}
	}()
}
