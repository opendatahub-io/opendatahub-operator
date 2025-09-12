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
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
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

func (h *serviceHandler) GetManagementState(platform common.Platform, dsci *dsciv2.DSCInitialization) operatorv1.ManagementState {
	// Managed cluster must have monitoring enabled even if user manually turns it off
	if platform == cluster.ManagedRhoai {
		return operatorv1.Managed
	}

	// If DSCI exists, use its monitoring configuration
	if dsci != nil {
		return dsci.Spec.Monitoring.ManagementState
	}

	return operatorv1.Unmanaged
}

// monitoringNamespace returns the namespace where monitoring resources should be deployed.
func monitoringNamespace(_ context.Context, rr *odhtypes.ReconciliationRequest) (string, error) {
	m, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return "", errors.New("instance is not of type *services.Monitoring")
	}

	return m.Spec.Namespace, nil
}

func (h *serviceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &serviceApi.Monitoring{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&appsv1.Deployment{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		// operands - openshift
		Owns(&routev1.Route{}).
		// operands - owned dynmically depends on external operators are installed for monitoring
		// TODO: add more here later when enable other operator
		OwnsGVK(gvk.MonitoringStack, reconciler.Dynamic(reconciler.CrdExists(gvk.MonitoringStack))).
		OwnsGVK(gvk.TempoMonolithic, reconciler.Dynamic(reconciler.CrdExists(gvk.TempoMonolithic))).
		OwnsGVK(gvk.TempoStack, reconciler.Dynamic(reconciler.CrdExists(gvk.TempoStack))).
		OwnsGVK(gvk.Instrumentation, reconciler.Dynamic(reconciler.CrdExists(gvk.Instrumentation))).
		OwnsGVK(gvk.OpenTelemetryCollector, reconciler.Dynamic(reconciler.CrdExists(gvk.OpenTelemetryCollector))).
		OwnsGVK(gvk.ServiceMonitor, reconciler.Dynamic(reconciler.CrdExists(gvk.ServiceMonitor))).
		OwnsGVK(gvk.PrometheusRule, reconciler.Dynamic(reconciler.CrdExists(gvk.PrometheusRule))).
		OwnsGVK(gvk.ThanosQuerier, reconciler.Dynamic(reconciler.CrdExists(gvk.ThanosQuerier))).
		OwnsGVK(gvk.Perses, reconciler.Dynamic(reconciler.CrdExists(gvk.Perses))).
		OwnsGVK(gvk.PersesDatasource, reconciler.Dynamic(reconciler.CrdExists(gvk.PersesDatasource))).
		OwnsGVK(gvk.PersesDashboard, reconciler.Dynamic(reconciler.CrdExists(gvk.PersesDashboard))).
		// operands - watched
		//
		// By default the Watches functions adds:
		// - an event handler mapping to a cluster scope resource identified by the
		//   components.platform.opendatahub.io/part-of annotation
		// - a predicate that check for generation change for Delete/Updates events
		//   for to objects that have the label components.platform.opendatahub.io/part-of
		// or services.platform.opendatahub.io/part-of set to the current owner
		//
		Watches(
			&dscv2.DataScienceCluster{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.MonitoringInstanceName)),
			reconciler.WithPredicates(resources.DSCComponentUpdatePredicate),
		).
		// actions
		WithAction(deployments.NewAction(
			deployments.InNamespaceFn(monitoringNamespace),
		)).
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(serviceApi.MonitoringInstanceName)),
		).
		// Watch ConfigMaps for CA rotation sync (specifically prometheus-web-tls-ca)
		Watches(
			&corev1.ConfigMap{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.MonitoringInstanceName)),
			reconciler.WithPredicates(resources.CMContentChangedPredicate),
		).
		// These are only for SRE Monitoring
		WithAction(initialize).
		WithAction(updatePrometheusConfigMap).
		// These are only for new monitoring stack dependent Operators
		WithAction(addMonitoringCapability).
		WithAction(deployMonitoringStackWithQuerierAndRestrictions).
		WithAction(deployTracingStack).
		WithAction(deployAlerting).
		WithAction(deployOpenTelemetryCollector).
		WithAction(deployPerses).
		WithAction(deployPersesTempoIntegration).
		WithAction(deployPersesPrometheusIntegration).
		WithAction(deployNodeMetricsEndpoint).
		WithAction(template.NewAction(
			template.WithDataFn(getTemplateData),
		)).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		// Sync CA from ConfigMap to Secret (handles initial creation and rotation updates)
		WithAction(syncPrometheusWebTLSCA).
		WithAction(gc.NewAction()).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the monitoring controller: %w", err)
	}
	return nil
}
