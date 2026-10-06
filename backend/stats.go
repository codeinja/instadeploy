package main

import (
	"sync"
	"time"
)

// Stats is a container's current resource usage, as last reported by its
// agent. Kept in memory only: it changes every few seconds and isn't worth
// storing.
type Stats struct {
	CPUPercent  float64   `json:"cpu_percent"`
	MemoryBytes uint64    `json:"memory_bytes"`
	MemoryLimit uint64    `json:"memory_limit"`
	NetRxBytes  uint64    `json:"net_rx_bytes"`
	NetTxBytes  uint64    `json:"net_tx_bytes"`
	At          time.Time `json:"at"`
}

type statsStore struct {
	mu sync.Mutex
	m  map[string]Stats // service ID -> stats
}

func (s *statsStore) set(serviceID string, st Stats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]Stats{}
	}
	st.At = time.Now()
	s.m[serviceID] = st
}

// get returns stats reported in the last minute, or nil.
func (s *statsStore) get(serviceID string) *Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[serviceID]
	if !ok || time.Since(st.At) > time.Minute {
		return nil
	}
	return &st
}
