package providerconfig

import (
	"sync"
)

// unpauseHandler is used to manage unpause operations and ensure
// that we do no duplicate go routines for unpausing Bucket CRs.
type unpauseHandler struct {
	mu         sync.RWMutex
	inProgress map[string]bool
}

func newUnpauseHandler() *unpauseHandler {
	return &unpauseHandler{
		inProgress: make(map[string]bool),
	}
}

func (h *unpauseHandler) IsUnpauseInProgress(backendName string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if inProgress, ok := h.inProgress[backendName]; ok {
		return inProgress
	}

	return false
}

func (h *unpauseHandler) SetUnpauseInProgress(backendName string, inProgress bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.inProgress[backendName] = inProgress
}
