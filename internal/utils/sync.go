package utils

import (
	"reflect"
	"sync"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

type SyncInt64ObjectChan struct {
	mu sync.RWMutex
	m  map[int64]chan tl.Object
}

func NewSyncInt64ObjectChan() *SyncInt64ObjectChan {
	return &SyncInt64ObjectChan{
		m: make(map[int64]chan tl.Object),
	}
}

func (s *SyncInt64ObjectChan) Add(key int64, value chan tl.Object) {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

func (s *SyncInt64ObjectChan) Get(key int64) (chan tl.Object, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

func (s *SyncInt64ObjectChan) Pop(key int64) (chan tl.Object, bool) {
	s.mu.Lock()
	v, ok := s.m[key]
	delete(s.m, key)
	s.mu.Unlock()
	return v, ok
}

func (s *SyncInt64ObjectChan) Has(key int64) bool {
	s.mu.RLock()
	_, ok := s.m[key]
	s.mu.RUnlock()
	return ok
}

func (s *SyncInt64ObjectChan) Delete(key int64) bool {
	s.mu.Lock()
	_, ok := s.m[key]
	delete(s.m, key)
	s.mu.Unlock()
	return ok
}

func (s *SyncInt64ObjectChan) Keys() []int64 {
	s.mu.RLock()
	keys := make([]int64, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	s.mu.RUnlock()
	return keys
}

func (s *SyncInt64ObjectChan) SwapAndClear() map[int64]chan tl.Object {
	s.mu.Lock()
	old := s.m
	s.m = make(map[int64]chan tl.Object)
	s.mu.Unlock()
	return old
}

func (s *SyncInt64ObjectChan) Close() {
	old := s.SwapAndClear()
	for _, ch := range old {
		closeChanNoPanic(ch)
	}
}

type SyncInt64ReflectTypes struct {
	mu sync.RWMutex
	m  map[int64][]reflect.Type
}

func NewSyncInt64ReflectTypes() *SyncInt64ReflectTypes {
	return &SyncInt64ReflectTypes{
		m: make(map[int64][]reflect.Type),
	}
}

func (s *SyncInt64ReflectTypes) Add(key int64, value []reflect.Type) {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

func (s *SyncInt64ReflectTypes) Get(key int64) ([]reflect.Type, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

func (s *SyncInt64ReflectTypes) Has(key int64) bool {
	s.mu.RLock()
	_, ok := s.m[key]
	s.mu.RUnlock()
	return ok
}

func (s *SyncInt64ReflectTypes) Delete(key int64) bool {
	s.mu.Lock()
	_, ok := s.m[key]
	delete(s.m, key)
	s.mu.Unlock()
	return ok
}

func (s *SyncInt64ReflectTypes) Keys() []int64 {
	s.mu.RLock()
	keys := make([]int64, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	s.mu.RUnlock()
	return keys
}

func (s *SyncInt64ReflectTypes) SwapAndClear() map[int64][]reflect.Type {
	s.mu.Lock()
	old := s.m
	s.m = make(map[int64][]reflect.Type)
	s.mu.Unlock()
	return old
}

func closeChanNoPanic(c chan tl.Object) {
	defer func() { _ = recover() }()
	close(c)
}

// SyncInt64Int64 is a thread-safe map for int64 -> int64 (e.g., msgID -> timestamp)
type SyncInt64Int64 struct {
	mu sync.RWMutex
	m  map[int64]int64
}

func NewSyncInt64Int64() *SyncInt64Int64 {
	return &SyncInt64Int64{
		m: make(map[int64]int64),
	}
}

func (s *SyncInt64Int64) Add(key int64, value int64) {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

func (s *SyncInt64Int64) Get(key int64) (int64, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

func (s *SyncInt64Int64) Delete(key int64) bool {
	s.mu.Lock()
	_, ok := s.m[key]
	delete(s.m, key)
	s.mu.Unlock()
	return ok
}

func (s *SyncInt64Int64) Len() int {
	s.mu.RLock()
	l := len(s.m)
	s.mu.RUnlock()
	return l
}

func (s *SyncInt64Int64) Clear() {
	s.mu.Lock()
	s.m = make(map[int64]int64)
	s.mu.Unlock()
}
