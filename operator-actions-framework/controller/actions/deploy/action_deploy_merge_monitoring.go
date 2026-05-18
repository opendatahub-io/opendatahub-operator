package deploy

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func MergeObservabilityResources(source *unstructured.Unstructured, target *unstructured.Unstructured) error {
	resourcesPath := []string{"spec", "resources"}

	// Merge spec.resources from source (existing) to target (new template)
	sourceResources, ok, err := unstructured.NestedFieldNoCopy(source.Object, resourcesPath...)
	if err != nil {
		return err
	}
	if ok && sourceResources != nil {
		if err := unstructured.SetNestedField(target.Object, sourceResources, resourcesPath...); err != nil {
			return err
		}
	}

	return nil
}
