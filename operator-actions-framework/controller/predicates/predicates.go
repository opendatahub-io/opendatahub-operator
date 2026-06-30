package predicates

import (
	"github.com/opendatahub-io/operator-actions-framework/controller/predicates/generation"
	"github.com/opendatahub-io/operator-actions-framework/controller/predicates/resources"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var (
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
