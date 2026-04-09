package imagestreams

import (
	"context"
	"fmt"
	"maps"
	"strings"

	imagev1 "github.com/openshift/api/image/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	// maxConditionMessageLen caps the per-tag error message length to avoid
	// leaking verbose registry errors (CWE-209).
	maxConditionMessageLen = 100

	// maxFailedTags caps the number of failed tags reported in the condition
	// message to keep it readable in oc get / dashboard views.
	maxFailedTags = 10
)

type Action struct {
	labels      map[string]string
	namespaceFn actions.Getter[string]
}

type ActionOpts func(*Action)

func WithSelectorLabel(k string, v string) ActionOpts {
	return func(action *Action) {
		action.labels[k] = v
	}
}

func WithSelectorLabels(values map[string]string) ActionOpts {
	return func(action *Action) {
		maps.Copy(action.labels, values)
	}
}

func InNamespace(ns string) ActionOpts {
	return func(action *Action) {
		action.namespaceFn = func(_ context.Context, _ *types.ReconciliationRequest) (string, error) {
			return ns, nil
		}
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	obj, ok := rr.Instance.(types.ResourceObject)
	if !ok {
		return fmt.Errorf("resource instance %v is not a ResourceObject", rr.Instance)
	}

	l := make(map[string]string, len(a.labels))
	maps.Copy(l, a.labels)

	if l[labels.PlatformPartOf] == "" {
		kind, err := resources.KindForObject(rr.Client.Scheme(), rr.Instance)
		if err != nil {
			return err
		}

		l[labels.PlatformPartOf] = strings.ToLower(kind)
	}

	imageStreams := &imagev1.ImageStreamList{}

	ns, err := a.namespaceFn(ctx, rr)
	if err != nil {
		return fmt.Errorf("unable to compute namespace: %w", err)
	}

	err = rr.Client.List(
		ctx,
		imageStreams,
		client.InNamespace(ns),
		client.MatchingLabels(l),
	)

	if meta.IsNoMatchError(err) {
		// ImageStreams CRD not available (vanilla K8s) — default to healthy.
		return nil
	}

	if err != nil {
		return fmt.Errorf("error fetching list of ImageStreams: %w", err)
	}

	s := obj.GetStatus()

	// Default to healthy
	rr.Conditions.MarkTrue(status.ConditionImageStreamsAvailable, conditions.WithObservedGeneration(s.ObservedGeneration))

	if len(imageStreams.Items) == 0 {
		return nil
	}

	// Check each ImageStream tag for import failures.
	// A tag is considered failed when:
	//   - .status.tags[].items is empty (no resolved image), AND
	//   - .status.tags[].conditions contains ImportSuccess with status False
	// This dual check avoids false positives on fresh deploys where imports
	// haven't been attempted yet.
	var failedTags []string

	for i := range imageStreams.Items {
		is := &imageStreams.Items[i]
		for _, tagStatus := range is.Status.Tags {
			if len(tagStatus.Items) > 0 {
				continue
			}
			for _, cond := range tagStatus.Conditions {
				if cond.Type == imagev1.ImportSuccess && cond.Status == corev1.ConditionFalse {
					msg := cond.Message
					if len(msg) > maxConditionMessageLen {
						msg = msg[:maxConditionMessageLen] + "..."
					}
					failedTags = append(failedTags, fmt.Sprintf("%s:%s (%s)", is.Name, tagStatus.Tag, msg))
				}
			}
		}
	}

	if len(failedTags) > 0 {
		reported := failedTags
		suffix := ""
		if len(reported) > maxFailedTags {
			suffix = fmt.Sprintf("; ... and %d more", len(reported)-maxFailedTags)
			reported = reported[:maxFailedTags]
		}

		rr.Conditions.MarkFalse(
			status.ConditionImageStreamsAvailable,
			conditions.WithObservedGeneration(s.ObservedGeneration),
			conditions.WithReason(status.ConditionImageStreamsNotAvailableReason),
			conditions.WithMessage("Warning: %d ImageStream tag(s) failed to import: %s%s", len(failedTags), strings.Join(reported, "; "), suffix),
		)
	}

	return nil
}

// NewAction creates a status action that checks whether operator-deployed
// ImageStreams have successfully imported their image tags.
func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		labels: map[string]string{},
		namespaceFn: func(ctx context.Context, rr *types.ReconciliationRequest) (string, error) {
			return cluster.ApplicationNamespace(ctx, rr.Client)
		},
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}
