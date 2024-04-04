package feature

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// CreateNamespaceNoOwnership will create namespace with the given name if it does not exist yet
// it does not set ownership nor apply extra metadata to the existing namespace.
func CreateNamespaceNoOwnership(namespace string) Action {
	return func(f *Feature) error {
		_, err := cluster.CreateNamespaceIfNotExists(f.Client, namespace)
		return err
	}
}
