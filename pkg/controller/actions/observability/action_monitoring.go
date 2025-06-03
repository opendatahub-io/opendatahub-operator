package observability

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type Action struct {
	labels map[string]string
}

type ActionOpts func(*Action)

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		labels: map[string]string{
			labels.Scrape: labels.True,
		},
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	err := rr.ForEachResource(func(obj *unstructured.Unstructured) (bool, error) {
		if obj.GetKind() != "Deployment" {
			return false, nil
		}

		resources.SetLabels(obj, a.labels)

		tmplLabels, found, err := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "labels")
		if err != nil {
			return false, fmt.Errorf("failed to get pod template labels: %w", err)
		}
		if !found {
			tmplLabels = make(map[string]string)
		}

		for k, v := range a.labels {
			if _, exists := tmplLabels[k]; !exists {
				tmplLabels[k] = v
			}
		}

		if err := unstructured.SetNestedStringMap(obj.Object, tmplLabels, "spec", "template", "metadata", "labels"); err != nil {
			return false, fmt.Errorf("failed to set pod template labels: %w", err)
		}

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("failed to process resources: %w", err)
	}

	return nil
}
