package testf

import (
	"context"
	"slices"
	"sync"

	"k8s.io/client-go/rest"
)

// WarningRecorder collects warning messages delivered by a Kubernetes REST client.
type WarningRecorder struct {
	mu       sync.RWMutex
	messages []string
}

var _ rest.WarningHandlerWithContext = (*WarningRecorder)(nil)

// NewWarningRecorder creates a warning recorder suitable for WithWarningHandler.
func NewWarningRecorder() *WarningRecorder {
	return &WarningRecorder{}
}

// HandleWarningHeaderWithContext records a warning message.
func (r *WarningRecorder) HandleWarningHeaderWithContext(_ context.Context, _ int, _ string, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.messages = append(r.messages, message)
}

// Messages returns a snapshot of the recorded warning messages.
func (r *WarningRecorder) Messages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return slices.Clone(r.messages)
}
