package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/generation"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
)

var (
	// DefaultPredicate is the default set of predicates associated to
	// resources when there is no specific predicate configured via the
	// builder.
	//
	// It would trigger a reconciliation if either the generation or
	// metadata (labels, annotations) have changed.
	DefaultPredicate = predicate.Or(
		generation.New(),
		predicate.LabelChangedPredicate{},
		predicate.AnnotationChangedPredicate{},
	)

	DefaultDeploymentPredicate = predicate.Or(
		resources.NewDeploymentPredicate(),
		predicate.LabelChangedPredicate{},
		predicate.AnnotationChangedPredicate{},
	)
)
