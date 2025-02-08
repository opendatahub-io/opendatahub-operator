package deploy

import (
	"errors"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func MergeDeployments(source *unstructured.Unstructured, target *unstructured.Unstructured) error {
	containersPath := []string{"spec", "template", "spec", "containers"}
	replicasPath := []string{"spec", "replicas"}

	//
	// Resources
	//

	sc, ok, err := unstructured.NestedFieldNoCopy(source.Object, containersPath...)
	if err != nil && ok {
		return err
	}
	tc, ok, err := unstructured.NestedFieldNoCopy(target.Object, containersPath...)
	if err != nil && ok {
		return err
	}

	resources := make(map[string]interface{})

	var sourceContainers []interface{}
	if sc != nil {
		sourceContainers, ok = sc.([]interface{})
		if !ok {
			return errors.New("field is not a slice")
		}
	}

	var targetContainers []interface{}
	if tc != nil {
		targetContainers, ok = tc.([]interface{})
		if !ok {
			return errors.New("field is not a slice")
		}
	}

	for i := range sourceContainers {
		m, ok := sourceContainers[i].(map[string]interface{})
		if !ok {
			return errors.New("field is not a map")
		}

		name, ok := m["name"]
		if !ok {
			// can't deal with unnamed containers
			continue
		}

		r, ok := m["resources"]
		if !ok {
			r = make(map[string]interface{})
		}

		//nolint:forcetypeassert,errcheck
		resources[name.(string)] = r
	}

	for i := range targetContainers {
		m, ok := targetContainers[i].(map[string]interface{})
		if !ok {
			return errors.New("field is not a map")
		}

		name, ok := m["name"]
		if !ok {
			// can't deal with unnamed containers
			continue
		}

		//nolint:errcheck
		nr, ok := resources[name.(string)]
		if !ok {
			continue
		}

		//nolint:forcetypeassert,errcheck
		if len(nr.(map[string]interface{})) == 0 {
			delete(m, "resources")
		} else {
			m["resources"] = nr
		}
	}

	//
	// Replicas
	//

	sourceReplica, ok, err := unstructured.NestedFieldNoCopy(source.Object, replicasPath...)
	if err != nil {
		return err
	}
	if !ok {
		unstructured.RemoveNestedField(target.Object, replicasPath...)
	} else {
		if err := unstructured.SetNestedField(target.Object, sourceReplica, replicasPath...); err != nil {
			return err
		}
	}

	return nil
}

// for the case: in manifests, Recreate is used as the target and rollingupdate is set, we remove it totally.
func ConvertRollingUpdate(source, target map[string]interface{}) error {
	strategyPath := []string{"spec", "strategy"}

	targetStrategy, exists, err := unstructured.NestedMap(target, strategyPath...)
	if err != nil {
		return err
	}
	if !exists {
		targetStrategy = nil
	}

	if targetStrategy != nil && targetStrategy["type"] == appsv1.RecreateDeploymentStrategyType {
		// New creation or source has no strategy, remove rollingUpdate
		delete(targetStrategy, "rollingUpdate")
	}

	// set target from memory
	err = unstructured.SetNestedMap(target, targetStrategy, strategyPath...)
	if err != nil {
		return err
	}

	return nil
}
