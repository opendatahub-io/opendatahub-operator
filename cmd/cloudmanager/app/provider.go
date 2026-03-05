package app

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Provider defines what each cloud provider must supply to the unified cloud manager binary.
type Provider struct {
	// Name is the provider name (e.g., "azure", "coreweave"), used in logs.
	Name string
	// AddToScheme registers the provider's API types with the runtime scheme.
	AddToScheme func(*runtime.Scheme) error
	// LeaderElectionID is the unique leader election identifier for this provider.
	LeaderElectionID string
	// NewReconciler creates and registers the provider's controller with the manager.
	NewReconciler func(ctx context.Context, mgr ctrl.Manager) error
	// CacheOptions returns the provider-specific cache configuration.
	CacheOptions func(scheme *runtime.Scheme) cache.Options
	// ClientOptions returns the provider-specific client configuration.
	// It always sets the unstructured cache to true.
	ClientOptions func() client.Options
}

// Validate checks that all required Provider fields are set.
func (p *Provider) Validate() error {
	p.SetDefaults()

	if p.Name == "" {
		return errors.New("provider Name is required")
	}
	if p.AddToScheme == nil {
		return errors.New("provider AddToScheme is required")
	}
	if p.LeaderElectionID == "" {
		return errors.New("provider LeaderElectionID is required")
	}
	if p.NewReconciler == nil {
		return errors.New("provider NewReconciler is required")
	}
	if p.CacheOptions == nil {
		return errors.New("provider CacheOptions is required")
	}
	if p.ClientOptions == nil {
		return errors.New("provider ClientOptions is required")
	}

	return nil
}

// SetDefaults fills in default values for optional Provider fields.
func (p *Provider) SetDefaults() {
	if p.ClientOptions == nil {
		p.ClientOptions = func() client.Options {
			return client.Options{}
		}
	}
}
