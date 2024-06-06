package cluster

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// MetaOptions allows to add additional settings for the object being created through a chain
// of functions which are applied on metav1.Object before actual resource creation.
type MetaOptions func(obj metav1.Object) error

func ApplyMetaOptions(obj metav1.Object, opts ...MetaOptions) error {
	for _, opt := range opts {
		if err := opt(obj); err != nil {
			return err
		}
	}

	return nil
}

func OwnedBy(owner metav1.Object, scheme *runtime.Scheme) MetaOptions {
	return func(obj metav1.Object) error {
		return controllerutil.SetOwnerReference(owner, obj, scheme)
	}
}

func WithLabels(labels ...string) MetaOptions {
	return func(obj metav1.Object) error {
		labelsMap, err := extractKeyValues(labels)
		if err != nil {
			return fmt.Errorf("failed unable to set labels: %w", err)
		}

		obj.SetLabels(labelsMap)

		return nil
	}
}

func extractKeyValues(kv []string) (map[string]string, error) {
	lenKV := len(kv)
	if lenKV%2 != 0 {
		return nil, fmt.Errorf("passed elements should be in key/value pairs, but got %d elements", lenKV)
	}

	kvMap := make(map[string]string, lenKV%2)
	for i := 0; i < lenKV; i += 2 {
		kvMap[kv[i]] = kv[i+1]
	}

	return kvMap, nil
}
