/*
Copyright 2023.

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

package monitoring

import (
	"context"
	"errors"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// NewServiceReconciler creates a ServiceReconciler for the Monitoring API.
func NewServiceReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &serviceApi.Monitoring{}).
		// operands - watched
		//
		// By default the Watches functions adds:
		// - an event handler mapping to a cluster scope resource identified by the
		//   components.platform.opendatahub.io/part-of annotation
		// - a predicate that check for generation change for Delete/Updates events
		//   for to objects that have the label components.platform.opendatahub.io/part-of
		// or services.platform.opendatahub.io/part-of set to the current owner
		//
		Watches(&dscv1.DataScienceCluster{}, reconciler.WithEventHandler(handlers.ToNamed(serviceApi.MonitoringInstanceName)),
			reconciler.WithPredicates(resources.DSCComponentUpdatePredicate)).
		// actions
		WithAction(deployments.NewAction(
			deployments.InNamespaceFn(func(_ context.Context, rr *types.ReconciliationRequest) (string, error) {
				m, ok := rr.Instance.(*serviceApi.Monitoring)
				if !ok {
					return "", errors.New("instance is not of type *services.Monitoring")
				}

				return m.Spec.Namespace, nil
			}),
		)).
		WithAction(initialize).
		WithAction(updatePrometheusConfigMap).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the monitoring controller: %w", err)
	}

	return nil
}
