/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package datasciencecluster contains controller logic of CRD DataScienceCluster
package datasciencecluster

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// ValidateInstanceName validates instanceName as a DNS-1123 label using k8s apimachinery.
// It trims leading/trailing spaces and returns the trimmed value along with any validation error.
// Returns empty string if the input is empty or contains only whitespace.
// Empty strings are explicitly allowed and mean "use default instance name" - this bypasses DNS-1123 validation
// since the default name will be validated separately.
func ValidateInstanceName(instanceName string) (string, error) {
	trimmed := strings.TrimSpace(instanceName)
	if trimmed == "" {
		return "", nil // Empty means "use default" – validated after defaulting.
	}
	if errs := validation.IsDNS1123Label(trimmed); len(errs) > 0 {
		return "", fmt.Errorf("invalid instanceName %q: %s", trimmed, strings.Join(errs, "; "))
	}
	return trimmed, nil
}

func NewDataScienceClusterReconciler(ctx context.Context, mgr ctrl.Manager) error {
	return NewDataScienceClusterReconcilerWithName(ctx, mgr, "")
}

// NewDataScienceClusterReconcilerWithName creates a DataScienceCluster reconciler with a custom instance name.
// If instanceName is empty or contains only whitespace, it defaults to the resource's GVK Kind in lowercase.
// The resulting name (whether provided or defaulted) is subject to DNS-1123 label validation.
// This is useful for testing to avoid controller name conflicts.
// Callers can pass an empty string to use the default validated name.
func NewDataScienceClusterReconcilerWithName(ctx context.Context, mgr ctrl.Manager, instanceName string) error {
	componentsPredicate := dependent.New(dependent.WithWatchStatus(true))

	builder := reconciler.ReconcilerFor(mgr, &dscv1.DataScienceCluster{}).
		Owns(&componentApi.Dashboard{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Workbenches{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Ray{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.ModelRegistry{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.TrustyAI{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Kueue{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.CodeFlare{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.TrainingOperator{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.DataSciencePipelines{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Kserve{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.ModelMeshServing{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.ModelController{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.FeastOperator{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.LlamaStackOperator{}, reconciler.WithPredicates(componentsPredicate)).
		Watches(
			&dsciv1.DSCInitialization{},
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return watchDataScienceClusters(ctx, mgr.GetClient())
			})).
		WithAction(initialize).
		WithAction(checkPreConditions).
		WithAction(updateStatus).
		WithAction(provisionComponents).
		WithAction(deploy.NewAction(
			deploy.WithCache()),
		).
		WithAction(gc.NewAction(
			gc.WithTypePredicate(
				func(rr *types.ReconciliationRequest, objGVK schema.GroupVersionKind) (bool, error) {
					return rr.Controller.Owns(objGVK), nil
				},
			),
		)).
		WithConditions(status.ConditionTypeComponentsReady)

	// Validate the instanceName first (before normalization)
	validatedInstanceName, err := ValidateInstanceName(instanceName)
	if err != nil {
		return err
	}

	// Use validated value if not empty, otherwise default to lowercase GVK Kind
	if validatedInstanceName != "" {
		// Normalize the validated name: convert to lowercase
		instanceName = strings.ToLower(validatedInstanceName)
	} else {
		// Default instanceName to lowercase GVK Kind when empty or whitespace-only
		if gvk, err := apiutil.GVKForObject(&dscv1.DataScienceCluster{}, mgr.GetScheme()); err != nil {
			return fmt.Errorf("failed to get GVK for DataScienceCluster: %w", err)
		} else {
			instanceName = strings.ToLower(gvk.Kind)
			ctrl.Log.Info("defaulted instanceName", "instanceName", instanceName)
		}

		// Validate the defaulted instance name conforms to DNS-1123 label rules
		if errs := validation.IsDNS1123Label(instanceName); len(errs) > 0 {
			return fmt.Errorf("defaulted instanceName %q is invalid: %s", instanceName, strings.Join(errs, "; "))
		}
	}
	// Always set instance name - use provided name or default to lowercase GVK Kind
	builder = builder.WithInstanceName(instanceName)

	if _, err := builder.Build(ctx); err != nil {
		return fmt.Errorf("failed to build DataScienceCluster reconciler: %w", err)
	}

	return nil
}
