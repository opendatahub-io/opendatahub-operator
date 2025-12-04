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

package gateway

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

func (h *ServiceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	gw := reconciler.ReconcilerFor(mgr, &serviceApi.GatewayConfig{})
	// special for ROSA: auth is defined in day0 and OAuth not registered in apiserver
	if ok, err := cluster.IsIntegratedOAuth(ctx, mgr.GetAPIReader()); err == nil && ok {
		gw.OwnsGVK(gvk.OAuthClient)
	}

	gw.OwnsGVK(gvk.GatewayClass).
		OwnsGVK(gvk.KubernetesGateway).
		OwnsGVK(gvk.Secret).
		OwnsGVK(gvk.Service).
		OwnsGVK(gvk.Deployment).
		OwnsGVK(gvk.HTTPRoute).
		OwnsGVK(gvk.EnvoyFilter, reconciler.Dynamic(reconciler.CrdExists(gvk.EnvoyFilter))).
		OwnsGVK(gvk.DestinationRule, reconciler.Dynamic(reconciler.CrdExists(gvk.DestinationRule))).
		// Watch for certificate secrets (both OpenShift default ingress and provided).
		Watches(
			&corev1.Secret{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(
				resources.GatewayCertificateSecret(func(obj client.Object) bool {
					return cluster.IsGatewayCertificateSecret(ctx, mgr.GetClient(), obj, GatewayNamespace)
				}),
			),
		).
		WithAction(createGatewayInfrastructure).
		WithAction(createKubeAuthProxyInfrastructure). //  include destinationrule
		WithAction(createEnvoyFilter).
		WithAction(createNetworkPolicy).
		WithAction(template.NewAction(
			template.WithDataFn(getTemplateData),
		)).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(syncGatewayConfigStatus).
		WithAction(gc.NewAction())

	if _, err := gw.Build(ctx); err != nil {
		return fmt.Errorf("could not create the GatewayConfig controller: %w", err)
	}
	return nil
}
