package deploy

import (
	"errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func RemoveDeploymentsResources(obj *unstructured.Unstructured) error {
	containersPath := []string{"spec", "template", "spec", "containers"}
	replicasPath := []string{"spec", "replicas"}

	//
	// Resources
	//

	sc, ok, err := unstructured.NestedFieldNoCopy(obj.Object, containersPath...)
	if err != nil && ok {
		return err
	}

	var sourceContainers []interface{}
	if sc != nil {
		sourceContainers, ok = sc.([]interface{})
		if !ok {
			return errors.New("field is not a slice")
		}
	}

	for i := range sourceContainers {
		m, ok := sourceContainers[i].(map[string]interface{})
		if !ok {
			return errors.New("field is not a map")
		}

		delete(m, "resources")
	}

	//
	// Replicas
	//

	unstructured.RemoveNestedField(obj.Object, replicasPath...)

	return nil
}
