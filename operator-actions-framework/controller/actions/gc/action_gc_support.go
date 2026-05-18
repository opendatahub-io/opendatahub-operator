package gc

import (
	"fmt"
	"strconv"

	odhTypes "github.com/opendatahub-io/operator-actions-framework/controller/types"
	"github.com/opendatahub-io/operator-actions-framework/resources"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func DefaultObjectPredicate(annotationPrefix string) ObjectPredicateFn {
	versionKey := annotationPrefix + "/version"
	typeKey := annotationPrefix + "/type"
	generationKey := annotationPrefix + "/instance.generation"
	uidKey := annotationPrefix + "/instance.uid"

	return func(rr *odhTypes.ReconciliationRequest, obj unstructured.Unstructured) (bool, error) {
		if obj.GetAnnotations() == nil {
			return false, nil
		}

		pv := resources.GetAnnotation(&obj, versionKey)
		pt := resources.GetAnnotation(&obj, typeKey)
		ig := resources.GetAnnotation(&obj, generationKey)
		iu := resources.GetAnnotation(&obj, uidKey)

		if pv == "" || pt == "" || ig == "" || iu == "" {
			return true, nil
		}

		if pv != rr.Release.Version.String() {
			return true, nil
		}

		if pt != string(rr.Release.Name) {
			return true, nil
		}

		if iu != string(rr.Instance.GetUID()) {
			return true, nil
		}

		g, err := strconv.Atoi(ig)
		if err != nil {
			return false, fmt.Errorf("cannot determine generation: %w", err)
		}

		return rr.Instance.GetGeneration() != int64(g), nil
	}
}

func DefaultTypePredicate(_ *odhTypes.ReconciliationRequest, _ schema.GroupVersionKind) (bool, error) {
	return true, nil
}
