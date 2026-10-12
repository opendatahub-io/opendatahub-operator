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

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// gatewayCRDWatchPredicate matches CRD events that must re-trigger a GatewayConfig reconcile:
//
//   - the Dashboard CRD, which gates the dashboard redirect resources;
//   - the cert-manager Certificate CRD, which gates certificate handling and retries a
//     previously blocked XKS reconcile when cert-manager is installed after the operator.
func gatewayCRDWatchPredicate() predicate.Predicate {
	return predicate.Or(
		resources.CreatedOrUpdatedOrDeletedNamed(gvk.DashboardComponentCRDName),
		resources.CreatedOrUpdatedOrDeletedNamed(gvk.CertManagerCertificateCRDName),
	)
}

// gatewayDeploymentWatchPredicate also observes proxy availability, which changes in the
// Deployment status without changing its generation, labels, or annotations.
func gatewayDeploymentWatchPredicate() predicate.Predicate {
	return predicate.Or(
		predicates.DefaultPredicate,
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldDeployment, oldOK := e.ObjectOld.(*unstructured.Unstructured)
				newDeployment, newOK := e.ObjectNew.(*unstructured.Unstructured)
				if !oldOK || !newOK ||
					newDeployment.GetName() != KubeAuthProxyName ||
					newDeployment.GetNamespace() != GetGatewayNamespace() {
					return false
				}

				for _, field := range []string{"availableReplicas", "observedGeneration"} {
					oldValue, oldFound, oldErr := unstructured.NestedInt64(oldDeployment.Object, "status", field)
					newValue, newFound, newErr := unstructured.NestedInt64(newDeployment.Object, "status", field)
					if oldErr != nil || newErr != nil || oldFound != newFound || oldValue != newValue {
						return true
					}
				}
				return false
			},
		},
	)
}

func gatewayCertManagerPrecondition() precondition.PreCondition {
	return precondition.MonitorCRD(
		gvk.CertManagerCertificateCRDName,
		precondition.WithClusterTypes(cluster.ClusterTypeKubernetes),
		precondition.WithSkipFunc(func(_ context.Context, rr *odhtypes.ReconciliationRequest) (bool, error) {
			gatewayConfig, err := validateGatewayConfig(rr)
			if err != nil {
				return false, err
			}
			return gatewayConfig.Spec.Certificate != nil &&
				gatewayConfig.Spec.Certificate.Type == infrav1.Provided &&
				gatewayConfig.Spec.OIDC == nil, nil
		}),
		precondition.WithStopReconciliation(),
		precondition.WithMessage("cert-manager Certificate CRD is required for XKS certificate issuance"),
	)
}

func (h *ServiceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	gw := reconciler.ReconcilerFor(mgr, &serviceApi.GatewayConfig{})
	// special for ROSA: auth is defined in day0 and OAuth not registered in apiserver
	if ok, err := cluster.IsIntegratedOAuth(ctx, mgr.GetAPIReader()); err == nil && ok {
		gw.OwnsGVK(gvk.OAuthClient)
	}

	gw.OwnsGVK(gvk.GatewayClass).
		OwnsGVK(gvk.KubernetesGateway, reconciler.WithPredicates(resources.GatewayStatusChanged())).
		OwnsGVK(gvk.Secret).
		OwnsGVK(gvk.ConfigMap).
		OwnsGVK(gvk.Service).
		OwnsGVK(gvk.Deployment, reconciler.WithPredicates(gatewayDeploymentWatchPredicate())).
		OwnsGVK(gvk.HorizontalPodAutoscaler).
		OwnsGVK(gvk.HTTPRoute).
		OwnsGVK(gvk.Route,
			reconciler.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
			reconciler.Dynamic(reconciler.ClusterIsOpenShift())).
		OwnsGVK(gvk.ClusterRoleBinding).
		OwnsGVK(gvk.EnvoyFilter, reconciler.Dynamic(reconciler.CrdExists(gvk.EnvoyFilter))).
		OwnsGVK(gvk.DestinationRule, reconciler.Dynamic(reconciler.CrdExists(gvk.DestinationRule))).
		OwnsGVK(gvk.CertManagerCertificate, reconciler.Dynamic(reconciler.CrdExists(gvk.CertManagerCertificate))).
		// Only the default provider Service is read to detect an unset ingress mode.
		// Additional provider Services do not affect reconciliation or readiness.
		Watches(
			&corev1.Service{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(resources.GatewayProviderService(GetDefaultGatewayName(), GetGatewayNamespace())),
		).
		Watches(
			&operatorv1.IngressController{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(predicate.And(
				predicate.ResourceVersionChangedPredicate{},
				predicate.NewPredicateFuncs(func(obj client.Object) bool {
					return obj.GetNamespace() == cluster.IngressControllerName.Namespace
				}),
			)),
			reconciler.Dynamic(reconciler.ClusterIsOpenShift()),
		).
		Watches(
			&corev1.Namespace{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(resources.CreatedOrUpdatedOrDeletedNamed(GetGatewayNamespace())),
			reconciler.Dynamic(reconciler.ClusterIsOpenShift()),
		).
		Watches(
			&configv1.Ingress{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(resources.CreatedOrUpdatedOrDeletedNamed("cluster")),
			reconciler.Dynamic(reconciler.ClusterIsOpenShift()),
		).
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(gatewayCRDWatchPredicate()),
		).
		// Watch Gateway certificates, referenced secrets, and XKS cert-manager TLS Secrets.
		Watches(
			&corev1.Secret{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(predicate.Or(
				resources.GatewayCertificateSecret(func(obj client.Object) bool {
					return isGatewayCertificateOrReferencedSecret(ctx, mgr.GetClient(), obj, GetGatewayNamespace())
				}),
				resources.GatewayCertificateSecret(func(obj client.Object) bool {
					return IsXKSCertManagerSecret(ctx, mgr.GetClient(), obj, GetGatewayNamespace())
				}),
			)),
		).
		Watches(
			&gwapiv1.HTTPRoute{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(resources.HTTPRouteReferencesGateway(GetDefaultGatewayName(), GetGatewayNamespace())),
		).
		// Reconcile when Dashboard CR is created or deleted so dashboard redirect
		// resources are deployed or cleaned up accordingly.
		WatchesGVK(
			gvk.Dashboard,
			reconciler.Dynamic(reconciler.CrdExists(gvk.Dashboard)),
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(resources.CreatedOrUpdatedOrDeletedNamed(componentApi.DashboardInstanceName)),
		).
		// Reconcile when cluster APIServer TLS profile changes (kube-auth-proxy TLS args).
		WatchesGVK(
			gvk.OpenshiftAPIServer,
			reconciler.Dynamic(reconciler.ClusterIsOpenShift()),
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(resources.APIServerTLSSecurityProfileChanged()),
		).
		WithReconcilerOpts(reconciler.WithPreConditions([]precondition.PreCondition{
			gatewayCertManagerPrecondition(),
		})).
		WithAction(syncAdditionalIngressStatus).
		WithAction(createGatewayInfrastructure).
		WithAction(createKubeAuthProxyInfrastructure). //  include destinationrule
		WithAction(createEnvoyFilter).
		WithAction(createNetworkPolicy).
		WithAction(createOCPRoutes).
		WithAction(createDashboardRedirectsAction).
		WithAction(template.NewAction(
			template.WithDataFn(getTemplateData),
		)).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(syncAdditionalIngressReadiness).
		WithAction(syncGatewayConfigStatus).
		WithAction(gc.NewAction()).
		WithPostStatusFn(syncAdditionalIngressReadyStatuses).
		WithConditions(reconciler.DependentConditions(ReadyConditionType, serviceApi.AdditionalGatewaysReadyConditionType)...)

	if _, err := gw.Build(ctx); err != nil {
		return fmt.Errorf("could not create the GatewayConfig controller: %w", err)
	}
	return nil
}
