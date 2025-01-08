package resource

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// Applier is an interface that allows to apply a set of resources.
type Applier interface {
	Apply(ctx context.Context, cli client.Client, data map[string]any, options ...cluster.MetaOptions) error
}

// Creator is an interface that allows to create a set of resources to be applied.
type Creator interface {
	Create() ([]Applier, error)
}
