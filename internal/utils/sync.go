package utils

import (
	"reflect"
	"sync"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
)

type SyncIntObjectChan struct {
	mu sync.RWMutex
	m  map[int]chan tl.Object
}

func NewSyncIntObjectChan() *SyncIntObjectChan {
	return &SyncIntObjectChan{
		m: make(map[int]chan tl.Object),
	}
}

func (s *SyncIntObjectChan) Add(key int, value chan tl.Object) {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

func (s *SyncIntObjectChan) Get(key int) (chan tl.Object, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

func (s *SyncIntObjectChan) Has(key int) bool {
	s.mu.RLock()
	_, ok := s.m[key]
	s.mu.RUnlock()
	return ok
}

func (s *SyncIntObjectChan) Delete(key int) bool {
	s.mu.Lock()
	_, ok := s.m[key]
	delete(s.m, key)
	s.mu.Unlock()
	return ok
}

func (s *SyncIntObjectChan) Keys() []int {
	s.mu.RLock()
	keys := make([]int, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	s.mu.RUnlock()
	return keys
}

func (s *SyncIntObjectChan) SwapAndClear() map[int]chan tl.Object {
	s.mu.Lock()
	old := s.m
	s.m = make(map[int]chan tl.Object)
	s.mu.Unlock()
	return old
}

func (s *SyncIntObjectChan) Close() {
	old := s.SwapAndClear()
	for _, ch := range old {
		closeChanNoPanic(ch)
	}
}

type SyncIntReflectTypes struct {
	mu sync.RWMutex
	m  map[int][]reflect.Type
}

func NewSyncIntReflectTypes() *SyncIntReflectTypes {
	return &SyncIntReflectTypes{
		m: make(map[int][]reflect.Type),
	}
}

func (s *SyncIntReflectTypes) Add(key int, value []reflect.Type) {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

func (s *SyncIntReflectTypes) Get(key int) ([]reflect.Type, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

func (s *SyncIntReflectTypes) Has(key int) bool {
	s.mu.RLock()
	_, ok := s.m[key]
	s.mu.RUnlock()
	return ok
}

func (s *SyncIntReflectTypes) Delete(key int) bool {
	s.mu.Lock()
	_, ok := s.m[key]
	delete(s.m, key)
	s.mu.Unlock()
	return ok
}

func (s *SyncIntReflectTypes) Keys() []int {
	s.mu.RLock()
	keys := make([]int, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	s.mu.RUnlock()
	return keys
}

func (s *SyncIntReflectTypes) SwapAndClear() map[int][]reflect.Type {
	s.mu.Lock()
	old := s.m
	s.m = make(map[int][]reflect.Type)
	s.mu.Unlock()
	return old
}

func closeChanNoPanic(c chan tl.Object) {
	defer func() { _ = recover() }()
	close(c)
}

// SyncIntInt64 is a thread-safe map for int -> int64 (e.g., msgID -> timestamp)
type SyncIntInt64 struct {
	mu sync.RWMutex
	m  map[int]int64
}

func NewSyncIntInt64() *SyncIntInt64 {
	return &SyncIntInt64{
		m: make(map[int]int64),
	}
}

func (s *SyncIntInt64) Add(key int, value int64) {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

func (s *SyncIntInt64) Get(key int) (int64, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

func (s *SyncIntInt64) Delete(key int) bool {
	s.mu.Lock()
	_, ok := s.m[key]
	delete(s.m, key)
	s.mu.Unlock()
	return ok
}

func (s *SyncIntInt64) Len() int {
	s.mu.RLock()
	l := len(s.m)
	s.mu.RUnlock()
	return l
}
