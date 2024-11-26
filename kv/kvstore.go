package liferaft

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

var KeyNotFoundError = fmt.Errorf("key not found")
var CASMismatchError = fmt.Errorf("CAS mismatch")

type Store struct {
	mu   sync.RWMutex
	data map[string]any
}

func NewKVStore() *Store {
	return &Store{
		data: make(map[string]any),
	}
}

func (s *Store) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res, ok := s.data[key]
	return res, ok
}

func (s *Store) Set(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

func (s *Store) CAS(key string, from, to any) (haskey, casok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.data[key]
	if !ok {
		return false, false
	}
	if res == from {
		s.data[key] = to
		return true, true
	}
	return true, false
}

type Command struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value any    `json:"value"`
	From  any    `json:"from,omitempty"`
	To    any    `json:"to,omitempty"`
}

func (c Command) Marshal() []byte {
	res, _ := json.Marshal(c)
	return res
}

func (s *Store) Apply(cmd []byte) (any, error) {
	var c Command
	err := json.Unmarshal(cmd, &c)
	if err != nil {
		panic(err)
	}
	switch c.Op {
	case "set":
		s.Set(c.Key, c.Value)
		return nil, nil
	case "get":
		val, ok := s.Get(c.Key)
		if !ok {
			return nil, KeyNotFoundError
		}
		return val, nil
	case "cas":
		haskey, casok := s.CAS(c.Key, c.From, c.To)
		fmt.Fprintf(os.Stderr, "cas-op %s from %v to %v (%v, %v)\n", c.Key, c.From, c.To, haskey, casok)
		if !haskey {
			return nil, KeyNotFoundError
		} else if !casok {
			return nil, CASMismatchError
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unknown op %s", c.Op)
}
