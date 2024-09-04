package cluster

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

func WithOwnerReference(ownerReferences ...metav1.OwnerReference) MetaOptions {
	return func(obj metav1.Object) error {
		existingOwnerRef := obj.GetOwnerReferences()
		existingOwnerRef = append(existingOwnerRef, ownerReferences...)
		obj.SetOwnerReferences(existingOwnerRef)

		return nil
	}
}

func AsOwnerRef(owner metav1.Object) (MetaOptions, error) {
	ownerRef, err := ToOwnerReference(owner)
	if err != nil {
		return nil, fmt.Errorf("failed to create owner reference: %w", err)
	}

	return WithOwnerReference(ownerRef), nil
}

func ToOwnerReference(obj metav1.Object) (metav1.OwnerReference, error) {
	runtimeOwner, ok := obj.(runtime.Object)
	if !ok {
		return metav1.OwnerReference{}, fmt.Errorf("%T is not a runtime.Object", obj)
	}

	gvk := runtimeOwner.GetObjectKind().GroupVersionKind()

	ownerRef := metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       obj.GetName(),
		UID:        obj.GetUID(),
	}
	return ownerRef, nil
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

func InNamespace(ns string) MetaOptions {
	return func(obj metav1.Object) error {
		obj.SetNamespace(ns)
		return nil
	}
}

func WithAnnotations(annotationKeyValue ...string) MetaOptions {
	return func(obj metav1.Object) error {
		annotationsMap, err := extractKeyValues(annotationKeyValue)
		if err != nil {
			return fmt.Errorf("failed to set labels: %w", err)
		}

		obj.SetAnnotations(annotationsMap)

		return nil
	}
}

func extractKeyValues(keyValues []string) (map[string]string, error) {
	lenKV := len(keyValues)
	if lenKV%2 != 0 {
		return nil, fmt.Errorf("passed elements should be in key/value pairs, but got %d elements", lenKV)
	}

	kvMap := make(map[string]string)
	for i := 0; i < lenKV; i += 2 {
		kvMap[keyValues[i]] = keyValues[i+1]
	}

	return kvMap, nil
}
