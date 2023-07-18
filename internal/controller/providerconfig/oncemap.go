package providerconfig

import "sync"

// onceMap is used by the health-check controller to ensure
// initial checks are only carried out once for each backend.
type onceMap struct {
	onceMap map[string]*sync.Once
	mu      sync.RWMutex
}

func newOnceMap() *onceMap {
	return &onceMap{
		onceMap: make(map[string]*sync.Once),
	}
}

func (s *onceMap) addEntryWithOnce(backendName string) *sync.Once {
	s.mu.Lock()
	defer s.mu.Unlock()

	if once, ok := s.onceMap[backendName]; ok {
		// already exists, return existing.
		return once
	}

	once := &sync.Once{}
	s.onceMap[backendName] = once

	return once
}

func (s *onceMap) deleteEntry(backendName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.onceMap, backendName)
}
