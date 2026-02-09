package manager

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	opclient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/client"
)

// Manager wraps a controller-runtime manager to return a custom client
// from GetClient(). This allows replacing the default client with a
// wrapped client (e.g., UnstructuredClient) while preserving all other
// manager functionality.
type Manager struct {
	manager.Manager

	wrappedClient *opclient.Client
}

// New creates a new Manager that wraps the given manager and
// returns the wrapped client from GetClient().
func New(mgr manager.Manager) *Manager {
	wrappedClient := opclient.New(mgr.GetClient())

	return &Manager{
		Manager:       mgr,
		wrappedClient: wrappedClient,
	}
}

// GetClient returns the wrapped client instead of the default manager client.
func (m *Manager) GetClient() client.Client {
	return m.wrappedClient
}
