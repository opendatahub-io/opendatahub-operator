package gc

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func DefaultObjectPredicate(rr *odhTypes.ReconciliationRequest, obj unstructured.Unstructured) (bool, error) {
	if obj.GetAnnotations() == nil {
		return false, nil
	}

	pv := resources.GetAnnotation(&obj, odhAnnotations.PlatformVersion)
	pt := resources.GetAnnotation(&obj, odhAnnotations.PlatformType)
	ig := resources.GetAnnotation(&obj, odhAnnotations.InstanceGeneration)
	iu := resources.GetAnnotation(&obj, odhAnnotations.InstanceUID)

	if pv == "" || pt == "" || ig == "" || iu == "" {
		return false, nil
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

func DefaultTypePredicate(_ *odhTypes.ReconciliationRequest, _ schema.GroupVersionKind) (bool, error) {
	return true, nil
}
