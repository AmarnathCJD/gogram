package utils

import (
	"maps"
	"sync"
)

const minSizeSet = 128

type null = struct{}

type SyncSet[T comparable] struct {
	mu sync.RWMutex
	m  map[T]null
}

func NewSyncSet[T comparable]() *SyncSet[T] {
	return &SyncSet[T]{m: make(map[T]null, minSizeSet)}
}

func (s *SyncSet[T]) Clear() {
	s.mu.Lock()
	clear(s.m)
	s.mu.Unlock()
}

func (s *SyncSet[T]) Add(key T) bool {
	s.mu.Lock()
	prevLen := len(s.m)
	s.m[key] = null{}
	cLen := len(s.m)
	s.mu.Unlock()
	return prevLen != cLen
}

func (s *SyncSet[T]) Has(key T) bool {
	s.mu.RLock()
	_, ok := s.m[key]
	s.mu.RUnlock()
	return ok
}

func (s *SyncSet[T]) Delete(key T) {
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}

func (s *SyncSet[T]) Pop(key T) bool {
	s.mu.Lock()
	_, ok := s.m[key]
	if ok {
		delete(s.m, key)
	}
	s.mu.Unlock()
	return ok
}

func (s *SyncSet[T]) ForEach(fn func(key T) bool) {
	s.mu.RLock()
	for key := range s.m {
		if !fn(key) {
			break
		}
	}
	s.mu.RUnlock()
}

func (s *SyncSet[T]) Keys() []T {
	s.mu.RLock()
	keys := make([]T, 0, len(s.m))
	for key := range s.m {
		keys = append(keys, key)
	}
	s.mu.RUnlock()
	return keys
}

func (s *SyncSet[T]) Len() int {
	s.mu.RLock()
	c := len(s.m)
	s.mu.RUnlock()
	return c
}

func (s *SyncSet[T]) Clone() *SyncSet[T] {
	s.mu.RLock()
	m := maps.Clone(s.m)
	s.mu.RUnlock()
	return &SyncSet[T]{m: m}
}
