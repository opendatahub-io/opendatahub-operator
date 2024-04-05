package feature

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// CreateNamespaceIfNotExists will create namespace with the given name if it does not exist yet, but will not own it.
func CreateNamespaceIfNotExists(namespace string) Action {
	return func(f *Feature) error {
		_, err := cluster.CreateNamespace(f.Client, namespace)

		return err
	}
}
