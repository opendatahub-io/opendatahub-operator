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

	operatorv1 "github.com/openshift/api/operator/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//nolint:gochecknoinits
func init() {
	sr.Add(&serviceHandler{})
}

type serviceHandler struct {
}

func (h *serviceHandler) Init(_ common.Platform) error {
	return nil
}

func (h *serviceHandler) GetName() string {
	return ServiceName
}

func (h *serviceHandler) GetManagementState(platform common.Platform, dsci *dsciv1.DSCInitialization) operatorv1.ManagementState {
	// If DSCI exists, use its monitoring configuration
	if dsci != nil {
		return dsci.Spec.Monitoring.ManagementState
	}

	// Fallback to platform-based logic if DSCI is not available
	if platform == cluster.ManagedRhoai {
		return operatorv1.Managed
	}

	return operatorv1.Unmanaged
}

func (h *serviceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
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
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(serviceApi.MonitoringInstanceName)),
		).
		WithAction(initialize).
		WithAction(updatePrometheusConfigMap).
		WithAction(createMonitoringStack).
		WithAction(template.NewAction(
			template.WithCache(true),
			template.WithDataFn(getTemplateData),
		)).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the monitoring controller: %w", err)
	}

	return nil
}
