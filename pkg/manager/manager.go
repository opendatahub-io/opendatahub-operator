package manager

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	opclient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/client"
)

// Option configures a Manager.
type Option func(*Manager)

// WithManifestsBasePath sets the base path for component manifests.
func WithManifestsBasePath(p string) Option {
	return func(m *Manager) {
		m.manifestsBasePath = p
	}
}

// Manager wraps a controller-runtime manager to return a custom client
// from GetClient(). This allows replacing the default client with a
// wrapped client (e.g., UnstructuredClient) while preserving all other
// manager functionality.
type Manager struct {
	manager.Manager

	wrappedClient     *opclient.Client
	manifestsBasePath string
}

// New creates a new Manager that wraps the given manager and
// returns the wrapped client from GetClient().
func New(mgr manager.Manager, opts ...Option) *Manager {
	wrappedClient := opclient.New(mgr.GetClient())

	m := &Manager{
		Manager:       mgr,
		wrappedClient: wrappedClient,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// GetClient returns the wrapped client instead of the default manager client.
func (m *Manager) GetClient() client.Client {
	return m.wrappedClient
}

// GetManifestsBasePath returns the base path for component manifests.
func (m *Manager) GetManifestsBasePath() string {
	return m.manifestsBasePath
}
