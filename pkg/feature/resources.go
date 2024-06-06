package feature

import (
	"context"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// CreateNamespaceIfNotExists will create a namespace with the given name if it does not exist yet.
// It does not set ownership nor apply extra metadata to the existing namespace.
func CreateNamespaceIfNotExists(namespace string) Action {
	return func(f *Feature) error {
		_, err := cluster.CreateNamespace(context.TODO(), f.Client, namespace)

		return err
	}
}
