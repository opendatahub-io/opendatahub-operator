package gc

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func DefaultPredicate(rr *odhTypes.ReconciliationRequest, obj unstructured.Unstructured) (bool, error) {
	if obj.GetAnnotations() == nil {
		return false, nil
	}

	pv := resources.GetAnnotation(&obj, odhAnnotations.PlatformVersion)
	pt := resources.GetAnnotation(&obj, odhAnnotations.PlatformType)
	cg := resources.GetAnnotation(&obj, odhAnnotations.ComponentGeneration)

	if pv == "" || pt == "" || cg == "" {
		return false, nil
	}

	if pv != rr.Release.Version.String() {
		return true, nil
	}

	if pt != string(rr.Release.Name) {
		return true, nil
	}

	g, err := strconv.Atoi(cg)
	if err != nil {
		return false, fmt.Errorf("cannot determine generation: %w", err)
	}

	return rr.Instance.GetGeneration() != int64(g), nil
}
